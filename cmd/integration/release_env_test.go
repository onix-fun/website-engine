package integration

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"testing"

	"websiteengine/pkg/environment"
)

// ---- helpers ----

func (e *testEnv) requestOn(base, method, path, token string, body any) (int, map[string]any, http.Header) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	if len(raw) > 0 && resp.Header.Get("Content-Type") != "" {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out, resp.Header
}

func (e *testEnv) public(env environment.Environment, method, path string, body any) (int, map[string]any, http.Header) {
	e.t.Helper()
	base := e.devServer.URL
	if env == environment.Prod {
		base = e.prodServer.URL
	}
	return e.requestOn(base, method, path, "", body)
}

// zipArtifact builds a release archive in memory.
func zipArtifact(t *testing.T, files map[string]string, dirs []string) []byte {
	t.Helper()
	buf := &bytes.Buffer{}
	w := zip.NewWriter(buf)
	for _, dir := range dirs {
		h := &zip.FileHeader{Name: dir, Method: zip.Deflate}
		h.SetMode(os.ModeDir | 0o755)
		if _, err := w.CreateHeader(h); err != nil {
			t.Fatalf("zip dir: %v", err)
		}
	}
	for name, content := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0o644)
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatalf("zip create %q: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// importRelease posts the artifact through the admin multipart endpoint.
func (e *testEnv) importRelease(t *testing.T, version, tag string, artifact []byte) map[string]any {
	t.Helper()
	buf := &bytes.Buffer{}
	form := multipart.NewWriter(buf)
	if err := form.WriteField("version", version); err != nil {
		t.Fatalf("multipart field: %v", err)
	}
	if err := form.WriteField("tag", tag); err != nil {
		t.Fatalf("multipart field: %v", err)
	}
	part, err := form.CreateFormFile("artifact", "site.zip")
	if err != nil {
		t.Fatalf("multipart: %v", err)
	}
	if _, err := part.Write(artifact); err != nil {
		t.Fatalf("artifact write: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	contentType := form.FormDataContentType()
	status, body, _ := e.rawRequest(http.MethodPost, "/api/v1/admin/releases", e.adminToken, buf, contentType)
	if status != http.StatusCreated {
		t.Fatalf("import release %q: status=%d body=%v", version, status, body)
	}
	return body
}

// releaseEvents reads release_events (newest first) straight from the DB.
func (e *testEnv) releaseEvents(t *testing.T) []map[string]string {
	t.Helper()
	rows, err := e.pool.Query(t.Context(), `SELECT environment, action, metadata::text FROM release_events ORDER BY created_at DESC`)
	if err != nil {
		t.Fatalf("release events query: %v", err)
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var envName, action, metadata string
		if err := rows.Scan(&envName, &action, &metadata); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		out = append(out, map[string]string{"environment": envName, "action": action, "metadata": metadata})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("release events: %v", err)
	}
	return out
}

// versionPointerRejected runs UPDATE and reports whether the DB rejected it.
func (e *testEnv) versionPointerRejected(t *testing.T, sql string, args ...any) bool {
	t.Helper()
	_, err := e.pool.Exec(t.Context(), sql, args...)
	return err != nil
}

var baseSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"title": map[string]any{"type": "string", "minLength": 1}},
	"required":             []string{"title"},
	"additionalProperties": false,
}

func contentEntries(t *testing.T, body map[string]any) []any {
	t.Helper()
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("content response without data: %v", body)
	}
	entries, ok := data["entries"].([]any)
	if !ok {
		t.Fatalf("content response without entries: %v", data)
	}
	return entries
}

