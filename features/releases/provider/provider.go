package provider

import (
	"context"
	"io"
)

type ReleaseAsset struct {
	TagName            string
	Name               string
	BrowserDownloadURL string
	Size               int64
}

type Release struct {
	TagName     string
	Assets      []ReleaseAsset
	PublishedAt string
}

type Provider interface {
	ListReleases(ctx context.Context, repoURL string, limit int) ([]Release, error)
	DownloadAsset(ctx context.Context, url string) (io.ReadCloser, int64, error)
}
