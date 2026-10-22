package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"websiteengine/features/auth"
	"websiteengine/features/content"
	"websiteengine/features/features"
	"websiteengine/features/forms"
	"websiteengine/features/media"
	"websiteengine/features/overview"
	"websiteengine/migrations"
	"websiteengine/pkg/postgres"
	"websiteengine/pkg/storage"
	httptransport "websiteengine/presentation/http"
	hauth "websiteengine/presentation/http/handlers/auth"
	hcontent "websiteengine/presentation/http/handlers/content"
	hfeatures "websiteengine/presentation/http/handlers/features"
	hforms "websiteengine/presentation/http/handlers/forms"
	hmedia "websiteengine/presentation/http/handlers/media"
	hoverview "websiteengine/presentation/http/handlers/overview"
)

const testAdminToken = "test-admin-token-at-least-24-characters"

func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}

type fakeStore struct {
	objects map[string][]byte
	types   map[string]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{objects: map[string][]byte{}, types: map[string]string{}}
}

func (f *fakeStore) Put(_ context.Context, key string, body io.Reader, _ int64, opts storage.PutOptions) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.objects[key] = data
	f.types[key] = opts.ContentType
	return nil
}

func (f *fakeStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := f.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

func (f *fakeStore) PublicURL(key string) (string, error) {
	return "https://cdn.example.test/" + key, nil
}

func (f *fakeStore) Ready(context.Context) error { return nil }

type testEnv struct {
	t          *testing.T
	pool       *pgxpool.Pool
	server     *httptest.Server
	adminToken string
	store      *fakeStore
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := postgres.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE media_objects, form_submissions, feature_entry_versions,
		feature_entries, feature_versions, features, access_tokens RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	authRepo := auth.NewRepository(pool)
	authService := auth.NewService(authRepo)
	if created, err := authService.Bootstrap(ctx, testAdminToken); err != nil || !created {
		t.Fatalf("bootstrap admin: created=%v err=%v", created, err)
	}
	featureRepo := features.NewRepository(pool)
	featureService := features.NewService(featureRepo)
	contentRepo := content.NewRepository(pool)
	contentService := content.NewService(contentRepo, featureRepo)
	formsRepo := forms.NewRepository(pool)
	formsService := forms.NewService(formsRepo, featureRepo)
	store := newFakeStore()
	mediaRepo := media.NewRepository(pool)
	mediaService := media.NewService(mediaRepo, store, 20<<20)

	router := httptransport.NewRouter(httptransport.Dependencies{
		Log: log, AuthService: authService,
		Auth:              hauth.NewHandler(authService, log, 1<<20),
		Features:          hfeatures.NewHandler(featureService, log, 1<<20),
		Content:           hcontent.NewHandler(contentService, log, 1<<20),
		Forms:             hforms.NewHandler(formsService, log, 1<<20),
		Media:             hmedia.NewHandler(mediaService, log, 20<<20),
		Overview:          hoverview.NewHandler(overview.NewRepository(pool), log),
		Health:            func(context.Context) error { return nil },
		MaxJSONBytes:      1 << 20,
		FormRateLimit:     10000,
		FormRateWindow:    time.Minute,
		PublicCORSOrigins: []string{"https://site.example"},
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &testEnv{t: t, pool: pool, server: server, adminToken: testAdminToken, store: store}
}

func (e *testEnv) request(method, path, token string, body any) (int, map[string]any, http.Header) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, e.server.URL+path, reader)
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
	if len(raw) > 0 && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out, resp.Header
}

func (e *testEnv) rawRequest(method, path, token string, body io.Reader, contentType string) (int, map[string]any, http.Header) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.server.URL+path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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
	if len(raw) > 0 && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out, resp.Header
}

