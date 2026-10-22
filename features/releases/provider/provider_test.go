package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
