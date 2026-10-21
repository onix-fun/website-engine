// Package site serves the active immutable release for a public environment.
package site

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"websiteengine/internal/environment"
	"websiteengine/internal/storage"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

type Release struct {
	ID        uuid.UUID
	Version   string
	ObjectKey string
	SHA256    string
}

func (r *Repository) ActiveRelease(ctx context.Context, env environment.Environment) (*Release, error) {
	var release Release
	err := r.pool.QueryRow(ctx, `
		SELECT r.id, r.version, r.artifact_object_key, r.artifact_sha256
		  FROM site_environments e
		  JOIN site_releases r ON r.id = e.active_release_id
		 WHERE e.key = $1`, env.String(),
	).Scan(&release.ID, &release.Version, &release.ObjectKey, &release.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &release, nil
}

func (r *Repository) SiteName(ctx context.Context) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, `SELECT name FROM site_settings WHERE id = TRUE`).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "Website", nil
	}
	return name, err
}

type Manager struct {
	repo     *Repository
	store    storage.ObjectStore
	cacheDir string

	mu    sync.Mutex
	cache map[environment.Environment]*cachedRelease
}

type cachedRelease struct {
	id  uuid.UUID
	dir string
}

func NewManager(repo *Repository, store storage.ObjectStore, cacheDir string) *Manager {
	return &Manager{repo: repo, store: store, cacheDir: cacheDir, cache: make(map[environment.Environment]*cachedRelease)}
}

type Handler struct {
	manager *Manager
	env     environment.Environment
	baseURL string
}

func (m *Manager) Handler(env environment.Environment, baseURL string) http.Handler {
	return &Handler{manager: m, env: env, baseURL: strings.TrimRight(baseURL, "/")}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	release, dir, err := h.manager.ready(r.Context(), h.env)
	if err != nil {
		http.Error(w, "site release is not ready", http.StatusServiceUnavailable)
		return
	}
	if release == nil {
		http.Error(w, "no active release for this environment", http.StatusServiceUnavailable)
		return
	}

	name := path.Clean("/" + r.URL.Path)
	if name == "/" || name == "." {
		h.serveIndex(w, r, dir)
		return
	}
	trimmed := strings.TrimPrefix(name, "/")
	if strings.HasPrefix(trimmed, ".git/") || trimmed == ".git" {
		http.NotFound(w, r)
		return
	}
	target := filepath.Join(dir, filepath.FromSlash(trimmed))
	if !within(dir, target) {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(target)
	if err == nil && info.Mode().IsRegular() {
		h.serveFile(w, r, target, info)
		return
	}
	if strings.HasPrefix(name, "/assets/") || path.Ext(name) != "" {
		http.NotFound(w, r)
		return
	}
	h.serveIndex(w, r, dir)
}

func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, name string, info os.FileInfo) {
	f, err := os.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	if contentType := mime.TypeByExtension(filepath.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func (h *Handler) serveIndex(w http.ResponseWriter, r *http.Request, dir string) {
	indexPath := filepath.Join(dir, "index.html")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		http.Error(w, "active release has no index.html", http.StatusServiceUnavailable)
		return
	}
	name, err := h.manager.repo.SiteName(r.Context())
	if err != nil || name == "" {
		name = "Website"
	}
	config := map[string]any{
		"environment": h.env.String(),
		"site_name":   name,
		"api_base":    "/api/v1",
		"public_url":  h.baseURL,
	}
	injected, err := injectConfig(data, config)
	if err != nil {
		http.Error(w, "invalid index.html", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; media-src 'self' blob: https:; connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(injected)
	}
}

func injectConfig(index []byte, config map[string]any) ([]byte, error) {
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	raw = bytes.ReplaceAll(raw, []byte("<"), []byte(`\u003c`))
	script := []byte(`<script>window.__SITE_CONFIG__=` + string(raw) + `;</script>`)
	lower := bytes.ToLower(index)
	if i := bytes.Index(lower, []byte("</head>")); i >= 0 {
		out := make([]byte, 0, len(index)+len(script))
		out = append(out, index[:i]...)
		out = append(out, script...)
		out = append(out, index[i:]...)
		return out, nil
	}
	out := make([]byte, 0, len(script)+len(index))
	out = append(out, script...)
	out = append(out, index...)
	return out, nil
}

func (m *Manager) ready(ctx context.Context, env environment.Environment) (*Release, string, error) {
	release, err := m.repo.ActiveRelease(ctx, env)
	if err != nil || release == nil {
		return nil, "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if cached, ok := m.cache[env]; ok && cached.id == release.ID {
		return release, cached.dir, nil
	}
	dir := filepath.Join(m.cacheDir, release.ID.String())
	if !m.readyDir(dir) {
		if err := m.install(ctx, release, dir); err != nil {
			return nil, "", err
		}
	}
	m.cache[env] = &cachedRelease{id: release.ID, dir: dir}
	return release, dir, nil
}

func (m *Manager) readyDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".ready"))
	return err == nil
}

func (m *Manager) install(ctx context.Context, release *Release, dir string) error {
	if err := os.MkdirAll(m.cacheDir, 0o755); err != nil {
		return err
	}
	body, err := m.store.Get(ctx, release.ObjectKey)
	if err != nil {
		return err
	}
	defer body.Close()
	tmp, err := os.CreateTemp(m.cacheDir, "download-*.zip")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := ExtractTo(tmpName, dir); err != nil {
		return err
	}
	return nil
}

// ExtractTo is wired to the artifact package in extract.go to keep this file
// focused on HTTP and cache orchestration.

func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
