package provider

import (
	"net/http"
	"net/url"
	"strings"
)

func setProviderToken(req *http.Request, provider, token string) {
	token = strings.TrimSpace(token)
	if token == "" || req.URL == nil || !strings.EqualFold(req.URL.Scheme, "https") {
		return
	}
	switch provider {
	case "github":
		req.Header.Set("Authorization", "Bearer "+token)
	case "gitlab":
		req.Header.Set("PRIVATE-TOKEN", token)
	case "gitea":
		req.Header.Set("Authorization", "token "+token)
	}
}

func tokenForAsset(repoURL, assetURL, token string) string {
	if strings.TrimSpace(token) == "" {
		return ""
	}
	repo, err := url.Parse(repoURL)
	if err != nil {
		return ""
	}
	asset, err := url.Parse(assetURL)
	if err != nil ||
		!strings.EqualFold(repo.Scheme, "https") ||
		!strings.EqualFold(asset.Scheme, "https") ||
		!strings.EqualFold(repo.Hostname(), asset.Hostname()) ||
		normalizedPort(repo) != normalizedPort(asset) {
		return ""
	}
	return token
}

func normalizedPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	if strings.EqualFold(u.Scheme, "http") {
		return "80"
	}
	return ""
}
