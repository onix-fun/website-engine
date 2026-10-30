package integration

import (
	"net/http"
	"testing"
)

func TestContentCatalogWithoutDevSnapshotReturnsEmptyList(t *testing.T) {
	env := newTestEnv(t)
	status, body, _ := env.request(http.MethodGet, "/api/v2/admin/content/features", env.adminToken, nil)
	if status != http.StatusOK {
		t.Fatalf("content catalog without a dev snapshot: status=%d body=%v", status, body)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 0 {
		t.Fatalf("expected an empty content catalog, got %v", body["items"])
	}
}
