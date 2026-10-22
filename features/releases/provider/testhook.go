package provider

import "net/http"

// testClient and testAllowedHosts exist only for unit tests: package-level
// hooks that a fake httptest server can use. Production never sets them;
// NewGitHub/NewGitLab/NewGitea and isAllowedHost keep their strict defaults.
// Production-variant of the host list / insecure flag is configured via
// Configure (configure.go) and carries no default.
var (
	testClient       *http.Client
	testAllowedHosts []string
	testInsecureOK   bool
	testAPIBase      string
)

// UseTestClient installs a custom HTTP client (typically with a trust-on-first
// use TLS config for httptest.NewTLSServer), extends the host allow-list,
// optionally permits http and redirects GitHub API calls to a fake base
// (without a path). The returned func restores the production behaviour.
func UseTestClient(c *http.Client, extraAllowedHosts []string, allowInsecure bool, apiBase string) (restore func()) {
	prevClient, prevHosts, prevInsecure, prevBase := testClient, testAllowedHosts, testInsecureOK, testAPIBase
	testClient, testAllowedHosts, testInsecureOK, testAPIBase = c, extraAllowedHosts, allowInsecure, apiBase
	return func() {
		testClient, testAllowedHosts, testInsecureOK, testAPIBase = prevClient, prevHosts, prevInsecure, prevBase
	}
}

// testHTTPClient returns the constructor client.
func testHTTPClient() *http.Client {
	if testClient != nil {
		return testClient
	}
	return nil // caller falls back to its own default
}

// allowedHost extends the production allow-list during tests.
func allowedHost(host string) bool {
	if isAllowedHost(host) {
		return true
	}
	for _, h := range extraHosts {
		if h == host {
			return true
		}
	}
	for _, h := range testAllowedHosts {
		if h == host {
			return true
		}
	}
	return false
}

// schemeOK accepts only https in production; the insecure option (and tests)
// may allow http.
func schemeOK(scheme string) bool {
	if scheme == "https" {
		return true
	}
	return scheme == "http" && (allowInsecure || testInsecureOK)
}
