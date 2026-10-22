// Package https runs the HTTPS listeners: an ACME-managed :443 server with
// SNI routing per content environment and a plain HTTP listener that answers
// ACME HTTP-01 challenges and redirects everything else to HTTPS.
//
// Domain names come from the database (site_environments.domains) and are
// refreshed periodically, so operators manage domains in the admin panel; a
// restart is not required for new certificates.
package https

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"websiteengine/pkg/config"
	"websiteengine/pkg/environment"
)

// DomainEntry is one environment's routed domain names.
type DomainEntry struct {
	Env     environment.Environment
	Domains []string
}

// DomainLister serves the live domain map. The database stays the source of
// truth; the caller adapts the repository into this narrow contract.
type DomainLister interface {
	ListDomains(ctx context.Context) ([]DomainEntry, error)
}

// Router owns the ACME manager, the host→environment routing table and the
// periodic domain refresh.
type Router struct {
	lister   DomainLister
	log      *slog.Logger
	cfg      config.TLSConfig
	mgr      *autocert.Manager
	handlers map[environment.Environment]http.Handler

	mu     sync.RWMutex
	routes map[string]environment.Environment
}

// NewRouter builds the router. The manager defers certificate issuance until
// the first handshake for a configured host. The initial domain load must
// succeed: a DB outage should not silently disable HTTPS routing.
func NewRouter(cfg config.TLSConfig, lister DomainLister, handlers map[environment.Environment]http.Handler, log *slog.Logger) (*Router, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("TLS is not enabled")
	}
	if lister == nil {
		return nil, fmt.Errorf("domain lister is required")
	}
	if len(handlers) == 0 {
		return nil, fmt.Errorf("no public handlers configured for TLS")
	}
	if cfg.CacheDir == "" {
		return nil, fmt.Errorf("TLS_CACHE_DIR must not be empty")
	}
	entries, err := lister.ListDomains(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load domains for TLS: %w", err)
	}
	mgr := &autocert.Manager{
		Cache:      autocert.DirCache(cfg.CacheDir),
		Prompt:     autocert.AcceptTOS,
		HostPolicy: hostPolicy(entries),
	}
	if cfg.Email != "" {
		mgr.Email = cfg.Email
	}
	if cfg.Staging {
		mgr.Client = &acme.Client{
			DirectoryURL: "https://acme-staging-v02.api.letsencrypt.org/directory",
		}
	}
	return &Router{
		lister:   lister,
		log:      log,
		cfg:      cfg,
		mgr:      mgr,
		handlers: handlers,
		routes:   routeTable(entries),
	}, nil
}

// Start runs the periodic domain refresh until ctx is cancelled.
func (r *Router) Start(ctx context.Context) {
	interval := r.cfg.RefreshInterval
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			entries, err := r.lister.ListDomains(ctx)
			if err != nil {
				r.log.Error("TLS domain refresh failed", "error", err)
				continue
			}
			r.mu.Lock()
			r.routes = routeTable(entries)
			r.mu.Unlock()
			// HostPolicy is consulted by the manager on each issuance request,
			// so swapping the matcher keeps the allow-list fresh.
			r.mgr.HostPolicy = hostPolicy(entries)
			r.log.Info("TLS domains refreshed", "count", len(entries))
		}
	}()
}

// ServeHTTP(host) resolves the request host to its environment. Unknown hosts
// are rejected with 421 Misdirected Request: they have no certificate and no
// site assigned to them.
func (r *Router) resolve(env environment.Environment) (http.Handler, bool) {
	r.mu.RLock()
	h, ok := r.handlers[env]
	r.mu.RUnlock()
	return h, ok
}

func (r *Router) environmentForHost(host string) environment.Environment {
	r.mu.RLock()
	env, ok := r.routes[host]
	r.mu.RUnlock()
	if !ok {
		return ""
	}
	return env
}

// HTTPSServer builds the :443 server. Certificates are issued and renewed by
// the ACME manager; unknown SNI names fail the handshake.
func (r *Router) HTTPSServer(addr string) *http.Server {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			host := normalizeHost(hello.ServerName)
			if r.environmentForHost(host) == "" {
				return nil, fmt.Errorf("no TLS route for host %q", host)
			}
			return r.mgr.GetCertificate(hello)
		},
	}
	return &http.Server{
		Addr:              addr,
		Handler:           r,
		TLSConfig:         cfg,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// HTTP01Server builds the plain listener on the well-known HTTP port: it
// answers ACME HTTP-01 challenges and redirects every other request to HTTPS.
func (r *Router) HTTP01Server(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           r.mgr.HTTPHandler(nil),
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	host := normalizeHost(req.Host)
	env := r.environmentForHost(host)
	if env == "" {
		http.Error(w, "Неизвестный домен: домен не привязан к среде сайта", http.StatusMisdirectedRequest)
		return
	}
	handler, ok := r.resolve(env)
	if !ok {
		http.Error(w, "Среда не обслуживается по HTTPS", http.StatusMisdirectedRequest)
		return
	}
	handler.ServeHTTP(w, req)
}

// routeTable maps normalized host names to environments.
func routeTable(entries []DomainEntry) map[string]environment.Environment {
	table := make(map[string]environment.Environment, 16)
	for _, e := range entries {
		for _, d := range e.Domains {
			host := normalizeHost(d)
			if host == "" {
				continue
			}
			if _, exists := table[host]; exists {
				// Both environments share a host: prod wins because it is the
				// public default; uniqueness is enforced on environment edits.
				continue
			}
			table[host] = e.Env
		}
	}
	return table
}

// hostPolicy allows issuance only for the currently routed hosts, keeping the
// ACME allow-list synchronized with the database.
func hostPolicy(entries []DomainEntry) autocert.HostPolicy {
	return func(ctx context.Context, host string) error {
		for _, e := range entries {
			for _, d := range e.Domains {
				if normalizeHost(d) == normalizeHost(host) {
					return nil
				}
			}
		}
		return fmt.Errorf("acme: host %q is not configured", host)
	}
}

func normalizeHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}
