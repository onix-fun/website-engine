package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"websiteengine/pkg/environment"
)

// Config is the complete runtime configuration. All values come from the
// environment; startup fails fast when a value is invalid.
type Config struct {
	HTTPAddr            string
	DevHTTPAddr         string
	ProdHTTPAddr        string
	DevPublicBaseURL    string
	ProdPublicBaseURL   string
	DatabaseURL         string
	MigrateOnStart      bool
	BootstrapAdminToken string
	LogLevel            string
	TrustProxyHeaders   bool
	PublicCORSOrigins   []string
	PublicCORSByEnv     map[environment.Environment][]string

	MaxJSONBytes   int64
	MaxUploadBytes int64
	FormRateLimit  int
	FormRateWindow time.Duration

	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration

	S3 S3Config

	ReleaseCacheDir  string
	ReleaseMasterKey string
	MaxReleaseBytes  int64

	// Release sources (git providers): extra exact hostnames that may be
	// addressed as repository hosts (e.g. a self-hosted Gitea/GitLab), and
	// whether plain http:// is accepted (local development only).
	ProviderExtraHosts    []string
	ProviderAllowInsecure bool
}

type S3Config struct {
	Endpoint       string
	Region         string
	Bucket         string
	AccessKey      string
	SecretKey      string
	PublicBaseURL  string
	ForcePathStyle bool
	EnsureBucket   bool
}

