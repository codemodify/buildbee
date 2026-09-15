// Package ws fans committed events out to WebSocket subscribers.
//
// Each connection has a bounded send queue drained by its own writer
// goroutine with a write deadline, so Publish never blocks: a client that
// falls too far behind is disconnected and reconnects with a cursor.
// Subscribing with a cursor replays missed events from the database; live
// events published during the replay are held back and de-duplicated, so
// the subscriber sees every event with no gap.
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/codemodify/buildbee/internal/core"
	"github.com/gorilla/websocket"
)

const (
	sendQueue     = 8192             // frames buffered per connection (fits a full replay)
	maxPending    = 4096             // live events held while a replay runs
	maxTopics     = 256              // subscriptions per connection
	writeWait     = 10 * time.Second // per-message write deadline
	pongWait      = 60 * time.Second // peer must answer pings within this
	pingEvery     = 25 * time.Second
	maxClientMsg  = 16 << 10
	replayTimeout = 15 * time.Second
)

// Replayer returns a topic's events after a cursor, oldest first.
type Replayer func(ctx context.Context, topic string, after int64) ([]core.Event, error)

// Hub routes events to subscribed connections. It implements core.Publisher.
type Hub struct {
	replay Replayer
	log    *slog.Logger
	queue  int // per-connection send queue; tests shrink it

	mu     sync.RWMutex
	topics map[string]map[*client]struct{}
	conns  map[*client]struct{}
}

// NewHub returns a Hub; replay may be nil (no catch-up).
func NewHub(replay Replayer, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	return &Hub{replay: replay, log: log, queue: sendQueue, topics: map[string]map[*client]struct{}{}, conns: map[*client]struct{}{}}
}

// upgrader keeps gorilla's default same-origin check: a page on another
// site cannot read Project streams through a LAN user's browser.
var upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 16384}

// frame is what one connection receives for one event.
type frame struct {
	envelope []byte // {topic, cursor, type, data} for /v1/ws
	data     []byte // bare data for single-topic endpoints
}

// Publish delivers e to its topic's subscribers without blocking.
func (h *Hub) Publish(e core.Event) {
	h.mu.RLock()
	subs := h.topics[e.Topic]
	targets := make([]*client, 0, len(subs))
	for c := range subs {
		targets = append(targets, c)
	}
	h.mu.RUnlock()
	if len(targets) == 0 {
		return
	}
	f, err := encode(e)
	if err != nil {
		h.log.Error("ws: encode event", "topic", e.Topic, "err", err)
		return
	}
	for _, c := range targets {
		c.deliver(e.Topic, e.Cursor, f)
	}
}

func encode(e core.Event) (frame, error) {
	env, err := json.Marshal(e)
	if err != nil {
		return frame{}, err
	}
	data, err := json.Marshal(e.Data)
	return frame{envelope: env, data: data}, err
}

// Close disconnects every client (server shutdown).
func (h *Hub) Close() {
	h.mu.Lock()
	conns := make([]*client, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()
	for _, c := range conns {
		c.close()
	}
}

// Subscribers reports how many connections follow a topic (tests, metrics).
func (h *Hub) Subscribers(topic string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.topics[topic])
}

// ServeMulti upgrades GET /v1/ws. The client sends
// {"op":"subscribe","topic":"run:<id>","after":<cursor>} and
// {"op":"unsubscribe","topic":...}; it receives {topic, cursor, type, data}.
func (h *Hub) ServeMulti(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already answered the request
	}
	c := h.attach(conn, false)
	defer c.close()
	for {
		var msg struct {
			Op    string `json:"op"`
			Topic string `json:"topic"`
			After *int64 `json:"after"` // omitted: live events only
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Op {
		case "subscribe":
			after := int64(-1)
			if msg.After != nil {
				after = *msg.After
			}
			if err := h.subscribe(c, msg.Topic, after); err != nil {
				c.sendError(msg.Topic, err)
			}
		case "unsubscribe":
			h.unsubscribe(c, msg.Topic)
		default:
			c.sendError(msg.Topic, errUnknownOp)
		}
	}
}

// ServeTopic upgrades a single-topic stream such as GET /v1/runs/{id}/ws.
// ?after=<cursor> replays missed events; without it, replayAll replays the
// whole topic (right for a Run transcript) and otherwise only live events
// flow. Frames are the bare event data.
func (h *Hub) ServeTopic(topic func(*http.Request) string, replayAll bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t := topic(r)
		after := int64(-1)
		if replayAll {
			after = 0
		}
		if v := r.URL.Query().Get("after"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
				after = n
			}
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := h.attach(conn, true)
		defer c.close()
		if err := h.subscribe(c, t, after); err != nil {
			return
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}
}

type wsError string

