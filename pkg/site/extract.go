package site

import (
	"os"
	"path/filepath"

	"websiteengine/pkg/artifact"
)

func ExtractTo(zipPath, dir string) error {
	parent := filepath.Dir(dir)
	tmp, err := os.MkdirTemp(parent, "release-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if _, err := artifact.Extract(zipPath, tmp, artifact.DefaultLimits()); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, ".ready"), []byte("ok"), 0o644); err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.Rename(tmp, dir)
}