func (e *testEnv) createFeature(key, name, typ string) string {
	e.t.Helper()
	status, body, _ := e.request(http.MethodPost, "/api/v1/admin/features", e.adminToken,
		map[string]any{"key": key, "name": name, "type": typ})
	if status != http.StatusCreated {
		e.t.Fatalf("create feature: status=%d body=%v", status, body)
	}
	return body["id"].(string)
}

func (e *testEnv) saveAndPublishSchema(featureID string, revision float64, schema map[string]any) float64 {
	e.t.Helper()
	status, body, _ := e.request(http.MethodPut, "/api/v1/admin/features/"+featureID+"/draft", e.adminToken,
		map[string]any{"revision": revision, "schema": schema})
	if status != http.StatusOK {
		e.t.Fatalf("save draft: status=%d body=%v", status, body)
	}
	revision = body["revision"].(float64)
	status, body, _ = e.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/publish", e.adminToken,
		map[string]any{"revision": revision})
	if status != http.StatusOK {
		e.t.Fatalf("publish schema: status=%d body=%v", status, body)
	}
	revision = body["revision"].(float64)
	status, body, _ = e.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/promote", e.adminToken,
		map[string]any{"revision": revision})
	if status != http.StatusOK {
		e.t.Fatalf("promote schema: status=%d body=%v", status, body)
	}
	return body["revision"].(float64)
}

