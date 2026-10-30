package sitebuilder

import (
	"encoding/json"
	"testing"
)

func TestRouteCapturesNamedArgumentsAndRespectsPriority(t *testing.T) {
	routes, err := CompileRoutes([]Route{
		{ID: "special", Pattern: `^/products/(?<slug>[a-z0-9-]+)$`, PageID: "product"},
		{ID: "fallback", Pattern: `^/products/.*$`, PageID: "not-found"},
	})
	if err != nil {
		t.Fatal(err)
	}
	route, args, matched, err := MatchRoute(routes, "/products/green-feed")
	if err != nil || !matched {
		t.Fatalf("MatchRoute() = (%#v, %#v, %v, %v)", route, args, matched, err)
	}
	if route.ID != "special" || args["slug"] != "green-feed" {
		t.Fatalf("unexpected match: route=%#v args=%#v", route, args)
	}
}

func TestResolveArgumentsAcceptsLiteralAndRouteBindings(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"slug":{"type":"string"},"page":{"type":"integer","default":1},"enabled":{"type":"boolean"}},"required":["slug","page","enabled"],"additionalProperties":false}`)
	resolved, err := ResolveArguments(schema, map[string]ArgumentBinding{
		"slug":    {Source: "route", RouteArg: "slug"},
		"enabled": {Source: "literal", Value: json.RawMessage(`true`)},
	}, map[string]string{"slug": "product-7"})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(resolved, &got); err != nil {
		t.Fatal(err)
	}
	if got["slug"] != "product-7" || got["page"].(float64) != 1 || got["enabled"] != true {
		t.Fatalf("unexpected resolved arguments: %s", resolved)
	}
}

func TestResolveArgumentsRejectsMissingAndInvalidBindings(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"],"additionalProperties":false}`)
	cases := []map[string]ArgumentBinding{
		{},
		{"unknown": {Source: "literal", Value: json.RawMessage(`1`)}},
		{"id": {Source: "route", RouteArg: "missing"}},
		{"id": {Source: "literal", Value: json.RawMessage(`"not an integer"`)}},
	}
	for i, bindings := range cases {
		if _, err := ResolveArguments(schema, bindings, map[string]string{}); err == nil {
			t.Fatalf("case %d: expected binding error", i)
		}
	}
}
