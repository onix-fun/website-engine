package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const ffmpegTimeout = 15 * time.Minute

type ProcessedOutput struct {
	Data     []byte
	MIMEType string
	Width    int
	Height   int
}

type RenditionProcessor interface {
	Process(context.Context, Media, FieldProfile, []byte) (map[string]ProcessedOutput, error)
	Ready(context.Context) error
}

type FFmpegProcessor struct {
	ffmpeg  string
	ffprobe string
}

func NewFFmpegProcessor() (*FFmpegProcessor, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg executable not found: %w", err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe executable not found: %w", err)
	}
	return &FFmpegProcessor{ffmpeg: ffmpeg, ffprobe: ffprobe}, nil
}

func (p *FFmpegProcessor) Ready(_ context.Context) error {
	if _, err := os.Stat(p.ffmpeg); err != nil {
		return fmt.Errorf("ffmpeg is unavailable: %w", err)
	}
	if _, err := os.Stat(p.ffprobe); err != nil {
		return fmt.Errorf("ffprobe is unavailable: %w", err)
	}
	return nil
}

func (p *FFmpegProcessor) Process(ctx context.Context, original Media, profile FieldProfile, data []byte) (map[string]ProcessedOutput, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	if err := validateProfileType(profile, original.ContentType); err != nil {
		return nil, err
	}
	if profile.Accept == "raster-image" && (original.ContentType == "image/gif" || original.ContentType == "image/svg+xml") {
		// Preserve source formats whose animation/vector semantics must not be
		// flattened. Runtime hydration falls back to the original media URL.
		return map[string]ProcessedOutput{}, nil
	}
	if original.ContentType == "image/webp" && isAnimatedWebP(data) {
		// Preserve animation. A profile can still be considered ready; runtime
		// hydration falls back to the original when no static rendition exists.
		return map[string]ProcessedOutput{}, nil
	}
	workDir, err := os.MkdirTemp("", "website-engine-media-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	ext := filepath.Ext(original.ObjectKey)
	if ext == "" {
		ext = ".media"
	}
	inputPath := filepath.Join(workDir, "original"+ext)
	if err := os.WriteFile(inputPath, data, 0600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, ffmpegTimeout)
	defer cancel()

	if profile.Accept == "video" {
		return p.processVideo(ctx, workDir, inputPath, profile)
	}
	outputs := make(map[string]ProcessedOutput, len(profile.Renditions))
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read source image dimensions: %w", err)
	}
	for _, rendition := range profile.Renditions {
		outputPath := filepath.Join(workDir, rendition.Name+".webp")
		filter, err := renditionFilter(rendition, config.Width, config.Height)
		if err != nil {
			return nil, fmt.Errorf("build image rendition %q filter: %w", rendition.Name, err)
		}
		if _, err := runCommand(ctx, p.ffmpeg,
			"-hide_banner", "-loglevel", "error", "-y", "-i", inputPath,
			"-map_metadata", "-1", "-frames:v", "1", "-vf", filter,
			"-c:v", "libwebp", "-quality", "82", "-compression_level", "6", outputPath); err != nil {
			return nil, fmt.Errorf("encode image rendition %q: %w", rendition.Name, err)
		}
		output, err := readImageOutput(outputPath)
		if err != nil {
			return nil, fmt.Errorf("read image rendition %q: %w", rendition.Name, err)
		}
		outputs[rendition.Name] = output
	}
	return outputs, nil
}