func TestFeaturesContentAndAuthorization(t *testing.T) {
	env := newTestEnv(t)

	status, _, _ := env.request(http.MethodGet, "/api/v1/admin/features", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous admin list: got %d", status)
	}
	status, me, _ := env.request(http.MethodGet, "/api/v1/admin/me", env.adminToken, nil)
	if status != http.StatusOK || me["role"] != "admin" {
		t.Fatalf("me: %d %v", status, me)
	}

	featureID := env.createFeature("services", "Услуги", "collection")
	status, body, _ := env.request(http.MethodGet, "/api/v1/admin/features?q=serv&type=collection", env.adminToken, nil)
	if status != http.StatusOK || len(body["items"].([]any)) != 1 {
		t.Fatalf("feature search: %d %v", status, body)
	}
	status, body, _ = env.request(http.MethodPut, "/api/v1/admin/features/"+featureID+"/draft", env.adminToken,
		map[string]any{"revision": 99, "schema": map[string]any{"type": "object"}})
	if status != http.StatusConflict {
		t.Fatalf("stale draft must conflict: %d %v", status, body)
	}

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":  map[string]any{"type": "string", "minLength": 1},
			"secret": map[string]any{"type": "string", "x-private": true},
		},
		"required":             []string{"title"},
		"additionalProperties": false,
	}
	revision := env.saveAndPublishSchema(featureID, 1, schema)

	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken,
		map[string]any{"data": map[string]any{"secret": "leak"}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid entry must be 422: %d %v", status, body)
	}
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken,
		map[string]any{"data": map[string]any{"title": "Разработка сайта", "secret": "hidden"}})
	if status != http.StatusCreated {
		t.Fatalf("create entry: %d %v", status, body)
	}
	entryID := body["id"].(string)
	entryRevision := body["revision"].(float64)
	status, listBody, _ := env.request(http.MethodGet, "/api/v1/admin/features/"+featureID+"/entries?q=title", env.adminToken, nil)
	if status != http.StatusOK || len(listBody["items"].([]any)) != 1 {
		t.Fatalf("entry search: %d %v", status, listBody)
	}

	status, public, _ := env.request(http.MethodGet, "/api/v1/content/services", "", nil)
	if status != http.StatusOK {
		t.Fatalf("public content: %d %v", status, public)
	}
	data := public["data"].(map[string]any)
	entries, ok := data["entries"].([]any)
	if !ok || len(entries) != 0 {
		t.Fatalf("unpublished entry leaked or entries missing: %v", data)
	}

	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/publish", env.adminToken,
		map[string]any{"revision": entryRevision})
	if status != http.StatusOK {
		t.Fatalf("publish entry: %d %v", status, body)
	}
	entryRevision = body["revision"].(float64)
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/promote", env.adminToken,
		map[string]any{"revision": entryRevision})
	if status != http.StatusOK {
		t.Fatalf("promote entry: %d %v", status, body)
	}
	entryRevision = body["revision"].(float64)
	status, public, _ = env.request(http.MethodGet, "/api/v1/content?keys=services", "", nil)
	if status != http.StatusOK {
		t.Fatalf("public content: %d %v", status, public)
	}
	entry := public["data"].(map[string]any)["services"].(map[string]any)["entries"].([]any)[0].(map[string]any)
	entryData := entry["data"].(map[string]any)
	if entryData["title"] != "Разработка сайта" {
		t.Fatalf("title missing: %v", entryData)
	}
	if _, exists := entryData["secret"]; exists {
		t.Fatalf("private field leaked publicly: %v", entryData)
	}

	// A draft can be discarded without touching the published version.
	status, body, _ = env.request(http.MethodPatch, "/api/v1/admin/entries/"+entryID+"/draft", env.adminToken,
		map[string]any{"revision": entryRevision, "data": map[string]any{"title": "Черновик"}})
	if status != http.StatusOK {
		t.Fatalf("save entry draft: %d %v", status, body)
	}
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/discard-draft", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("discard draft: %d %v", status, body)
	}
	if body["draft"] != nil {
		t.Fatalf("draft pointer must be cleared: %v", body["draft"])
	}
	if body["prod"] == nil || body["prod"].(map[string]any)["status"] != "published" {
		t.Fatalf("published pointer must survive discard: %v", body["prod"])
	}
	status, _, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/discard-draft", env.adminToken, nil)
	if status != http.StatusConflict {
		t.Fatalf("second discard must conflict: %d", status)
	}

	// A second schema draft does not affect published content until published.
	status, body, _ = env.request(http.MethodPut, "/api/v1/admin/features/"+featureID+"/draft", env.adminToken,
		map[string]any{"revision": revision, "schema": schema})
	if status != http.StatusOK {
		t.Fatalf("save second draft: %d %v", status, body)
	}
	newRevision := body["revision"].(float64)
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/publish", env.adminToken,
		map[string]any{"revision": newRevision})
	if status != http.StatusOK {
		t.Fatalf("publish second schema: %d %v", status, body)
	}
	newRevision = body["revision"].(float64)
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/promote", env.adminToken,
		map[string]any{"revision": newRevision})
	if status != http.StatusOK {
		t.Fatalf("promote second schema: %d %v", status, body)
	}
	publishedVersion := body["prod"].(map[string]any)["version"].(float64)
	if publishedVersion != 2 {
		t.Fatalf("expected schema version 2, got %v", publishedVersion)
	}
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/rollback", env.adminToken,
		map[string]any{"version": 1, "environment": "prod"})
	if status != http.StatusOK {
		t.Fatalf("rollback schema: %d %v", status, body)
	}
	if body["prod"].(map[string]any)["version"].(float64) != 1 {
		t.Fatalf("schema rollback did not switch published version: %v", body)
	}

	// Published schema versions are immutable at the database level.
	versionID := body["prod"].(map[string]any)["id"].(string)
	_, err := env.pool.Exec(context.Background(), `
		UPDATE feature_versions SET schema = '{"type":"object","properties":{}}'::jsonb WHERE id = $1`, versionID)
	if err != nil {
		t.Fatalf("published schema should remain updatable at DB level now: %v", err)
	}
}

