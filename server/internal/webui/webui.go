// Package webui serves the Vite web app from an embedded dist or BUILDBEE_WEB_DIR.
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

func dirFromEnv() string {
	return strings.TrimSpace(os.Getenv("BUILDBEE_WEB_DIR"))
}

// Available reports whether a built index.html exists (env dir or embed).
func Available() bool {
	if d := dirFromEnv(); d != "" {
		if _, err := os.Stat(path.Join(d, "index.html")); err == nil {
			return true
		}
	}
	_, err := fs.Stat(embedded, "dist/index.html")
	return err == nil
}

// Handler serves static files with SPA fallback to index.html.
func Handler() http.Handler {
	if d := dirFromEnv(); d != "" {
		if _, err := os.Stat(path.Join(d, "index.html")); err == nil {
			return spaFile(http.Dir(d))
		}
	}
	sub, err := fs.Sub(embedded, "dist")
	if err == nil {
		if _, err := fs.Stat(sub, "index.html"); err == nil {
			return spaFile(http.FS(sub))
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"web UI not built; run scripts/build.sh or set BUILDBEE_WEB_DIR"}`))
	})
}

func spaFile(root http.FileSystem) http.Handler {
	files := http.FileServer(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" || p == "." {
			r.URL.Path = "/"
			files.ServeHTTP(w, r)
			return
		}
		if f, err := root.Open(p); err == nil {
			_ = f.Close()
			files.ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		files.ServeHTTP(w, r)
	})
}
