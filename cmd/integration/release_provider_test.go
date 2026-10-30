package integration

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"websiteengine/features/releases/provider"
)

func TestReleaseSourceEndpointsAuthenticateAndImport(t *testing.T) {
	archive := releaseArchive(t)
	cases := []struct {
		provider string
		path     string
		listAuth func(http.Header) string
		wantAuth string
	}{
		{provider: "github", path: "/owner/repo", listAuth: func(h http.Header) string { return h.Get("Authorization") }, wantAuth: "Bearer integration-source-token"},
		{provider: "gitlab", path: "/group/project", listAuth: func(h http.Header) string { return h.Get("PRIVATE-TOKEN") }, wantAuth: "integration-source-token"},
		{provider: "gitea", path: "/owner/repo", listAuth: func(h http.Header) string { return h.Get("Authorization") }, wantAuth: "token integration-source-token"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			var gotListAuth, gotDownloadAuth string
			var providerServer *httptest.Server
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/download/site.zip" {
					gotDownloadAuth = tc.listAuth(r.Header)
					w.Header().Set("Content-Type", "application/zip")
					_, _ = w.Write(archive)
					return
				}
				gotListAuth = tc.listAuth(r.Header)
				w.Header().Set("Content-Type", "application/json")
				assetURL := providerServer.URL + "/download/site.zip"
				switch tc.provider {
				case "github":
					_ = json.NewEncoder(w).Encode([]map[string]any{{
						"tag_name": "v1.2.0",
						"assets":   []map[string]any{{"name": "site.zip", "browser_download_url": assetURL, "size": len(archive)}},
					}})
				case "gitlab":
					_ = json.NewEncoder(w).Encode([]map[string]any{{
						"tag_name": "v1.2.0",
						"assets":   map[string]any{"links": []map[string]any{{"name": "site.zip", "direct_asset_url": assetURL}}},
					}})
				case "gitea":
					_ = json.NewEncoder(w).Encode([]map[string]any{{
						"tag_name": "v1.2.0",
						"assets":   []map[string]any{{"name": "site.zip", "browser_download_url": assetURL, "size": len(archive)}},
					}})
				}
			})
			providerServer = httptest.NewTLSServer(mux)
			t.Cleanup(providerServer.Close)
			restore := provider.UseTestClient(providerServer.Client(), []string{"127.0.0.1"}, false, providerServer.URL)
			t.Cleanup(restore)
			e := newTestEnv(t)

			if tc.provider == "github" {
				status, body, _ := e.request(http.MethodPost, "/api/v1/admin/release-sources", e.adminToken, map[string]any{
					"provider": "github", "repo_url": "https://github.com/owner/repo",
					"asset_pattern": "[invalid", "token": "integration-source-token",
				})
				if status != http.StatusBadRequest {
					t.Fatalf("invalid asset pattern status = %d, body = %v", status, body)
				}
			}

			sourceStatus, source, _ := e.request(http.MethodPost, "/api/v1/admin/release-sources", e.adminToken, map[string]any{
				"provider": tc.provider, "repo_url": providerServer.URL + tc.path,
				"asset_pattern": "^site\\.zip$", "token": "integration-source-token",
			})
			if sourceStatus != http.StatusCreated {
				t.Fatalf("create source: status=%d body=%v", sourceStatus, source)
			}
			if source["has_credential"] != true || strings.Contains(fmt.Sprint(source), "integration-source-token") {
				t.Fatalf("credential metadata leaked or missing: %v", source)
			}
			sourceID, ok := source["id"].(string)
			if !ok || sourceID == "" {
				t.Fatalf("source ID missing: %v", source)
			}
			var ciphertext []byte
			if err := e.pool.QueryRow(t.Context(), "SELECT auth_ciphertext FROM release_sources WHERE id = $1", sourceID).Scan(&ciphertext); err != nil {
				t.Fatalf("read encrypted credential: %v", err)
			}
			if len(ciphertext) == 0 || bytes.Contains(ciphertext, []byte("integration-source-token")) {
				t.Fatal("provider credential was not stored encrypted")
			}

			listStatus, list, _ := e.request(http.MethodGet, "/api/v1/admin/release-sources/"+sourceID+"/releases", e.adminToken, nil)
			if listStatus != http.StatusOK {
				t.Fatalf("list remote releases: status=%d body=%v", listStatus, list)
			}
			if gotListAuth != tc.wantAuth {
				t.Fatalf("provider list credential = %q, want %q", gotListAuth, tc.wantAuth)
			}
			items, ok := list["items"].([]any)
			if !ok || len(items) != 1 || items[0].(map[string]any)["tag_name"] != "v1.2.0" {
				t.Fatalf("unexpected remote releases: %v", list)
			}

			syncStatus, imported, _ := e.request(http.MethodPost, "/api/v1/admin/release-sources/"+sourceID+"/sync", e.adminToken, map[string]any{
				"asset_url": providerServer.URL + "/download/site.zip",
				"version":   "1.2.0",
				"tag":       "v1.2.0",
			})
			if syncStatus != http.StatusCreated {
				t.Fatalf("sync release: status=%d body=%v", syncStatus, imported)
			}
			if gotDownloadAuth != tc.wantAuth {
				t.Fatalf("asset download credential = %q, want %q", gotDownloadAuth, tc.wantAuth)
			}
			if imported["version"] != "1.2.0" || imported["tag"] != "v1.2.0" {
				t.Fatalf("unexpected imported release: %v", imported)
			}
			if _, err := e.pool.Exec(t.Context(), "UPDATE release_sources SET is_enabled = false WHERE id = $1", sourceID); err != nil {
				t.Fatalf("disable source: %v", err)
			}
			disabledListStatus, _, _ := e.request(http.MethodGet, "/api/v1/admin/release-sources/"+sourceID+"/releases", e.adminToken, nil)
			if disabledListStatus != http.StatusBadRequest {
				t.Fatalf("disabled source list status = %d, want 400", disabledListStatus)
			}
			disabledSyncStatus, _, _ := e.request(http.MethodPost, "/api/v1/admin/release-sources/"+sourceID+"/sync", e.adminToken, map[string]any{
				"asset_url": providerServer.URL + "/download/site.zip",
				"version":   "1.2.0",
			})
			if disabledSyncStatus != http.StatusBadRequest {
				t.Fatalf("disabled source sync status = %d, want 400", disabledSyncStatus)
			}

			if tc.provider == "github" {
				publicStatus, publicSource, _ := e.request(http.MethodPost, "/api/v1/admin/release-sources", e.adminToken, map[string]any{
					"provider": "github", "repo_url": providerServer.URL + tc.path,
					"asset_pattern": "^site\\.zip$",
				})
				if publicStatus != http.StatusCreated || publicSource["has_credential"] != false {
					t.Fatalf("create public source: status=%d body=%v", publicStatus, publicSource)
				}
				publicID := publicSource["id"].(string)
				gotListAuth = "unexpected"
				status, _, _ := e.request(http.MethodGet, "/api/v1/admin/release-sources/"+publicID+"/releases", e.adminToken, nil)
				if status != http.StatusOK || gotListAuth != "" {
					t.Fatalf("public source list status=%d auth=%q", status, gotListAuth)
				}
			}
		})
	}
}

func releaseArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	file, err := archive.Create("index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("<!doctype html><title>Release</title>")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
