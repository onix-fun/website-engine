package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeGit serves the shapes of GitHub / GitLab / Gitea release APIs against a
// single httptest TLS server. Handlers dispatch on the path prefix.
func fakeGit(t *testing.T, assets map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	write := func(w http.ResponseWriter, v any) {
		t.Helper()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}

	// GitHub: /repos/owner/repo/releases?per_page=N
	mux.HandleFunc("GET /repos/", func(w http.ResponseWriter, r *http.Request) {
		write(w, []map[string]any{{
			"tag_name": "v1.0.0",
			"assets": []map[string]any{{
				"name":                 "site.zip",
				"browser_download_url": "https://" + r.Host + "/download/site.zip",
				"size":                 10,
			}},
		}})
	})
	// GitLab: /api/v4/projects/<slug>/releases
	mux.HandleFunc("GET /api/v4/projects/", func(w http.ResponseWriter, r *http.Request) {
		write(w, []map[string]any{{
			"tag_name": "v2.0.0",
			"assets": map[string]any{
				"links": []map[string]any{{
					"name":             "site.zip",
					"direct_asset_url": "https://" + r.Host + "/download/site.zip",
				}},
			},
		}})
	})
	// Gitea: /api/v1/repos/<owner>/<repo>/releases
	mux.HandleFunc("GET /api/v1/repos/", func(w http.ResponseWriter, r *http.Request) {
		write(w, []map[string]any{{
			"tag_name": "v3.0.0",
			"assets": []map[string]any{{
				"name":                 "site.zip",
				"browser_download_url": "https://" + r.Host + "/download/site.zip",
			}},
		}})
	})
	mux.HandleFunc("GET /download/site.zip", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(assets["site.zip"]))
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func installHooks(srv *httptest.Server) func() {
	// The allow-list compares u.Hostname() values: host without port.
	return UseTestClient(srv.Client(), []string{"127.0.0.1"}, false, srv.URL)
}

func TestGitHubListReleasesFake(t *testing.T) {
	srv := fakeGit(t, map[string]string{"site.zip": "zip"})
	restore := installHooks(srv)
	defer restore()

	releases, err := NewGitHub().ListReleases(context.Background(), srv.URL+"/owner/repo", 10)
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	if len(releases) != 1 || releases[0].TagName != "v1.0.0" {
		t.Fatalf("unexpected releases: %+v", releases)
	}
	if len(releases[0].Assets) != 1 {
		t.Fatalf("expected one asset: %+v", releases[0].Assets)
	}
}

func TestGitLabListReleasesFake(t *testing.T) {
	srv := fakeGit(t, map[string]string{"site.zip": "zip"})
	restore := installHooks(srv)
	defer restore()

	releases, err := NewGitLab().ListReleases(context.Background(), srv.URL+"/group/project", 5)
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	if len(releases) != 1 || releases[0].TagName != "v2.0.0" {
		t.Fatalf("unexpected releases: %+v", releases)
	}
}

func TestGiteaListReleasesFake(t *testing.T) {
	srv := fakeGit(t, map[string]string{"site.zip": "zip"})
	restore := installHooks(srv)
	defer restore()

	releases, err := NewGitea().ListReleases(context.Background(), srv.URL+"/owner/repo", 0)
	if err != nil {
		t.Fatalf("list releases: %v", err)
	}
	if len(releases) != 1 || releases[0].TagName != "v3.0.0" {
		t.Fatalf("unexpected releases: %+v", releases)
	}
}

func TestDownloadAssetFake(t *testing.T) {
	srv := fakeGit(t, map[string]string{"site.zip": "PK-actual-zip-bytes"})
	restore := installHooks(srv)
	defer restore()

	url := srv.URL + "/download/site.zip"
	releases, err := NewGitea().ListReleases(context.Background(), srv.URL+"/o/r", 1)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	body, size, err := NewGitea().DownloadAsset(context.Background(), releases[0].Assets[0].BrowserDownloadURL)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if size != int64(len(data)) || string(data) != "PK-actual-zip-bytes" {
		t.Fatalf("asset mismatch: size %d, body %q", size, data)
	}
	_ = url
}

func TestDownloadAssetFailsHTTP404(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	restore := UseTestClient(srv.Client(), []string{"127.0.0.1"}, false, "")
	defer restore()

	if _, _, err := NewGitea().DownloadAsset(context.Background(), srv.URL+"/missing.zip"); err == nil {
		t.Fatal("expected download failure for missing asset")
	}
}

func TestSSRFRejectionsInProductionDefaults(t *testing.T) {
	// With no test hooks installed, the production guards must still reject.
	g := NewGitea()
	cases := []string{
		"http://github.com/owner/repo",             // http scheme
		"https://127.0.0.1/owner/repo",             // private host
		"https://169.254.169.254/latest/meta-data", // cloud metadata
		"https://internal.host.local/owner/repo",   // not in the allow-list
		"https://github.com/owner",                 // malformed repo path
	}
	for _, repoURL := range cases {
		if _, err := g.ListReleases(context.Background(), repoURL, 5); err == nil {
			t.Errorf("expected rejection for %q", repoURL)
		}
	}
}

func TestProviderHooksRestore(t *testing.T) {
	restore := UseTestClient(http.DefaultClient, []string{"test.example"}, true, "https://fake")
	restore()
	if testClient != nil || len(testAllowedHosts) != 0 || testInsecureOK || testAPIBase != "" {
		t.Fatal("test hooks were not restored")
	}
}

func TestCredentialedProvidersAuthenticateListingAndSameHostAsset(t *testing.T) {
	var srv *httptest.Server
	listingAuth := map[string]string{}
	var downloadAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/private.zip" {
			downloadAuth = r.Header.Get("Authorization") + "|" + r.Header.Get("PRIVATE-TOKEN")
			_, _ = w.Write([]byte("private archive"))
			return
		}
		var authHeader string
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/"):
			authHeader = "Authorization"
			listingAuth["github"] = r.Header.Get(authHeader)
			_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v1", "assets": []map[string]any{{
				"name": "site.zip", "browser_download_url": srv.URL + "/private.zip",
			}}}})
		case strings.HasPrefix(r.URL.Path, "/api/v4/"):
			authHeader = "PRIVATE-TOKEN"
			listingAuth["gitlab"] = r.Header.Get(authHeader)
			_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v1", "assets": map[string]any{"links": []map[string]any{{
				"name": "site.zip", "direct_asset_url": srv.URL + "/private.zip",
			}}}}})
		default:
			authHeader = "Authorization"
			listingAuth["gitea"] = r.Header.Get(authHeader)
			_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v1", "assets": []map[string]any{{
				"name": "site.zip", "browser_download_url": srv.URL + "/private.zip",
			}}}})
		}
	})
	srv = httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	restore := UseTestClient(srv.Client(), []string{"127.0.0.1"}, false, srv.URL)
	defer restore()

	cases := []struct {
		name         string
		provider     CredentialedProvider
		path         string
		wantAuth     string
		wantDownload string
	}{
		{name: "github", provider: NewGitHub(), path: "/owner/repo", wantAuth: "Bearer test-token", wantDownload: "Bearer test-token|"},
		{name: "gitlab", provider: NewGitLab(), path: "/group/project", wantAuth: "test-token", wantDownload: "|test-token"},
		{name: "gitea", provider: NewGitea(), path: "/owner/repo", wantAuth: "token test-token", wantDownload: "token test-token|"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			list, err := tc.provider.ListReleasesWithToken(context.Background(), srv.URL+tc.path, 5, "test-token")
			if err != nil {
				t.Fatalf("list releases: %v", err)
			}
			if len(list) != 1 || len(list[0].Assets) != 1 {
				t.Fatalf("unexpected release response: %+v", list)
			}
			if got := listingAuth[tc.name]; got != tc.wantAuth {
				t.Fatalf("listing auth header = %q, want %q", got, tc.wantAuth)
			}
			body, _, err := tc.provider.DownloadAssetWithToken(context.Background(), srv.URL+tc.path, list[0].Assets[0].BrowserDownloadURL, "test-token")
			if err != nil {
				t.Fatalf("download asset: %v", err)
			}
			if err := body.Close(); err != nil {
				t.Fatalf("close asset body: %v", err)
			}
			if downloadAuth != tc.wantDownload {
				t.Fatalf("download auth headers = %q, want %q", downloadAuth, tc.wantDownload)
			}
		})
	}
}

