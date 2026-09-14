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
	h.serveTopic(w, r, r.PathValue("channelID"))
}

// ServeRun upgrades GET /v1/runs/{runID}/ws and streams RunEvent JSON.
func (h *Hub) ServeRun(w http.ResponseWriter, r *http.Request) {
	h.serveTopic(w, r, runTopic(r.PathValue("runID")))
}

func runTopic(runID string) string { return "run:" + runID }

func (h *Hub) serveTopic(w http.ResponseWriter, r *http.Request, topic string) {
	if topic == "" || topic == "run:" {
		http.Error(w, "missing topic", http.StatusBadRequest)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	h.add(topic, conn)
	go func() {
		defer func() {
			h.remove(topic, conn)
			_ = conn.Close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

// PublishRun sends a payload to every subscriber of a Run.
func (h *Hub) PublishRun(runID string, payload any) {
	h.Publish(runTopic(runID), payload)
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
