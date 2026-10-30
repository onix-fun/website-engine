package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Gitea struct {
	client *http.Client
}

func NewGitea() *Gitea {
	c := &http.Client{Timeout: 30 * time.Second}
	if testHTTPClient() != nil {
		c = testHTTPClient()
	}
	return &Gitea{client: c}
}

type gtRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []gtAsset `json:"assets"`
}

type gtAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func (g *Gitea) ListReleases(ctx context.Context, repoURL string, limit int) ([]Release, error) {
	return g.ListReleasesWithToken(ctx, repoURL, limit, "")
}

func (g *Gitea) ListReleasesWithToken(ctx context.Context, repoURL string, limit int, token string) ([]Release, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return nil, err
	}
	if !schemeOK(u.Scheme) {
		return nil, fmt.Errorf("repo_url must be https")
	}
	if !allowedHost(u.Hostname()) {
		return nil, fmt.Errorf("repo host not allowed")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid repo url")
	}
	owner, repo := parts[0], parts[1]
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	endpoint := fmt.Sprintf("%s://%s/api/v1/repos/%s/%s/releases?limit=%d", u.Scheme, u.Host, owner, repo, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "website-engine")
	setProviderToken(req, "gitea", token)
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gitea api error: %d", resp.StatusCode)
	}
	var items []gtRelease
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(items))
	for _, item := range items {
		assets := make([]ReleaseAsset, 0, len(item.Assets))
		for _, a := range item.Assets {
			assets = append(assets, ReleaseAsset{TagName: item.TagName, Name: a.Name, BrowserDownloadURL: a.BrowserDownloadURL, Size: a.Size})
		}
		out = append(out, Release{TagName: item.TagName, Assets: assets})
	}
	return out, nil
}

func (g *Gitea) DownloadAsset(ctx context.Context, downloadURL string) (io.ReadCloser, int64, error) {
	return g.DownloadAssetWithToken(ctx, "", downloadURL, "")
}

func (g *Gitea) DownloadAssetWithToken(ctx context.Context, repoURL, downloadURL, token string) (io.ReadCloser, int64, error) {
	u, err := url.Parse(downloadURL)
	if err != nil {
		return nil, 0, err
	}
	if !schemeOK(u.Scheme) {
		return nil, 0, fmt.Errorf("invalid asset url")
	}
	if !allowedHost(u.Hostname()) {
		return nil, 0, fmt.Errorf("asset host not allowed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "website-engine")
	setProviderToken(req, "gitea", tokenForAsset(repoURL, downloadURL, token))
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("download failed: %d", resp.StatusCode)
	}
	return resp.Body, resp.ContentLength, nil
}
