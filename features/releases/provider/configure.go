package provider

import (
	"strings"
)

// extraHosts and allowInsecure are production runtime options, set once at
// process startup from configuration. They extend the built-in allow-list
// with exact hostnames (e.g. a self-hosted Gitea or GitLab) and optionally
// permit plain http:// source URLs (intended for local development only).
var (
	extraHosts    []string
	allowInsecure bool
)

// Configure sets the production runtime options for release sources. The
// built-in allow-list is unchanged; extra hosts must match exactly (no
// wildcards, no substring matching). Entries are normalized to bare
// lowercased hostnames (a port, if present, is stripped, mirroring the
// url.Hostname() comparison used by the providers).
func Configure(extraAllowedHosts []string, allowInsecureSources bool) {
	extraHosts = make([]string, 0, len(extraAllowedHosts))
	for _, h := range extraAllowedHosts {
		if i := strings.LastIndex(h, ":"); i >= 0 && h[i+1:] != "" {
			h = h[:i]
		}
		extraHosts = append(extraHosts, strings.ToLower(strings.TrimSpace(h)))
	}
	allowInsecure = allowInsecureSources
}

// InsecureSourcesAllowed reports whether plain http:// source URLs are
// currently permitted (local development only).
func InsecureSourcesAllowed() bool { return allowInsecure }
