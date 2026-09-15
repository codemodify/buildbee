// Command buildbee-server runs the BuildBee Server: REST, WebSockets,
// the Routines scheduler and the web UI. Unless BUILDBEE_LOCAL_WORKER=off
// it also runs agents itself when this machine can (see docs/workers.md).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/codemodify/buildbee/internal/blob"
	"github.com/codemodify/buildbee/internal/config"
	"github.com/codemodify/buildbee/internal/core"
	"github.com/codemodify/buildbee/internal/httpapi"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/codemodify/buildbee/internal/worker"
	"github.com/codemodify/buildbee/internal/ws"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("buildbee-server stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadServer(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	pool, err := store.Open(connectCtx, cfg.DatabaseURL)
	if err == nil {
		err = store.Migrate(connectCtx, pool)
	}
	cancel()
	if err != nil {
		return err
	}
	defer pool.Close()
	slog.Info("postgres connected; migrations applied")

	// The hub replays through the service and the service publishes through
	// the hub.
	var svc *core.Service
	hub := ws.NewHub(func(ctx context.Context, topic string, after int64) ([]core.Event, error) {
		return svc.Replay(ctx, topic, after)
	}, nil)
	blobs, err := openBlobs(ctx, cfg)
	if err != nil {
		return err
	}
	slog.Info("files stored in", "blobs", blobs.Name())
	svc = core.New(store.New(pool), hub, core.Options{GitHub: cfg.GitHub, Blobs: blobs, MaxUploadBytes: cfg.MaxUploadBytes})

	api := httpapi.NewServer(svc, hub, httpapi.Options{
		GitHub:       cfg.GitHub,
		WebDir:       cfg.WebDir,
		MaxBodyBytes: cfg.MaxBodyBytes,
	})
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	srv.RegisterOnShutdown(svc.StopWaiting) // release workers' long-polls

	tickerDone := make(chan struct{})
	go func() {
		defer close(tickerDone)
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				svc.TickRoutines(ctx)
				if n, err := svc.SweepUploads(ctx); err != nil {
					slog.Error("sweep uploads", "err", err)
				} else if n > 0 {
					slog.Info("deleted uploads never posted", "files", n)
				}
				if n, err := svc.ReapRuns(ctx); err != nil {
					slog.Error("reap runs", "err", err)
				} else if n > 0 {
					slog.Warn("failed runs whose worker stopped heartbeating", "runs", n)
				}
			}
		}
	}()

	// The local worker talks to this Server over loopback, so it starts
	// once the listener is up; "on" checks first and refuses to start.
	var local *worker.Worker
	localCfg, err := config.LocalWorker(os.Getenv, cfg.Addr)
	if err != nil && cfg.LocalWorker != config.LocalOff {
		return fmt.Errorf("local worker: %w", err)
	}
	if cfg.LocalWorker == config.LocalOn {
		if local, err = startLocal(ctx, &localCfg); err != nil {
			return fmt.Errorf("BUILDBEE_LOCAL_WORKER=on: %w", err)
		}
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	slog.Info("buildbee-server listening", "addr", cfg.Addr)

	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		if cfg.LocalWorker == config.LocalOff {
			svc.SetLocalWorker(core.LocalWorker{State: "off", Reason: "Turned off (BUILDBEE_LOCAL_WORKER=off)."})
			return
		}
		if local == nil {
			svc.SetLocalWorker(core.LocalWorker{State: "starting", Name: localCfg.Name})
			var err error
			if local, err = startLocal(ctx, &localCfg); err != nil {
				if ctx.Err() == nil {
					slog.Info("not running agents on this machine", "reason", err)
					svc.SetLocalWorker(core.LocalWorker{State: "unavailable", Reason: localReason(err), Name: localCfg.Name})
				}
				return
			}
		}
		slog.Info("running agents on this machine", "worker", localCfg.Name, "agents", local.Agents(), "slots", localCfg.Slots)
		svc.SetLocalWorker(core.LocalWorker{State: "running", Name: localCfg.Name, Isolation: localCfg.Isolation})
		local.Run(ctx)
	}()

	select {
	case err := <-serveErr:
		stop()
		<-tickerDone
		<-workerDone
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	// The local worker reports the Runs it stops before the Server goes.
	select {
	case <-workerDone:
	case <-time.After(15 * time.Second):
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	hub.Close()
	<-tickerDone
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// openBlobs returns where attachments and large Artifacts go: the S3
// bucket when one is configured (created if missing), else a directory.
func openBlobs(ctx context.Context, cfg config.Server) (blob.Store, error) {
	if cfg.S3.Endpoint != "" {
		st := &blob.S3{Endpoint: cfg.S3.Endpoint, Bucket: cfg.S3.Bucket, Region: cfg.S3.Region,
			AccessKey: cfg.S3.AccessKey, SecretKey: cfg.S3.SecretKey}
		bctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := st.EnsureBucket(bctx); err != nil {
			return nil, fmt.Errorf("S3 bucket %s: %w", cfg.S3.Bucket, err)
		}
		return st, nil
	}
	if err := os.MkdirAll(cfg.BlobDir, 0o750); err != nil {
		return nil, fmt.Errorf("BUILDBEE_BLOB_DIR: %w", err)
	}
	return blob.Dir{Root: cfg.BlobDir}, nil
}

// startLocal prepares the Server's own worker: agents in containers when
// Docker and the agent image are here, otherwise directly on this machine,
// unless BUILDBEE_WORKER_ISOLATION pins one.
func startLocal(ctx context.Context, cfg *config.Worker) (*worker.Worker, error) {
	w, err := worker.FromConfig(ctx, *cfg, slog.Default())
	pinned := os.Getenv("BUILDBEE_WORKER_ISOLATION") != ""
	if err == nil || pinned || cfg.Isolation != worker.IsolationContainer ||
		!(errors.Is(err, worker.ErrNoDocker) || errors.Is(err, worker.ErrNoImage) || errors.Is(err, worker.ErrNoAgents)) {
		return w, err
	}
	slog.Info("no agent containers here; agents run directly on this machine", "why", err)
	cfg.Isolation = worker.IsolationHost
	return worker.FromConfig(ctx, *cfg, slog.Default())
}

// localReason says in a line why this machine runs no agents.
func localReason(err error) string {
	switch {
	case errors.Is(err, worker.ErrNoDocker):
		return "Docker isn't installed on the server's machine."
	case errors.Is(err, worker.ErrNoImage):
		return "The agent image isn't built. On the server's machine run: make agents-image"
	case errors.Is(err, worker.ErrNoAgents):
		return "No agent is installed and logged in for the server's user. Install one (docs/workers.md), log in, then restart the server."
	}
	return err.Error()
}
