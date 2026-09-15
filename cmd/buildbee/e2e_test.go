package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/client"
	"github.com/codemodify/buildbee/internal/httpapi"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/codemodify/buildbee/internal/testdb"
	"github.com/codemodify/buildbee/internal/worker/acp"
	"github.com/codemodify/buildbee/internal/ws"
	"github.com/gorilla/websocket"
)

func TestMain(m *testing.M) { testdb.Main(m) }

// TestFakeACPRunStreamsLive drives `buildbee run start --acp --agent fake`
// against a real Server on Postgres and checks that RunEvents reach a
// WebSocket subscriber while the Run is still running, then that the Run
// ends succeeded with its acp.log Artifact.
func TestFakeACPRunStreamsLive(t *testing.T) {
	prev := acp.StreamStep
	acp.StreamStep = 25 * time.Millisecond
	t.Cleanup(func() { acp.StreamStep = prev })

	srv := httptest.NewServer(httpapi.NewServer(store.NewPostgres(testdb.New(t)), ws.NewHub(), httpapi.Options{}).Handler())
	t.Cleanup(srv.Close)
	c := &client.Client{Base: srv.URL, HTTP: srv.Client(), Output: &bytes.Buffer{}}

	proj, err := c.CreateProject("Stream")
	if err != nil {
		t.Fatal(err)
	}
	var task map[string]any
	if err := doJSON(c, "POST", "/v1/projects/"+proj["id"].(string)+"/tasks?handoff=none", map[string]string{"title": "Stream the Run"}, &task); err != nil {
		t.Fatal(err)
	}
	tid := task["id"].(string)

	var runErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = run([]string{"run", "start", "--task", tid, "--acp", "--agent", "fake"}, c)
	}()

	runID := waitForRun(t, c, tid)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/v1/runs/"+runID+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	live := 0
	for {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var ev struct {
			Kind    string         `json:"kind"`
			Payload map[string]any `json:"payload"`
		}
		if err := conn.ReadJSON(&ev); err != nil {
			t.Fatalf("stream ended before the Run finished (%d live events): %v", live, err)
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
	if live < 3 {
		t.Fatalf("saw %d agent events before the Run finished, want >= 3", live)
	}

	var detail struct {
		Runs      []struct{ Status string } `json:"runs"`
		Artifacts []struct{ Name string }   `json:"artifacts"`
	}
	if err := doJSON(c, "GET", "/v1/tasks/"+tid+"/detail", nil, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].Status != "succeeded" {
		t.Fatalf("runs: %+v", detail.Runs)
	}
	var names []string
	for _, a := range detail.Artifacts {
		names = append(names, a.Name)
	}
	if !contains(names, "acp.log") {
		t.Fatalf("artifacts %v, want acp.log", names)
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