func TestReleaseLifecycleAndEventSnapshot(t *testing.T) {
	env := newTestEnv(t)
	featureID := env.createFeature("hero", "Обложка", "single")
	env.saveAndPublishSchema(featureID, 1, baseSchema)

	good := zipArtifact(t, map[string]string{
		"index.html":    "<html><body>site</body></html>",
		"assets/app.js": "console.log(1)",
	}, []string{"assets/"})
	rel := env.importRelease(t, "1.0.0", "v1.0.0", good)
	relID := rel["id"].(string)
	if rel["version"] != "1.0.0" || rel["artifact_sha256"] == "" {
		t.Fatalf("unexpected release payload: %v", rel)
	}

	// The import must produce an audit event.
	events := env.releaseEvents(t)
	if len(events) == 0 || events[0]["action"] != "import" {
		t.Fatalf("import event missing: %v", events)
	}

	status, active, _ := env.request(http.MethodPost, "/api/v1/admin/releases/"+relID+"/activate", env.adminToken,
		map[string]any{"environment": "dev"})
	if status != http.StatusOK {
		t.Fatalf("activate dev: %d %v", status, active)
	}
	status, envs, _ := env.request(http.MethodGet, "/api/v1/admin/site/environments", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("environments: %d %v", status, envs)
	}
	found := false
	for _, item := range envs["items"].([]any) {
		state := item.(map[string]any)
		if state["key"] == "dev" {
			if state["active_release_id"] != relID {
				t.Fatalf("dev active release mismatch: %v", state)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("dev environment row missing")
	}

	// The activation event must contain the feature version snapshot.
	events = env.releaseEvents(t)
	if events[0]["action"] != "activate" || events[0]["environment"] != "dev" {
		t.Fatalf("activation event missing: %v", events)
	}
	var metadata struct {
		ReleaseVersion  string `json:"release_version"`
		FeatureVersions struct {
			Features []struct {
				Key        string `json:"key"`
				DevVersion *int64 `json:"dev_version"`
			} `json:"features"`
		} `json:"feature_versions"`
	}
	if err := json.Unmarshal([]byte(events[0]["metadata"]), &metadata); err != nil {
		t.Fatalf("event metadata not an object: %v", err)
	}
	if metadata.ReleaseVersion != "1.0.0" {
		t.Fatalf("event must record the release version: %s", metadata.ReleaseVersion)
	}
	var heroSnapshot bool
	for _, f := range metadata.FeatureVersions.Features {
		if f.Key == "hero" && f.DevVersion != nil && *f.DevVersion >= 1 {
			heroSnapshot = true
		}
	}
	if !heroSnapshot {
		t.Fatalf("hero feature version snapshot missing: %s", events[0]["metadata"])
	}

	// Rollback in the same environment writes a rollback event.
	status, rolled, _ := env.request(http.MethodPost, "/api/v1/admin/releases/"+relID+"/rollback", env.adminToken,
		map[string]any{"environment": "dev"})
	if status != http.StatusOK {
		t.Fatalf("rollback: %d %v", status, rolled)
	}
	events = env.releaseEvents(t)
	if events[0]["action"] != "rollback" {
		t.Fatalf("rollback event missing: %v", events)
	}
}

// releaseForm packs version/tag/artifact into a multipart body.
func releaseForm(t *testing.T, version, tag string, artifact []byte) (*bytes.Buffer, string) {
	t.Helper()
	buf := &bytes.Buffer{}
	form := multipart.NewWriter(buf)
	if err := form.WriteField("version", version); err != nil {
		t.Fatalf("multipart: %v", err)
	}
	if tag != "" {
		if err := form.WriteField("tag", tag); err != nil {
			t.Fatalf("multipart: %v", err)
		}
	}
	part, err := form.CreateFormFile("artifact", "site.zip")
	if err != nil {
		t.Fatalf("multipart: %v", err)
	}
	if _, err := part.Write(artifact); err != nil {
		t.Fatalf("multipart: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("multipart: %v", err)
	}
	return buf, form.FormDataContentType()
}

func (e *testEnv) importRaw(t *testing.T, version, tag string, artifact []byte) (int, map[string]any, http.Header) {
	t.Helper()
	buf, contentType := releaseForm(t, version, tag, artifact)
	return e.rawRequest(http.MethodPost, "/api/v1/admin/releases", e.adminToken, buf, contentType)
}

func TestReleaseArtifactValidation(t *testing.T) {
	env := newTestEnv(t)

	t.Run("missing index.html rejected", func(t *testing.T) {
		bad := zipArtifact(t, map[string]string{"main.html": "<html/>"}, nil)
		status, body, _ := env.importRaw(t, "0.1.0", "v0", bad)
		if status != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d %v", status, body)
		}
	})
	t.Run("traversal entry rejected", func(t *testing.T) {
		evil := zipArtifact(t, map[string]string{"index.html": "ok", "../escape.txt": "x"}, nil)
		status, body, _ := env.importRaw(t, "0.2.0", "v0", evil)
		if status != http.StatusBadRequest {
			t.Fatalf("expected 400 for traversal entry, got %d %v", status, body)
		}
	})
	t.Run("symlink entry rejected", func(t *testing.T) {
		buf := &bytes.Buffer{}
		w := zip.NewWriter(buf)
		link := &zip.FileHeader{Name: "index.html", Method: zip.Deflate}
		link.SetMode(os.ModeSymlink | 0o777)
		fw, _ := w.CreateHeader(link)
		_, _ = fw.Write([]byte("/etc/shadow"))
		_ = w.Close()
		status, body, _ := env.importRaw(t, "0.3.0", "v0", buf.Bytes())
		if status != http.StatusBadRequest {
			t.Fatalf("expected 400 for symlink entry, got %d %v", status, body)
		}
	})
	t.Run("zip bomb rejected", func(t *testing.T) {
		bomb := zipArtifact(t, map[string]string{
			"index.html": "ok",
			"payload":    strings.Repeat("\x00", 8<<20),
		}, nil)
		status, body, _ := env.importRaw(t, "0.4.0", "v0", bomb)
		if status != http.StatusBadRequest {
			t.Fatalf("expected 400 for zip bomb, got %d %v", status, body)
		}
	})
	t.Run("non-zip rejected", func(t *testing.T) {
		status, body, _ := env.importRaw(t, "0.5.0", "v0", []byte("plain text, not a zip"))
		if status != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d %v", status, body)
		}
	})
}

func TestEnvironmentIsolationAndPrivateFields(t *testing.T) {
	env := newTestEnv(t)
	featureID := env.createFeature("pages", "Страницы", "collection")
	featureRev := env.saveAndPublishSchema(featureID, 1, baseSchema)

	// Entry v1: publish to dev, then promote to prod.
	status, entry, _ := env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken,
		map[string]any{"data": map[string]any{"title": "v1"}})
	if status != http.StatusCreated {
		t.Fatalf("create entry v1: %d %v", status, entry)
	}
	entryID := entry["id"].(string)
	rev := entry["revision"].(float64)
	status, body, _ := env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/publish", env.adminToken,
		map[string]any{"revision": rev})
	if status != http.StatusOK {
		t.Fatalf("publish entry v1: %d %v", status, body)
	}
	rev = body["revision"].(float64)
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/promote", env.adminToken,
		map[string]any{"revision": rev})
	if status != http.StatusOK {
		t.Fatalf("promote entry v1: %d %v", status, body)
	}
	rev = body["revision"].(float64)

	// dev-only schema v2 introduces a private field.
	schemaV2 := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":  map[string]any{"type": "string", "minLength": 1},
			"secret": map[string]any{"type": "string", "x-private": true},
		},
		"required": []string{"title"},
	}
	status, body, _ = env.request(http.MethodPut, "/api/v1/admin/features/"+featureID+"/draft", env.adminToken,
		map[string]any{"revision": featureRev, "schema": schemaV2})
	if status != http.StatusOK {
		t.Fatalf("draft schema v2: %d %v", status, body)
	}
	featureRev = body["revision"].(float64)
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/publish", env.adminToken,
		map[string]any{"revision": featureRev})
	if status != http.StatusOK {
		t.Fatalf("publish schema v2: %d %v", status, body)
	}
	// dev entry v2 carries the private field; prod stays on v1.
	status, body, _ = env.request(http.MethodPatch, "/api/v1/admin/entries/"+entryID+"/draft", env.adminToken,
		map[string]any{"revision": rev, "data": map[string]any{"title": "v2-dev", "secret": "hidden"}})
	if status != http.StatusOK {
		t.Fatalf("entry draft v2: %d %v", status, body)
	}
	rev = body["revision"].(float64)
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/publish", env.adminToken,
		map[string]any{"revision": rev})
	if status != http.StatusOK {
		t.Fatalf("publish entry v2: %d %v", status, body)
	}

	// dev public API must serve dev data with the private field filtered.
	status, pub, _ := env.public(environment.Dev, http.MethodGet, "/api/v1/content/pages", nil)
	if status != http.StatusOK {
		t.Fatalf("dev public content: %d %v", status, pub)
	}
	entries := contentEntries(t, pub)
	if len(entries) != 1 {
		t.Fatalf("dev entries: %v", entries)
	}
	devData := entries[0].(map[string]any)["data"].(map[string]any)
	if devData["title"] != "v2-dev" {
		t.Fatalf("dev must see v2-dev: %v", devData)
	}
	if _, exists := devData["secret"]; exists {
		t.Fatalf("private field leaked on dev: %v", devData)
	}

	// prod public API stays on the prod version.
	status, pub, _ = env.public(environment.Prod, http.MethodGet, "/api/v1/content/pages", nil)
	if status != http.StatusOK {
		t.Fatalf("prod public content: %d %v", status, pub)
	}
	entries = contentEntries(t, pub)
	prodData := entries[0].(map[string]any)["data"].(map[string]any)
	if prodData["title"] != "v1" {
		t.Fatalf("prod must still serve v1: %v", prodData)
	}
}

