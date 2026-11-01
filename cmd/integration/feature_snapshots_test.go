package integration

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"websiteengine/features/forms"
	"websiteengine/pkg/environment"
)

func featureArtifact(t *testing.T, manifest map[string]any, entry string) []byte {
	t.Helper()
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	for _, asset := range []struct {
		name string
		data []byte
	}{{name: "manifest.json", data: manifestRaw}, {name: "entry.js", data: []byte(entry)}} {
		file, err := w.Create(asset.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(asset.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func uploadFeatureBuild(t *testing.T, env *testEnv, manifest map[string]any, entry string) map[string]any {
	t.Helper()
	status, body, _ := env.rawRequest(http.MethodPost, "/api/v2/admin/feature-builds", env.adminToken,
		bytes.NewReader(featureArtifact(t, manifest, entry)), "application/zip")
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("upload feature build: status=%d body=%v", status, body)
	}
	return body
}

func manifestFor(key, version, mode string, schema map[string]any) map[string]any {
	return map[string]any{
		"formatVersion": 1,
		"key":           key,
		"name":          key,
		"version":       version,
		"engineApi":     "1",
		"entry":         "entry.js",
		"slots":         []string{"main"},
		"arguments": map[string]any{
			"type": "object", "properties": map[string]any{}, "additionalProperties": false,
		},
		"content": map[string]any{"mode": mode, "schema": schema},
	}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func singlePageConfig(key, version string) map[string]any {
	return map[string]any{
		"formatVersion": 2,
		"pages": []any{map[string]any{
			"id": "home", "name": "Home", "kind": "page",
			"placements": []any{map[string]any{
				"id": "single", "build": map[string]any{"featureKey": key, "version": version}, "slot": "main",
			}},
		}},
		"routeTree": map[string]any{"id": "root", "type": "index", "pageId": "home"},
	}
}

func saveAndDeploySnapshot(t *testing.T, env *testEnv, config map[string]any, name string) map[string]any {
	t.Helper()
	status, draft, _ := env.request(http.MethodGet, "/api/v2/admin/site/draft", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("get site draft: status=%d body=%v", status, draft)
	}
	status, draft, _ = env.request(http.MethodPut, "/api/v2/admin/site/draft", env.adminToken, map[string]any{
		"revision": draft["revision"], "config": config,
	})
	if status != http.StatusOK {
		t.Fatalf("save site draft: status=%d body=%v", status, draft)
	}
	status, snapshot, _ := env.request(http.MethodPost, "/api/v2/admin/site/snapshots", env.adminToken, map[string]any{"name": name})
	if status != http.StatusCreated {
		t.Fatalf("create site snapshot: status=%d body=%v", status, snapshot)
	}
	status, deployed, _ := env.request(http.MethodPost, "/api/v2/admin/site/snapshots/"+snapshot["id"].(string)+"/deploy", env.adminToken, map[string]any{"environment": "dev"})
	if status != http.StatusOK || deployed["id"] != snapshot["id"] {
		t.Fatalf("deploy site snapshot to dev: status=%d body=%v", status, deployed)
	}
	return snapshot
}

func createAndPublishEntry(t *testing.T, env *testEnv, featureID string, data map[string]any) (string, float64) {
	t.Helper()
	status, entry, _ := env.request(http.MethodPost, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken, map[string]any{"data": data})
	if status != http.StatusCreated {
		t.Fatalf("create entry: status=%d body=%v", status, entry)
	}
	id := entry["id"].(string)
	revision := entry["revision"].(float64)
	status, entry, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+id+"/publish", env.adminToken, map[string]any{"revision": revision})
	if status != http.StatusOK {
		t.Fatalf("publish entry: status=%d body=%v", status, entry)
	}
	return id, entry["revision"].(float64)
}

func updateAndPublishEntry(t *testing.T, env *testEnv, id string, revision float64, data map[string]any) float64 {
	t.Helper()
	status, entry, _ := env.request(http.MethodPatch, "/api/v1/admin/entries/"+id+"/draft", env.adminToken, map[string]any{"revision": revision, "data": data})
	if status != http.StatusOK {
		t.Fatalf("update entry draft: status=%d body=%v", status, entry)
	}
	revision = entry["revision"].(float64)
	status, entry, _ = env.request(http.MethodPost, "/api/v1/admin/entries/"+id+"/publish", env.adminToken, map[string]any{"revision": revision})
	if status != http.StatusOK {
		t.Fatalf("publish updated entry: status=%d body=%v", status, entry)
	}
	return entry["revision"].(float64)
}

func runtimePlacement(t *testing.T, env *testEnv, serverURL, path string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, serverURL+"/api/v2/runtime?path="+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get runtime composition: status=%d body=%s", resp.StatusCode, raw)
	}
	var composition map[string]any
	if err := json.Unmarshal(raw, &composition); err != nil {
		t.Fatal(err)
	}
	placements := composition["placements"].([]any)
	if len(placements) != 1 {
		t.Fatalf("expected one runtime placement, got %v", placements)
	}
	return placements[0].(map[string]any)
}

func TestFeatureBuildListReturnsEmptyArray(t *testing.T) {
	env := newTestEnv(t)
	status, body, _ := env.request(http.MethodGet, "/api/v2/admin/feature-builds", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("list feature builds: status=%d body=%v", status, body)
	}
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("feature build items must be an array, got %T (%v)", body["items"], body["items"])
	}
	if len(items) != 0 {
		t.Fatalf("expected empty feature build list, got %d items", len(items))
	}
}

func TestSnapshotPinsDraftEditorialContentAndDeploysIndependently(t *testing.T) {
	env := newTestEnv(t)
	env.content.SetSchemaProvider(env.snapshots)
	key := "snapshot-single"
	manifest := manifestFor(key, "1.0.0", "single", objectSchema(map[string]any{"title": map[string]any{"type": "string", "minLength": 1}}, "title"))
	build := uploadFeatureBuild(t, env, manifest, "export default function Feature() { return 'v1'; }")
	featureID := build["featureId"].(string)
	first := saveAndDeploySnapshot(t, env, singlePageConfig(key, "1.0.0"), "first")
	firstID := first["id"].(string)
	entryID, revision := createAndPublishEntry(t, env, featureID, map[string]any{"title": "Pinned title"})

	second := saveAndDeploySnapshot(t, env, singlePageConfig(key, "1.0.0"), "pinned content")
	secondID := second["id"].(string)
	status, duplicateSnapshot, _ := env.request(http.MethodPost, "/api/v2/admin/site/snapshots", env.adminToken, map[string]any{"name": "same content"})
	if status != http.StatusCreated || duplicateSnapshot["id"] == secondID || duplicateSnapshot["digest"] != second["digest"] || firstID == secondID {
		t.Fatalf("identical content should still create a new snapshot with a matching digest: status=%d body=%v", status, duplicateSnapshot)
	}
	status, listed, _ := env.request(http.MethodGet, "/api/v2/admin/site/snapshots", env.adminToken, nil)
	if status != http.StatusOK || listed["active"].(map[string]any)["dev"] != secondID {
		t.Fatalf("creating a snapshot must not change the active dev version: status=%d body=%v", status, listed)
	}
	duplicateID := duplicateSnapshot["id"].(string)
	status, deployed, _ := env.request(http.MethodPost, "/api/v2/admin/site/snapshots/"+duplicateID+"/deploy", env.adminToken, map[string]any{"environment": "prod"})
	if status != http.StatusOK || deployed["id"] != duplicateID {
		t.Fatalf("directly deploy a snapshot to prod: status=%d body=%v", status, deployed)
	}

	status, currentEntry, _ := env.request(http.MethodGet, "/api/v1/admin/entries/"+entryID, env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("get entry revision after snapshot deploy: status=%d body=%v", status, currentEntry)
	}
	revision = currentEntry["revision"].(float64)
	status, entryDraft, _ := env.request(http.MethodPatch, "/api/v1/admin/entries/"+entryID+"/draft", env.adminToken, map[string]any{"revision": revision, "data": map[string]any{"title": "New draft title"}})
	if status != http.StatusOK {
		t.Fatalf("save content draft: status=%d body=%v", status, entryDraft)
	}
	third := saveAndDeploySnapshot(t, env, singlePageConfig(key, "1.0.0"), "new draft content")
	if third["id"] == secondID || third["digest"] == second["digest"] {
		t.Fatalf("content drafts must be part of a new immutable snapshot: %v", third)
	}
	devPlacement := runtimePlacement(t, env, env.devServer.URL, "/")
	if got := devPlacement["content"].(map[string]any)["title"]; got != "New draft title" {
		t.Fatalf("dev runtime should use the draft content pinned by its active snapshot, got %v", got)
	}
	prodPlacement := runtimePlacement(t, env, env.prodServer.URL, "/")
	if got := prodPlacement["content"].(map[string]any)["title"]; got != "Pinned title" {
		t.Fatalf("prod should retain the content from its independently deployed snapshot, got %v", got)
	}

	status, deployed, _ = env.request(http.MethodPost, "/api/v2/admin/site/snapshots/"+secondID+"/deploy", env.adminToken, map[string]any{"environment": "dev"})
	if status != http.StatusOK || deployed["id"] != secondID {
		t.Fatalf("deploy an older snapshot back to dev: status=%d body=%v", status, deployed)
	}
	devPlacement = runtimePlacement(t, env, env.devServer.URL, "/")
	if got := devPlacement["content"].(map[string]any)["title"]; got != "Pinned title" {
		t.Fatalf("deploying an older snapshot should restore its pinned content, got %v", got)
	}
	var snapshotContentCount int
	if err := env.pool.QueryRow(context.Background(), `SELECT count(*) FROM site_snapshot_content WHERE snapshot_id=$1`, uuid.MustParse(secondID)).Scan(&snapshotContentCount); err != nil {
		t.Fatal(err)
	}
	if snapshotContentCount != 1 {
		t.Fatalf("snapshot must pin its single editorial record, found %d", snapshotContentCount)
	}

	_, err := env.pool.Exec(context.Background(), `UPDATE site_snapshots SET name='tampered' WHERE id=$1`, uuid.MustParse(firstID))
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("snapshot update should be rejected by the database, got %v", err)
	}
	_, err = env.pool.Exec(context.Background(), `UPDATE feature_builds SET digest=repeat('0',64) WHERE id=$1`, uuid.MustParse(build["id"].(string)))
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("feature build update should be rejected by the database, got %v", err)
	}

	_, duplicate, _ := env.rawRequest(http.MethodPost, "/api/v2/admin/feature-builds", env.adminToken,
		bytes.NewReader(featureArtifact(t, manifest, "export default function Feature() { return 'v1'; }")), "application/zip")
	if duplicate["id"] != build["id"] {
		t.Fatalf("re-importing the same digest should be idempotent: %v", duplicate)
	}
	status, changed, _ := env.rawRequest(http.MethodPost, "/api/v2/admin/feature-builds", env.adminToken,
		bytes.NewReader(featureArtifact(t, manifest, "export default function Feature() { return 'changed'; }")), "application/zip")
	if status != http.StatusBadRequest || !strings.Contains(fmt.Sprint(changed), "immutable") {
		t.Fatalf("same feature version with a different digest must be rejected: status=%d body=%v", status, changed)
	}
}

