// Command buildbee-worker claims queued Runs from the Server and executes
// them with coding agents over ACP. It opens no port: point it at the
// Server with BUILDBEE_URL and run as many workers as the LAN has machines.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codemodify/buildbee/internal/config"
	"github.com/codemodify/buildbee/internal/worker"
	"github.com/codemodify/buildbee/internal/worker/acp"
)

func main() {
	// Speak ACP as the fake agent on stdio: for trying container isolation
	// without a real agent (BUILDBEE_AGENT_CLAUDE="buildbee-worker fake-agent").
	if len(os.Args) == 2 && os.Args[1] == "fake-agent" {
		acp.ServeFake(os.Stdin, os.Stdout)
		return
	}
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	w, err := worker.FromConfig(ctx, cfg, slog.Default())
	if err != nil {
		return err
	}
	slog.Info("buildbee-worker started", "name", cfg.Name, "server", cfg.ServerURL, "agents", w.Agents(),
		"slots", cfg.Slots, "isolation", cfg.Isolation)
	w.Run(ctx)
	return nil
}