func TestFormsPerEnvironment(t *testing.T) {
	env := newTestEnv(t)
	formID := env.createFeature("feedback", "Форма", "form")
	schemaV1 := map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"message": map[string]any{"type": "string", "minLength": 1}},
		"required":             []string{"message"},
		"additionalProperties": false,
	}
	formRev := env.saveAndPublishSchema(formID, 1, schemaV1)

	// v2 in dev adds a required rating.
	schemaV2 := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{"type": "string", "minLength": 1},
			"rating":  map[string]any{"type": "integer", "minimum": 1},
		},
		"required":             []string{"message", "rating"},
		"additionalProperties": false,
	}
	status, body, _ := env.request(http.MethodPut, "/api/v1/admin/features/"+formID+"/draft", env.adminToken,
		map[string]any{"revision": formRev, "schema": schemaV2})
	if status != http.StatusOK {
		t.Fatalf("draft schema v2: %d %v", status, body)
	}
	formRev = body["revision"].(float64)
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/features/"+formID+"/publish", env.adminToken,
		map[string]any{"revision": formRev})
	if status != http.StatusOK {
		t.Fatalf("publish schema v2: %d %v", status, body)
	}

	// prod runs schema v1: a rating is not required.
	status, _, _ = env.public(environment.Prod, http.MethodPost, "/api/v1/forms/feedback/submissions",
		map[string]any{"message": "hello prod"})
	if status != http.StatusCreated {
		t.Fatalf("prod submission without rating must pass (schema v1), got %d", status)
	}
	// dev runs schema v2: rating required.
	status, _, _ = env.public(environment.Dev, http.MethodPost, "/api/v1/forms/feedback/submissions",
		map[string]any{"message": "missing rating"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("dev submission without rating must fail, got %d", status)
	}
	status, _, _ = env.public(environment.Dev, http.MethodPost, "/api/v1/forms/feedback/submissions",
		map[string]any{"message": "with rating", "rating": 5})
	if status != http.StatusCreated {
		t.Fatalf("dev submission with rating must pass, got %d", status)
	}

	// The admin listing is tagged and filterable by environment.
	status, listed, _ := env.request(http.MethodGet, "/api/v1/admin/forms/feedback/submissions?environment=dev", env.adminToken, nil)
	if status != http.StatusOK || len(listed["items"].([]any)) != 1 {
		t.Fatalf("dev submissions filter: %d %v", status, listed)
	}
	item := listed["items"].([]any)[0].(map[string]any)
	if item["environment"] != "dev" {
		t.Fatalf("submission must be tagged dev: %v", item)
	}
	status, listed, _ = env.request(http.MethodGet, "/api/v1/admin/forms/feedback/submissions?environment=prod", env.adminToken, nil)
	if status != http.StatusOK || len(listed["items"].([]any)) != 1 {
		t.Fatalf("prod submissions filter: %d %v", status, listed)
	}
	if listed["items"].([]any)[0].(map[string]any)["environment"] != "prod" {
		t.Fatal("submission must be tagged prod")
	}
}

