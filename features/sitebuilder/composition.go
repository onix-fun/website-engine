package sitebuilder

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// SiteConfig is the editable page/route composition authored in the admin UI.
type SiteConfig struct {
	FormatVersion int           `json:"formatVersion"`
	Pages         []Page        `json:"pages"`
	RouteTree     RouteTreeNode `json:"routeTree"`
}

// RouteTreeNode is one ordered URL route. A path node defines a complete root
// URL, while its children append individual segments. Any node may resolve a
// page and still have children, so /section and /section/item can share a prefix.
type RouteTreeNode struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Path     string          `json:"path,omitempty"`
	Value    string          `json:"value,omitempty"`
	Name     string          `json:"name,omitempty"`
	Pattern  string          `json:"pattern,omitempty"`
	PageID   string          `json:"pageId,omitempty"`
	Children []RouteTreeNode `json:"children,omitempty"`
}

type Page struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Kind       string      `json:"kind"` // page, collection-list, detail
	Placements []Placement `json:"placements"`
}

type BuildRef struct {
	FeatureKey string `json:"featureKey"`
	Version    string `json:"version"`
}

type Placement struct {
	ID               string                     `json:"id"`
	Build            BuildRef                   `json:"build"`
	Slot             string                     `json:"slot"`
	Arguments        map[string]ArgumentBinding `json:"arguments,omitempty"`
	CollectionSource *CollectionSource          `json:"collectionSource,omitempty"`
	DetailLink       *DetailLink                `json:"detailLink,omitempty"`
}

// CollectionSource configures a detail feature to read one record from a
// collection feature using a route-bound argument as the lookup key.
type CollectionSource struct {
	FeatureKey string `json:"featureKey"`
	Version    string `json:"version"`
	Field      string `json:"field"`
	Argument   string `json:"argument"`
}

// DetailLink tells a collection-list feature how to generate links to the
// detail route for each collection record.
type DetailLink struct {
	RouteID      string `json:"routeId"`
	Field        string `json:"field"`
	PathTemplate string `json:"pathTemplate"`
}

var pathTemplateArgument = regexp.MustCompile(`\{([A-Za-z][A-Za-z0-9_]{0,63})\}`)

// BuildInfo is the registry metadata needed to validate a page placement.
type BuildInfo struct {
	Manifest     Manifest
	Digest       string
	Version      string
	AssetDigests map[string]string
}

type BuildLookup interface {
	Build(key, version string) (BuildInfo, bool)
}

func ParseSiteConfig(raw []byte) (SiteConfig, error) {
	var cfg SiteConfig
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return SiteConfig{}, fmt.Errorf("decode site config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return SiteConfig{}, errors.New("decode site config: trailing JSON value")
		}
		return SiteConfig{}, fmt.Errorf("decode site config: %w", err)
	}
	if err := cfg.Validate(nil); err != nil {
		return SiteConfig{}, err
	}
	return cfg, nil
}

