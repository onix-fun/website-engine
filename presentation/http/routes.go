// Package httptransport wires HTTP routes and their access policies.
package httptransport

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"websiteengine/features/auth"
	"websiteengine/pkg/environment"
	"websiteengine/pkg/response"
	"websiteengine/pkg/site"
	"websiteengine/pkg/static"
	hauth "websiteengine/presentation/http/handlers/auth"
	hcontent "websiteengine/presentation/http/handlers/content"
	hfeatures "websiteengine/presentation/http/handlers/features"
	hforms "websiteengine/presentation/http/handlers/forms"
	hmedia "websiteengine/presentation/http/handlers/media"
	hoverview "websiteengine/presentation/http/handlers/overview"
	hreleases "websiteengine/presentation/http/handlers/releases"
	"websiteengine/presentation/http/middleware"
)

type Dependencies struct {
	DB interface {
		QueryRow(context.Context, string, ...interface{}) interface{ Scan(...interface{}) error }
	}
	Log               *slog.Logger
	AuthService       *auth.Service
	Auth              *hauth.Handler
	Features          *hfeatures.Handler
	Content           *hcontent.Handler
	Forms             *hforms.Handler
	Media             *hmedia.Handler
	Overview          *hoverview.Handler
	Releases          *hreleases.Handler
	Site              *site.Manager
	DevPublicBaseURL  string
	ProdPublicBaseURL string
	Health            func(ctx context.Context) error
	MaxJSONBytes      int64
	FormRateLimit     int
	FormRateWindow    time.Duration
	TrustProxy        bool
	PublicCORSOrigins []string
	PublicCORSByEnv   map[environment.Environment][]string
}

