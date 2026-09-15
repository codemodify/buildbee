package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/codemodify/buildbee/internal/config"
	"github.com/codemodify/buildbee/internal/worker/acp"
)

// KnownAgents are the real agents a worker can offer.
var KnownAgents = []string{"claude", "codex", "grok", "opencode", "goose"}

// ErrNoAgents means no agent can run here: none installed, or, in a
// container, none logged in.
var ErrNoAgents = errors.New("no agents")

// FromConfig checks what this machine can run and returns a worker for it.
// Without BUILDBEE_WORKER_AGENTS it offers every agent it finds; in a
// container, only those the worker user is logged in to.
func FromConfig(ctx context.Context, cfg config.Worker, log *slog.Logger) (*Worker, error) {
	if log == nil {
		log = slog.Default()
	}
	box := Container{Image: cfg.Image, Memory: cfg.Memory, CPUs: cfg.CPUs, Network: cfg.Network}
	var sandbox *Sandbox
	agents := cfg.Agents
	onlyFake := len(agents) > 0 && !slices.ContainsFunc(agents, func(a string) bool { return a != "fake" })
	switch {
	case onlyFake:
	case cfg.Isolation == IsolationHost:
		if cfg.Sandbox != SandboxOff {
			sb, err := FindSandbox(ctx)
			switch {
			case err == nil:
				sandbox = sb
				log.Info("agents run on this machine, sandboxed by bubblewrap: the home is hidden, only the Run's checkout is writable")
			case cfg.Sandbox == SandboxRequire:
				return nil, fmt.Errorf("BUILDBEE_WORKER_SANDBOX=require: %w", err)
			default:
				log.Warn("agents run on this machine unsandboxed, with this user's files and logins", "why", err)
			}
		} else {
			log.Warn("BUILDBEE_WORKER_SANDBOX=off: agents run on this machine with this user's files and logins")
		}
		if len(agents) == 0 {
			if agents = acp.Installed(cfg.AgentCommands); len(agents) == 0 {
				return nil, fmt.Errorf("%w: no agent's ACP command is on PATH (see docs/workers.md); install one or set BUILDBEE_WORKER_AGENTS=fake", ErrNoAgents)
			}
		}
	default: // container
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		found, err := box.Probe(probeCtx, KnownAgents, cfg.AgentCommands)
		cancel()
		if err != nil {
			return nil, err
		}
		if len(agents) == 0 {
			if agents, err = box.LoggedIn(found); err != nil {
				return nil, err
			}
			if skipped := slices.DeleteFunc(slices.Clone(found), func(a string) bool { return slices.Contains(agents, a) }); len(skipped) > 0 {
				log.Info("agents in the image with no login here; log in to offer them", "agents", skipped, "home", box.Home)
			}
			if len(agents) == 0 {
				return nil, fmt.Errorf("%w: none of %v is logged in for this user (%s); log in with the agent's CLI (docs/workers.md)", ErrNoAgents, found, box.Home)
			}
		}
		for _, a := range agents {
			if a != "fake" && !slices.Contains(found, a) {
				return nil, errors.New("agent " + a + " has no ACP command in image " + cfg.Image + " (see docs/workers.md)")
			}
		}
		if err := box.RemoveLeftovers(ctx, cfg.Name); err != nil {
			return nil, err
		}
	}
	return New(Config{Server: cfg.ServerURL, Name: cfg.Name, Agents: agents, Slots: cfg.Slots,
		Isolation: cfg.Isolation, Container: box, Sandbox: sandbox, Commands: cfg.AgentCommands, RunTimeout: cfg.RunTimeout,
		Dir: cfg.Dir, OpenPRs: cfg.OpenPRs, Log: log})
}

// Agents are the agents this worker offers.
func (w *Worker) Agents() []string { return slices.Clone(w.cfg.Agents) }

// Sandboxed reports whether host agents run under bubblewrap.
func (w *Worker) Sandboxed() bool { return w.cfg.Sandbox != nil }
