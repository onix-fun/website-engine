package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"websiteengine/features/auth"
	"websiteengine/features/content"
	"websiteengine/features/features"
	"websiteengine/features/forms"
	"websiteengine/features/media"
	"websiteengine/features/overview"
	"websiteengine/features/releases"
	"websiteengine/features/settings"
	"websiteengine/features/sitebuilder"
	"websiteengine/migrations"
	"websiteengine/pkg/config"
	"websiteengine/pkg/environment"
	"websiteengine/pkg/https"
	"websiteengine/pkg/postgres"
	"websiteengine/pkg/secrets"
	"websiteengine/pkg/storage/s3"
	httptransport "websiteengine/presentation/http"
	hauth "websiteengine/presentation/http/handlers/auth"
	hcontent "websiteengine/presentation/http/handlers/content"
	hfeatures "websiteengine/presentation/http/handlers/features"
	hforms "websiteengine/presentation/http/handlers/forms"
	hmedia "websiteengine/presentation/http/handlers/media"
	hoverview "websiteengine/presentation/http/handlers/overview"
	hpublication "websiteengine/presentation/http/handlers/publication"
	hreleases "websiteengine/presentation/http/handlers/releases"
	hsettings "websiteengine/presentation/http/handlers/settings"
	hsitebuilder "websiteengine/presentation/http/handlers/sitebuilder"
)

