package provider

import (
	"context"
	"io"
)

type ReleaseAsset struct {
	TagName            string `json:"-"`
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type Release struct {
	TagName     string         `json:"tag_name"`
	Assets      []ReleaseAsset `json:"assets"`
	PublishedAt string         `json:"published_at,omitempty"`
}

type Provider interface {
	ListReleases(ctx context.Context, repoURL string, limit int) ([]Release, error)
	DownloadAsset(ctx context.Context, url string) (io.ReadCloser, int64, error)
}
