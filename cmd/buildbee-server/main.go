// Command buildbee-server runs the BuildBee Server: REST, WebSockets,
// the Routines scheduler and the web UI. Agents connect to it: each Bot is
// a buildbee-agent process on whichever machine runs its AI.
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
	svc = core.New(store.New(pool), hub, core.Options{GitHub: cfg.GitHub, Blobs: blobs,
		MaxUploadBytes: cfg.MaxUploadBytes, RetentionDays: cfg.RetentionDays})

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
	srv.RegisterOnShutdown(svc.StopWaiting) // release agents' long-polls

	tickerDone := make(chan struct{})
	swept := time.Now().Add(-time.Hour) // sweep once at startup
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
				if time.Since(swept) > time.Hour {
					swept = time.Now()
					if got, err := svc.Sweep(ctx); err != nil {
						slog.Error("housekeeping", "err", err)
					} else if got != (core.Swept{}) {
						slog.Info("housekeeping", "uploads", got.Uploads, "run_events", got.RunEvents, "notifications", got.Notifications)
					}
				}
				if n, err := svc.ReapRuns(ctx); err != nil {
					slog.Error("reap runs", "err", err)
				} else if n > 0 {
					slog.Warn("failed runs whose agent stopped heartbeating", "runs", n)
				}
			}
		}
	}()

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	slog.Info("buildbee-server listening", "addr", cfg.Addr)

	select {
	case err := <-serveErr:
		stop()
		<-tickerDone
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
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
