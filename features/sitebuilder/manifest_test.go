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
