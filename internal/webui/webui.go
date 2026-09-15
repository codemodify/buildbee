// Package webui serves the Vite web app from a directory or the embedded build.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Handler serves the web UI with an SPA fallback to index.html.
// A non-empty dir (BUILDBEE_WEB_DIR) wins over the embedded build.
func Handler(dir string) http.Handler {
	if dir != "" {
		if _, err := os.Stat(path.Join(dir, "index.html")); err == nil {
			return spa(http.Dir(dir))
		}
	}
	if sub, err := fs.Sub(embedded, "dist"); err == nil {
		if _, err := fs.Stat(sub, "index.html"); err == nil {
			return spa(http.FS(sub))
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"web UI not built; run make build or set BUILDBEE_WEB_DIR"}`))
	})
}

// spa serves files that exist, 404s for missing asset files, and falls back
// to index.html for client-side routes.
//
// Vite emits content-hashed files under /assets, so those are cached forever;
// index.html is never cached, so an upgrade is picked up on the next load.
// A missing asset returns 404 instead of index.html: after an upgrade an old
// tab asking for a stale chunk gets a clear error, not HTML parsed as JS.
func spa(root http.FileSystem) http.Handler {
	files := http.FileServer(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "." {
			if f, err := root.Open(p); err == nil {
				_ = f.Close()
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(p, "assets/") || path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	})
}
