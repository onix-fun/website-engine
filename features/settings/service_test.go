package settings

import "testing"

func TestPublicAPIPointsToSnapshotRuntime(t *testing.T) {
	got := NewService(Config{}).Get().PublicAPI
	if got.RuntimePath != "/api/v2/runtime?path={path}" {
		t.Fatalf("runtime path = %q", got.RuntimePath)
	}
	if got.CollectionPath != "/api/v2/content/{key}/records?field={field}&value={value}&offset={offset}&limit={limit}" {
		t.Fatalf("collection path = %q", got.CollectionPath)
	}
	if got.FormsPath != "/api/v2/forms/{key}/submissions" {
		t.Fatalf("forms path = %q", got.FormsPath)
	}
}