func TestCORSPerEnvironment(t *testing.T) {
	env := newTestEnv(t)

	preflight := func(base, origin string) (int, http.Header) {
		req, err := http.NewRequest(http.MethodOptions, base+"/api/v1/content", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "GET")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode, resp.Header
	}
	allowed := func(code int, headers http.Header, origin string) bool {
		return code == http.StatusNoContent && headers.Get("Access-Control-Allow-Origin") == origin
	}

	// Bootstrap list only: site.example on both envs.
	code, headers := preflight(e2eBase(env, environment.Dev), "https://site.example")
	if !allowed(code, headers, "https://site.example") {
		t.Fatalf("bootstrap origin denied on dev: %d", code)
	}

	// PATCH dev origins; prod stays on the bootstrap list.
	status, states, _ := env.request(http.MethodGet, "/api/v1/admin/site/environments", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("environments: %d", status)
	}
	var devRevision float64
	for _, item := range states["items"].([]any) {
		if s := item.(map[string]any); s["key"] == "dev" {
			devRevision = s["revision"].(float64)
		}
	}
	status, updated, _ := env.request(http.MethodPatch, "/api/v1/admin/site/environments/dev", env.adminToken,
		map[string]any{"cors_origins": []string{"https://app.example"}, "revision": devRevision})
	if status != http.StatusOK {
		t.Fatalf("patch dev cors: %d %v", status, updated)
	}
	if origins := updated["cors_origins"].([]any); len(origins) != 1 || origins[0] != "https://app.example" {
		t.Fatalf("unexpected cors origins: %v", updated)
	}

	code, headers = preflight(e2eBase(env, environment.Dev), "https://app.example")
	if !allowed(code, headers, "https://app.example") {
		t.Fatalf("new origin must be allowed on dev immediately: %d", code)
	}
	code, headers = preflight(e2eBase(env, environment.Prod), "https://app.example")
	if code == http.StatusNoContent {
		t.Fatalf("dev origins must not leak into prod: %d", code)
	}
	// The bootstrap origin is no longer allowed on dev because the DB list
	// is non-empty and does not include it.
	code, _ = preflight(e2eBase(env, environment.Dev), "https://site.example")
	if code == http.StatusNoContent {
		t.Fatal("stale bootstrap origin must be denied after the DB edit")
	}

	// Stale revision rejected with 409 and the current revision in bursting.
	status, conflict, _ := env.request(http.MethodPatch, "/api/v1/admin/site/environments/dev", env.adminToken,
		map[string]any{"cors_origins": []string{"https://other.example"}, "revision": 1})
	if status != http.StatusConflict {
		t.Fatalf("stale revision must conflict, got %d %v", status, conflict)
	}

	// Empty DB list falls back to the bootstrap origins.
	status, refreshed, _ := env.request(http.MethodGet, "/api/v1/admin/site/environments", env.adminToken, nil)
	var freshRev float64
	for _, item := range refreshed["items"].([]any) {
		if s := item.(map[string]any); s["key"] == "dev" {
			freshRev = s["revision"].(float64)
		}
	}
	status, _, _ = env.request(http.MethodPatch, "/api/v1/admin/site/environments/dev", env.adminToken,
		map[string]any{"cors_origins": []string{}, "revision": freshRev})
	if status != http.StatusOK {
		t.Fatalf("clear cors: %d", status)
	}
	code, headers = preflight(e2eBase(env, environment.Dev), "https://site.example")
	if !allowed(code, headers, "https://site.example") {
		t.Fatalf("bootstrap fallback expected on empty DB list, got %d", code)
	}

	// Non-admins cannot edit environments.
	status, tokenBody, _ := env.request(http.MethodPost, "/api/v1/admin/tokens", env.adminToken,
		map[string]any{"name": "plain", "role": "user"})
	if status != http.StatusCreated {
		t.Fatalf("create user: %d %v", status, tokenBody)
	}
	userToken := tokenBody["token"].(string)
	status, _, _ = env.request(http.MethodPatch, "/api/v1/admin/site/environments/dev", userToken,
		map[string]any{"cors_origins": []string{"*"}, "revision": freshRev + 1})
	if status != http.StatusForbidden {
		t.Fatalf("user must not patch environments, got %d", status)
	}
}

