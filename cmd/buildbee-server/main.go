// Command buildbee-server runs the BuildBee Server: REST, WebSockets,
// the Routines scheduler and the web UI.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	svc = core.New(store.New(pool), hub, core.Options{GitHub: cfg.GitHub})

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
				if n, err := svc.ReapRuns(ctx); err != nil {
					slog.Error("reap runs", "err", err)
				} else if n > 0 {
					slog.Warn("failed runs whose worker stopped heartbeating", "runs", n)
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
