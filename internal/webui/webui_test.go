package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandlerMissingUI(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(t.TempDir()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d want 404 (no index.html)", rec.Code)
	}
}

func TestHandlerSPAFromDir(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "<html>buildbee</html>")
	write("assets/app-abc123.js", "ok")
	h := Handler(dir)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	index := get("/")
	if index.Code != http.StatusOK || index.Body.String() != "<html>buildbee</html>" {
		t.Fatalf("index: %d %s", index.Code, index.Body.String())
	}
	if index.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index Cache-Control: %q", index.Header().Get("Cache-Control"))
	}

	asset := get("/assets/app-abc123.js")
	if asset.Code != http.StatusOK || asset.Body.String() != "ok" {
		t.Fatalf("asset: %d %s", asset.Code, asset.Body.String())
	}
	if asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset Cache-Control: %q", asset.Header().Get("Cache-Control"))
	}

	if stale := get("/assets/app-old999.js"); stale.Code != http.StatusNotFound {
		t.Fatalf("stale asset: got %d want 404", stale.Code)
	}

	route := get("/projects/abc")
	if route.Code != http.StatusOK || route.Body.String() != "<html>buildbee</html>" {
		t.Fatalf("spa fallback: %d %s", route.Code, route.Body.String())
	}

	post := httptest.NewRecorder()
	h.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/projects/abc", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST to UI: got %d want 405", post.Code)
	}
}
