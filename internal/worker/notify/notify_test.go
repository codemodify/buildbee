package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFakeSuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/tasks/t1/runs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "r1", "task_id": "t1", "status": "pending"})
	})
	mux.HandleFunc("PATCH /v1/runs/r1", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "r1", "task_id": "t1", "status": "succeeded"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	run, err := New(srv.URL).FakeSuccess("t1")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "succeeded" {
		t.Fatalf("status: %q", run.Status)
	}
}