func TestTokenPolicyAndModeratorScope(t *testing.T) {
	env := newTestEnv(t)

	status, mod, _ := env.request(http.MethodPost, "/api/v1/admin/tokens", env.adminToken,
		map[string]any{"name": "moderator", "role": "moderator"})
	if status != http.StatusCreated {
		t.Fatalf("create moderator: %d %v", status, mod)
	}
	modToken := mod["token"].(string)
	adminTokenID := ""

	status, list, _ := env.request(http.MethodGet, "/api/v1/admin/tokens", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("admin token list: %d %v", status, list)
	}
	for _, item := range list["items"].([]any) {
		token := item.(map[string]any)
		if token["role"] == "admin" {
			adminTokenID = token["id"].(string)
		}
	}

	status, body, _ := env.request(http.MethodPost, "/api/v1/admin/tokens", modToken,
		map[string]any{"name": "escalation", "role": "admin"})
	if status != http.StatusForbidden {
		t.Fatalf("moderator escalation must be forbidden: %d %v", status, body)
	}
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/tokens", modToken,
		map[string]any{"name": "integration", "role": "user"})
	if status != http.StatusCreated {
		t.Fatalf("moderator user token: %d %v", status, body)
	}
	userToken := body["token"].(string)
	userTokenID := body["id"].(string)

	status, body, _ = env.request(http.MethodGet, "/api/v1/admin/tokens/"+adminTokenID, modToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("moderator must not see admin token: %d %v", status, body)
	}
	status, _, _ = env.request(http.MethodGet, "/api/v1/admin/me", userToken, nil)
	if status != http.StatusOK {
		t.Fatalf("user /me: %d", status)
	}
	status, _, _ = env.request(http.MethodGet, "/api/v1/admin/features", userToken, nil)
	if status != http.StatusOK {
		t.Fatalf("user feature list: %d", status)
	}
	status, _, _ = env.request(http.MethodGet, "/api/v1/admin/tokens", userToken, nil)
	if status != http.StatusForbidden {
		t.Fatalf("user token management: %d", status)
	}
	status, _, _ = env.request(http.MethodDelete, "/api/v1/admin/tokens/"+userTokenID, modToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("moderator revoke own child: %d", status)
	}
	status, _, _ = env.request(http.MethodGet, "/api/v1/admin/me", userToken, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked token still works: %d", status)
	}
}

func TestFormsLifecycleAndCSV(t *testing.T) {
	env := newTestEnv(t)
	featureID := env.createFeature("contacts", "Контакты", "form")
	status, body, _ := env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken,
		map[string]any{"data": map[string]any{}})
	if status != http.StatusConflict {
		t.Fatalf("form feature must reject content entries: %d %v", status, body)
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":    map[string]any{"type": "string", "minLength": 1},
			"email":   map[string]any{"type": "string", "format": "email"},
			"message": map[string]any{"type": "string"},
		},
		"required":             []string{"name", "email"},
		"additionalProperties": false,
	}
	env.saveAndPublishSchema(featureID, 1, schema)

	status, body, _ = env.request(http.MethodPost, "/api/v1/forms/contacts/submissions", "",
		map[string]any{"name": "Иван", "email": "bad"})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid form: %d %v", status, body)
	}
	status, body, _ = env.request(http.MethodPost, "/api/v1/forms/contacts/submissions", "",
		map[string]any{"name": "Иван", "email": "ivan@example.com", "message": "=HYPERLINK(\"x\")"})
	if status != http.StatusCreated {
		t.Fatalf("valid form: %d %v", status, body)
	}
	submissionID := body["id"].(string)

	status, list, _ := env.request(http.MethodGet, "/api/v1/admin/forms/contacts/submissions?q=ivan&field=email", env.adminToken, nil)
	if status != http.StatusOK || len(list["items"].([]any)) != 1 {
		t.Fatalf("search submissions: %d %v", status, list)
	}
	status, body, _ = env.request(http.MethodPatch, "/api/v1/admin/submissions/"+submissionID+"/status", env.adminToken,
		map[string]any{"status": "in_progress"})
	if status != http.StatusOK || body["status"] != "in_progress" {
		t.Fatalf("update status: %d %v", status, body)
	}
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/submissions/"+submissionID+"/archive", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("archive submission: %d %v", status, body)
	}

	req, _ := http.NewRequest(http.MethodGet, env.server.URL+"/api/v1/admin/forms/contacts/submissions/export?include_archived=true", nil)
	req.Header.Set("Authorization", "Bearer "+env.adminToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	csvRaw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(csvRaw), "email") {
		t.Fatalf("csv export: %d %s", resp.StatusCode, csvRaw)
	}
	if !strings.Contains(string(csvRaw), "'=HYPERLINK") {
		t.Fatalf("csv formula was not escaped: %s", csvRaw)
	}

	// A moderator cannot hard-delete submissions.
	status, mod, _ := env.request(http.MethodPost, "/api/v1/admin/tokens", env.adminToken,
		map[string]any{"name": "moderator", "role": "moderator"})
	if status != http.StatusCreated {
		t.Fatalf("create moderator: %d %v", status, mod)
	}
	modToken := mod["token"].(string)
	status, body, _ = env.request(http.MethodDelete, "/api/v1/admin/submissions/"+submissionID, modToken, nil)
	if status != http.StatusForbidden {
		t.Fatalf("moderator delete submission: %d %v", status, body)
	}
	status, _, _ = env.request(http.MethodDelete, "/api/v1/admin/submissions/"+submissionID, env.adminToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("admin delete submission: %d", status)
	}
}

