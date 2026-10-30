// Package runtime serves the embedded public site runtime. Feature code is
// loaded from immutable build assets at request time; this package only owns
// the stable shell that resolves routes and mounts feature modules.
package runtime

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Handler serves the public runtime SPA and its content-hashed assets.
func Handler() http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := path.Clean("/" + r.URL.Path)
		if clean == "/" || clean == "." {
			serveIndex(w, r, sub)
			return
		}
		name := strings.TrimPrefix(clean, "/")
		if info, statErr := fs.Stat(sub, name); statErr == nil && !info.IsDir() {
			if strings.HasPrefix(clean, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
			files.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(clean, "/assets/") || path.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}
		serveIndex(w, r, sub)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, filesystem fs.FS) {
	data, err := fs.ReadFile(filesystem, "index.html")
	if err != nil {
		http.Error(w, "public runtime is not built", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data: blob: https:; media-src 'self' blob: https:; "+
			"connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'; "+
			"base-uri 'self'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}
