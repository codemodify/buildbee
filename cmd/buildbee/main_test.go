package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/codemodify/buildbee/internal/client"
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
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "p1", "name": "Project"})
	})
	mux.HandleFunc("PATCH /v1/projects/p1", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["auto_run"] != true || in["merge_policy"] != "approval" || in["instructions"] != "" {
			t.Errorf("project update sent %v", in)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "p1", "auto_run": true})
	})
	mux.HandleFunc("POST /v1/projects/p1/tasks", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "t9", "title": in["title"]})
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
	mux.HandleFunc("GET /v1/runs/r1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "r1", "status": "running"})
	})
	mux.HandleFunc("PATCH /v1/runs/r1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "r1", "status": "canceled"})
	})
	mux.HandleFunc("GET /v1/projects/p1/routines", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"id": "rt1", "name": "morning-digest"}}})
	})
	mux.HandleFunc("POST /v1/projects/p1/routines", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["prompt"] != "Bump deps" || in["enabled"] != true {
			t.Errorf("routine create sent %v", in)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rt2"})
	})
	mux.HandleFunc("POST /v1/routines/rt1/run", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rt1", "name": "morning-digest"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var buf bytes.Buffer
	c := &client.Client{Base: srv.URL, HTTP: srv.Client(), Output: &buf}
	if err := run([]string{"project", "create", "--name", "Project"}, c); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run([]string{"project", "update", "--id", "p1", "--autopilot", "on", "--merge-policy", "approval", "--instructions="}, c); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run([]string{"task", "create", "--project", "p1", "--title", "Ship it"}, c); err != nil || !strings.Contains(buf.String(), "t9") {
		t.Fatalf("task create: %v %s", err, buf.String())
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
	for _, args := range [][]string{
		{"run", "start", "--task", "t1", "--agent", "fake"},
		{"run", "show", "--id", "r1"},
		{"run", "cancel", "--id", "r1"},
	} {
		if err := run(args, c); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.Contains(buf.String(), `"r1"`) {
			t.Fatalf("%v printed %s", args, buf.String())
		}
		buf.Reset()
	}
	if err := run([]string{"handoff", "create", "--task", "t1", "--from", "a", "--to-role", "builder", "--autorun"}, c); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run([]string{"routine", "list", "--project", "p1"}, c); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := run([]string{"routine", "create", "--project", "p1", "--name", "deps", "--prompt", "Bump deps", "--enabled"}, c); err != nil {
		t.Fatal(err)
	}
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