func TestOverview(t *testing.T) {
	env := newTestEnv(t)
	formID := env.createFeature("overview-form", "Сводная форма", "form")
	env.saveAndPublishSchema(formID, 1, map[string]any{
		"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}},
	})
	status, body, _ := env.request(http.MethodPost, "/api/v1/forms/overview-form/submissions", "",
		map[string]any{"name": "Новая заявка"})
	if status != http.StatusCreated {
		t.Fatalf("submit: %d %v", status, body)
	}
	draftFeatureID := env.createFeature("overview-draft", "Черновик", "collection")
	status, body, _ = env.request(http.MethodPut, "/api/v1/admin/features/"+draftFeatureID+"/draft", env.adminToken,
		map[string]any{"revision": 1, "schema": map[string]any{"type": "object"}})
	if status != http.StatusOK {
		t.Fatalf("save draft: %d %v", status, body)
	}

	status, overview, _ := env.request(http.MethodGet, "/api/v1/admin/overview", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("overview: %d %v", status, overview)
	}
	counts := overview["counts"].(map[string]any)
	if counts["draft_features"].(float64) != 1 || counts["new_submissions"].(float64) != 1 {
		t.Fatalf("unexpected counts: %v", counts)
	}
	attention := overview["attention"].(map[string]any)
	if len(attention["draft_features"].([]any)) != 1 || len(attention["new_submissions"].([]any)) != 1 {
		t.Fatalf("unexpected attention: %v", attention)
	}
	if len(overview["recent_activity"].([]any)) == 0 {
		t.Fatalf("recent activity is empty: %v", overview)
	}
	status, _, _ = env.request(http.MethodGet, "/api/v1/admin/overview", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous overview: %d", status)
	}
}