// Load reads and validates configuration from the process environment.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:              getenv("HTTP_ADDR", ":8080"),
		DevHTTPAddr:           getenv("DEV_HTTP_ADDR", ":8081"),
		ProdHTTPAddr:          getenv("PROD_HTTP_ADDR", ":8082"),
		DevPublicBaseURL:      strings.TrimRight(getenv("DEV_PUBLIC_BASE_URL", "http://localhost:8081"), "/"),
		ProdPublicBaseURL:     strings.TrimRight(getenv("PROD_PUBLIC_BASE_URL", "http://localhost:8082"), "/"),
		ReleaseCacheDir:       getenv("RELEASE_CACHE_DIR", "./data/releases"),
		ReleaseMasterKey:      strings.TrimSpace(os.Getenv("RELEASE_MASTER_KEY")),
		MaxReleaseBytes:       getenvInt64("MAX_RELEASE_BYTES", 256<<20),
		ProviderExtraHosts:    splitCSV(os.Getenv("RELEASE_SOURCE_HOSTS")),
		ProviderAllowInsecure: getenvBool("RELEASE_SOURCE_ALLOW_INSECURE", false),
		DatabaseURL:           strings.TrimSpace(os.Getenv("DATABASE_URL")),
		MigrateOnStart:        getenvBool("MIGRATE_ON_START", true),
		BootstrapAdminToken:   strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_TOKEN")),
		LogLevel:              strings.ToLower(getenv("LOG_LEVEL", "info")),
		TrustProxyHeaders:     getenvBool("TRUST_PROXY_HEADERS", false),
		PublicCORSOrigins:     splitCSV(os.Getenv("PUBLIC_CORS_ORIGINS")),
		MaxJSONBytes:          getenvInt64("MAX_JSON_BYTES", 1<<20),
		MaxUploadBytes:        getenvInt64("MAX_UPLOAD_BYTES", 20<<20),
		FormRateLimit:         int(getenvInt64("FORM_RATE_LIMIT", 20)),
		FormRateWindow:        time.Minute,
		ReadHeaderTimeout:     10 * time.Second,
		ShutdownTimeout:       20 * time.Second,
		S3: S3Config{
			Endpoint:       getenv("S3_ENDPOINT", "http://localhost:9000"),
			Region:         getenv("S3_REGION", "us-east-1"),
			Bucket:         getenv("S3_BUCKET", "media"),
			AccessKey:      getenv("S3_ACCESS_KEY", "minioadmin"),
			SecretKey:      getenv("S3_SECRET_KEY", "minioadmin"),
			PublicBaseURL:  getenv("S3_PUBLIC_BASE_URL", "http://localhost:9000"),
			ForcePathStyle: getenvBool("S3_FORCE_PATH_STYLE", true),
			EnsureBucket:   getenvBool("S3_ENSURE_BUCKET", false),
		},
	}
	cfg.S3.PublicBaseURL = strings.TrimRight(cfg.S3.PublicBaseURL, "/")
	cfg.S3.Endpoint = strings.TrimRight(cfg.S3.Endpoint, "/")
	for i, origin := range cfg.PublicCORSOrigins {
		cfg.PublicCORSOrigins[i] = strings.TrimRight(origin, "/")
	}
	devOrigins := splitCSV(os.Getenv("PUBLIC_CORS_ORIGINS_DEV"))
	prodOrigins := splitCSV(os.Getenv("PUBLIC_CORS_ORIGINS_PROD"))
	if devOrigins == nil {
		devOrigins = cfg.PublicCORSOrigins
	}
	if prodOrigins == nil {
		prodOrigins = cfg.PublicCORSOrigins
	}
	cfg.PublicCORSByEnv = map[environment.Environment][]string{
		environment.Dev:  trimOrigins(devOrigins),
		environment.Prod: trimOrigins(prodOrigins),
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if c.HTTPAddr == "" || c.DevHTTPAddr == "" || c.ProdHTTPAddr == "" {
		return errors.New("HTTP_ADDR, DEV_HTTP_ADDR and PROD_HTTP_ADDR must not be empty")
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("LOG_LEVEL must be debug, info, warn or error")
	}
	if c.BootstrapAdminToken != "" && len(c.BootstrapAdminToken) < 24 {
		return errors.New("BOOTSTRAP_ADMIN_TOKEN must be at least 24 characters")
	}
	if c.MaxJSONBytes < 1024 || c.MaxJSONBytes > 64<<20 {
		return errors.New("MAX_JSON_BYTES must be between 1 KiB and 64 MiB")
	}
	if c.MaxUploadBytes < 1024 || c.MaxUploadBytes > 512<<20 {
		return errors.New("MAX_UPLOAD_BYTES must be between 1 KiB and 512 MiB")
	}
	if c.MaxReleaseBytes < 1<<20 || c.MaxReleaseBytes > 2<<30 {
		return errors.New("MAX_RELEASE_BYTES must be between 1 MiB and 2 GiB")
	}
	if c.ReleaseCacheDir == "" {
		return errors.New("RELEASE_CACHE_DIR must not be empty")
	}
	if c.FormRateLimit < 1 {
		return errors.New("FORM_RATE_LIMIT must be positive")
	}
	for _, origins := range [][]string{c.PublicCORSOrigins, c.PublicCORSByEnv[environment.Dev], c.PublicCORSByEnv[environment.Prod]} {
		for _, origin := range origins {
			if origin == "*" {
				continue
			}
			u, err := url.Parse(origin)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") ||
				(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
				return fmt.Errorf("PUBLIC_CORS_ORIGINS entry %q must be an http(s) origin without a path", origin)
			}
		}
	}
	if c.S3.Endpoint == "" || c.S3.Bucket == "" || c.S3.AccessKey == "" || c.S3.SecretKey == "" {
		return errors.New("S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY are required")
	}
	if !bucketPattern.MatchString(c.S3.Bucket) {
		return errors.New("S3_BUCKET must be a valid S3 bucket name (3-63 lowercase letters, digits, dots and hyphens)")
	}
	if c.S3.Region == "" {
		return errors.New("S3_REGION must not be empty")
	}
	if err := validateURL(c.S3.Endpoint, "S3_ENDPOINT"); err != nil {
		return err
	}
	if err := validateURL(c.S3.PublicBaseURL, "S3_PUBLIC_BASE_URL"); err != nil {
		return err
	}
	return nil
}

func validateURL(raw, name string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s must be an absolute http(s) URL", name)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", name)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s must not contain a query or fragment", name)
	}
	return nil
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return v
}

func getenvInt64(key string, fallback int64) int64 {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return v
}

var bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

func trimOrigins(origins []string) []string {
	out := make([]string, len(origins))
	for i, origin := range origins {
		out[i] = strings.TrimRight(origin, "/")
	}
	return out
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
