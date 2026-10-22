package https

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"websiteengine/pkg/config"
	"websiteengine/pkg/environment"
)

// ---- helpers ----

type fakeLister struct {
	entries []DomainEntry
	err     error
}

func (f *fakeLister) ListDomains(context.Context) ([]DomainEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.entries, nil
}

func tlsCfg(enabled bool) config.TLSConfig {
	return config.TLSConfig{
		Enabled:   enabled,
		CacheDir:  filepath.Join(os.TempDir(), "https-test-cache"),
		HTTPAddr:  ":0",
		HTTPSAddr: ":0",
	}
}

func logw() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func fakeRouter(t *testing.T, lister *fakeLister) (*Router, error) {
	t.Helper()
	handlers := map[environment.Environment]http.Handler{
		environment.Dev:  http.NotFoundHandler(),
		environment.Prod: http.NotFoundHandler(),
	}
	cfg := tlsCfg(true)
	cfg.CacheDir = t.TempDir()
	return NewRouter(cfg, lister, handlers, logw())
}

// ---- tests ----

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"example.com":     "example.com",
		"EXAMPLE.COM":     "example.com",
		"example.com:443": "example.com",
		"[::1]:8080":      "::1",
		"example.com.":    "example.com",
		"  example.com  ": "example.com",
	}
	for raw, want := range cases {
		if got := normalizeHost(raw); got != want {
			t.Errorf("normalizeHost(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestRouteTable(t *testing.T) {
	table := routeTable([]DomainEntry{
		{Env: environment.Dev, Domains: []string{"dev.example.com", "DEV2.Example.ORG:8443"}},
		{Env: environment.Prod, Domains: []string{"example.com"}},
		{Env: environment.Prod, Domains: []string{""}}, // blank entries ignored
	})
	if table["dev.example.com"] != environment.Dev {
		t.Errorf("dev domain missing: %v", table)
	}
	if table["dev2.example.org"] != environment.Dev {
		t.Errorf("dev domain normalized incorrectly: %v", table)
	}
	if table["example.com"] != environment.Prod {
		t.Errorf("prod domain missing: %v", table)
	}
	if len(table) != 3 {
		t.Errorf("unexpected table size %d: %v", len(table), table)
	}
}

func TestHostPolicy(t *testing.T) {
	policy := hostPolicy([]DomainEntry{
		{Env: environment.Dev, Domains: []string{"dev.example.com"}},
		{Env: environment.Prod, Domains: []string{"example.com"}},
	})
	if err := policy(context.Background(), "dev.example.com"); err != nil {
		t.Errorf("allowed host rejected: %v", err)
	}
	if err := policy(context.Background(), "EVIL.example.org"); err == nil {
		t.Error("unconfigured host must be rejected")
	}
}

func TestEnvironmentForHost(t *testing.T) {
	fake := &fakeLister{entries: []DomainEntry{
		{Env: environment.Dev, Domains: []string{"dev.local"}},
		{Env: environment.Prod, Domains: []string{"prod.local"}},
	}}
	router, err := fakeRouter(t, fake)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	if got := router.environmentForHost("dev.local"); got != environment.Dev {
		t.Errorf("dev.local routed to %q", got)
	}
	if got := router.environmentForHost("prod.local:443"); got != environment.Prod {
		t.Errorf("prod.local:443 routed to %q", got)
	}
	if got := router.environmentForHost("unknown.local"); got != "" {
		t.Errorf("unknown host routed to %q", got)
	}
}

func TestServeHTTPUnknownHost(t *testing.T) {
	fake := &fakeLister{entries: []DomainEntry{
		{Env: environment.Prod, Domains: []string{"prod.local"}},
	}}
	router, err := fakeRouter(t, fake)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "http://unknown.local/", nil)
	req.Host = "unknown.local"
	router.ServeHTTP(w, req)
	if w.Code != http.StatusMisdirectedRequest {
		t.Fatalf("expected 421 for unknown host, got %d", w.Code)
	}
}

func TestServeHTTPRoutesToEnvironment(t *testing.T) {
	devHit := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	prodHit := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotAcceptable) })
	handlers := map[environment.Environment]http.Handler{
		environment.Dev:  devHit,
		environment.Prod: prodHit,
	}
	fake := &fakeLister{entries: []DomainEntry{
		{Env: environment.Dev, Domains: []string{"dev.local"}},
		{Env: environment.Prod, Domains: []string{"prod.local"}},
	}}
	cfg := tlsCfg(true)
	cfg.CacheDir = t.TempDir()
	router, err := NewRouter(cfg, fake, handlers, logw())
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://dev.local/", nil)
	req.Host = "dev.local"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTeapot {
		t.Fatalf("dev.local must hit the dev handler, got %d", w.Code)
	}

	req, _ = http.NewRequest(http.MethodGet, "http://prod.local/", nil)
	req.Host = "prod.local"
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotAcceptable {
		t.Fatalf("prod.local must hit the prod handler, got %d", w.Code)
	}
}

func TestNewRouterValidation(t *testing.T) {
	entries := &fakeLister{entries: []DomainEntry{{Env: environment.Dev, Domains: []string{"dev.local"}}}}
	handlers := map[environment.Environment]http.Handler{environment.Dev: http.NotFoundHandler()}

	if _, err := NewRouter(tlsCfg(false), entries, handlers, logw()); err == nil {
		t.Error("expected error when TLS disabled")
	}
	if _, err := NewRouter(tlsCfg(true), entries, map[environment.Environment]http.Handler{}, logw()); err == nil {
		t.Error("expected error without handlers")
	}
	broken := &fakeLister{err: errors.New("db down")}
	if _, err := NewRouter(tlsCfg(true), broken, handlers, logw()); err == nil {
		t.Error("expected error when initial domain load fails")
	}
	cfg := tlsCfg(true)
	cfg.CacheDir = t.TempDir()
	if _, err := NewRouter(cfg, entries, handlers, logw()); err != nil {
		t.Errorf("valid configuration rejected: %v", err)
	}
}