func NewRouter(d Deps) http.Handler {
	if d.FormRateWindow <= 0 {
		d.FormRateWindow = time.Minute
	}
	mux := http.NewServeMux()
	authMW := middleware.Authenticate(d.AuthService, d.Log)
	user := func(h http.HandlerFunc) http.Handler {
		return authMW(middleware.RequireUser(http.HandlerFunc(h)))
	}
	staff := func(h http.HandlerFunc) http.Handler {
		return authMW(middleware.RequireStaff(http.HandlerFunc(h)))
	}
	admin := func(h http.HandlerFunc) http.Handler {
		return authMW(middleware.RequireAdmin(http.HandlerFunc(h)))
	}
	jsonLimit := middleware.BodyLimit(d.MaxJSONBytes)
	jsonUser := func(h http.HandlerFunc) http.Handler {
		return jsonLimit(user(h))
	}
	jsonStaff := func(h http.HandlerFunc) http.Handler {
		return jsonLimit(staff(h))
	}

	// Service health.
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if d.Health != nil {
			if err := d.Health(ctx); err != nil {
				d.Log.Warn("readiness check failed", "request_id", response.RequestID(r.Context()), "error", err)
				response.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
				return
			}
		}
		response.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	// Current token and token management.
	mux.Handle("GET /api/v1/admin/me", authMW(http.HandlerFunc(d.Auth.Me)))
	mux.Handle("GET /api/v1/admin/tokens", staff(d.Auth.ListTokens))
	mux.Handle("POST /api/v1/admin/tokens", jsonStaff(d.Auth.CreateToken))
	mux.Handle("GET /api/v1/admin/tokens/{id}", staff(d.Auth.GetToken))
	mux.Handle("DELETE /api/v1/admin/tokens/{id}", staff(d.Auth.RevokeToken))

	// Operational summary for the admin dashboard.
	mux.Handle("GET /api/v1/admin/overview", user(d.Overview.Get))

	// Feature metadata and schema publication.
	mux.Handle("GET /api/v1/admin/features", user(d.Features.List))
	mux.Handle("POST /api/v1/admin/features", jsonStaff(d.Features.Create))
	mux.Handle("GET /api/v1/admin/features/{id}", user(d.Features.Get))
	mux.Handle("PATCH /api/v1/admin/features/{id}", jsonStaff(d.Features.Update))
	mux.Handle("POST /api/v1/admin/features/{id}/archive", staff(d.Features.Archive))
	mux.Handle("GET /api/v1/admin/features/{id}/versions", user(d.Features.Versions))
	mux.Handle("GET /api/v1/admin/features/{id}/draft", user(d.Features.Draft))
	mux.Handle("PUT /api/v1/admin/features/{id}/draft", jsonStaff(d.Features.SaveDraft))
	mux.Handle("POST /api/v1/admin/features/{id}/publish", jsonStaff(d.Features.Publish))
	mux.Handle("POST /api/v1/admin/features/{id}/promote", jsonStaff(d.Features.Promote))
	mux.Handle("POST /api/v1/admin/features/{id}/rollback", jsonStaff(d.Features.Rollback))

	// Content entries.
	mux.Handle("GET /api/v1/admin/features/{id}/entries", user(d.Content.ListEntries))
	mux.Handle("POST /api/v1/admin/features/{id}/entries", jsonUser(d.Content.Create))
	mux.Handle("GET /api/v1/admin/entries/{id}", user(d.Content.Get))
	mux.Handle("PATCH /api/v1/admin/entries/{id}/draft", jsonUser(d.Content.SaveDraft))
	mux.Handle("POST /api/v1/admin/entries/{id}/publish", jsonUser(d.Content.Publish))
	mux.Handle("POST /api/v1/admin/entries/{id}/promote", jsonStaff(d.Content.Promote))
	mux.Handle("POST /api/v1/admin/entries/{id}/rollback", jsonStaff(d.Content.Rollback))
	mux.Handle("POST /api/v1/admin/entries/{id}/unpublish", jsonStaff(d.Content.Unpublish))
	mux.Handle("POST /api/v1/admin/entries/{id}/discard-draft", user(d.Content.DiscardDraft))
	mux.Handle("POST /api/v1/admin/entries/{id}/archive", staff(d.Content.Archive))
	mux.Handle("GET /api/v1/admin/entries/{id}/versions", user(d.Content.Versions))

	// Site releases and environments.
	if d.Releases != nil {
		mux.Handle("GET /api/v1/admin/releases", user(d.Releases.List))
		mux.Handle("POST /api/v1/admin/releases", staff(d.Releases.Import))
		mux.Handle("POST /api/v1/admin/releases/{id}/activate", jsonUser(d.Releases.Activate))
		mux.Handle("POST /api/v1/admin/releases/{id}/rollback", jsonUser(d.Releases.Rollback))
		mux.Handle("GET /api/v1/admin/site/environments", user(d.Releases.Environments))
		mux.Handle("GET /api/v1/admin/release-sources", staff(d.Releases.Sources))
		mux.Handle("POST /api/v1/admin/release-sources", jsonStaff(d.Releases.CreateSource))
		mux.Handle("DELETE /api/v1/admin/release-sources/{id}", staff(d.Releases.DeleteSource))
		mux.Handle("GET /api/v1/admin/release-sources/{id}/releases", staff(d.Releases.ListReleasesForSource))
		mux.Handle("POST /api/v1/admin/release-sources/{id}/sync", jsonStaff(d.Releases.SyncSource))
	}

	// Form submissions.
	mux.Handle("GET /api/v1/admin/forms/{key}/submissions", user(d.Forms.List))
	mux.Handle("GET /api/v1/admin/forms/{key}/submissions/export", user(d.Forms.Export))
	mux.Handle("GET /api/v1/admin/submissions/{id}", user(d.Forms.Get))
	mux.Handle("PATCH /api/v1/admin/submissions/{id}/status", jsonStaff(d.Forms.UpdateStatus))
	mux.Handle("POST /api/v1/admin/submissions/{id}/archive", staff(d.Forms.Archive))
	mux.Handle("DELETE /api/v1/admin/submissions/{id}", admin(d.Forms.Delete))

	// Media administration and public redirect.
	mux.Handle("POST /api/v1/admin/media", user(d.Media.Upload))
	mux.Handle("GET /api/v1/admin/media", user(d.Media.List))
	mux.Handle("GET /api/v1/admin/media/{id}", user(d.Media.Get))
	mux.Handle("PATCH /api/v1/admin/media/{id}", jsonUser(d.Media.Update))
	mux.Handle("GET /api/v1/admin/media/{id}/usage", user(d.Media.Usage))
	mux.Handle("POST /api/v1/admin/media/{id}/retry", staff(d.Media.Retry))
	mux.Handle("DELETE /api/v1/admin/media/{id}", staff(d.Media.Delete))
	mux.HandleFunc("GET /media/{key}", d.Media.Redirect)

	// Public content and forms. CORS is opt-in per environment; the legacy
	// PUBLIC_CORS_ORIGINS value applies to both environments when set.
	corsByEnv := d.PublicCORSByEnv
	if corsByEnv == nil {
		corsByEnv = map[environment.Environment][]string{
			environment.Dev:  d.PublicCORSOrigins,
			environment.Prod: d.PublicCORSOrigins,
		}
	}
	cors := middleware.CORSFor(corsByEnv)
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, pattern := range []string{
		"OPTIONS /api/v1/content",
		"OPTIONS /api/v1/content/{key}",
		"OPTIONS /api/v1/content/{key}/entries",
		"OPTIONS /api/v1/forms/{key}/submissions",
	} {
		mux.Handle(pattern, cors(noop))
	}
	mux.Handle("GET /api/v1/content", cors(http.HandlerFunc(d.Content.PublicContent)))
	mux.Handle("GET /api/v1/content/{key}", cors(http.HandlerFunc(d.Content.PublicFeature)))
	mux.Handle("GET /api/v1/content/{key}/entries", cors(http.HandlerFunc(d.Content.PublicEntries)))
	formLimiter := middleware.NewRateLimiter(d.FormRateLimit, d.FormRateWindow, d.TrustProxy)
	mux.Handle("POST /api/v1/forms/{key}/submissions",
		cors(jsonLimit(formLimiter.Middleware(http.HandlerFunc(d.Forms.Submit)))))

	// Admin SPA.
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusFound)
	})
	mux.Handle("GET /admin/", http.StripPrefix("/admin", static.Handler()))

	// Consistent fallbacks for the API and the root URL.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || len(r.URL.Path) > 5 && r.URL.Path[:5] == "/api/" {
			response.Error(w, r, http.StatusNotFound, "not_found", "Маршрут API не найден")
			return
		}
		response.Error(w, r, http.StatusNotFound, "not_found", "Страница не найдена")
	})

	// Order matters: outermost middleware runs first.
	handler := middleware.RequestID(mux)
	handler = middleware.SecurityHeaders(handler)
	handler = middleware.Recover(d.Log)(handler)
	handler = middleware.Logger(d.Log)(handler)
	return handler
}