func TestTokenForAssetDoesNotForwardAcrossHosts(t *testing.T) {
	if got := tokenForAsset("https://github.com/owner/repo", "https://release-assets.githubusercontent.com/file.zip", "secret"); got != "" {
		t.Fatalf("cross-host asset received credential %q", got)
	}
	if got := tokenForAsset("https://gitlab.example/group/project", "https://gitlab.example/group/project/file.zip", "secret"); got != "secret" {
		t.Fatalf("same-host asset token = %q", got)
	}
	for _, assetURL := range []string{
		"https://gitlab.example:8443/group/project/file.zip",
		"http://gitlab.example/group/project/file.zip",
	} {
		if got := tokenForAsset("https://gitlab.example/group/project", assetURL, "secret"); got != "" {
			t.Errorf("credential forwarded to unsafe asset URL %q", assetURL)
		}
	}
	if got := tokenForAsset("http://gitlab.example/group/project", "http://gitlab.example/group/project/file.zip", "secret"); got != "" {
		t.Fatalf("credential forwarded over HTTP: %q", got)
	}
	request, err := http.NewRequest(http.MethodGet, "http://gitlab.example/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	setProviderToken(request, "gitlab", "secret")
	if request.Header.Get("PRIVATE-TOKEN") != "" {
		t.Fatal("credential forwarded to an HTTP provider API")
	}
}