func TestSnapshotPinnedFormSchemaAndPersistentSubmissions(t *testing.T) {
	env := newTestEnv(t)
	key := "snapshot-contact-form"
	formConfig := func(version string) map[string]any {
		return map[string]any{
			"formatVersion": 2,
			"pages": []any{map[string]any{"id": "home", "name": "Home", "kind": "page", "placements": []any{map[string]any{
				"id": "form", "build": map[string]any{"featureKey": key, "version": version}, "slot": "main",
			}}}},
			"routeTree": map[string]any{"id": "root", "type": "index", "pageId": "home"},
		}
	}
	manifestV1 := manifestFor(key, "1.0.0", "form", objectSchema(map[string]any{"name": map[string]any{"type": "string", "minLength": 1}}, "name"))
	buildV1 := uploadFeatureBuild(t, env, manifestV1, "export default function Feature() {}")
	first := saveAndDeploySnapshot(t, env, formConfig("1.0.0"), "form v1")
	env.forms.SetSnapshotSchemaProvider(env.snapshots)

	status, response, _ := env.public(environment.Dev, http.MethodPost, "/api/v2/forms/"+key+"/submissions", map[string]any{"data": map[string]any{"name": "Ada"}})
	if status != http.StatusCreated {
		t.Fatalf("valid form v1 submission: status=%d body=%v", status, response)
	}
	submissionID := response["id"].(string)

	status, allEnvironments, _ := env.request(http.MethodGet, "/api/v1/admin/forms/"+key+"/submissions?environment=all", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("list form submissions across all environments: status=%d body=%v", status, allEnvironments)
	}
	items, ok := allEnvironments["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected the dev submission in the all-environments list: %v", allEnvironments)
	}
	fields, ok := allEnvironments["fields"].([]any)
	if !ok || len(fields) != 1 {
		t.Fatalf("expected submission schema fields in list response: %v", allEnvironments)
	}
	field, _ := fields[0].(map[string]any)
	if field["key"] != "name" || field["label"] != "name" {
		t.Fatalf("unexpected searchable field metadata: %v", fields)
	}
	status, emptyProd, _ := env.request(http.MethodGet, "/api/v1/admin/forms/"+key+"/submissions?environment=prod", env.adminToken, nil)
	prodItems, itemsOK := emptyProd["items"].([]any)
	prodFields, fieldsOK := emptyProd["fields"].([]any)
	if _, _, _, err := env.forms.List(context.Background(), key, forms.ListFilter{Environment: environment.Prod, Limit: 50}); err != nil {
		t.Fatalf("list submissions through service before prod snapshot is published: %v", err)
	}
	if status != http.StatusOK || !itemsOK || !fieldsOK || len(prodItems) != 0 || len(prodFields) != 0 {
		t.Fatalf("list submissions before prod snapshot is published: status=%d body=%v", status, emptyProd)
	}
	status, filtered, _ := env.request(http.MethodGet, "/api/v1/admin/forms/"+key+"/submissions?field=name&q=Ada", env.adminToken, nil)
	filteredItems, _ := filtered["items"].([]any)
	if status != http.StatusOK || len(filteredItems) != 1 {
		t.Fatalf("filter all-environments list by active schema field: status=%d body=%v", status, filtered)
	}

	manifestV2 := manifestFor(key, "1.1.0", "form", objectSchema(map[string]any{"email": map[string]any{"type": "string", "format": "email"}}, "email"))
	buildV2 := uploadFeatureBuild(t, env, manifestV2, "export default function Feature() { return 'v2'; }")
	if buildV1["schemaVersionId"] == buildV2["schemaVersionId"] {
		t.Fatal("each feature build must pin its own content schema version")
	}
	second := saveAndDeploySnapshot(t, env, formConfig("1.1.0"), "form v2")
	status, invalid, _ := env.public(environment.Dev, http.MethodPost, "/api/v2/forms/"+key+"/submissions", map[string]any{"data": map[string]any{"name": "Grace"}})
	if status != http.StatusBadRequest {
		t.Fatalf("dev v2 should validate with its snapshot schema: status=%d body=%v", status, invalid)
	}
	status, valid, _ := env.public(environment.Dev, http.MethodPost, "/api/v2/forms/"+key+"/submissions", map[string]any{"data": map[string]any{"email": "grace@example.com"}})
	if status != http.StatusCreated {
		t.Fatalf("valid form v2 submission: status=%d body=%v", status, valid)
	}

	secondID := second["id"].(string)
	status, promoted, _ := env.request(http.MethodPost, "/api/v2/admin/site/snapshots/"+secondID+"/promote", env.adminToken, map[string]any{})
	if status != http.StatusOK || promoted["id"] != secondID {
		t.Fatalf("promote form v2 snapshot: status=%d body=%v", status, promoted)
	}
	status, oldInvalid, _ := env.public(environment.Prod, http.MethodPost, "/api/v2/forms/"+key+"/submissions", map[string]any{"data": map[string]any{"name": "Grace"}})
	if status != http.StatusBadRequest {
		t.Fatalf("prod should use the same promoted v2 schema: status=%d body=%v", status, oldInvalid)
	}
	status, oldValid, _ := env.public(environment.Prod, http.MethodPost, "/api/v2/forms/"+key+"/submissions", map[string]any{"data": map[string]any{"email": "grace@example.com"}})
	if status != http.StatusCreated {
		t.Fatalf("prod should use the same promoted v2 schema: status=%d body=%v", status, oldValid)
	}
	prodSubmissionID := oldValid["id"].(string)
	status, updatedStatus, _ := env.request(http.MethodPatch, "/api/v1/admin/submissions/"+prodSubmissionID+"/status", env.adminToken, map[string]any{"status": "in_progress"})
	if status != http.StatusOK || updatedStatus["status"] != "in_progress" {
		t.Fatalf("update persistent submission status: status=%d body=%v", status, updatedStatus)
	}
	status, rollback, _ := env.request(http.MethodPost, "/api/v2/admin/site/snapshots/"+first["id"].(string)+"/rollback", env.adminToken, map[string]any{"environment": "prod"})
	if status != http.StatusOK || rollback["id"] != first["id"] {
		t.Fatalf("rollback prod snapshot: status=%d body=%v", status, rollback)
	}
	status, stillValid, _ := env.public(environment.Prod, http.MethodPost, "/api/v2/forms/"+key+"/submissions", map[string]any{"data": map[string]any{"name": "Lin"}})
	if status != http.StatusCreated {
		t.Fatalf("rolled-back prod should use v1 schema: status=%d body=%v", status, stillValid)
	}
	status, persisted, _ := env.request(http.MethodGet, "/api/v1/admin/submissions/"+prodSubmissionID, env.adminToken, nil)
	if status != http.StatusOK || persisted["status"] != "in_progress" {
		t.Fatalf("submission status should survive snapshot rollback: status=%d body=%v", status, persisted)
	}
	status, list, _ := env.request(http.MethodGet, "/api/v1/admin/forms/"+key+"/submissions?environment=prod", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("list persistent form submissions: status=%d body=%v", status, list)
	}
	items, ok = list["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("submissions should persist across snapshot promotion and rollback: %v", list)
	}
	status, allAfterSchemaChange, _ := env.request(http.MethodGet, "/api/v1/admin/forms/"+key+"/submissions", env.adminToken, nil)
	allFields, _ := allAfterSchemaChange["fields"].([]any)
	if status != http.StatusOK || len(allFields) != 2 {
		t.Fatalf("all-environments list should expose fields from both active schemas: status=%d body=%v", status, allAfterSchemaChange)
	}
	if submissionID == "" {
		t.Fatal("missing first form submission id")
	}
}

