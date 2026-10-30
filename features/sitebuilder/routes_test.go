package sitebuilder

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestRouteCapturesNamedArgumentsAndRespectsPriority(t *testing.T) {
	routes, err := CompileRouteTree(RouteTreeNode{ID: "root", Type: "index", PageID: "home", Children: []RouteTreeNode{
		{ID: "special", Type: "static", Value: "products", Children: []RouteTreeNode{
			{ID: "sku", Type: "regex", Pattern: `(?<slug>[a-z0-9-]+)`, PageID: "product"},
			{ID: "fallback", Type: "param", Name: "fallback", PageID: "not-found"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	route, args, matched, err := MatchRoute(routes, "/products/green-feed")
	if err != nil || !matched {
		t.Fatalf("MatchRoute() = (%#v, %#v, %v, %v)", route, args, matched, err)
	}
	if route.ID != "sku" || args["slug"] != "green-feed" {
		t.Fatalf("unexpected match: route=%#v args=%#v", route, args)
	}
}

func TestResolveArgumentsAcceptsLiteralPathAndQueryBindings(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"slug":{"type":"string"},"page":{"type":"integer","default":1},"enabled":{"type":"boolean"},"tags":{"type":"array","items":{"type":"string"}}},"required":["slug","page","enabled"],"additionalProperties":false}`)
	resolved, err := ResolveArguments(schema, map[string]ArgumentBinding{
		"slug":    {Source: "path", PathArg: "slug"},
		"enabled": {Source: "literal", Value: json.RawMessage(`true`)},
		"tags":    {Source: "query", QueryKey: "tag"},
	}, map[string]string{"slug": "product-7"}, url.Values{"tag": {"feed", "mineral"}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(resolved, &got); err != nil {
		t.Fatal(err)
	}
	if got["slug"] != "product-7" || got["page"].(float64) != 1 || got["enabled"] != true || len(got["tags"].([]any)) != 2 {
		t.Fatalf("unexpected resolved arguments: %s", resolved)
	}
}

func TestResolveArgumentsRejectsMissingAndInvalidBindings(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"],"additionalProperties":false}`)
	cases := []map[string]ArgumentBinding{
		{},
		{"unknown": {Source: "literal", Value: json.RawMessage(`1`)}},
		{"id": {Source: "path", PathArg: "missing"}},
		{"id": {Source: "literal", Value: json.RawMessage(`"not an integer"`)}},
	}
	for i, bindings := range cases {
		if _, err := ResolveArguments(schema, bindings, map[string]string{}, nil); err == nil {
			t.Fatalf("case %d: expected binding error", i)
		}
	}
}

func TestResolveArgumentsRejectsRepeatedScalarQueryAndCoercesTypedArray(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"page":{"type":"integer"},"ids":{"type":"array","items":{"type":"integer"}}},"required":["page","ids"]}`)
	bindings := map[string]ArgumentBinding{
		"page": {Source: "query", QueryKey: "page"},
		"ids":  {Source: "query", QueryKey: "id"},
	}
	if _, err := ResolveArguments(schema, bindings, nil, url.Values{"page": {"1", "2"}, "id": {"3"}}); err == nil {
		t.Fatal("expected repeated scalar query to fail")
	}
	resolved, err := ResolveArguments(schema, bindings, nil, url.Values{"page": {"2"}, "id": {"3", "4"}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(resolved, &got); err != nil {
		t.Fatal(err)
	}
	if got["page"].(float64) != 2 || len(got["ids"].([]any)) != 2 {
		t.Fatalf("unexpected resolved query arguments: %s", resolved)
	}
}

func TestCompileRouteTreeSupportsIntermediateRegexCapturesAndCatchAll(t *testing.T) {
	routes, err := CompileRouteTree(RouteTreeNode{ID: "root", Type: "index", Children: []RouteTreeNode{
		{ID: "category", Type: "regex", Pattern: `(?<category>[a-z]+)`, Children: []RouteTreeNode{
			{ID: "asset", Type: "catchAll", Name: "path", PageID: "asset"},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	route, args, matched, err := MatchRoute(routes, "/images/feed/icons/logo.svg")
	if err != nil || !matched || route.ID != "asset" || args["category"] != "images" || args["path"] != "feed/icons/logo.svg" {
		t.Fatalf("unexpected catch-all match: route=%#v args=%#v matched=%v err=%v", route, args, matched, err)
	}
}

func TestCompileRouteTreeRequiresPagesOnlyAtRouteLeaves(t *testing.T) {
	valid := RouteTreeNode{ID: "root", Type: "index", Children: []RouteTreeNode{
		{ID: "catalog", Type: "static", Value: "catalog", Children: []RouteTreeNode{
			{ID: "item", Type: "param", Name: "slug", PageID: "detail"},
		}},
	}}
	if _, err := CompileRouteTree(valid); err != nil {
		t.Fatalf("valid routed tree was rejected: %v", err)
	}

	branchPage := valid
	branchPage.Children = []RouteTreeNode{{ID: "catalog", Type: "static", Value: "catalog", PageID: "list", Children: []RouteTreeNode{
		{ID: "item", Type: "param", Name: "slug", PageID: "detail"},
	}}}
	if _, err := CompileRouteTree(branchPage); err == nil {
		t.Fatal("expected a mapped intermediate branch to be rejected")
	}

	missingLeafPage := RouteTreeNode{ID: "root", Type: "index", Children: []RouteTreeNode{{ID: "catalog", Type: "static", Value: "catalog"}}}
	if _, err := CompileRouteTree(missingLeafPage); err == nil {
		t.Fatal("expected a route leaf without a page to be rejected")
	}
}
