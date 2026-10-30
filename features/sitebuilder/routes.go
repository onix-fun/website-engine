package sitebuilder

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dlclark/regexp2"

	"websiteengine/pkg/platform/validate"
)

const routeMatchTimeout = 25 * time.Millisecond

type Route struct {
	ID      string `json:"id"`
	Pattern string `json:"pattern"`
	PageID  string `json:"pageId"`
}

type CompiledRoute struct {
	Route
	compiled *regexp2.Regexp
	captures []string
}

// CompileRoute validates a PCRE/.NET-compatible route expression. Matching is
// anchored to the complete URL path, while named capture groups become args.
func CompileRoute(route Route) (CompiledRoute, error) {
	if strings.TrimSpace(route.ID) == "" || strings.TrimSpace(route.PageID) == "" {
		return CompiledRoute{}, errors.New("route id and pageId are required")
	}
	if len(route.Pattern) == 0 || len(route.Pattern) > 500 {
		return CompiledRoute{}, errors.New("route pattern must contain between 1 and 500 characters")
	}
	if route.Pattern[0] != '^' {
		return CompiledRoute{}, errors.New("route pattern must start with ^")
	}
	re, err := regexp2.Compile(`\A(?:`+route.Pattern+`)\z`, regexp2.None)
	if err != nil {
		return CompiledRoute{}, fmt.Errorf("invalid route pattern: %w", err)
	}
	re.MatchTimeout = routeMatchTimeout
	var captures []string
	for _, name := range re.GetGroupNames() {
		if _, err := strconv.Atoi(name); err == nil {
			continue
		}
		if !argumentPattern.MatchString(name) {
			return CompiledRoute{}, fmt.Errorf("route capture name %q is invalid", name)
		}
		captures = append(captures, name)
	}
	return CompiledRoute{Route: route, compiled: re, captures: captures}, nil
}

func (r CompiledRoute) CaptureNames() []string { return append([]string(nil), r.captures...) }

func CompileRoutes(routes []Route) ([]CompiledRoute, error) {
	if len(routes) == 0 {
		return nil, errors.New("at least one route is required")
	}
	seenIDs := make(map[string]bool, len(routes))
	compiled := make([]CompiledRoute, 0, len(routes))
	for _, route := range routes {
		if seenIDs[route.ID] {
			return nil, fmt.Errorf("duplicate route id %q", route.ID)
		}
		seenIDs[route.ID] = true
		item, err := CompileRoute(route)
		if err != nil {
			return nil, fmt.Errorf("route %q: %w", route.ID, err)
		}
		compiled = append(compiled, item)
	}
	return compiled, nil
}

func MatchRoute(routes []CompiledRoute, requestPath string) (Route, map[string]string, bool, error) {
	if len(requestPath) > 4096 {
		return Route{}, nil, false, errors.New("request path exceeds 4096 characters")
	}
	for _, route := range routes {
		match, err := route.compiled.FindStringMatch(requestPath)
		if err != nil {
			return Route{}, nil, false, fmt.Errorf("route %q match failed: %w", route.ID, err)
		}
		if match == nil {
			continue
		}
		args := make(map[string]string)
		for _, group := range match.Groups() {
			name := group.Name
			if name == "0" || name == "" || !argumentPattern.MatchString(name) || len(group.Captures) == 0 {
				continue
			}
			args[name] = group.Captures[len(group.Captures)-1].String()
		}
		return route.Route, args, true, nil
	}
	return Route{}, nil, false, nil
}

type ArgumentBinding struct {
	Source   string          `json:"source"` // literal, path or query
	Value    json.RawMessage `json:"value,omitempty"`
	PathArg  string          `json:"pathArg,omitempty"`
	QueryKey string          `json:"queryKey,omitempty"`
}