func e2eBase(env *testEnv, key environment.Environment) string {
	if key == environment.Prod {
		return env.prodServer.URL
	}
	return env.devServer.URL
}

func TestContentInvariants(t *testing.T) {
	env := newTestEnv(t)

	t.Run("prod version cannot outrun dev (DB invariant)", func(t *testing.T) {
		featureID := env.createFeature("invariant", "Инвариант", "single")
		rev := env.saveAndPublishSchema(featureID, 1, baseSchema)
		// Fresh draft is a higher version; prod may not point at it.
		status, body, _ := env.request(http.MethodPut, "/api/v1/admin/features/"+featureID+"/draft", env.adminToken,
			map[string]any{"revision": rev, "schema": map[string]any{"type": "object"}})
		if status != http.StatusOK {
			t.Fatalf("draft: %d %v", status, body)
		}
		rejected := env.versionPointerRejected(t,
			`UPDATE features SET prod_version_id = draft_version_id WHERE id = $1`, featureID)
		if !rejected {
			t.Fatal("prod pointer must not be allowed ahead of dev")
		}
	})

	t.Run("dev unpublish blocked while prod published", func(t *testing.T) {
		featureID := env.createFeature("order", "Порядок", "single")
		env.saveAndPublishSchema(featureID, 1, baseSchema)
		status, entry, _ := env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken,
			map[string]any{"data": map[string]any{"title": "order"}})
		if status != http.StatusCreated {
			t.Fatalf("entry: %d %v", status, entry)
		}
		entryID := entry["id"].(string)
		rev := entry["revision"].(float64)
		status, body, _ := env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/publish", env.adminToken,
			map[string]any{"revision": rev})
		if status != http.StatusOK {
			t.Fatalf("publish: %d %v", status, body)
		}
		rev = body["revision"].(float64)
		status, body, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/promote", env.adminToken,
			map[string]any{"revision": rev})
		if status != http.StatusOK {
			t.Fatalf("promote: %d %v", status, body)
		}
		status, body, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/unpublish", env.adminToken,
			map[string]any{"environment": "dev"})
		if status != http.StatusConflict {
			t.Fatalf("dev unpublish must conflict while prod published: %d %v", status, body)
		}
		// The documented order works: prod first, dev afterwards.
		status, body, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/unpublish", env.adminToken,
			map[string]any{"environment": "prod"})
		if status != http.StatusOK {
			t.Fatalf("prod unpublish: %d %v", status, body)
		}
		status, body, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/unpublish", env.adminToken,
			map[string]any{"environment": "dev"})
		if status != http.StatusOK {
			t.Fatalf("dev unpublish after prod: %d %v", status, body)
		}
	})

	t.Run("schema change validates active dev entries", func(t *testing.T) {
		featureID := env.createFeature("strict", "Строгая", "collection")
		loose := map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}}
		env.saveAndPublishSchema(featureID, 1, loose)
		status, entry, _ := env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken,
			map[string]any{"data": map[string]any{"title": ""}})
		if status != http.StatusCreated {
			t.Fatalf("entry: %d %v", status, entry)
		}
		entryID := entry["id"].(string)
		rev := entry["revision"].(float64)
		status, _, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/publish", env.adminToken,
			map[string]any{"revision": rev})
		if status != http.StatusOK {
			t.Fatalf("publish entry: %d", status)
		}
		// The strict schema invalidates the empty title; publishing must conflict.
		envFeatureRev := float64(0)
		status, detail, _ := env.request(http.MethodGet, "/api/v1/admin/features/"+featureID, env.adminToken, nil)
		if status != http.StatusOK {
			t.Fatalf("feature detail: %d", status)
		}
		envFeatureRev = detail["revision"].(float64)
		strict := map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"title": map[string]any{"type": "string", "minLength": 3}},
			"required":             []string{"title"},
			"additionalProperties": false,
		}
		status, body, _ := env.request(http.MethodPut, "/api/v1/admin/features/"+featureID+"/draft", env.adminToken,
			map[string]any{"revision": envFeatureRev, "schema": strict})
		if status != http.StatusOK {
			t.Fatalf("strict draft: %d %v", status, body)
		}
		envFeatureRev = body["revision"].(float64)
		status, body, _ = env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/publish", env.adminToken,
			map[string]any{"revision": envFeatureRev})
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("strict publish must be a schema validation conflict, got %d %v", status, body)
		}
		if msg, _ := body["error"].(map[string]any)["message"].(string); !strings.Contains(msg, "несовместима") {
			t.Fatalf("expected incompatible-entries message, got %v", body)
		}
	})
}