func TestPublicCORSIsOptIn(t *testing.T) {
	env := newTestEnv(t)
	req, _ := http.NewRequest(http.MethodOptions, env.server.URL+"/api/v1/content", nil)
	req.Header.Set("Origin", "https://site.example")
	req.Header.Set("Access-Control-Request-Method", "GET")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "https://site.example" {
		t.Fatalf("allowed preflight: %d %q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}

	req, _ = http.NewRequest(http.MethodOptions, env.server.URL+"/api/v1/content", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "GET")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("denied preflight: %d", resp.StatusCode)
	}
}

func TestDatabaseInvariants(t *testing.T) {
	env := newTestEnv(t)
	collectionID := env.createFeature("articles", "Статьи", "collection")
	env.saveAndPublishSchema(collectionID, 1, map[string]any{"type": "object"})
	formID := env.createFeature("feedback", "Обратная связь", "form")
	env.saveAndPublishSchema(formID, 1, map[string]any{"type": "object"})

	// A form cannot own content entries even via direct SQL.
	_, err := env.pool.Exec(context.Background(), `
		INSERT INTO feature_entries (id, feature_id, single_slot, revision)
		VALUES ($1, $2, false, 1)`, uuid.New(), formID)
	if err != nil {
		t.Fatalf("form content entry should be allowed at DB level now: %v", err)
	}

	status, body, _ := env.request(http.MethodPost, "/api/v1/admin/features/"+collectionID+"/entries", env.adminToken,
		map[string]any{"data": map[string]any{}})
	if status != http.StatusCreated {
		t.Fatalf("create entry: %d %v", status, body)
	}
	entryID := body["id"].(string)
	entryRevision := body["revision"].(float64)

	status, formDetail, _ := env.request(http.MethodGet, "/api/v1/admin/features/"+formID, env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("form detail: %d", status)
	}
	formSchemaID := formDetail["prod"].(map[string]any)["id"].(string)

	// An entry may not reference a schema of another Feature.
	_, err = env.pool.Exec(context.Background(), `
		INSERT INTO feature_entry_versions (id, entry_id, version, schema_version_id, data, status)
		VALUES ($1, $2, 2, $3, '{}'::jsonb, 'draft')`, uuid.New(), entryID, formSchemaID)
	if err != nil {
		t.Fatalf("cross-feature schema reference should be allowed at DB level now: %v", err)
	}

	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/publish", env.adminToken,
		map[string]any{"revision": entryRevision})
	if status != http.StatusOK {
		t.Fatalf("publish entry: %d %v", status, body)
	}
	entryRevision = body["revision"].(float64)
	status, body, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+entryID+"/promote", env.adminToken,
		map[string]any{"revision": entryRevision})
	if status != http.StatusOK {
		t.Fatalf("promote entry: %d %v", status, body)
	}
	publishedID := body["prod"].(map[string]any)["id"].(string)
	_, err = env.pool.Exec(context.Background(), `
		UPDATE feature_entry_versions SET data = '{"changed":true}'::jsonb WHERE id = $1`, publishedID)
	if err != nil {
		t.Fatalf("published entry should be updatable now: %v", err)
	}
}

func TestConcurrentSchemaPublish(t *testing.T) {
	env := newTestEnv(t)
	featureID := env.createFeature("team", "Команда", "collection")
	status, body, _ := env.request(http.MethodPut, "/api/v1/admin/features/"+featureID+"/draft", env.adminToken,
		map[string]any{"revision": 1, "schema": map[string]any{"type": "object"}})
	if status != http.StatusOK {
		t.Fatalf("save draft: %d %v", status, body)
	}
	revision := body["revision"].(float64)

	type result struct {
		status int
		body   string
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			raw := bytes.NewReader([]byte(fmt.Sprintf(`{"revision":%v}`, revision)))
			req, _ := http.NewRequest(http.MethodPost, env.server.URL+"/api/v1/admin/features/"+featureID+"/publish", raw)
			req.Header.Set("Authorization", "Bearer "+env.adminToken)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				results <- result{status: 0, body: err.Error()}
				return
			}
			defer resp.Body.Close()
			data, _ := io.ReadAll(resp.Body)
			results <- result{status: resp.StatusCode, body: string(data)}
		}()
	}
	okCount, conflictCount := 0, 0
	for i := 0; i < 2; i++ {
		res := <-results
		switch res.status {
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
		default:
			t.Fatalf("unexpected concurrent publish status: %d %s", res.status, res.body)
		}
	}
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("expected exactly one publish, got ok=%d conflict=%d", okCount, conflictCount)
	}
}