func isAnimatedWebP(data []byte) bool {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return false
	}
	for offset := 12; offset+8 <= len(data); {
		chunk := string(data[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start := offset + 8
		end := start + size
		if end > len(data) {
			return false
		}
		if chunk == "ANIM" || chunk == "ANMF" {
			return true
		}
		if chunk == "VP8X" && size > 0 && data[start]&0x02 != 0 {
			return true
		}
		offset = end + (size & 1)
	}
	return false
}

func (p *FFmpegProcessor) processVideo(ctx context.Context, workDir, inputPath string, profile FieldProfile) (map[string]ProcessedOutput, error) {
	v := profile.Video
	videoPath := filepath.Join(workDir, "video.mp4")
	videoFilter := fmt.Sprintf("scale=w='min(iw,%d)':h='min(ih,%d)':force_original_aspect_ratio=decrease:force_divisible_by=2", v.MaxWidth, v.MaxHeight)
	if _, err := runCommand(ctx, p.ffmpeg,
		"-hide_banner", "-loglevel", "error", "-y", "-i", inputPath,
		"-map", "0:v:0", "-map", "0:a?", "-map_metadata", "-1", "-vf", videoFilter,
		"-c:v", "libx264", "-preset", "medium", "-crf", "23", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", videoPath); err != nil {
		return nil, fmt.Errorf("encode H.264 video: %w", err)
	}
	video, err := os.ReadFile(videoPath)
	if err != nil {
		return nil, err
	}
	width, height, err := p.probeDimensions(ctx, videoPath)
	if err != nil {
		return nil, fmt.Errorf("probe encoded video: %w", err)
	}

	posterPath := filepath.Join(workDir, "poster.webp")
	posterWidth, posterHeight, err := p.probeDimensions(ctx, inputPath)
	if err != nil {
		return nil, fmt.Errorf("probe source video for poster: %w", err)
	}
	posterFilter, err := renditionFilter(v.Poster, posterWidth, posterHeight)
	if err != nil {
		return nil, fmt.Errorf("build video poster filter: %w", err)
	}
	if _, err := runCommand(ctx, p.ffmpeg,
		"-hide_banner", "-loglevel", "error", "-y", "-i", inputPath,
		"-map_metadata", "-1", "-vf", posterFilter, "-frames:v", "1",
		"-c:v", "libwebp", "-quality", "82", "-compression_level", "6", posterPath); err != nil {
		return nil, fmt.Errorf("encode video poster: %w", err)
	}
	poster, err := readImageOutput(posterPath)
	if err != nil {
		return nil, fmt.Errorf("read video poster: %w", err)
	}
	return map[string]ProcessedOutput{
		"video":  {Data: video, MIMEType: "video/mp4", Width: width, Height: height},
		"poster": poster,
	}, nil
}

func renditionFilter(r Rendition, sourceWidth, sourceHeight int) (string, error) {
	if r.Fit == "cover" {
		if sourceWidth < 1 || sourceHeight < 1 {
			return "", fmt.Errorf("source dimensions must be positive, got %dx%d", sourceWidth, sourceHeight)
		}
		cropWidth, cropHeight := coverCropDimensions(sourceWidth, sourceHeight, r.Width, r.Height)
		return fmt.Sprintf("crop=w=%d:h=%d:x='(iw-ow)/2':y='(ih-oh)/2',scale=w='min(iw,%d)':h='min(ih,%d)':force_original_aspect_ratio=decrease",
			cropWidth, cropHeight, r.Width, r.Height), nil
	}
	return fmt.Sprintf("scale=w='min(iw,%d)':h='min(ih,%d)':force_original_aspect_ratio=decrease", r.Width, r.Height), nil
}

// coverCropDimensions calculates a positive, center-crop rectangle before it
// reaches FFmpeg. Passing dynamic crop expressions for both dimensions can be
// evaluated as a zero-height crop by FFmpeg 6 on some source/profile pairs.
func coverCropDimensions(sourceWidth, sourceHeight, targetWidth, targetHeight int) (int, int) {
	sourceRatio := float64(sourceWidth) / float64(sourceHeight)
	targetRatio := float64(targetWidth) / float64(targetHeight)
	if sourceRatio > targetRatio {
		return max(1, int(math.Floor(float64(sourceHeight)*targetRatio))), sourceHeight
	}
	return sourceWidth, max(1, int(math.Floor(float64(sourceWidth)/targetRatio)))
}

func (p *FFmpegProcessor) probeDimensions(ctx context.Context, path string) (int, int, error) {
	out, err := runCommand(ctx, p.ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", path)
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Split(strings.TrimSpace(string(out)), "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected ffprobe dimensions %q", strings.TrimSpace(string(out)))
	}
	width, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	height, err := strconv.Atoi(parts[1])
	return width, height, err
}

func runCommand(ctx context.Context, executable string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("%w: %s", err, message)
		}
		return nil, err
	}
	return out, nil
}

func readImageOutput(path string) (ProcessedOutput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProcessedOutput{}, err
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ProcessedOutput{}, err
	}
	return ProcessedOutput{Data: data, MIMEType: "image/webp", Width: config.Width, Height: config.Height}, nil
}

func validateProfileType(profile FieldProfile, contentType string) error {
	switch profile.Accept {
	case "image":
		if !strings.HasPrefix(contentType, "image/") {
			return &ValidationError{Message: "Для поля требуется изображение"}
		}
	case "raster-image":
		switch contentType {
		case "image/jpeg", "image/png", "image/webp", "image/gif", "image/svg+xml":
		default:
			return &ValidationError{Message: "Для изображения поддерживаются JPEG, PNG, WebP, GIF и SVG"}
		}
	case "video":
		if !strings.HasPrefix(contentType, "video/") {
			return &ValidationError{Message: "Для поля требуется видео"}
		}
	case "file":
		return nil
	}
	return nil
}