func TestPermissionMatrix(t *testing.T) {
	env := newTestEnv(t)

	mkToken := func(role, name string) (string, string) {
		env.t.Helper()
		status, body, _ := env.request(http.MethodPost, "/api/v1/admin/tokens", env.adminToken,
			map[string]any{"name": name, "role": role})
		if status != http.StatusCreated {
			env.t.Fatalf("create %s token: %d %v", role, status, body)
		}
		return body["token"].(string), body["id"].(string)
	}
	userToken, userID := mkToken("user", "plain user")
	modToken, modID := mkToken("moderator", "mod")

	featureID := env.createFeature("matrix", "Матрица", "collection")
	env.saveAndPublishSchema(featureID, 1, baseSchema)

	status, entryBody, _ := env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken,
		map[string]any{"data": map[string]any{"title": "entry"}})
	if status != http.StatusCreated {
		t.Fatalf("entry: %d %v", status, entryBody)
	}
	entryID := entryBody["id"].(string)
	entryRev := entryBody["revision"].(float64)

	rel := env.importRelease(t, "1.0.0", "v1", zipArtifact(t, map[string]string{"index.html": "ok"}, nil))
	relID := rel["id"].(string)

	type probe struct {
		name   string
		token  string
		method string
		path   string
		body   map[string]any
		want   int
	}
	probes := []probe{
		{name: "user cannot create features", token: userToken, method: http.MethodPost,
			path: "/api/v1/admin/features", body: map[string]any{"key": "nope", "name": "Нет", "type": "collection"}, want: http.StatusForbidden},
		{name: "user cannot save schema draft", token: userToken, method: http.MethodPut,
			path: "/api/v1/admin/features/" + featureID + "/draft", body: map[string]any{"revision": 999, "schema": baseSchema}, want: http.StatusForbidden},
		{name: "user cannot promote entry", token: userToken, method: http.MethodPost,
			path: "/api/v1/admin/entries/" + entryID + "/promote", body: map[string]any{"revision": entryRev}, want: http.StatusForbidden},
		{name: "user cannot import release", token: userToken, method: http.MethodPost,
			path: "/api/v1/admin/releases", body: map[string]any{"version": "9"}, want: http.StatusForbidden},
		{name: "user cannot activate release", token: userToken, method: http.MethodPost,
			path: "/api/v1/admin/releases/" + relID + "/activate", body: map[string]any{"environment": "dev"}, want: http.StatusForbidden},
		{name: "user cannot read settings", token: userToken, method: http.MethodGet,
			path: "/api/v1/admin/settings", want: http.StatusForbidden},
		{name: "user cannot read publication events", token: userToken, method: http.MethodGet,
			path: "/api/v1/admin/publication-events", want: http.StatusForbidden},
		{name: "user can create entry", token: userToken, method: http.MethodPost,
			path: "/api/v1/admin/features/" + featureID + "/entries", body: map[string]any{"data": map[string]any{"title": "user"}}, want: http.StatusCreated},
		{name: "moderator cannot activate prod release", token: modToken, method: http.MethodPost,
			path: "/api/v1/admin/releases/" + relID + "/activate", body: map[string]any{"environment": "prod"}, want: http.StatusForbidden},
		{name: "moderator cannot rollback prod release", token: modToken, method: http.MethodPost,
			path: "/api/v1/admin/releases/" + relID + "/rollback", body: map[string]any{"environment": "prod"}, want: http.StatusForbidden},
		{name: "moderator cannot patch environments", token: modToken, method: http.MethodPatch,
			path: "/api/v1/admin/site/environments/dev", body: map[string]any{"revision": 1}, want: http.StatusForbidden},
		{name: "moderator reads publication events", token: modToken, method: http.MethodGet,
			path: "/api/v1/admin/publication-events", want: http.StatusOK},
		{name: "moderator reads settings", token: modToken, method: http.MethodGet,
			path: "/api/v1/admin/settings", want: http.StatusOK},
		{name: "moderator reads own tokens scope", token: modToken, method: http.MethodGet,
			path: "/api/v1/admin/tokens", want: http.StatusOK},
	}
	_ = userID
	_ = modID
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			status, body, _ := env.request(p.method, p.path, p.token, p.body)
			if status != p.want {
				t.Fatalf("got %d want %d: %v", status, p.want, body)
			}
		})
	}

	// Moderator CAN activate in dev and CAN publish schema.
	status, body, _ := env.request(http.MethodPost, "/api/v1/admin/releases/"+relID+"/activate", modToken,
		map[string]any{"environment": "dev"})
	if status != http.StatusOK {
		t.Fatalf("moderator dev activation: %d %v", status, body)
	}
	status, detail, _ := env.request(http.MethodGet, "/api/v1/admin/features/"+featureID, env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("feature detail: %d", status)
	}
	rev := detail["revision"].(float64)
	status, body, _ = env.request(http.MethodPut, "/api/v1/admin/features/"+featureID+"/draft", modToken,
		map[string]any{"revision": rev, "schema": map[string]any{"type": "object"}})
	if status != http.StatusOK {
		t.Fatalf("moderator draft: %d %v", status, body)
	}
	rev = body["revision"].(float64)
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/publish", modToken,
		map[string]any{"revision": rev})
	if status != http.StatusOK {
		t.Fatalf("moderator publish schema: %d %v", status, body)
	}
}
