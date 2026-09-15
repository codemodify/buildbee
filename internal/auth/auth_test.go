package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDevMe(t *testing.T) {
	s := NewDev()
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	ident, mode, ok := s.Me(req)
	if !ok || mode != "dev" || ident.DisplayName != "You" {
		t.Fatalf("%v %s %#v", ok, mode, ident)
	}
}

func TestPublicPath(t *testing.T) {
	s := New(Config{ClientID: "x"})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects", nil)
	if s.PublicPath(req) {
		t.Fatal("mutating should not be public")
	}
	h := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	if !s.PublicPath(h) {
		t.Fatal("healthz")
	}
}
