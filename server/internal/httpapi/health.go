package httpapi

import "net/http"

// Health is GET /healthz. It does not require Postgres in this scaffold.
func Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
