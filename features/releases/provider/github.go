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

type GitHub struct {
	client *http.Client
}

func NewGitHub() *GitHub {
	c := &http.Client{Timeout: 30 * time.Second}
	if testHTTPClient() != nil {
		c = testHTTPClient()
	}
	return &GitHub{client: c}
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Assets      []ghAsset `json:"assets"`
	PublishedAt string    `json:"published_at"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func (g *GitHub) ListReleases(ctx context.Context, repoURL string, limit int) ([]Release, error) {
	owner, repo, err := parseRepo(repoURL)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	base := "https://api.github.com"
	if testAPIBase != "" {
		base = testAPIBase
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/releases?per_page=%d", base, owner, repo, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "website-engine")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github api error: %d", resp.StatusCode)
	}
	var items []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(items))
	for _, item := range items {
		assets := make([]ReleaseAsset, 0, len(item.Assets))
		for _, a := range item.Assets {
			assets = append(assets, ReleaseAsset{
				TagName: item.TagName, Name: a.Name, BrowserDownloadURL: a.BrowserDownloadURL, Size: a.Size,
			})
		}
		out = append(out, Release{TagName: item.TagName, Assets: assets, PublishedAt: item.PublishedAt})
	}
	return out, nil
}

func (g *GitHub) DownloadAsset(ctx context.Context, downloadURL string) (io.ReadCloser, int64, error) {
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
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "website-engine")
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

func parseRepo(repoURL string) (string, string, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", "", err
	}
	if u.Scheme != "https" {
		return "", "", fmt.Errorf("repo_url must be https")
	}
	if !allowedHost(u.Hostname()) {
		return "", "", fmt.Errorf("repo host not allowed")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid repo url")
	}
	return parts[0], parts[1], nil
}

func isAllowedHost(host string) bool {
	host = strings.ToLower(host)
	if host == "github.com" || host == "api.github.com" || host == "objects.githubusercontent.com" || host == "release-assets.githubusercontent.com" {
		return true
	}
	if host == "gitlab.com" || host == "gitlab.example.com" {
		return true
	}
	if strings.Contains(host, "gitea") {
		return true
	}
	return false
}
