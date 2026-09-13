package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/codemodify/buildbee/server/internal/ws"
)

// NewMux returns the Server HTTP mux.
//
// Auth is stubbed: humans will use GitHub OAuth later; Bots receive
// server-issued Identities. Persistence (Project, Channel, Task, Decision)
// is not wired in this scaffold.
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	hub := ws.NewHub()

	mux.HandleFunc("GET /healthz", Health)
	mux.HandleFunc("GET /v1/", V1)
	mux.HandleFunc("GET /v1/ws", hub.Placeholder)

	return mux
}

// V1 is the placeholder /v1/ router. Real Project, Channel, Task, and
// Decisions endpoints land here later.
func V1(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/" && r.URL.Path != "/v1" {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "buildbee",
		"api":     "v1",
		"message": "placeholder router",
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