func TestManifestBackedJSONContentImportIsAtomicAndDraftOnly(t *testing.T) {
	env := newTestEnv(t)
	featureID := env.createFeature("import-products", "Imported products", "collection")
	schema := objectSchema(map[string]any{"title": map[string]any{"type": "string", "minLength": 1}}, "title")
	env.saveAndPublishSchema(featureID, 1, schema)

	status, invalid, _ := env.request(http.MethodPost, "/api/v2/admin/features/"+featureID+"/entries/import", env.adminToken, map[string]any{
		"data": []any{map[string]any{"title": "Valid first row"}, map[string]any{"title": ""}},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("invalid item in import batch should reject whole file: status=%d body=%v", status, invalid)
	}
	status, empty, _ := env.request(http.MethodGet, "/api/v1/admin/features/"+featureID+"/entries", env.adminToken, nil)
	if status != http.StatusOK || len(empty["items"].([]any)) != 0 {
		t.Fatalf("invalid import must not partially create drafts: status=%d body=%v", status, empty)
	}

	status, imported, _ := env.request(http.MethodPost, "/api/v2/admin/features/"+featureID+"/entries/import", env.adminToken, map[string]any{
		"data": []any{map[string]any{"title": "First"}, map[string]any{"title": "Second"}},
	})
	if status != http.StatusCreated || imported["imported"].(float64) != 2 {
		t.Fatalf("valid array import should create two drafts: status=%d body=%v", status, imported)
	}
	status, public, _ := env.request(http.MethodGet, "/api/v1/content/import-products", "", nil)
	if status != http.StatusOK {
		t.Fatalf("read public feature before publishing imported drafts: status=%d body=%v", status, public)
	}
	entries := public["data"].(map[string]any)["entries"].([]any)
	if len(entries) != 0 {
		t.Fatalf("JSON import must not publish its new drafts: %v", public)
	}
}

func TestCollectionRuntimeFiltersAndResolvesDetailRoutes(t *testing.T) {
	env := newTestEnv(t)
	collectionKey := "snapshot-products"
	detailKey := "snapshot-product-detail"
	collectionSchema := objectSchema(map[string]any{
		"slug":     map[string]any{"type": "string", "minLength": 1},
		"category": map[string]any{"type": "string", "minLength": 1},
		"title":    map[string]any{"type": "string", "minLength": 1},
	}, "slug", "category", "title")
	collectionBuild := uploadFeatureBuild(t, env, manifestFor(collectionKey, "1.0.0", "collection", collectionSchema), "export default function Feature() {}")
	detailManifest := manifestFor(detailKey, "1.0.0", "single", objectSchema(map[string]any{"slug": map[string]any{"type": "string"}}, "slug"))
	detailManifest["arguments"] = objectSchema(map[string]any{"slug": map[string]any{"type": "string", "minLength": 1}}, "slug")
	uploadFeatureBuild(t, env, detailManifest, "export default function Feature() {}")

	firstID, _ := createAndPublishEntry(t, env, collectionBuild["featureId"].(string), map[string]any{"slug": "alpha", "category": "feed", "title": "Alpha"})
	secondID, _ := createAndPublishEntry(t, env, collectionBuild["featureId"].(string), map[string]any{"slug": "beta", "category": "feed", "title": "Beta"})
	status, _, _ := env.request(http.MethodPut, "/api/v2/admin/features/"+collectionBuild["featureId"].(string)+"/entries/order", env.adminToken,
		map[string]any{"entryIds": []string{secondID, firstID}})
	if status != http.StatusNoContent {
		t.Fatalf("reorder collection: status=%d", status)
	}

	config := map[string]any{
		"formatVersion": 2,
		"pages": []any{
			map[string]any{"id": "catalog", "name": "Catalog", "kind": "collection-list", "placements": []any{map[string]any{
				"id": "products", "build": map[string]any{"featureKey": collectionKey, "version": "1.0.0"}, "slot": "main",
				"detailLink": map[string]any{"routeId": "product-detail", "field": "slug", "pathTemplate": "/product/{slug}"},
			}}},
			map[string]any{"id": "product", "name": "Product", "kind": "detail", "placements": []any{map[string]any{
				"id": "product-detail", "build": map[string]any{"featureKey": detailKey, "version": "1.0.0"}, "slot": "main",
				"arguments":        map[string]any{"slug": map[string]any{"source": "path", "pathArg": "slug"}},
				"collectionSource": map[string]any{"featureKey": collectionKey, "version": "1.0.0", "field": "slug", "argument": "slug"},
			}}},
		},
		"routeTree": map[string]any{"id": "root", "type": "index", "pageId": "catalog", "children": []any{
			map[string]any{"id": "product", "type": "static", "value": "product", "children": []any{
				map[string]any{"id": "product-detail", "type": "regex", "pattern": `(?<slug>[a-z0-9-]+)`, "pageId": "product"},
			}},
		}},
	}
	snapshot := saveAndDeploySnapshot(t, env, config, "products")
	if _, ok := snapshot["id"].(string); !ok {
		t.Fatalf("snapshot missing id: %v", snapshot)
	}

	status, page, _ := env.public(environment.Dev, http.MethodGet, "/api/v2/content/"+collectionKey+"/records?snapshot="+snapshot["id"].(string)+"&field=category&value=feed&offset=0&limit=1", nil)
	if status != http.StatusOK {
		t.Fatalf("filtered collection query: status=%d body=%v", status, page)
	}
	items := page["items"].([]any)
	if len(items) != 1 || page["total"].(float64) != 2 || items[0].(map[string]any)["entryId"] != secondID {
		t.Fatalf("collection should be filtered, paged and retain manual order: %v", page)
	}

	detail := runtimePlacement(t, env, env.devServer.URL, "/product/beta")
	if detail["arguments"].(map[string]any)["slug"] != "beta" || detail["content"].(map[string]any)["title"] != "Beta" {
		t.Fatalf("named route argument should resolve the matching collection record: %v", detail)
	}
	request, _ := http.NewRequest(http.MethodGet, env.devServer.URL+"/api/v2/runtime?path=/product/missing", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown detail record should return 404, got %d", response.StatusCode)
	}
	if secondID == "" {
		t.Fatal("missing expected collection entry id")
	}
}
