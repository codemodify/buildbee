package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/agent"
	"github.com/codemodify/buildbee/internal/agent/acp"
	"github.com/codemodify/buildbee/internal/client"
	"github.com/codemodify/buildbee/internal/core"
	"github.com/codemodify/buildbee/internal/httpapi"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/codemodify/buildbee/internal/testdb"
	"github.com/codemodify/buildbee/internal/ws"
	"github.com/gorilla/websocket"
)

func TestMain(m *testing.M) { testdb.Main(m) }

// TestRunThroughAgent drives `buildbee run start --follow` against a real
// Server on Postgres with an in-process agent running the fake AI. It
// checks that the Run is claimed, that RunEvents reach a WebSocket
// subscriber while the Run is still running, that --follow prints the
// agent's output, and that the Run ends succeeded with its transcript.
func TestRunThroughAgent(t *testing.T) {
	prev, prevFollow := acp.StreamStep, client.FollowInterval
	acp.StreamStep, client.FollowInterval = 25*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { acp.StreamStep, client.FollowInterval = prev, prevFollow })

	var svc *core.Service
	hub := ws.NewHub(func(ctx context.Context, topic string, after int64) ([]core.Event, error) {
		return svc.Replay(ctx, topic, after)
	}, nil)
	svc = core.New(store.New(testdb.New(t)), hub, core.Options{})
	srv := httptest.NewServer(httpapi.NewServer(svc, hub, httpapi.Options{}).Handler())
	t.Cleanup(srv.Close)
	t.Cleanup(hub.Close)
	var out bytes.Buffer
	c := &client.Client{Base: srv.URL, As: "Ada", HTTP: srv.Client(), Output: &out}

	proj, err := c.CreateProject("Stream", true)
	if err != nil {
		t.Fatal(err)
	}
	// The Bot whose agent runs below.
	var bot map[string]any
	if err := doJSON(c, "POST", "/v1/projects/"+proj["id"].(string)+"/members",
		map[string]string{"kind": "bot", "display_name": "Runner", "role": "runner", "agent": "fake"}, &bot); err != nil {
		t.Fatal(err)
	}
	botID := bot["id"].(string)
	var task map[string]any
	if err := doJSON(c, "POST", "/v1/projects/"+proj["id"].(string)+"/tasks", map[string]string{"title": "Stream the Run", "handoff_role": "none"}, &task); err != nil {
		t.Fatal(err)
	}
	tid := task["id"].(string)

	// Subscribe before the agent exists so no event can be missed.
	var runErr error
	var wg sync.WaitGroup
	wg.Go(func() { runErr = run([]string{"run", "start", "--task", tid, "--agent", "fake", "--follow"}, c) })
	runID := waitForRun(t, c, tid)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/runs/"+runID+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	wk, err := agent.New(agent.Config{Server: srv.URL, Name: "Builder@test", BotID: botID, AI: "fake", Slots: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() { defer close(workerDone); wk.Run(ctx) }()
	t.Cleanup(func() { stop(); <-workerDone })

	live, claimed := 0, false
	for {
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		var ev struct {
			Kind    string         `json:"kind"`
			Payload map[string]any `json:"payload"`
		}
		if err := conn.ReadJSON(&ev); err != nil {
			t.Fatalf("stream ended before the Run finished (%d live events): %v", live, err)
		}
		if ev.Kind == "status" && strings.Contains(fmt.Sprint(ev.Payload["detail"]), "claimed by Builder@test") {
			claimed = true
		}
		if ev.Kind == "status" && (ev.Payload["status"] == "succeeded" || ev.Payload["status"] == "failed") {
			if ev.Payload["status"] != "succeeded" {
				t.Fatalf("run finished %v", ev.Payload)
			}
			break
		}
		if ev.Kind == "token" || ev.Kind == "tool_call" || ev.Kind == "tool_result" {
			live++
		}
	}
	wg.Wait()
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !claimed || live < 3 {
		t.Fatalf("claimed=%v, saw %d agent events before the Run finished, want >= 3", claimed, live)
	}
	if !strings.Contains(out.String(), "Done.") {
		t.Fatalf("--follow printed %q, want the agent's output", out.String())
	}

	var detail struct {
		Runs []struct {
			Status string `json:"status"`
			Worker string `json:"worker"`
		} `json:"runs"`
		Artifacts []struct{ Name string } `json:"artifacts"`
	}
	if err := doJSON(c, "GET", "/v1/tasks/"+tid+"/detail", nil, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].Status != "succeeded" || detail.Runs[0].Worker != "Builder@test" {
		t.Fatalf("runs: %+v", detail.Runs)
	}
	var names []string
	for _, a := range detail.Artifacts {
		names = append(names, a.Name)
	}
	if !contains(names, "fake.log") {
		t.Fatalf("artifacts %v, want fake.log", names)
	}
}

func waitForRun(t *testing.T, c *client.Client, taskID string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var runs struct {
			Items []struct{ ID string } `json:"items"`
		}
		if err := doJSON(c, "GET", "/v1/tasks/"+taskID+"/runs", nil, &runs); err == nil && len(runs.Items) > 0 {
			return runs.Items[0].ID
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no Run appeared")
	return ""
}

func doJSON(c *client.Client, method, path string, body, dest any) error {
	var rdr *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, c.Base+path, rdr)
	req.RequestURI = ""
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BuildBee-As", c.As)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return json.NewDecoder(res.Body).Decode(dest)
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
