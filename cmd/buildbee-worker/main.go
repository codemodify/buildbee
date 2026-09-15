// Command buildbee-worker executes Runs (Docker Sandbox or ACP agent) and
// reports status, events and Artifacts to the Server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/codemodify/buildbee/internal/config"
	"github.com/codemodify/buildbee/internal/worker"
	"github.com/codemodify/buildbee/internal/worker/sandbox"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("buildbee-worker stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadWorker(os.Getenv)
	if err != nil {
		return err
	}
	var engine sandbox.Engine
	switch {
	case cfg.FakeSandbox:
		slog.Warn("BUILDBEE_FAKE_SANDBOX=1: Runs use the fake Sandbox engine")
		engine = sandbox.FakeEngine{Logs: "fake sandbox (worker)\n"}
	case !sandbox.Available():
		return errors.New("docker is not on PATH; install it or set BUILDBEE_FAKE_SANDBOX=1")
	default:
		engine = sandbox.DockerEngine{}
	}
	sup := worker.NewSupervisor(cfg.ServerURL, engine)
	sup.AllowHostAgents = cfg.AllowHostAgents
	if cfg.AllowHostAgents {
		slog.Warn("BUILDBEE_WORKER_ALLOW_HOST_AGENTS=1: agent CLIs run on this host with its credentials")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /runs", func(w http.ResponseWriter, r *http.Request) {
		var in worker.Request
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil || in.TaskID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id is required"})
			return
		}
		out, err := sup.Execute(r.Context(), in)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, out)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := &http.Server{Addr: cfg.Addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	slog.Info("buildbee-worker listening", "addr", cfg.Addr, "server", cfg.ServerURL)

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
