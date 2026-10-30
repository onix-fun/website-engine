package sitebuilder

import (
	"strings"
	"testing"
)

func validManifest() []byte {
	return []byte(`{
		"formatVersion":1,
		"key":"product-detail",
		"name":"Product detail",
		"version":"1.2.3",
		"engineApi":"1",
		"entry":"assets/entry.js",
		"styles":["assets/entry.css"],
		"slots":["main"],
		"arguments":{"type":"object","properties":{"slug":{"type":"string","title":"Slug"}},"required":["slug"],"additionalProperties":false},
		"content":{"mode":"single","schema":{"type":"object","properties":{"title":{"type":"string","x-ui":{"widget":"text"}}},"required":["title"],"additionalProperties":false}}
	}`)
}

func TestParseManifestValidatesDeveloperContract(t *testing.T) {
	m, err := ParseManifest(validManifest())
	if err != nil {
		t.Fatal(err)
	}
	if m.Key != "product-detail" || m.Content.Mode != "single" || m.Entry != "assets/entry.js" {
		t.Fatalf("unexpected manifest: %#v", m)
	}
}

func TestParseManifestRejectsUnsafeOrUnknownContract(t *testing.T) {
	cases := []struct {
		name string
		edit func(string) string
	}{
		{"unsafe entry", func(s string) string { return strings.Replace(s, `assets/entry.js`, `../entry.js`, 1) }},
		{"invalid mode", func(s string) string { return strings.Replace(s, `"single"`, `"page"`, 1) }},
		{"unsupported engine api", func(s string) string { return strings.Replace(s, `"engineApi":"1"`, `"engineApi":"9"`, 1) }},
		{"remote schema ref", func(s string) string {
			return strings.Replace(s, `"title":"Slug"`, `"$ref":"https://example.test/schema.json"`, 1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(tc.edit(string(validManifest())))); err == nil {
				t.Fatal("expected invalid manifest to be rejected")
			}
		})
	}
}

func TestParseManifestSupportsCollectionSettingsAndFormSubmissionSchema(t *testing.T) {
	collection := strings.Replace(string(validManifest()), `"single"`, `"collection"`, 1)
	collection = strings.Replace(collection, `"content":{"mode":"collection","schema":`, `"content":{"mode":"collection","settingsSchema":{"type":"object","properties":{"title":{"type":"string"}},"additionalProperties":false},"schema":`, 1)
	if _, err := ParseManifest([]byte(collection)); err != nil {
		t.Fatalf("collection settings schema should be accepted: %v", err)
	}

	form := strings.Replace(string(validManifest()), `"single"`, `"form"`, 1)
	form = strings.Replace(form, `"content":`, `"form":{"submissionSchema":{"type":"object","properties":{"email":{"type":"string","format":"email"}},"required":["email"],"additionalProperties":false}},"content":`, 1)
	if manifest, err := ParseManifest([]byte(form)); err != nil || len(manifest.SubmissionSchema()) == 0 {
		t.Fatalf("form submission schema should be accepted: %v", err)
	}
	legacySubmission := strings.Replace(string(validManifest()), `"single"`, `"form"`, 1)
	legacySubmission = strings.Replace(legacySubmission, `"content":{"mode":"form","schema":`, `"content":{"mode":"form","form":{"submissionSchema":{"type":"object","properties":{"email":{"type":"string","format":"email"}},"required":["email"],"additionalProperties":false}},"schema":`, 1)
	if manifest, err := ParseManifest([]byte(legacySubmission)); err != nil || len(manifest.SubmissionSchema()) == 0 {
		t.Fatalf("manifest from the earlier nested form contract should remain readable: %v", err)
	}

	legacyForm := strings.Replace(string(validManifest()), `"single"`, `"form"`, 1)
	if manifest, err := ParseManifest([]byte(legacyForm)); err != nil || manifest.Form != nil {
		t.Fatalf("legacy form manifest without form.submissionSchema should remain valid: manifest=%#v err=%v", manifest, err)
	}
}

func TestParseManifestRejectsMisplacedCollectionSettingsSchema(t *testing.T) {
	raw := strings.Replace(string(validManifest()), `"content":{"mode":"single","schema":`, `"content":{"mode":"single","settingsSchema":{"type":"object","properties":{}},"schema":`, 1)
	if _, err := ParseManifest([]byte(raw)); err == nil {
		t.Fatal("settingsSchema must be rejected for non-collection features")
	}
}