func (cfg SiteConfig) Validate(builds BuildLookup) error {
	if cfg.FormatVersion != 2 && cfg.FormatVersion != 3 {
		return errors.New("formatVersion must be 2 or 3")
	}
	if cfg.FormatVersion == 2 && cfg.RouteTree.Type != "index" {
		return errors.New("formatVersion 2 requires an index route tree root")
	}
	if cfg.FormatVersion == 3 && cfg.RouteTree.Type != "site" {
		return errors.New("formatVersion 3 requires a site route tree root")
	}
	if len(cfg.Pages) == 0 {
		return errors.New("at least one page is required")
	}
	pages := make(map[string]Page, len(cfg.Pages))
	placements := make(map[string]Placement)
	buildVersions := make(map[string]string)
	for _, page := range cfg.Pages {
		if page.ID == "" || page.Name == "" || (page.Kind != "page" && page.Kind != "collection-list" && page.Kind != "detail") {
			return fmt.Errorf("page id, name or kind is invalid")
		}
		if _, exists := pages[page.ID]; exists {
			return fmt.Errorf("duplicate page id %q", page.ID)
		}
		pages[page.ID] = page
		for _, placement := range page.Placements {
			if placement.ID == "" || placement.Build.FeatureKey == "" || placement.Build.Version == "" {
				return fmt.Errorf("page %q has a placement with missing id or build", page.ID)
			}
			if _, exists := placements[placement.ID]; exists {
				return fmt.Errorf("duplicate placement id %q", placement.ID)
			}
			placements[placement.ID] = placement
			if previous, ok := buildVersions[placement.Build.FeatureKey]; ok && previous != placement.Build.Version {
				return fmt.Errorf("snapshot composition mixes versions %s and %s for feature %q", previous, placement.Build.Version, placement.Build.FeatureKey)
			}
			buildVersions[placement.Build.FeatureKey] = placement.Build.Version
			if placement.CollectionSource != nil {
				source := placement.CollectionSource
				if source.FeatureKey == "" || source.Version == "" || source.Field == "" || source.Argument == "" {
					return fmt.Errorf("placement %q has an incomplete collection source", placement.ID)
				}
				if previous, ok := buildVersions[source.FeatureKey]; ok && previous != source.Version {
					return fmt.Errorf("snapshot composition mixes versions %s and %s for feature %q", previous, source.Version, source.FeatureKey)
				}
				buildVersions[source.FeatureKey] = source.Version
			}
		}
	}
	routes, err := CompileRouteTree(cfg.RouteTree)
	if err != nil {
		return err
	}
	routeByID := make(map[string]CompiledRoute, len(routes))
	pageRouteCaptures := make(map[string][]string)
	for _, route := range routes {
		if _, ok := pages[route.PageID]; !ok {
			return fmt.Errorf("route %q references missing page %q", route.ID, route.PageID)
		}
		routeByID[route.ID] = route
		captures := route.CaptureNames()
		if previous, ok := pageRouteCaptures[route.PageID]; ok {
			if !sameStrings(previous, captures) {
				return fmt.Errorf("all routes for page %q must expose the same path parameters", route.PageID)
			}
		} else {
			pageRouteCaptures[route.PageID] = captures
		}
	}
	if builds == nil {
		return nil
	}
	sharedStyleDigests := make(map[string]string)
	for _, page := range cfg.Pages {
		if page.Kind == "detail" && len(pageRouteCaptures[page.ID]) == 0 {
			return fmt.Errorf("detail page %q must have a route capture", page.ID)
		}
		hasCollectionSource := false
		for _, placement := range page.Placements {
			info, ok := builds.Build(placement.Build.FeatureKey, placement.Build.Version)
			if !ok {
				return fmt.Errorf("placement %q references unknown feature build %s@%s", placement.ID, placement.Build.FeatureKey, placement.Build.Version)
			}
			for _, style := range info.Manifest.SharedStyles {
				digest := info.AssetDigests[style]
				if digest == "" {
					return fmt.Errorf("placement %q shared stylesheet %q has no verified asset digest", placement.ID, style)
				}
				if previous, exists := sharedStyleDigests[style]; exists && previous != digest {
					return fmt.Errorf("shared stylesheet %q differs between feature builds; rebuild the snapshot features against the same core", style)
				}
				sharedStyleDigests[style] = digest
			}
			if !allowedSlots[placement.Slot] || !contains(info.Manifest.Slots, placement.Slot) {
				return fmt.Errorf("feature %q does not support slot %q", info.Manifest.Key, placement.Slot)
			}
			availableCaptureNames := pageRouteCaptures[page.ID]
			availableCaptures := make(map[string]bool, len(availableCaptureNames))
			for _, capture := range availableCaptureNames {
				availableCaptures[capture] = true
			}
			for name, binding := range placement.Arguments {
				if schemaPropertyFormat(info.Manifest.Arguments, name) == "media" && binding.Source != "literal" {
					return fmt.Errorf("placement %q must bind media argument %q to a selected file", placement.ID, name)
				}
				if binding.Source == "path" && !availableCaptures[binding.PathArg] {
					return fmt.Errorf("placement %q maps %q to unavailable path parameter %q", placement.ID, name, binding.PathArg)
				}
			}
			placeholderRouteArgs := make(map[string]string, len(availableCaptures))
			for capture := range availableCaptures {
				placeholderRouteArgs[capture] = "x"
			}
			for name, binding := range placement.Arguments {
				if binding.Source == "path" {
					propertyType := schemaPropertyType(info.Manifest.Arguments, name)
					switch propertyType {
					case "integer", "number":
						placeholderRouteArgs[binding.PathArg] = "0"
					case "boolean":
						placeholderRouteArgs[binding.PathArg] = "false"
					default:
						placeholderRouteArgs[binding.PathArg] = "x"
					}
				}
			}
			placeholderQuery := make(map[string][]string)
			for name, binding := range placement.Arguments {
				if binding.Source != "query" {
					continue
				}
				propertyType := schemaPropertyType(info.Manifest.Arguments, name)
				value := "x"
				if propertyType == "integer" || propertyType == "number" {
					value = "0"
				}
				if propertyType == "boolean" {
					value = "false"
				}
				placeholderQuery[binding.QueryKey] = []string{value}
			}
			if _, err := ResolveArguments(info.Manifest.Arguments, placement.Arguments, placeholderRouteArgs, placeholderQuery); err != nil {
				return fmt.Errorf("placement %q arguments: %w", placement.ID, err)
			}
			if placement.CollectionSource != nil {
				hasCollectionSource = true
				if page.Kind != "detail" {
					return fmt.Errorf("placement %q collectionSource is only valid on a detail page", placement.ID)
				}
				source, ok := builds.Build(placement.CollectionSource.FeatureKey, placement.CollectionSource.Version)
				if !ok {
					return fmt.Errorf("placement %q collection source build is unavailable", placement.ID)
				}
				if source.Manifest.Content.Mode != "collection" || !hasSchemaProperty(source.Manifest.Content.Schema, placement.CollectionSource.Field) {
					return fmt.Errorf("placement %q collectionSource must refer to a field on a collection feature", placement.ID)
				}
				if schemaPropertyType(info.Manifest.Arguments, placement.CollectionSource.Argument) == "" {
					return fmt.Errorf("placement %q collectionSource argument %q is not declared by the detail feature", placement.ID, placement.CollectionSource.Argument)
				}
				binding, ok := placement.Arguments[placement.CollectionSource.Argument]
				if !ok || binding.Source != "path" {
					return fmt.Errorf("placement %q must bind collection lookup argument %q from a named route capture", placement.ID, placement.CollectionSource.Argument)
				}
				if !availableCaptures[binding.PathArg] {
					return fmt.Errorf("placement %q collection lookup path parameter %q is unavailable", placement.ID, binding.PathArg)
				}
			}
			if placement.DetailLink != nil {
				route, ok := routeByID[placement.DetailLink.RouteID]
				if !ok || pages[route.PageID].Kind != "detail" {
					return fmt.Errorf("placement %q detailLink must point to a detail route", placement.ID)
				}
				if info.Manifest.Content.Mode != "collection" || !hasSchemaProperty(info.Manifest.Content.Schema, placement.DetailLink.Field) {
					return fmt.Errorf("placement %q detailLink field must exist on its collection feature", placement.ID)
				}
				if !strings.HasPrefix(placement.DetailLink.PathTemplate, "/") || strings.ContainsAny(placement.DetailLink.PathTemplate, "?#") {
					return fmt.Errorf("placement %q detailLink needs an absolute pathTemplate without query or fragment", placement.ID)
				}
				for _, match := range pathTemplateArgument.FindAllStringSubmatch(placement.DetailLink.PathTemplate, -1) {
					if !contains(route.CaptureNames(), match[1]) {
						return fmt.Errorf("placement %q pathTemplate uses route argument %q that is not captured", placement.ID, match[1])
					}
				}
				detailPage := pages[route.PageID]
				compatible := false
				for _, detailPlacement := range detailPage.Placements {
					if detailPlacement.CollectionSource == nil || detailPlacement.CollectionSource.FeatureKey != info.Manifest.Key || detailPlacement.CollectionSource.Field != placement.DetailLink.Field {
						continue
					}
					binding := detailPlacement.Arguments[detailPlacement.CollectionSource.Argument]
					for _, candidateRoute := range routes {
						if candidateRoute.PageID == detailPage.ID && binding.PathArg != "" && contains(candidateRoute.CaptureNames(), binding.PathArg) {
							compatible = true
						}
					}
				}
				if !compatible {
					return fmt.Errorf("placement %q detailLink does not match the selected detail collection source", placement.ID)
				}
			}
		}
		if page.Kind == "detail" && !hasCollectionSource {
			return fmt.Errorf("detail page %q must select a collection source", page.ID)
		}
	}
	return nil
}

func schemaPropertyType(schema json.RawMessage, field string) string {
	var shape struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(schema, &shape)
	return shape.Properties[field].Type
}

func schemaPropertyFormat(schema json.RawMessage, field string) string {
	var spec struct {
		Properties map[string]struct {
			Format string `json:"format"`
		} `json:"properties"`
	}
	if json.Unmarshal(schema, &spec) != nil {
		return ""
	}
	return spec.Properties[field].Format
}

func hasSchemaProperty(schema json.RawMessage, field string) bool {
	var shape struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	_ = json.Unmarshal(schema, &shape)
	_, ok := shape.Properties[field]
	return ok
}

func contains(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}
