package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codemodify/buildbee/cli/internal/client"
)

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must be set")
	}
	c := client.New()
	var buf bytes.Buffer
	c.Output = &buf
	if err := run([]string{"version"}, c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "buildbee") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestRunUnknown(t *testing.T) {
	if err := run([]string{"not-a-command"}, client.New()); err == nil {
		t.Fatal("expected error")
	}
}

func TestProjectCreateAndTaskList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/projects", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "p1", "name": "Hive"})
	})
	mux.HandleFunc("GET /v1/projects/p1/tasks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	})
	mux.HandleFunc("GET /v1/tasks/t1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "t1", "title": "Ship"})
	})
	mux.HandleFunc("POST /v1/tasks/t1/handoffs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "h1", "status": "open"})
	})
	mux.HandleFunc("POST /v1/tasks/t1/runs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "r1", "task_id": "t1", "status": "pending"})
	})
	mux.HandleFunc("PATCH /v1/runs/r1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "r1", "status": "succeeded"})
	})
	mux.HandleFunc("POST /v1/runs/r1/events", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "e1", "run_id": "r1", "seq": 1, "kind": "token"})
	})
	mux.HandleFunc("POST /v1/tasks/t1/artifacts", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "a1", "kind": "log"})
	})
	mux.HandleFunc("POST /v1/tasks/t1/pr", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"artifact": map[string]any{"id": "a2", "kind": "pr"}})
	})
	mux.HandleFunc("GET /v1/projects/p1/routines", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"id": "rt1", "name": "morning-digest"}}})
	})
	mux.HandleFunc("POST /v1/routines/rt1/run", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rt1", "name": "morning-digest"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	c := &client.Client{Base: srv.URL, HTTP: srv.Client(), Output: &buf}
	if err := run([]string{"project", "create", "--name", "Hive"}, c); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run([]string{"task", "list", "--project", "p1"}, c); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run([]string{"handoff", "create", "--task", "t1", "--from", "a", "--to", "b", "--note", "go"}, c); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run([]string{"run", "start", "--task", "t1", "--fake"}, c); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run([]string{"run", "start", "--task", "t1", "--acp", "--agent", "fake"}, c); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "acp.log") && !strings.Contains(buf.String(), "FakeACP") && !strings.Contains(buf.String(), "acp_agent") {
		t.Fatalf("acp output: %s", buf.String())
	}
	buf.Reset()
	if err := run([]string{"handoff", "create", "--task", "t1", "--from", "a", "--to-role", "builder", "--autorun"}, c); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run([]string{"routine", "list", "--project", "p1"}, c); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run([]string{"routine", "run", "--id", "rt1"}, c); err != nil {
		t.Fatal(err)
	}
}

func TestMissingFlags(t *testing.T) {
	c := &client.Client{Output: io.Discard}
	if err := run([]string{"project", "create"}, c); err == nil {
		t.Fatal("expected error")
	}
}
