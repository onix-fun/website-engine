package provider

import (
	"context"
	"net/http"
	"testing"
)

func TestConfigureExtendsHostsAndInsecure(t *testing.T) {
	restore := UseTestClient(http.DefaultClient, nil, false, "")
	defer restore()
	Configure([]string{"gitea.example:3000"}, true)
	defer Configure(nil, false)

	if !allowedHost("gitea.example") {
		t.Fatal("configured extra host must be allowed (hostname without port)")
	}
	if !schemeOK("http") {
		t.Fatal("http must be allowed when insecure sources are enabled")
	}

	// Hostname comparison is exact: unrelated hosts and substrings stay rejected.
	if allowedHost("evil-gitea.example") {
		t.Fatal("substring hostnames must not be allowed")
	}
	if allowedHost("internal.host.local") {
		t.Fatal("unrelated hostnames must not be allowed")
	}

	// Gitea derives the API endpoint from the repo URL, so an extra http host
	// becomes fully usable end to end.
	g := NewGitea()
	if _, err := g.ListReleases(context.Background(), "http://gitea.example:3000/owner/repo", 5); err == nil {
		t.Fatal("expected network error, not a host/scheme rejection")
	}
}

func TestProductionDefaultsUnchangedByDefault(t *testing.T) {
	if schemeOK("http") {
		t.Fatal("http must be rejected in production defaults")
	}
	if allowedHost("gitea.example") {
		t.Fatal("extra hosts must be rejected unless configured")
	}
	if !allowedHost("github.com") || !allowedHost("gitlab.com") || !allowedHost("gitea.com") {
		t.Fatal("built-in hosts must stay allowed")
	}
	if allowedHost("notgitea.com") {
		t.Fatal("substring host matching must not come back")
	}
}
