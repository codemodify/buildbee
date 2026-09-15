// Command runtime exposes an HTTP supervisor that starts Sandbox Runs.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/codemodify/buildbee/internal/worker"
	"github.com/codemodify/buildbee/internal/worker/sandbox"
)

func main() {
	addr := ":8090"
	if v := os.Getenv("BUILDBEE_RUNTIME_ADDR"); v != "" {
		addr = v
	}
	serverURL := os.Getenv("BUILDBEE_URL")
	if serverURL == "" {
		serverURL = "http://127.0.0.1:8080"
	}

	var engine sandbox.Engine
	if os.Getenv("BUILDBEE_FAKE_SANDBOX") == "1" || !sandbox.Available() {
		log.Print("using fake Sandbox engine (no Docker or BUILDBEE_FAKE_SANDBOX=1)")
		engine = sandbox.FakeEngine{Logs: "fake sandbox (runtime service)\n"}
	} else {
		engine = sandbox.DockerEngine{}
		log.Print("using Docker Sandbox engine")
	}
	sup := worker.NewSupervisor(serverURL, engine)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /runs", func(w http.ResponseWriter, r *http.Request) {
		var in worker.Request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.TaskID == "" {
			http.Error(w, `{"error":"task_id is required"}`, http.StatusBadRequest)
			return
		}
		out, err := sup.Execute(r.Context(), in)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(out)
	})

	log.Printf("buildbee runtime listening on %s (server %s)", addr, serverURL)
	log.Fatal(http.ListenAndServe(addr, mux))
}
