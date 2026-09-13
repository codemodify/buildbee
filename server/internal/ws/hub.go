package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Hub fans Channel messages out to WebSocket subscribers.
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[*websocket.Conn]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: map[string]map[*websocket.Conn]struct{}{}}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ServeChannel upgrades GET /v1/channels/{channelID}/ws and streams JSON messages.
func (h *Hub) ServeChannel(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("channelID")
	if channelID == "" {
		http.Error(w, "missing channel", http.StatusBadRequest)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	h.add(channelID, conn)
	go func() {
		defer func() {
			h.remove(channelID, conn)
			_ = conn.Close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

// Publish sends a payload to every subscriber of a Channel.
func (h *Hub) Publish(channelID string, payload any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.subs[channelID] {
		if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			_ = conn.Close()
			delete(h.subs[channelID], conn)
		}
	}
}

func (h *Hub) add(channelID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[channelID] == nil {
		h.subs[channelID] = map[*websocket.Conn]struct{}{}
	}
	h.subs[channelID][conn] = struct{}{}
}

func (h *Hub) remove(channelID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m := h.subs[channelID]; m != nil {
		delete(m, conn)
	}
}
