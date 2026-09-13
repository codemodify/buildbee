package ws

import (
	"encoding/json"
	"net/http"
)

// Hub is a placeholder for Channel Activity fan-out over WebSocket.
// A later revision will upgrade connections and stream Run / Handoff events.
type Hub struct{}

func NewHub() *Hub {
	return &Hub{}
}

// Placeholder reserves GET /v1/ws until the real upgrade path exists.
func (h *Hub) Placeholder(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "websocket hub not implemented",
	})
}
