package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codemodify/buildbee/runtime/sandbox"
)

func TestExecuteFakeSandbox(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/tasks/t1/runs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "r1", "task_id": "t1", "status": "pending"})
	})
	mux.HandleFunc("PATCH /v1/runs/r1", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "r1", "task_id": "t1", "status": in["status"]})
	})
	mux.HandleFunc("POST /v1/tasks/t1/artifacts", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "a1", "kind": "log", "name": "sandbox.log"})
	})
	mux.HandleFunc("POST /v1/tasks/t1/pr", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"artifact": map[string]string{"id": "a2", "kind": "pr", "url": "https://github.com/example/buildbee/pull/fake-1"}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	sup := NewSupervisor(srv.URL, sandbox.FakeEngine{Logs: "ok\n"})
	out, err := sup.Execute(context.Background(), Request{TaskID: "t1", Fake: true, FakePR: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Run.Status != "succeeded" || out.Artifact == nil || out.PR == nil {
		t.Fatalf("%#v", out)
	}
}

func TestStartRunStub(t *testing.T) {
	s := NewSupervisor("", sandbox.FakeEngine{})
	status, err := s.StartRun("run-placeholder")
	if err != nil {
		t.Fatal(err)
	}
	if status != StatusPending {
		t.Fatalf("status: got %q want %q", status, StatusPending)
	}
}