// ResolveArguments applies per-placement literal/route bindings and validates
// the resulting props against the feature's manifest schema.
func ResolveArguments(schema json.RawMessage, bindings map[string]ArgumentBinding, pathArgs map[string]string, query url.Values) (json.RawMessage, error) {
	var spec struct {
		Properties map[string]struct {
			Type    string          `json:"type"`
			Default json.RawMessage `json:"default"`
			Items   struct {
				Type string `json:"type"`
			} `json:"items"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schema, &spec); err != nil {
		return nil, fmt.Errorf("decode arguments schema: %w", err)
	}
	resolved := make(map[string]json.RawMessage, len(spec.Properties))
	for name, binding := range bindings {
		property, ok := spec.Properties[name]
		if !ok {
			return nil, fmt.Errorf("unknown feature argument %q", name)
		}
		var raw json.RawMessage
		switch binding.Source {
		case "literal":
			if len(binding.Value) == 0 || binding.PathArg != "" || binding.QueryKey != "" {
				return nil, fmt.Errorf("argument %q must have exactly one literal value", name)
			}
			raw = binding.Value
		case "path":
			if binding.PathArg == "" || len(binding.Value) != 0 || binding.QueryKey != "" {
				return nil, fmt.Errorf("argument %q must have exactly one path parameter", name)
			}
			value, ok := pathArgs[binding.PathArg]
			if !ok {
				return nil, fmt.Errorf("path parameter %q is unavailable", binding.PathArg)
			}
			var err error
			raw, err = coerceRouteValue(property.Type, value)
			if err != nil {
				return nil, fmt.Errorf("path parameter %q cannot populate %s: %w", binding.PathArg, name, err)
			}
		case "query":
			if strings.TrimSpace(binding.QueryKey) == "" || len(binding.Value) != 0 || binding.PathArg != "" {
				return nil, fmt.Errorf("argument %q must have exactly one query key", name)
			}
			values, ok := query[binding.QueryKey]
			if !ok || len(values) == 0 {
				continue
			}
			if property.Type != "array" && len(values) > 1 {
				return nil, fmt.Errorf("query key %q was repeated for non-array argument %q", binding.QueryKey, name)
			}
			if property.Type == "array" {
				items := make([]json.RawMessage, 0, len(values))
				for _, value := range values {
					item, err := coerceRouteValue(property.Items.Type, value)
					if err != nil {
						return nil, fmt.Errorf("query key %q cannot populate %s: %w", binding.QueryKey, name, err)
					}
					items = append(items, item)
				}
				encoded, err := json.Marshal(items)
				if err != nil {
					return nil, err
				}
				raw = encoded
			} else {
				var err error
				raw, err = coerceRouteValue(property.Type, values[0])
				if err != nil {
					return nil, fmt.Errorf("query key %q cannot populate %s: %w", binding.QueryKey, name, err)
				}
			}
		default:
			return nil, fmt.Errorf("argument %q source must be literal, path or query", name)
		}
		resolved[name] = raw
	}
	for name, property := range spec.Properties {
		if _, ok := resolved[name]; !ok && len(property.Default) > 0 {
			resolved[name] = property.Default
		}
	}
	for _, name := range spec.Required {
		if _, ok := resolved[name]; !ok {
			return nil, fmt.Errorf("required feature argument %q is not configured", name)
		}
	}
	out, err := json.Marshal(resolved)
	if err != nil {
		return nil, err
	}
	fieldErrors, err := validate.Validate(schema, out)
	if err != nil {
		return nil, err
	}
	if len(fieldErrors) > 0 {
		return nil, fmt.Errorf("feature arguments are invalid: %s", fieldErrors[0].Message)
	}
	return out, nil
}

// CompileRouteTree turns an ordered route tree into anchored segment-regex
// routes. Depth-first order preserves sibling priority throughout the tree.
func CompileRouteTree(root RouteTreeNode) ([]CompiledRoute, error) {
	if root.Type != "index" {
		return nil, errors.New("route tree root must be an index node")
	}
	seen := map[string]bool{}
	var routes []Route
	var walk func(RouteTreeNode, string, map[string]bool, bool) error
	walk = func(node RouteTreeNode, prefix string, inherited map[string]bool, rootNode bool) error {
		if strings.TrimSpace(node.ID) == "" || seen[node.ID] {
			return fmt.Errorf("route node id %q is missing or duplicated", node.ID)
		}
		seen[node.ID] = true
		pattern := prefix
		captures := cloneSet(inherited)
		switch node.Type {
		case "index":
			if !rootNode || prefix != "" || node.Value != "" || node.Name != "" || node.Pattern != "" {
				return fmt.Errorf("index node %q is only allowed as the root", node.ID)
			}
			pattern = ""
		case "static":
			if rootNode || node.Value == "" || strings.ContainsAny(node.Value, "/?#") || node.Name != "" || node.Pattern != "" {
				return fmt.Errorf("static node %q must contain one URL segment", node.ID)
			}
			pattern += "/" + regexp.QuoteMeta(node.Value)
		case "param":
			if rootNode || !argumentPattern.MatchString(node.Name) || node.Value != "" || node.Pattern != "" || captures[node.Name] {
				return fmt.Errorf("parameter node %q has an invalid or duplicate name", node.ID)
			}
			captures[node.Name] = true
			pattern += "/(?<" + node.Name + ">[^/]+)"
		case "regex":
			if rootNode || node.Pattern == "" || strings.Contains(node.Pattern, "/") || node.Value != "" || node.Name != "" {
				return fmt.Errorf("regex node %q must match one URL segment", node.ID)
			}
			segment, err := regexp2.Compile("\\A(?:"+node.Pattern+")\\z", regexp2.None)
			if err != nil {
				return fmt.Errorf("regex node %q: %w", node.ID, err)
			}
			segment.MatchTimeout = routeMatchTimeout
			for _, name := range segment.GetGroupNames() {
				if _, err := strconv.Atoi(name); err == nil {
					continue
				}
				if !argumentPattern.MatchString(name) || captures[name] {
					return fmt.Errorf("regex node %q has invalid or duplicate capture %q", node.ID, name)
				}
				captures[name] = true
			}
			pattern += "/(?:" + node.Pattern + ")"
		case "catchAll":
			if rootNode || !argumentPattern.MatchString(node.Name) || node.Value != "" || node.Pattern != "" || captures[node.Name] || len(node.Children) != 0 {
				return fmt.Errorf("catch-all node %q must be terminal and have a unique name", node.ID)
			}
			captures[node.Name] = true
			pattern += "/(?<" + node.Name + ">.+)"
		default:
			return fmt.Errorf("route node %q has unknown type %q", node.ID, node.Type)
		}
		if node.PageID != "" {
			if !rootNode && len(node.Children) > 0 {
				return fmt.Errorf("branch node %q cannot reference a page; only route leaves may reference pages", node.ID)
			}
			matchPattern := pattern
			if matchPattern == "" {
				matchPattern = "/"
			}
			routes = append(routes, Route{ID: node.ID, Pattern: "^" + matchPattern + "$", PageID: node.PageID})
		} else if len(node.Children) == 0 {
			return fmt.Errorf("route leaf %q must reference a page", node.ID)
		}
		for _, child := range node.Children {
			if err := walk(child, pattern, captures, false); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, "", map[string]bool{}, true); err != nil {
		return nil, err
	}
	if len(routes) == 0 {
		return nil, errors.New("route tree must contain at least one page")
	}
	return CompileRoutes(routes)
}

func cloneSet(input map[string]bool) map[string]bool {
	output := make(map[string]bool, len(input)+1)
	for key, value := range input {
		output[key] = value
	}
	return output
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa, bb := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func coerceRouteValue(targetType, value string) (json.RawMessage, error) {
	switch targetType {
	case "integer":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(strconv.FormatInt(n, 10)), nil
	case "number":
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(strconv.FormatFloat(n, 'f', -1, 64)), nil
	case "boolean":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, err
		}
		return json.RawMessage(strconv.FormatBool(b)), nil
	case "string", "":
		return json.Marshal(value)
	default:
		return nil, fmt.Errorf("unsupported route argument type %q", targetType)
	}
}
