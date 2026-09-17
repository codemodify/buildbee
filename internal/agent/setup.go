package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/codemodify/buildbee/internal/agent/acp"
	"github.com/codemodify/buildbee/internal/config"
)

// KnownAIs are the AIs an agent can run, besides the fake one.
var KnownAIs = []string{"claude", "codex", "grok", "opencode", "goose"}

// Why an AI cannot run here.
var (
	ErrNoAI    = errors.New("the AI is not installed here")
	ErrNoLogin = errors.New("the AI is not logged in here")
	ErrNoBot   = errors.New("no such Bot")
)

// FromConfig checks that this machine can run the Bot's AI and returns the
// agent for it: the AI's command in the image (container isolation) or on
// PATH (host isolation), with its login.
func FromConfig(ctx context.Context, cfg config.Agent, log *slog.Logger) (*Agent, error) {
	if log == nil {
		log = slog.Default()
	}
	box := Container{Image: cfg.Image, Memory: cfg.Memory, CPUs: cfg.CPUs, Network: cfg.Network}
	var sandbox *Sandbox
	// Ask the Server which Bot this is: a wrong id fails now, with a name
	// people recognise on the Runs this agent takes.
	name := cfg.Name
	if bot, err := newAPI(Config{Server: cfg.ServerURL, Name: cfg.Name, BotID: cfg.BotID, AI: cfg.AI}).whichBot(ctx); err != nil {
		if errors.Is(err, errRejected) {
			return nil, fmt.Errorf("%w: no Bot %s on %s; copy the line from Add bot in # status", ErrNoBot, cfg.BotID, cfg.ServerURL)
		}
		log.Warn("could not reach the Server yet; starting anyway", "err", err)
	} else {
		name = bot.DisplayName + "@" + cfg.Host
		log.Info("running a Bot", "bot", bot.DisplayName, "role", bot.Role, "ai", cfg.AI)
	}
	switch {
	case cfg.AI == "fake": // needs neither Docker nor a login
	case cfg.Isolation == IsolationHost:
		if cfg.Sandbox != SandboxOff {
			sb, err := FindSandbox(ctx)
			switch {
			case err == nil:
				sandbox = sb
				log.Info("the AI runs on this machine, sandboxed by bubblewrap: the home is hidden, only the Run's checkout is writable")
			case cfg.Sandbox == SandboxRequire:
				return nil, fmt.Errorf("BUILDBEE_SANDBOX=require: %w", err)
			default:
				log.Warn("the AI runs on this machine unsandboxed, with this user's files and logins", "why", err)
			}
		} else {
			log.Warn("BUILDBEE_SANDBOX=off: the AI runs on this machine with this user's files and logins")
		}
		if _, err := acp.Command(cfg.AI, cfg.AICommands); err != nil {
			return nil, fmt.Errorf("%w: %v (see docs/agents.md)", ErrNoAI, err)
		}
	default: // container
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		found, err := box.Probe(probeCtx, []string{cfg.AI}, cfg.AICommands)
		cancel()
		if err != nil {
			return nil, err
		}
		if !slices.Contains(found, cfg.AI) {
			return nil, fmt.Errorf("%w: image %s has no command for %s (see docs/agents.md)", ErrNoAI, cfg.Image, cfg.AI)
		}
		in, err := box.LoggedIn(found)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(in, cfg.AI) {
			return nil, fmt.Errorf("%w: %s is not logged in for this user (%s); run its CLI once and log in", ErrNoLogin, cfg.AI, box.Home)
		}
		if err := box.RemoveLeftovers(ctx, cfg.Name); err != nil {
			return nil, err
		}
	}
	return New(Config{Server: cfg.ServerURL, Name: name, BotID: cfg.BotID, AI: cfg.AI, Host: cfg.Host, Slots: cfg.Slots,
		Isolation: cfg.Isolation, Container: box, Sandbox: sandbox, Commands: cfg.AICommands, RunTimeout: cfg.RunTimeout,
		Dir: cfg.Dir, OpenPRs: cfg.OpenPRs, Log: log})
}

// AI is what this agent runs.
func (w *Agent) AI() string { return w.cfg.AI }

// Name is what this agent calls itself on the Runs it takes.
func (w *Agent) Name() string { return w.cfg.Name }

// Goodbye tells the Server this agent is stopping.
func (w *Agent) Goodbye(ctx context.Context) {
	if err := w.api.goodbye(ctx); err != nil {
		w.log.Debug("goodbye not delivered", "err", err)
	}
}

// Sandboxed reports whether host agents run under bubblewrap.
func (w *Agent) Sandboxed() bool { return w.cfg.Sandbox != nil }
