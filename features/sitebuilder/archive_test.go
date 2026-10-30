package sitebuilder

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func makeFeatureArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, value := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(0o644)
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestVerifyFeatureArchive(t *testing.T) {
	manifest := strings.Replace(string(validManifest()), `"assets/entry.js"`, `"entry.js"`, 1)
	manifest = strings.Replace(manifest, `"assets/entry.css"`, `"entry.css"`, 1)
	archive, err := VerifyArchive(makeFeatureArchive(t, map[string]string{"manifest.json": manifest, "entry.js": "export default function Feature() {}", "entry.css": ".feature { color: red }"}), 0)
	if err != nil {
		t.Fatal(err)
	}
	if archive.Manifest.Key != "product-detail" || len(archive.Assets) != 2 || len(archive.Digest) != 64 {
		t.Fatalf("unexpected verified archive: %#v", archive)
	}
}

func TestVerifyArchiveRejectsTraversalMissingAssetsAndOversize(t *testing.T) {
	manifest := strings.Replace(string(validManifest()), `"assets/entry.js"`, `"../entry.js"`, 1)
	archive := makeFeatureArchive(t, map[string]string{"manifest.json": manifest, "../entry.js": "bad"})
	if _, err := VerifyArchive(archive, 0); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}

	manifest = strings.Replace(string(validManifest()), `"assets/entry.js"`, `"entry.js"`, 1)
	manifest = strings.Replace(manifest, `"assets/entry.css"`, `"missing.css"`, 1)
	archive = makeFeatureArchive(t, map[string]string{"manifest.json": manifest, "entry.js": "export default 1"})
	if _, err := VerifyArchive(archive, 0); err == nil {
		t.Fatal("expected missing stylesheet to be rejected")
	}
	if _, err := VerifyArchive(archive, 1); err == nil {
		t.Fatal("expected compressed archive size limit to be enforced")
	}
}