func (e wsError) Error() string { return string(e) }

const (
	errUnknownOp    = wsError("op must be subscribe or unsubscribe")
	errTooMany      = wsError("too many subscriptions on this connection")
	errMissingTopic = wsError("topic is required")
)

func (h *Hub) attach(conn *websocket.Conn, bare bool) *client {
	c := &client{hub: h, conn: conn, bare: bare, send: make(chan []byte, h.queue), done: make(chan struct{}),
		subs: map[string]*sub{}}
	conn.SetReadLimit(maxClientMsg)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(pongWait)) })
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
	go c.writeLoop()
	return c
}

// subscribe registers c for topic, replays events after `after`, then
// releases live events that arrived meanwhile (minus duplicates).
func (h *Hub) subscribe(c *client, topic string, after int64) error {
	if topic == "" {
		return errMissingTopic
	}
	c.mu.Lock()
	if _, ok := c.subs[topic]; ok {
		c.mu.Unlock()
		return nil
	}
	if len(c.subs) >= maxTopics {
		c.mu.Unlock()
		return errTooMany
	}
	s := &sub{replayed: map[int64]struct{}{}}
	c.subs[topic] = s
	c.mu.Unlock()

	h.mu.Lock()
	if h.topics[topic] == nil {
		h.topics[topic] = map[*client]struct{}{}
	}
	h.topics[topic][c] = struct{}{}
	h.mu.Unlock()

	var backlog []core.Event
	if h.replay != nil && after >= 0 {
		ctx, cancel := context.WithTimeout(context.Background(), replayTimeout)
		var err error
		backlog, err = h.replay(ctx, topic, after)
		cancel()
		if err != nil {
			h.unsubscribe(c, topic)
			return err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range backlog {
		f, err := encode(e)
		if err != nil {
			continue
		}
		s.replayed[e.Cursor] = struct{}{}
		c.enqueueLocked(f)
	}
	for _, p := range s.pending {
		if _, dup := s.replayed[p.cursor]; !dup {
			c.enqueueLocked(p.frame)
		}
	}
	s.pending, s.ready = nil, true
	return nil
}

func (h *Hub) unsubscribe(c *client, topic string) {
	h.mu.Lock()
	if subs := h.topics[topic]; subs != nil {
		delete(subs, c)
		if len(subs) == 0 {
			delete(h.topics, topic)
		}
	}
	h.mu.Unlock()
	c.mu.Lock()
	delete(c.subs, topic)
	c.mu.Unlock()
}

func (h *Hub) detach(c *client) {
	h.mu.Lock()
	delete(h.conns, c)
	for topic := range c.topicNames() {
		if subs := h.topics[topic]; subs != nil {
			delete(subs, c)
			if len(subs) == 0 {
				delete(h.topics, topic)
			}
		}
	}
	h.mu.Unlock()
}

// --- client ---

type pendingFrame struct {
	cursor int64
	frame  frame
}

type sub struct {
	ready    bool
	pending  []pendingFrame
	replayed map[int64]struct{} // cursors already sent by the replay
}

type client struct {
	hub  *Hub
	conn *websocket.Conn
	bare bool
	send chan []byte
	done chan struct{}

	mu        sync.Mutex
	subs      map[string]*sub
	closeOnce sync.Once
}

func (c *client) topicNames() map[string]struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]struct{}, len(c.subs))
	for t := range c.subs {
		out[t] = struct{}{}
	}
	return out
}

func (c *client) deliver(topic string, cursor int64, f frame) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.subs[topic]
	if s == nil {
		return
	}
	if !s.ready {
		if len(s.pending) >= maxPending {
			go c.close()
			return
		}
		s.pending = append(s.pending, pendingFrame{cursor: cursor, frame: f})
		return
	}
	if _, dup := s.replayed[cursor]; dup {
		return
	}
	c.enqueueLocked(f)
}

// enqueueLocked queues a frame without blocking; a full queue means the
// client is too slow and is disconnected.
func (c *client) enqueueLocked(f frame) {
	msg := f.envelope
	if c.bare {
		msg = f.data
	}
	select {
	case c.send <- msg:
	default:
		go c.close()
	}
}

func (c *client) sendError(topic string, err error) {
	raw, _ := json.Marshal(map[string]string{"type": "error", "topic": topic, "error": err.Error()})
	select {
	case c.send <- raw:
	default:
		go c.close()
	}
}

func (c *client) writeLoop() {
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-c.done:
			return
		case msg := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				c.close()
				return
			}
		case <-ping.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				c.close()
				return
			}
		}
	}
}

func (c *client) close() {
	c.closeOnce.Do(func() {
		c.hub.detach(c)
		close(c.done)
		_ = c.conn.Close()
	})
}
