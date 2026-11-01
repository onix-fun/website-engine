package sitebuilder

import (
	"strings"
	"testing"
)

func TestDefaultDesignSystemIncludesTypedDesktopAndMobileTokens(t *testing.T) {
	value := DefaultDesignSystem()
	normalized, err := NormalizeDesignSystem(value)
	if err != nil {
		t.Fatalf("normalize defaults: %v", err)
	}
	for _, definition := range DesignTokenDefinitions() {
		got := normalized.Tokens[definition.Key]
		if definition.Scope == "mobile" {
			got = normalized.MobileTokens[definition.Key]
		}
		if got != definition.Default {
			t.Errorf("%s = %q, want %q", definition.Key, got, definition.Default)
		}
	}
}

func TestNormalizeDesignSystemRejectsUnknownAndUnsafeValues(t *testing.T) {
	unknown := DefaultDesignSystem()
	unknown.Tokens["--not-a-design-token"] = "1rem"
	if _, err := NormalizeDesignSystem(unknown); err == nil {
		t.Fatal("unknown token was accepted")
	}

	unsafe := DefaultDesignSystem()
	unsafe.Tokens["--color-ink"] = "#fff; color: red"
	if _, err := NormalizeDesignSystem(unsafe); err == nil {
		t.Fatal("CSS declaration injection was accepted")
	}

	unsafe = DefaultDesignSystem()
	unsafe.MobileTokens["--header-height"] = "url(https://example.test/tracker)"
	if _, err := NormalizeDesignSystem(unsafe); err == nil {
		t.Fatal("external CSS resource was accepted")
	}
}

func TestDesignSystemCSSContainsFontRolesAndMobileOverrides(t *testing.T) {
	value := DefaultDesignSystem()
	value.MobileTokens["--header-height"] = "4.5rem"
	css, err := value.CSS("")
	if err != nil {
		t.Fatalf("generate stylesheet: %v", err)
	}
	for _, expected := range []string{
		"--font-body:\"Alumni Sans SC\",sans-serif",
		"--background:var(--color-paper)",
		"@media(max-width:48rem)",
		"--header-height:4.5rem",
	} {
		if !strings.Contains(css, expected) {
			t.Errorf("stylesheet is missing %q", expected)
		}
	}
	if strings.Contains(css, "undefined") {
		t.Fatal("stylesheet contains an undefined value")
	}
}

func TestNormalizeDesignSystemSupportsMultipleFacesForOneFamily(t *testing.T) {
	value := DefaultDesignSystem()
	value.Fonts = []DesignFont{
		{Family: "Editorial Sans", MediaID: "11111111-1111-4111-8111-111111111111", Weight: "100 900", Style: "normal", UnicodeRange: "U+0000-00FF"},
		{Family: "Editorial Sans", MediaID: "22222222-2222-4222-8222-222222222222", Weight: "100 900", Style: "normal", UnicodeRange: "U+0400-04FF"},
	}
	value.FontRoles["body"] = "Editorial Sans"
	if _, err := NormalizeDesignSystem(value); err != nil {
		t.Fatalf("Unicode subsets of one variable face should be allowed: %v", err)
	}
	css, err := value.CSS("")
	if err != nil {
		t.Fatalf("generate font stylesheet: %v", err)
	}
	if strings.Count(css, "unicode-range:") != 2 {
		t.Fatalf("stylesheet should keep both subset declarations: %s", css)
	}

	value.Fonts[1].UnicodeRange = value.Fonts[0].UnicodeRange
	if _, err := NormalizeDesignSystem(value); err == nil {
		t.Fatal("duplicate font face was accepted")
	}

	value.Fonts[1].UnicodeRange = "U+0041; color:red"
	if _, err := NormalizeDesignSystem(value); err == nil {
		t.Fatal("unsafe unicode range was accepted")
	}
}