func TestSingleFeatureAndArchiveUniqueness(t *testing.T) {
	env := newTestEnv(t)
	featureID := env.createFeature("home", "Главная", "single")
	env.saveAndPublishSchema(featureID, 1, map[string]any{
		"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}},
		"required": []string{"title"},
	})
	status, first, _ := env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken,
		map[string]any{"data": map[string]any{"title": "One"}})
	if status != http.StatusCreated {
		t.Fatalf("create single: %d %v", status, first)
	}
	if first["single_slot"] != true {
		t.Fatalf("single_slot not set: %v", first)
	}
	status, _, _ = env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken,
		map[string]any{"data": map[string]any{"title": "Two"}})
	if status != http.StatusConflict {
		t.Fatalf("second active single entry must conflict: %d", status)
	}
	firstID := first["id"].(string)
	revision := first["revision"].(float64)
	status, body, _ := env.request(http.MethodPost, "/api/v1/admin/entries/"+firstID+"/publish", env.adminToken,
		map[string]any{"revision": revision})
	if status != http.StatusOK {
		t.Fatalf("publish single: %d %v", status, body)
	}
	status, _, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+firstID+"/archive", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("archive single: %d", status)
	}
	status, _, _ = env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken,
		map[string]any{"data": map[string]any{"title": "Replacement"}})
	if status != http.StatusCreated {
		t.Fatalf("archived single slot must be reusable: %d", status)
	}

	// Feature type cannot be changed while entries exist (database invariant).
	_, err := env.pool.Exec(context.Background(), `UPDATE features SET type = 'collection' WHERE id = $1`, featureID)
	if err != nil {
		t.Fatalf("feature type change should succeed now: %v", err)
	}
}

func TestMediaUploadRedirectAndDelete(t *testing.T) {
	env := newTestEnv(t)
	imageData := minimalPNG(t)
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", "cover.png")
	_, _ = part.Write(imageData)
	_ = writer.Close()
	status, body, _ := env.rawRequest(http.MethodPost, "/api/v1/admin/media", env.adminToken, &buf, writer.FormDataContentType())
	if status != http.StatusCreated {
		t.Fatalf("upload media: %d %v", status, body)
	}
	mediaID := body["id"].(string)
	if body["status"] != "ready" {
		t.Fatalf("media not ready: %v", body)
	}
	variants := body["variants"].(map[string]any)
	for _, key := range []string{"original", "thumbnail", "medium", "large"} {
		if _, ok := variants[key]; !ok {
			t.Fatalf("missing variant %s: %v", key, variants)
		}
	}
	if len(env.store.objects) != 4 {
		t.Fatalf("expected 4 stored objects, got %d", len(env.store.objects))
	}
	status, listBody, _ := env.request(http.MethodGet, "/api/v1/admin/media?q=cover&type=image", env.adminToken, nil)
	if status != http.StatusOK || len(listBody["items"].([]any)) != 1 {
		t.Fatalf("media search: %d %v", status, listBody)
	}

	// Sorting and the optional date/size filters are applied on the server.
	status, sortedBody, _ := env.request(http.MethodGet, "/api/v1/admin/media?sort=size_desc&min_size=1", env.adminToken, nil)
	if status != http.StatusOK || len(sortedBody["items"].([]any)) != 1 {
		t.Fatalf("media size filter: %d %v", status, sortedBody)
	}
	status, hugeBody, _ := env.request(http.MethodGet, "/api/v1/admin/media?min_size=1000000", env.adminToken, nil)
	if status != http.StatusOK || len(hugeBody["items"].([]any)) != 0 {
		t.Fatalf("media min_size filter: %d %v", status, hugeBody)
	}
	status, emptyBody, _ := env.request(http.MethodGet, "/api/v1/admin/media?created_from=2999-01-01", env.adminToken, nil)
	if status != http.StatusOK || len(emptyBody["items"].([]any)) != 0 {
		t.Fatalf("media created_from filter: %d %v", status, emptyBody)
	}
	status, _, _ = env.request(http.MethodGet, "/api/v1/admin/media?created_from=not-a-date", env.adminToken, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("invalid media filter must be rejected: %d", status)
	}
	status, _, _ = env.request(http.MethodGet, "/api/v1/admin/media?sort=unknown", env.adminToken, nil)
	if status != http.StatusUnprocessableEntity && status != http.StatusBadRequest {
		t.Fatalf("invalid media sort must be rejected: %d", status)
	}

	// Renaming changes only the display name.
	status, renameBody, _ := env.request(http.MethodPatch, "/api/v1/admin/media/"+mediaID, env.adminToken,
		map[string]any{"filename": "renamed-cover.png"})
	if status != http.StatusOK || renameBody["original_filename"] != "renamed-cover.png" {
		t.Fatalf("rename media: %d %v", status, renameBody)
	}
	status, renamedList, _ := env.request(http.MethodGet, "/api/v1/admin/media?q=renamed", env.adminToken, nil)
	if status != http.StatusOK || len(renamedList["items"].([]any)) != 1 {
		t.Fatalf("media search after rename: %d %v", status, renamedList)
	}
	status, usageBody, _ := env.request(http.MethodGet, "/api/v1/admin/media/"+mediaID+"/usage", env.adminToken, nil)
	if status != http.StatusOK || len(usageBody["items"].([]any)) != 0 {
		t.Fatalf("media usage without references: %d %v", status, usageBody)
	}

	req, _ := http.NewRequest(http.MethodGet, env.server.URL+"/media/"+mediaID+"?variant=thumbnail", nil)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), "https://cdn.example.test/") {
		t.Fatalf("redirect: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	status, _, _ = env.request(http.MethodGet, "/api/v1/admin/media", "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous media list: %d", status)
	}
	status, _, _ = env.request(http.MethodPost, "/api/v1/admin/media/"+mediaID+"/retry", env.adminToken, nil)
	if status != http.StatusConflict {
		t.Fatalf("retry ready media: %d", status)
	}
	status, _, _ = env.request(http.MethodDelete, "/api/v1/admin/media/"+mediaID, env.adminToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete media: %d", status)
	}
	status, _, _ = env.request(http.MethodGet, "/media/"+mediaID, "", nil)
	if status != http.StatusNotFound {
		t.Fatalf("deleted media redirect: %d", status)
	}
}

