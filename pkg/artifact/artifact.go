// Package artifact validates and extracts immutable site release archives.
package artifact

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type Limits struct {
	MaxFiles        int
	MaxUncompressed int64
	MaxFileSize     int64
	MaxRatio        int
}

func DefaultLimits() Limits {
	return Limits{MaxFiles: 20000, MaxUncompressed: 512 << 20, MaxFileSize: 128 << 20, MaxRatio: 200}
}

type Manifest struct {
	SHA256 string
	Size   int64
	Files  []string
}

// Extract validates the archive and writes its files into dir. The caller must
// pass an empty staging directory; activation is a separate atomic rename.
func Extract(zipPath, dir string, limits Limits) (Manifest, error) {
	if limits.MaxFiles <= 0 {
		limits = DefaultLimits()
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("open release archive: %w", err)
	}
	defer zr.Close()

	if len(zr.File) == 0 {
		return Manifest{}, errors.New("release archive is empty")
	}
	if len(zr.File) > limits.MaxFiles {
		return Manifest{}, fmt.Errorf("release archive has more than %d files", limits.MaxFiles)
	}

	var total int64
	seen := make(map[string]bool, len(zr.File))
	files := make([]string, 0, len(zr.File))
	for _, zf := range zr.File {
		name := path.Clean("/" + strings.ReplaceAll(zf.Name, "\\", "/"))
		name = strings.TrimPrefix(name, "/")
		if name == "" || name == "." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			return Manifest{}, fmt.Errorf("unsafe archive path %q", zf.Name)
		}
		if zf.Mode()&os.ModeSymlink != 0 {
			return Manifest{}, fmt.Errorf("symlink entries are not allowed: %q", zf.Name)
		}
		if zf.FileInfo().IsDir() {
			continue
		}
		if seen[name] {
			return Manifest{}, fmt.Errorf("duplicate archive path %q", name)
		}
		seen[name] = true
		if zf.UncompressedSize64 > uint64(limits.MaxFileSize) {
			return Manifest{}, fmt.Errorf("archive entry %q exceeds the size limit", name)
		}
		total += int64(zf.UncompressedSize64)
		if total > limits.MaxUncompressed {
			return Manifest{}, errors.New("release archive uncompressed size exceeds the limit")
		}
		if zf.CompressedSize64 > 0 && zf.UncompressedSize64/zf.CompressedSize64 > uint64(limits.MaxRatio) && zf.UncompressedSize64 > 1<<20 {
			return Manifest{}, fmt.Errorf("archive entry %q has a suspicious compression ratio", name)
		}
		files = append(files, name)
	}
	if !seen["index.html"] {
		return Manifest{}, errors.New("release archive must contain index.html at the root")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Manifest{}, err
	}
	for _, zf := range zr.File {
		name := path.Clean("/" + strings.ReplaceAll(zf.Name, "\\", "/"))
		name = strings.TrimPrefix(name, "/")
		if name == "" || name == "." || zf.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		if !within(dir, target) {
			return Manifest{}, fmt.Errorf("unsafe archive path %q", zf.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return Manifest{}, err
		}
		if err := extractFile(zf, target, limits.MaxFileSize); err != nil {
			return Manifest{}, err
		}
	}

	sum, size, err := FileSHA256(zipPath)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{SHA256: sum, Size: size, Files: files}, nil
}

func extractFile(zf *zip.File, target string, maxSize int64) error {
	rc, err := zf.Open()
	if err != nil {
		return fmt.Errorf("open archive entry %q: %w", zf.Name, err)
	}
	defer rc.Close()
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(rc, maxSize+1))
	if err != nil {
		return fmt.Errorf("extract archive entry %q: %w", zf.Name, err)
	}
	if n > maxSize {
		return fmt.Errorf("archive entry %q exceeds the size limit", zf.Name)
	}
	return f.Sync()
}

func FileSHA256(name string) (string, int64, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