// NewPublicRouter serves a single site environment. The handler tags itself
// with that environment, so content and CORS resolve to the right listener.
func NewPublicRouter(d Deps, env environment.Environment, baseURL string) http.Handler {
	if d.FormRateWindow <= 0 {
		d.FormRateWindow = time.Minute
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) {
		response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if d.Health != nil {
			if err := d.Health(ctx); err != nil {
				response.JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
				return
			}
		}
		response.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	corsByEnv := d.PublicCORSByEnv
	if corsByEnv == nil {
		corsByEnv = map[environment.Environment][]string{
			environment.Dev:  d.PublicCORSOrigins,
			environment.Prod: d.PublicCORSOrigins,
		}
	}
	cors := middleware.CORSFor(corsByEnv)
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, pattern := range []string{
		"OPTIONS /api/v1/content",
		"OPTIONS /api/v1/content/{key}",
		"OPTIONS /api/v1/content/{key}/entries",
		"OPTIONS /api/v1/forms/{key}/submissions",
	} {
		mux.Handle(pattern, cors(noop))
	}
	mux.Handle("GET /api/v1/content", cors(http.HandlerFunc(d.Content.PublicContent)))
	mux.Handle("GET /api/v1/content/{key}", cors(http.HandlerFunc(d.Content.PublicFeature)))
	mux.Handle("GET /api/v1/content/{key}/entries", cors(http.HandlerFunc(d.Content.PublicEntries)))
	formLimiter := middleware.NewRateLimiter(d.FormRateLimit, d.FormRateWindow, d.TrustProxy)
	mux.Handle("POST /api/v1/forms/{key}/submissions",
		cors(middleware.BodyLimit(d.MaxJSONBytes)(formLimiter.Middleware(http.HandlerFunc(d.Forms.Submit)))))
	mux.HandleFunc("GET /media/{key}", d.Media.Redirect)

	if d.Site != nil {
		mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
			response.Error(w, r, http.StatusNotFound, "not_found", "Маршрут API не найден")
		})
		mux.Handle("/", d.Site.Handler(env, baseURL))
	}

	handler := middleware.RequestID(mux)
	handler = middleware.SecurityHeaders(handler)
	handler = middleware.Recover(d.Log)(handler)
	handler = middleware.Logger(d.Log)(handler)
	// WithEnvironment stays outermost, mirroring the previous wiring where
	// each public server wrapped the router itself.
	return middleware.WithEnvironment(env)(handler)
}

// Deps is an alias used to keep composite literals readable in tests.
type Deps = Dependencies
