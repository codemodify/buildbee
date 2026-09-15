package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/core"
	"github.com/gorilla/websocket"
)

func serve(t *testing.T, h *Hub) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ws", h.ServeMulti)
	mux.HandleFunc("GET /v1/runs/{id}/ws", h.ServeTopic(func(r *http.Request) string { return "run:" + r.PathValue("id") }, true))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func dial(t *testing.T, srv *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func waitSubscribers(t *testing.T, h *Hub, topic string, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for h.Subscribers(topic) != n {
		if time.Now().After(deadline) {
			t.Fatalf("%s: %d subscribers, want %d", topic, h.Subscribers(topic), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

type envelope struct {
	Topic  string          `json:"topic"`
	Cursor int64           `json:"cursor"`
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data"`
	Error  string          `json:"error"`
}

func read(t *testing.T, conn *websocket.Conn) envelope {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var e envelope
	if err := conn.ReadJSON(&e); err != nil {
		t.Fatalf("read: %v", err)
	}
	return e
}

func TestPublishNeverBlocksOnAStalledSubscriber(t *testing.T) {
	h := NewHub(nil, nil)
	h.queue = 64
	srv := serve(t, h)

	stalled := dial(t, srv, "/v1/ws") // subscribes and never reads again
	if err := stalled.WriteJSON(map[string]string{"op": "subscribe", "topic": "run:r1"}); err != nil {
		t.Fatal(err)
	}
	healthy := dial(t, srv, "/v1/ws")
	if err := healthy.WriteJSON(map[string]string{"op": "subscribe", "topic": "run:r2"}); err != nil {
		t.Fatal(err)
	}
	waitSubscribers(t, h, "run:r1", 1)
	waitSubscribers(t, h, "run:r2", 1)

	big := strings.Repeat("x", 32<<10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Enough to fill the socket buffers and then the 64-frame queue.
		for i := 1; i <= 2000; i++ {
			h.Publish(core.Event{Topic: "run:r1", Cursor: int64(i), Type: "run_event", Data: big})
		}
		h.Publish(core.Event{Topic: "run:r2", Cursor: 1, Type: "run_event", Data: "still here"})
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Publish blocked on a subscriber that stopped reading")
	}
	if e := read(t, healthy); e.Topic != "run:r2" || string(e.Data) != `"still here"` {
		t.Fatalf("healthy subscriber got %+v", e)
	}
	waitSubscribers(t, h, "run:r1", 0) // the stalled client was dropped
}

func TestReplayThenLiveWithoutGapsOrDuplicates(t *testing.T) {
	var h *Hub
	release := make(chan struct{})
	h = NewHub(func(ctx context.Context, topic string, after int64) ([]core.Event, error) {
		<-release // live events are published while the replay is still running
		var out []core.Event
		for c := after + 1; c <= 5; c++ {
			out = append(out, core.Event{Topic: topic, Cursor: c, Type: "message", Data: c})
		}
		return out, nil
	}, nil)
	srv := serve(t, h)
	conn := dial(t, srv, "/v1/ws")
	if err := conn.WriteJSON(map[string]any{"op": "subscribe", "topic": "channel:c", "after": 2}); err != nil {
		t.Fatal(err)
	}
	waitSubscribers(t, h, "channel:c", 1)
	h.Publish(core.Event{Topic: "channel:c", Cursor: 5, Type: "message", Data: 5}) // also in the replay
	h.Publish(core.Event{Topic: "channel:c", Cursor: 6, Type: "message", Data: 6}) // only live
	close(release)

	var got []int64
	for len(got) < 4 {
		got = append(got, read(t, conn).Cursor)
	}
	want := []int64{3, 4, 5, 6}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got cursors %v, want %v", got, want)
		}
	}
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var extra envelope
	if err := conn.ReadJSON(&extra); err == nil {
		t.Fatalf("duplicate delivered: %+v", extra)
	}
}

func TestSingleTopicStreamSendsBareFramesAndReplaysByDefault(t *testing.T) {
	var mu sync.Mutex
	var asked int64 = -1
	h := NewHub(func(ctx context.Context, topic string, after int64) ([]core.Event, error) {
		mu.Lock()
		asked = after
		mu.Unlock()
		return []core.Event{{Topic: topic, Cursor: 1, Type: "run_event", Data: map[string]int{"seq": 1}}}, nil
	}, nil)
	srv := serve(t, h)
	conn := dial(t, srv, "/v1/runs/r9/ws")
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var ev map[string]int
	if err := conn.ReadJSON(&ev); err != nil {
		t.Fatal(err)
	}
	if ev["seq"] != 1 {
		t.Fatalf("bare frame: %+v", ev)
	}
	mu.Lock()
	defer mu.Unlock()
	if asked != 0 {
		t.Fatalf("a Run stream replays from the start by default, asked after=%d", asked)
	}
}

func TestSubscribeWithoutCursorIsLiveOnly(t *testing.T) {
	called := false
	h := NewHub(func(context.Context, string, int64) ([]core.Event, error) { called = true; return nil, nil }, nil)
	srv := serve(t, h)
	conn := dial(t, srv, "/v1/ws")
	if err := conn.WriteJSON(map[string]string{"op": "subscribe", "topic": "project:p"}); err != nil {
		t.Fatal(err)
	}
	waitSubscribers(t, h, "project:p", 1)
	h.Publish(core.Event{Topic: "project:p", Cursor: 9, Type: "activity", Data: 9})
	if e := read(t, conn); e.Cursor != 9 {
		t.Fatalf("got %+v", e)
	}
	if called {
		t.Fatal("no cursor means no replay")
	}
	if err := conn.WriteJSON(map[string]string{"op": "dance"}); err != nil {
		t.Fatal(err)
	}
	if e := read(t, conn); e.Type != "error" {
		t.Fatalf("unknown op: %+v", e)
	}
}
