// Command buildbee-worker claims queued Runs from the Server and executes
// them with coding agents over ACP. It opens no port: point it at the
// Server with BUILDBEE_URL and run as many workers as the LAN has machines.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

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

// knownAgents are the real agents a worker can offer.
var knownAgents = []string{"claude", "codex", "grok", "opencode", "goose"}

func run() error {
	cfg, err := config.LoadWorker(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	box := worker.Container{Image: cfg.Image, Memory: cfg.Memory, CPUs: cfg.CPUs, Network: cfg.Network}
	agents := cfg.Agents
	onlyFake := len(agents) > 0 && !slices.ContainsFunc(agents, func(a string) bool { return a != "fake" })
	switch {
	case onlyFake:
	case cfg.Isolation == worker.IsolationHost:
		slog.Warn("BUILDBEE_WORKER_ISOLATION=host: agents run on this machine with this user's files and logins")
		if len(agents) == 0 {
			if agents = acp.Installed(cfg.AgentCommands); len(agents) == 0 {
				return errors.New("no agent's ACP command is on PATH (see docs/workers.md); install one or set BUILDBEE_WORKER_AGENTS=fake")
			}
		}
	default: // container
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		found, err := box.Probe(probeCtx, knownAgents, cfg.AgentCommands)
		cancel()
		if err != nil {
			return err
		}
		if len(agents) == 0 {
			agents = found
		}
		for _, a := range agents {
			if a != "fake" && !slices.Contains(found, a) {
				return errors.New("agent " + a + " has no ACP command in image " + cfg.Image + " (see docs/workers.md)")
			}
		}
		if len(agents) == 0 {
			return errors.New("image " + cfg.Image + " has no agent's ACP command (see docs/workers.md)")
		}
		if err := box.RemoveLeftovers(ctx, cfg.Name); err != nil {
			return err
		}
	}
	w, err := worker.New(worker.Config{Server: cfg.ServerURL, Name: cfg.Name, Agents: agents, Slots: cfg.Slots,
		Isolation: cfg.Isolation, Container: box, Commands: cfg.AgentCommands, RunTimeout: cfg.RunTimeout,
		Dir: cfg.Dir, OpenPRs: cfg.OpenPRs})
	if err != nil {
		return err
	}
	slog.Info("buildbee-worker started", "name", cfg.Name, "server", cfg.ServerURL, "agents", agents,
		"slots", cfg.Slots, "isolation", cfg.Isolation)
	w.Run(ctx)
	return nil
}
