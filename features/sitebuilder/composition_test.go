package sitebuilder

import "testing"

func TestParseSiteConfigAcceptsRouteForestAndSharedPageAliases(t *testing.T) {
	cfg, err := ParseSiteConfig([]byte(`{
		"formatVersion":3,
		"pages":[
			{"id":"home","name":"Home","kind":"page","placements":[]},
			{"id":"detail","name":"Detail","kind":"detail","placements":[]}
		],
		"routeTree":{"id":"site","type":"site","children":[
			{"id":"home-route","type":"path","path":"/","pageId":"home"},
			{"id":"product-route","type":"path","path":"/products/:slug","pageId":"detail"},
			{"id":"item-alias","type":"path","path":"/item/~(?<slug>[a-z0-9-]+)","pageId":"detail"}
		]}
	}`))
	if err != nil {
		t.Fatalf("route forest with compatible aliases was rejected: %v", err)
	}
	if cfg.FormatVersion != 3 || cfg.RouteTree.Type != "site" || len(cfg.RouteTree.Children) != 3 {
		t.Fatalf("unexpected parsed site config: %#v", cfg)
	}
}

func TestParseSiteConfigRejectsIncompatibleAliasesButKeepsV2(t *testing.T) {
	invalid := []byte(`{
		"formatVersion":3,
		"pages":[{"id":"detail","name":"Detail","kind":"detail","placements":[]}],
		"routeTree":{"id":"site","type":"site","children":[
			{"id":"by-slug","type":"path","path":"/products/:slug","pageId":"detail"},
			{"id":"by-id","type":"path","path":"/items/:id","pageId":"detail"}
		]}
	}`)
	if _, err := ParseSiteConfig(invalid); err == nil {
		t.Fatal("expected aliases with different path argument names to be rejected")
	}

	legacy := []byte(`{"formatVersion":2,"pages":[{"id":"home","name":"Home","kind":"page","placements":[]}],"routeTree":{"id":"root","type":"index","pageId":"home"}}`)
	if _, err := ParseSiteConfig(legacy); err != nil {
		t.Fatalf("legacy v2 configuration should remain supported: %v", err)
	}
}
