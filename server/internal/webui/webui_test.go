package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandlerMissingUI(t *testing.T) {
	t.Setenv("BUILDBEE_WEB_DIR", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d want 404 (no dist)", rec.Code)
	}
}

func TestHandlerSPAFromDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>buildbee</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BUILDBEE_WEB_DIR", dir)
	h := Handler()

	index := httptest.NewRecorder()
	h.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusOK || index.Body.String() != "<html>buildbee</html>" {
		t.Fatalf("index: %d %s", index.Code, index.Body.String())
	}

	asset := httptest.NewRecorder()
	h.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "ok" {
		t.Fatalf("asset: %d %s", asset.Code, asset.Body.String())
	}

	spa := httptest.NewRecorder()
	h.ServeHTTP(spa, httptest.NewRequest(http.MethodGet, "/projects/abc", nil))
	if spa.Code != http.StatusOK || spa.Body.String() != "<html>buildbee</html>" {
		t.Fatalf("spa fallback: %d %s", spa.Code, spa.Body.String())
	}
}

func TestAvailable(t *testing.T) {
	t.Setenv("BUILDBEE_WEB_DIR", "")
	if Available() {
		t.Fatal("expected no embedded index.html in tests")
	}
	dir := t.TempDir()
	t.Setenv("BUILDBEE_WEB_DIR", dir)
	if Available() {
		t.Fatal("empty dir should not be available")
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Available() {
		t.Fatal("expected available after index.html")
	}
}
