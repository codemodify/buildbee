// Command buildbee-worker claims queued Runs from the Server and executes
// them with agent CLIs. It opens no port: point it at the Server with
// BUILDBEE_URL and run as many workers as the LAN has machines.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/codemodify/buildbee/internal/config"
	"github.com/codemodify/buildbee/internal/worker"
	"github.com/codemodify/buildbee/internal/worker/acp"
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
	agents := cfg.Agents
	switch {
	case len(agents) > 0:
	case cfg.AllowHostAgents:
		if agents = acp.Installed(cfg.AgentCommands); len(agents) == 0 {
			return errors.New("no agent's ACP command is on PATH (see docs/workers.md); install one or set BUILDBEE_WORKER_AGENTS=fake")
		}
	default:
		agents = []string{"fake"}
		slog.Warn("offering only the fake agent; set BUILDBEE_WORKER_ALLOW_HOST_AGENTS=1 to run real agent CLIs on this host")
	}
	if cfg.AllowHostAgents {
		slog.Warn("BUILDBEE_WORKER_ALLOW_HOST_AGENTS=1: agent CLIs run on this host with its files and logins")
	}
	w, err := worker.New(worker.Config{Server: cfg.ServerURL, Name: cfg.Name, Agents: agents, Slots: cfg.Slots,
		AllowHostAgents: cfg.AllowHostAgents, Commands: cfg.AgentCommands, RunTimeout: cfg.RunTimeout})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.Info("buildbee-worker started", "name", cfg.Name, "server", cfg.ServerURL, "agents", agents, "slots", cfg.Slots)
	w.Run(ctx)
	return nil
}
