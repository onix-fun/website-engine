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

type GitLab struct {
	client *http.Client
}

func NewGitLab() *GitLab {
	return &GitLab{client: &http.Client{Timeout: 30 * time.Second}}
}

type glRelease struct {
	TagName string   `json:"tag_name"`
	Assets  glAssets `json:"assets"`
}

type glAssets struct {
	Links   []glLink `json:"links"`
	Sources []struct {
		Format string `json:"format"`
		URL    string `json:"url"`
	} `json:"sources"`
}

type glLink struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	DirectAssetURL string `json:"direct_asset_url"`
}

func (g *GitLab) ListReleases(ctx context.Context, repoURL string, limit int) ([]Release, error) {
	u, err := url.Parse(repoURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("repo_url must be https")
	}
	if !isAllowedHost(u.Hostname()) {
		return nil, fmt.Errorf("repo host not allowed")
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid repo url")
	}
	project := url.PathEscape(strings.Join(parts, "/"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	endpoint := fmt.Sprintf("%s://%s/api/v4/projects/%s/releases?per_page=%d", u.Scheme, u.Host, project, limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "website-engine")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("gitlab api error: %d", resp.StatusCode)
	}
	var items []glRelease
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(items))
	for _, item := range items {
		assets := make([]ReleaseAsset, 0)
		for _, l := range item.Assets.Links {
			link := l.DirectAssetURL
			if link == "" {
				link = l.URL
			}
			assets = append(assets, ReleaseAsset{TagName: item.TagName, Name: l.Name, BrowserDownloadURL: link})
		}
		out = append(out, Release{TagName: item.TagName, Assets: assets})
	}
	return out, nil
}

func (g *GitLab) DownloadAsset(ctx context.Context, downloadURL string) (io.ReadCloser, int64, error) {
	u, err := url.Parse(downloadURL)
	if err != nil {
		return nil, 0, err
	}
	if u.Scheme != "https" {
		return nil, 0, fmt.Errorf("invalid asset url")
	}
	if !isAllowedHost(u.Hostname()) {
		return nil, 0, fmt.Errorf("asset host not allowed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, 0, err
	}
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
