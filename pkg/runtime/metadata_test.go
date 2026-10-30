package runtime

import (
	"strings"
	"testing"
)

func TestRenderShellFromWritesEscapedMetadata(t *testing.T) {
	template := []byte(`<!doctype html><html><head><title>old title</title><script type="module" src="/assets/app.js"></script></head><body><div id="root"></div></body></html>`)
	got, err := RenderShellFrom(template, HeadMetadata{
		Title:         `Vita <Sil>`,
		Description:   `Quality & care`,
		FaviconURL:    `/media/icon?id=1&v=2`,
		OGTitle:       `Share "this"`,
		OGDescription: `A <description>`,
		OGImageURL:    `/media/image`,
	})
	if err != nil {
		t.Fatal(err)
	}
	html := string(got)
	for _, expected := range []string{
		`<title>Vita &lt;Sil&gt;</title>`,
		`<meta name="description" content="Quality &amp; care" />`,
		`<link rel="icon" href="/media/icon?id=1&amp;v=2" />`,
		`<meta property="og:title" content="Share &#34;this&#34;" />`,
		`<meta property="og:description" content="A &lt;description&gt;" />`,
		`<meta property="og:image" content="/media/image" />`,
		`<script type="module" src="/assets/app.js"></script>`,
	} {
		if !strings.Contains(html, expected) {
			t.Errorf("rendered shell does not contain %q:\n%s", expected, html)
		}
	}
	if strings.Contains(html, "old title") || strings.Count(html, "<title>") != 1 {
		t.Fatalf("expected exactly one updated title, got:\n%s", html)
	}
}

func TestRenderShellFromRequiresHead(t *testing.T) {
	if _, err := RenderShellFrom([]byte(`<html><body></body></html>`), HeadMetadata{}); err == nil {
		t.Fatal("expected an error for a template without </head>")
	}
}
