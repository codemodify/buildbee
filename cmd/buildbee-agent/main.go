// Command buildbee-agent plugs one Bot's AI into a BuildBee Server: it
// runs the AI's CLI (claude, codex, grok, opencode, goose) in a container
// or on this machine, takes that Bot's Runs from the Server and passes the
// conversation between them. It opens no port; it connects out.
//
//	buildbee-agent --server http://buildbee.lan:8080 --bot <id> --ai claude
//
// The Bot's id comes from Add bot in # status, which shows this line ready
// to paste. Every flag is also an environment variable (BUILDBEE_URL,
// BUILDBEE_BOT, BUILDBEE_AI, and the rest in docs/agents.md).
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/codemodify/buildbee/internal/agent"
	"github.com/codemodify/buildbee/internal/agent/acp"
	"github.com/codemodify/buildbee/internal/config"
)

func main() {
	// Speak ACP as the fake agent on stdio: for trying container isolation
	// without a real AI (BUILDBEE_AI_CLAUDE="buildbee-agent fake-agent").
	if len(os.Args) == 2 && os.Args[1] == "fake-agent" {
		acp.ServeFake(os.Stdin, os.Stdout)
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err := run(); err != nil {
		slog.Error("buildbee-agent stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// Flags set the environment the configuration reads, so a flag and its
	// variable are always the same setting.
	flags := map[string]string{
		"server":    "BUILDBEE_URL",
		"bot":       "BUILDBEE_BOT",
		"ai":        "BUILDBEE_AI",
		"name":      "BUILDBEE_AGENT_NAME",
		"slots":     "BUILDBEE_SLOTS",
		"isolation": "BUILDBEE_ISOLATION",
		"sandbox":   "BUILDBEE_SANDBOX",
		"image":     "BUILDBEE_IMAGE",
		"dir":       "BUILDBEE_DIR",
	}
	help := map[string]string{
		"server":    "Server base URL",
		"bot":       "the Bot this agent runs (from Add bot in # status)",
		"ai":        "claude, codex, grok, opencode, goose or fake",
		"name":      "how this agent names itself (default <bot>@<host>)",
		"slots":     "Runs this Bot takes at once",
		"isolation": "container (default) or host",
		"sandbox":   "auto, require or off (host isolation)",
		"image":     "image container isolation starts",
		"dir":       "where repo mirrors and Run checkouts live",
	}
	set := map[string]*string{}
	for name, env := range flags {
		set[name] = flag.String(name, "", help[name]+" ["+env+"]")
	}
	flag.Parse()
	for name, env := range flags {
		if v := strings.TrimSpace(*set[name]); v != "" {
			if err := os.Setenv(env, v); err != nil {
				return err
			}
		}
	}

	cfg, err := config.LoadAgent(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := agent.FromConfig(ctx, cfg, slog.Default())
	if err != nil {
		return err
	}
	slog.Info("buildbee-agent started", "name", a.Name(), "bot", cfg.BotID, "ai", cfg.AI, "server", cfg.ServerURL,
		"slots", cfg.Slots, "isolation", cfg.Isolation)
	a.Run(ctx)

	// Say goodbye so the Bot shows offline at once, not after its lease.
	byeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Goodbye(byeCtx)
	return nil
}