// run starts the API server. The page-size constants mirror the limits that
// the HTTP handlers enforce, so the admin settings section reports the real
// values instead of placeholders.
const (
	adminPageSize           = 200
	publicPageSize          = 50
	submissionExportMaxRows = 10000
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	// API timestamps are always emitted in UTC regardless of host timezone.
	time.Local = time.UTC
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if cfg.MigrateOnStart {
		if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
			return fmt.Errorf("migrations: %w", err)
		}
		log.Info("database migrations applied")
	}

	authRepo := auth.NewRepository(pool)
	authService := auth.NewService(authRepo)
	created, err := authService.Bootstrap(ctx, cfg.BootstrapAdminToken)
	if err != nil {
		return fmt.Errorf("bootstrap admin token: %w", err)
	}
	if created {
		log.Warn("bootstrap administrator token created")
	} else {
		hasAdmin, err := authService.LookupForBootstrap(ctx)
		if err != nil {
			return err
		}
		if !hasAdmin {
			log.Warn("no active admin token exists; set a new BOOTSTRAP_ADMIN_TOKEN or restore an admin token")
		}
	}

	objectStore, err := s3.New(ctx, cfg.S3)
	if err != nil {
		return fmt.Errorf("S3 storage: %w", err)
	}
	if cfg.S3.EnsureBucket {
		if err := ensureBucket(ctx, objectStore); err != nil {
			return fmt.Errorf("ensure S3 bucket: %w", err)
		}
	}
	featureBuildService := sitebuilder.NewBuildService(pool, objectStore, cfg.MaxReleaseBytes)
	featureBuildHandler := hsitebuilder.NewBuildsHandler(featureBuildService, log, cfg.MaxReleaseBytes)
	snapshotService := sitebuilder.NewSnapshotService(pool, featureBuildService)

	featureRepo := features.NewRepository(pool)
	featureService := features.NewService(featureRepo)
	featureHandler := hfeatures.NewHandler(featureService, log, cfg.MaxJSONBytes)

	contentRepo := content.NewRepository(pool)
	contentService := content.NewService(contentRepo, featureRepo)
	contentHandler := hcontent.NewHandler(contentService, log, cfg.MaxJSONBytes)

	formsRepo := forms.NewRepository(pool)
	formsService := forms.NewService(formsRepo, featureRepo)
	formsHandler := hforms.NewHandler(formsService, log, cfg.MaxJSONBytes)
	snapshotHandler := hsitebuilder.NewSnapshotsHandler(snapshotService, formsService, log, cfg.MaxJSONBytes)

	mediaRepo := media.NewRepository(pool)
	mediaService := media.NewService(mediaRepo, objectStore, cfg.MaxUploadBytes)
	mediaHandler := hmedia.NewHandler(mediaService, log, cfg.MaxUploadBytes)

	overviewRepo := overview.NewRepository(pool)
	overviewHandler := hoverview.NewHandler(overviewRepo, log)

	authHandler := hauth.NewHandler(authService, log, cfg.MaxJSONBytes)

	settingsService := settings.NewService(settings.Config{
		MaxJSONBytes:            cfg.MaxJSONBytes,
		MaxUploadBytes:          cfg.MaxUploadBytes,
		FormRateLimit:           cfg.FormRateLimit,
		FormRateWindowSeconds:   int(cfg.FormRateWindow / time.Second),
		PublicCORSOrigins:       cfg.PublicCORSOrigins,
		AdminPageSize:           adminPageSize,
		PublicPageSize:          publicPageSize,
		SubmissionExportMaxRows: submissionExportMaxRows,
		AcceptedContentTypes:    media.AcceptedContentTypes(),
		ImageVariants:           media.ImageVariantNames(),
	})
	settingsHandler := hsettings.NewHandler(settingsService)
	publicationEventsHandler := hpublication.NewHandler(pool, log)

	health := func(checkCtx context.Context) error {
		if err := pool.Ping(checkCtx); err != nil {
			return fmt.Errorf("postgres: %w", err)
		}
		if err := mediaService.Ready(checkCtx); err != nil {
			return fmt.Errorf("media storage: %w", err)
		}
		return nil
	}

	releaseRepo := releases.NewRepository(pool)
	var box *secrets.Box
	if cfg.ReleaseMasterKey == "" {
		log.Warn("RELEASE_MASTER_KEY is not configured; release source credentials are disabled")
	} else {
		box, err = secrets.New(cfg.ReleaseMasterKey)
		if err != nil {
			return fmt.Errorf("release master key: %w", err)
		}
	}
	releaseService := releases.NewService(releaseRepo, objectStore, box, cfg.MaxReleaseBytes)
	releases.ConfigureSourceHosts(cfg.ProviderExtraHosts, cfg.ProviderAllowInsecure)
	releaseHandler := hreleases.NewHandler(releaseService, log, cfg.MaxReleaseBytes)

	deps := httptransport.Dependencies{
		Log: log, AuthService: authService, Auth: authHandler,
		Features: featureHandler, Content: contentHandler, Forms: formsHandler, Media: mediaHandler,
		Overview: overviewHandler, Health: health, MaxJSONBytes: cfg.MaxJSONBytes,
		FeatureBuilds: featureBuildHandler, SiteSnapshots: snapshotHandler,
		Releases: releaseHandler, Settings: settingsHandler,
		PublicationEvents: publicationEventsHandler,
		CorsResolver:      releaseService.Cors(),
		FormRateLimit:     cfg.FormRateLimit, FormRateWindow: cfg.FormRateWindow,
		TrustProxy: cfg.TrustProxyHeaders, PublicCORSOrigins: cfg.PublicCORSOrigins, PublicCORSByEnv: cfg.PublicCORSByEnv,
	}

	servers := []*http.Server{
		newServer(cfg.HTTPAddr, httptransport.NewRouter(deps), cfg),
		newServer(cfg.DevHTTPAddr, httptransport.NewPublicRouter(deps, environment.Dev), cfg),
		newServer(cfg.ProdHTTPAddr, httptransport.NewPublicRouter(deps, environment.Prod), cfg),
	}

	var tlsRouter *https.Router
	if cfg.TLS.Enabled {
		devPublic := httptransport.NewPublicRouter(deps, environment.Dev)
		prodPublic := httptransport.NewPublicRouter(deps, environment.Prod)
		tlsRouter, err = https.NewRouter(cfg.TLS, environmentDomains{service: releaseService}, map[environment.Environment]http.Handler{
			environment.Dev:  devPublic,
			environment.Prod: prodPublic,
		}, log)
		if err != nil {
			return fmt.Errorf("TLS router: %w", err)
		}
		tlsRouter.Start(ctx)
		httpsServer := tlsRouter.HTTPSServer(cfg.TLS.HTTPSAddr)
		httpsServer.ReadTimeout = cfg.ReadHeaderTimeout * 20
		httpsServer.WriteTimeout = 2 * time.Minute
		httpsServer.IdleTimeout = 120 * time.Second
		acmeServer := tlsRouter.HTTP01Server(cfg.TLS.HTTPAddr)
		servers = append(servers, httpsServer, acmeServer)
	}
	errCh := make(chan error, len(servers))
	for _, server := range servers {
		server := server
		go func() {
			log.Info("HTTP server listening", "addr", server.Addr)
			if server.TLSConfig != nil {
				errCh <- server.ListenAndServeTLS("", "")
				return
			}
			errCh <- server.ListenAndServe()
		}()
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		for _, server := range servers {
			if err := server.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("shutdown: %w", err)
			}
		}
		log.Info("server stopped")
		return nil
	case err := <-errCh:
		for _, server := range servers {
			_ = server.Close()
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// environmentDomains adapts the releases feature into the pkg/https domain
// contract: site_environments stays the single source of truth for routing.
type environmentDomains struct {
	service *releases.Service
}

func (l environmentDomains) ListDomains(ctx context.Context) ([]https.DomainEntry, error) {
	states, err := l.service.Environments(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]https.DomainEntry, 0, len(states))
	for _, st := range states {
		if len(st.Domains) == 0 {
			continue
		}
		out = append(out, https.DomainEntry{Env: st.Key, Domains: st.Domains})
	}
	return out, nil
}

func newServer(addr string, handler http.Handler, cfg config.Config) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}
}

// ensureBucket tolerates object storage starting slightly after the API in
// local Docker Compose setups.
func ensureBucket(ctx context.Context, store *s3.Store) error {
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for {
		if err := store.EnsureBucket(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
