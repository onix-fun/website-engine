package runtime

import (
	"bytes"
	"embed"
	"fmt"
	"html"
	"io/fs"
	"regexp"
	"strings"
)

//go:embed dist/index.html
var shellTemplate embed.FS

// HeadMetadata is the small public metadata contract shared with the page
// composer. Values are escaped before being written into the HTML document.
type HeadMetadata struct {
	Title         string `json:"title"`
	Description   string `json:"description,omitempty"`
	FaviconURL    string `json:"faviconUrl,omitempty"`
	OGTitle       string `json:"ogTitle,omitempty"`
	OGDescription string `json:"ogDescription,omitempty"`
	OGImageURL    string `json:"ogImageUrl,omitempty"`
}

var titleTag = regexp.MustCompile(`(?is)\s*<title>.*?</title>`)

func RenderShell(metadata HeadMetadata) ([]byte, error) {
	template, err := fs.ReadFile(shellTemplate, "dist/index.html")
	if err != nil {
		return nil, fmt.Errorf("read public runtime shell: %w", err)
	}
	return RenderShellFrom(template, metadata)
}

func RenderShellFrom(template []byte, metadata HeadMetadata) ([]byte, error) {
	template = titleTag.ReplaceAll(template, nil)
	headEnd := []byte("</head>")
	index := bytes.Index(bytes.ToLower(template), headEnd)
	if index < 0 {
		return nil, fmt.Errorf("public runtime shell has no closing head tag")
	}
	if metadata.Title == "" {
		metadata.Title = "Website"
	}
	if metadata.OGTitle == "" {
		metadata.OGTitle = metadata.Title
	}
	if metadata.OGDescription == "" {
		metadata.OGDescription = metadata.Description
	}
	var tags strings.Builder
	if metadata.Description != "" {
		fmt.Fprintf(&tags, "    <meta name=\"description\" content=\"%s\" />\n", html.EscapeString(metadata.Description))
	}
	if metadata.FaviconURL != "" {
		fmt.Fprintf(&tags, "    <link rel=\"icon\" href=\"%s\" />\n", html.EscapeString(metadata.FaviconURL))
	}
	if metadata.OGTitle != "" {
		fmt.Fprintf(&tags, "    <meta property=\"og:title\" content=\"%s\" />\n", html.EscapeString(metadata.OGTitle))
	}
	if metadata.OGDescription != "" {
		fmt.Fprintf(&tags, "    <meta property=\"og:description\" content=\"%s\" />\n", html.EscapeString(metadata.OGDescription))
	}
	if metadata.OGImageURL != "" {
		fmt.Fprintf(&tags, "    <meta property=\"og:image\" content=\"%s\" />\n", html.EscapeString(metadata.OGImageURL))
	}
	output := make([]byte, 0, len(template)+tags.Len()+128)
	output = append(output, template[:index]...)
	output = append(output, []byte("    <title>"+html.EscapeString(metadata.Title)+"</title>\n")...)
	output = append(output, tags.String()...)
	output = append(output, template[index:]...)
	return output, nil
}
