package releases

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"websiteengine/pkg/environment"
)

// ErrRevisionConflict is the sentinel for optimistic-revision failures on
// site environment edits.
var ErrRevisionConflict = errors.New("release revision conflict")

// ErrValidation marks malformed environment settings.
var ErrValidation = errors.New("release validation")

// ValidationError rejects malformed environment settings with a user-facing
// reason.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func (e *ValidationError) Unwrap() error { return ErrValidation }

// envConflict carries the revision the caller must retry against.
type envConflict struct{ current int64 }

func (e *envConflict) Error() string {
	return fmt.Sprintf("environment was updated concurrently (revision %d)", e.current)
}

func (e *envConflict) Unwrap() error { return ErrRevisionConflict }

// CurrentRevision reports the revision the caller must retry against.
func CurrentRevision(err error) (int64, bool) {
	var conflict *envConflict
	if errors.As(err, &conflict) {
		return conflict.current, true
	}
	return 0, false
}

// EnvironmentInput is the editable part of a site environment. The HTTP port
// is intentionally out of scope: ports change only through configuration and
// a server restart.
type EnvironmentInput struct {
	Name          *string
	PublicBaseURL *string
	Domains       *[]string
	CORSOrigins   *[]string
	TLSEnabled    *bool
	Revision      int64
}

var domainLimit = 32

// UpdateEnvironment stores the editable environment settings and returns the
// fresh state. Values are validated: domains are bare host names, CORS
// origins are absolute http(s) URLs or "*", and the public base URL must be
// an absolute URL.
func (s *Service) UpdateEnvironment(ctx context.Context, env environment.Environment, in EnvironmentInput) (EnvironmentState, error) {
	if !env.Valid() {
		return EnvironmentState{}, errors.New("environment must be dev or prod")
	}
	name := ""
	if in.Name != nil {
		name = sanitizeName(*in.Name)
		if name == "" {
			return EnvironmentState{}, &ValidationError{Message: "name must be 1-200 characters"}
		}
	}
	base := ""
	if in.PublicBaseURL != nil {
		trimmed := strings.TrimSpace(*in.PublicBaseURL)
		if trimmed == "" {
			base = ""
		} else {
			u, err := url.Parse(trimmed)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
				return EnvironmentState{}, &ValidationError{Message: "public_base_url must be an absolute http(s) URL"}
			}
			base = strings.TrimRight(u.Scheme+"://"+u.Host, "/") + u.Path
		}
	}
	domains := make([]string, 0, domainLimit)
	if in.Domains != nil {
		for _, d := range *in.Domains {
			d = sanitizeDomain(d)
			if d == "" {
				continue
			}
			if len(d) > 253 {
				return EnvironmentState{}, &ValidationError{Message: fmt.Sprintf("domain %q is too long", d)}
			}
			if len(domains) >= domainLimit {
				return EnvironmentState{}, &ValidationError{Message: fmt.Sprintf("at most %d domains are allowed", domainLimit)}
			}
			if !slices.Contains(domains, d) {
				domains = append(domains, d)
			}
		}
	}
	origins := make([]string, 0, domainLimit)
	if in.CORSOrigins != nil {
		for _, o := range *in.CORSOrigins {
			o = strings.TrimRight(strings.TrimSpace(o), "/")
			if o == "" {
				continue
			}
			if o != "*" {
				u, err := url.Parse(o)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					return EnvironmentState{}, &ValidationError{Message: fmt.Sprintf("cors origin %q must be an absolute http(s) URL", o)}
				}
				o = strings.TrimRight(u.Scheme+"://"+u.Host, "/") + strings.TrimSuffix(u.Path, "/")
			}
			if len(origins) >= domainLimit {
				return EnvironmentState{}, &ValidationError{Message: fmt.Sprintf("at most %d CORS origins are allowed", domainLimit)}
			}
			if !slices.Contains(origins, o) {
				origins = append(origins, o)
			}
		}
	}

	// Domains must be unique across environments; otherwise HTTPS routing
	// would be ambiguous (two sites for one host).
	if err := s.failIfDuplicateDomains(ctx, env, domains); err != nil {
		return EnvironmentState{}, err
	}

	updated, err := s.repo.UpdateEnvironment(ctx, env, name, base, domains, origins, in.TLSEnabled, in.Revision)
	if err != nil {
		return EnvironmentState{}, err
	}
	// The CORS cache must not serve stale origins after an update.
	s.cors.Invalidate()
	return updated, nil
}

// failIfDuplicateDomains rejects hosts already routed to another environment.
func (s *Service) failIfDuplicateDomains(ctx context.Context, env environment.Environment, domains []string) error {
	if len(domains) == 0 {
		return nil
	}
	states, err := s.repo.Environments(ctx)
	if err != nil {
		return err
	}
	for _, st := range states {
		if st.Key == env {
			continue
		}
		for _, existing := range st.Domains {
			for _, candidate := range domains {
				if strings.EqualFold(existing, candidate) {
					return &ValidationError{
						Message: fmt.Sprintf("domain %q already belongs to the %s environment", candidate, st.Key),
					}
				}
			}
		}
	}
	return nil
}

func sanitizeName(raw string) string {
	name := strings.TrimSpace(raw)
	name = strings.Join(strings.Fields(name), " ")
	if runes := []rune(name); len(runes) > 200 {
		name = string(runes[:200])
	}
	return name
}

// sanitizeDomain lowercases a bare host name and rejects schemes, paths,
// ports and wildcard labels to keep the SNI/host routing unambiguous.
func sanitizeDomain(raw string) string {
	d := strings.TrimSpace(raw)
	d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
	d = strings.TrimSuffix(d, "/")
	if strings.Contains(d, "/") {
		return ""
	}
	if i := strings.LastIndex(d, ":"); i > strings.LastIndex(d, "]") {
		d = d[:i]
	}
	d = strings.ToLower(d)
	if d == "" || strings.ContainsAny(d, " *") {
		return ""
	}
	return d
}

// CorsSource serves the per-environment public CORS allow-list from
// site_environments with a short-lived cache. Database outages keep the last
// known values instead of silently dropping every origin.
type CorsSource struct {
	repo      *Repository
	ttl       time.Duration
	mu        sync.Mutex
	cache     map[environment.Environment][]string
	fetchedAt time.Time
}

// NewCorsSource builds the resolver used by DynamicCORS middleware.
func NewCorsSource(repo *Repository) *CorsSource {
	return &CorsSource{repo: repo, ttl: 10 * time.Second, cache: map[environment.Environment][]string{}}
}

// Origins returns the allow-list for the environment. Cache hits avoid a
// database round-trip per request.
func (s *CorsSource) Origins(ctx context.Context, env environment.Environment) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fetchedAt.IsZero() || time.Since(s.fetchedAt) >= s.ttl {
		states, err := s.repo.Environments(ctx)
		if err == nil {
			next := make(map[environment.Environment][]string, len(states))
			for _, st := range states {
				origins := st.CORSOrigins
				if origins == nil {
					origins = []string{}
				}
				next[st.Key] = origins
			}
			s.cache = next
			s.fetchedAt = time.Now()
		}
	}
	return slices.Clone(s.cache[env])
}

// Invalidate drops the cached allow-list so the next Origins call refetches.
func (s *CorsSource) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fetchedAt = time.Time{}
}

// OriginsMap exposes the cached snapshot for diagnostics and tests.
func (s *CorsSource) OriginsMap() map[environment.Environment][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.cache)
}