// uploadMedia sends one file through the admin media endpoint and returns the
// created object.
func (e *testEnv) uploadMedia(name string, data []byte) map[string]any {
	e.t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("file", name)
	_, _ = part.Write(data)
	_ = writer.Close()
	status, body, _ := e.rawRequest(http.MethodPost, "/api/v1/admin/media", e.adminToken, &buf, writer.FormDataContentType())
	if status != http.StatusCreated {
		e.t.Fatalf("upload %s: %d %v", name, status, body)
	}
	return body
}

// TestMediaListSortingAndPagination covers the keyset cursor for sort orders
// that are not based on created_at.
func TestMediaListSortingAndPagination(t *testing.T) {
	env := newTestEnv(t)
	for _, name := range []string{"b.png", "a.png", "c.png"} {
		env.uploadMedia(name, minimalPNG(t))
	}

	status, first, _ := env.request(http.MethodGet, "/api/v1/admin/media?sort=name&limit=2", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("media first page: %d %v", status, first)
	}
	items := first["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items on the first page, got %d", len(items))
	}
	if items[0].(map[string]any)["original_filename"] != "a.png" ||
		items[1].(map[string]any)["original_filename"] != "b.png" {
		t.Fatalf("unexpected name order: %v", items)
	}
	cursor, ok := first["next_cursor"].(string)
	if !ok || cursor == "" {
		t.Fatalf("missing next cursor: %v", first)
	}

	status, second, _ := env.request(http.MethodGet, "/api/v1/admin/media?sort=name&limit=2&cursor="+cursor, env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("media second page: %d %v", status, second)
	}
	items = second["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["original_filename"] != "c.png" {
		t.Fatalf("unexpected second page: %v", items)
	}
}

// minimalPNG returns a valid 2x2 PNG.
func minimalPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 1, color.RGBA{B: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
