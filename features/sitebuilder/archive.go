package sitebuilder

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
)

const (
	defaultMaxArtifactBytes  = 100 << 20
	maxExpandedArtifactBytes = 300 << 20
	maxArchiveFiles          = 600
	maxArchiveFileBytes      = 80 << 20
)

// Asset is one verified regular file in a feature artifact.
type Asset struct {
	Path        string
	ContentType string
	Size        int64
	SHA256      string
	Data        []byte
}

// VerifiedArchive is a validated feature build ready for immutable storage.
type VerifiedArchive struct {
	Manifest    Manifest
	ManifestRaw []byte
	Digest      string
	Size        int64
	Assets      []Asset
}

// VerifyArchive validates a feature ZIP before any contents are persisted. It
// rejects traversal, links, duplicate paths, oversized entries and manifests
// that refer to absent ESM/CSS files.
func VerifyArchive(raw []byte, maxBytes int64) (VerifiedArchive, error) {
	if maxBytes <= 0 {
		maxBytes = defaultMaxArtifactBytes
	}
	if len(raw) == 0 || int64(len(raw)) > maxBytes {
		return VerifiedArchive{}, fmt.Errorf("feature archive must be between 1 and %d bytes", maxBytes)
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return VerifiedArchive{}, fmt.Errorf("read feature archive: %w", err)
	}
	if len(reader.File) == 0 || len(reader.File) > maxArchiveFiles {
		return VerifiedArchive{}, fmt.Errorf("feature archive must contain between 1 and %d files", maxArchiveFiles)
	}
	assets := make([]Asset, 0, len(reader.File))
	seen := make(map[string]bool, len(reader.File))
	var total int64
	var manifestRaw []byte
	for _, file := range reader.File {
		name := strings.TrimSuffix(file.Name, "/")
		if file.FileInfo().IsDir() {
			continue
		}
		if !validAssetPath(name) || strings.Contains(name, "//") || seen[name] {
			return VerifiedArchive{}, fmt.Errorf("unsafe or duplicate archive path %q", file.Name)
		}
		seen[name] = true
		if file.Mode()&0o170000 == 0o120000 || !file.Mode().IsRegular() {
			return VerifiedArchive{}, fmt.Errorf("archive entry %q is not a regular file", name)
		}
		if file.UncompressedSize64 > maxArchiveFileBytes {
			return VerifiedArchive{}, fmt.Errorf("archive entry %q exceeds the per-file size limit", name)
		}
		total += int64(file.UncompressedSize64)
		if total > maxExpandedArtifactBytes {
			return VerifiedArchive{}, fmt.Errorf("expanded feature archive exceeds %d bytes", maxExpandedArtifactBytes)
		}
		if file.CompressedSize64 > 0 && file.UncompressedSize64/file.CompressedSize64 > 1000 {
			return VerifiedArchive{}, fmt.Errorf("archive entry %q has an unsafe compression ratio", name)
		}
		body, err := file.Open()
		if err != nil {
			return VerifiedArchive{}, fmt.Errorf("open archive entry %q: %w", name, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(body, maxArchiveFileBytes+1))
		closeErr := body.Close()
		if readErr != nil {
			return VerifiedArchive{}, fmt.Errorf("read archive entry %q: %w", name, readErr)
		}
		if closeErr != nil {
			return VerifiedArchive{}, fmt.Errorf("close archive entry %q: %w", name, closeErr)
		}
		if int64(len(data)) != int64(file.UncompressedSize64) || int64(len(data)) > maxArchiveFileBytes {
			return VerifiedArchive{}, fmt.Errorf("archive entry %q has an invalid expanded size", name)
		}
		if name == "manifest.json" {
			manifestRaw = data
			continue
		}
		digest := sha256.Sum256(data)
		contentType := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		assets = append(assets, Asset{Path: name, ContentType: contentType, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:]), Data: data})
	}
	if len(manifestRaw) == 0 {
		return VerifiedArchive{}, fmt.Errorf("feature archive must contain a root manifest.json")
	}
	manifest, err := ParseManifest(manifestRaw)
	if err != nil {
		return VerifiedArchive{}, err
	}
	assetPaths := make(map[string]bool, len(assets))
	for _, asset := range assets {
		assetPaths[asset.Path] = true
	}
	if !assetPaths[manifest.Entry] {
		return VerifiedArchive{}, fmt.Errorf("manifest entry %q is absent from the archive", manifest.Entry)
	}
	for _, style := range append(append([]string(nil), manifest.Styles...), manifest.SharedStyles...) {
		if !assetPaths[style] {
			return VerifiedArchive{}, fmt.Errorf("manifest stylesheet %q is absent from the archive", style)
		}
	}
	digest := sha256.Sum256(raw)
	return VerifiedArchive{
		Manifest: manifest, ManifestRaw: append([]byte(nil), manifestRaw...),
		Digest: hex.EncodeToString(digest[:]), Size: int64(len(raw)), Assets: assets,
	}, nil
}
