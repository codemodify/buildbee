// Package acp talks to an ACP-compatible CLI during a Run.
//
// Detection order: claude, codex, opencode, goose. If none are on PATH
// (or agent=fake), FakeACP writes a plausible log and succeeds so e2e/CI
// work without real binaries.
package acp

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// KnownAgents are ACP-oriented CLIs BuildBee will spawn when present.
var KnownAgents = []string{"claude", "codex", "opencode", "goose"}

// Session is a short-lived ACP conversation: start, send one prompt, collect output.
type Session interface {
	Start(ctx context.Context) error
	Send(ctx context.Context, prompt string) error
	Collect(ctx context.Context) (string, error)
}

// Config selects which agent to use (claude|codex|opencode|goose|fake|auto).
type Config struct {
	Agent   string
	WorkDir string
}

// Detect returns the agent binary name to run. "fake" if none are available.
func Detect(preferred string) string {
	preferred = strings.ToLower(strings.TrimSpace(preferred))
	if preferred == "fake" {
		return "fake"
	}
	if preferred != "" && preferred != "auto" {
		if _, err := exec.LookPath(preferred); err == nil {
			return preferred
		}
		return "fake"
	}
	for _, name := range KnownAgents {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return "fake"
}

// Prompt builds the text sent to the agent from a Task + Handoff notes.
func Prompt(title, body, notes string) string {
	var b strings.Builder
	b.WriteString("You are a BuildBee Bot executing a Task in a Sandbox.\n")
	if title != "" {
		b.WriteString("Task: ")
		b.WriteString(title)
		b.WriteString("\n")
	}
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	if notes != "" {
		b.WriteString("Handoff notes: ")
		b.WriteString(notes)
		b.WriteString("\n")
	}
	b.WriteString("Implement the Task, then summarize what you did.\n")
	return b.String()
}

// FakeLog is the Artifact body FakeACP writes when no CLI is present.
func FakeLog(agent, prompt string) string {
	return fmt.Sprintf(`# FakeACP session
agent: %s
status: succeeded
---
prompt:
%s
---
Plan: implement the Task in a Sandbox.
No ACP binary on PATH; this is the e2e FakeACP log.
Done.
`, agent, strings.TrimSpace(prompt))
}

// Open returns a Session for the resolved agent (CLI or FakeACP).
func Open(cfg Config) (Session, string) {
	name := Detect(cfg.Agent)
	if name == "fake" {
		return &FakeSession{Agent: "fake"}, "fake"
	}
	return &CLISession{Bin: name, WorkDir: cfg.WorkDir}, name
}

// Run starts a session, sends the prompt, and collects output.
func Run(ctx context.Context, cfg Config, prompt string) (agent, output string, err error) {
	sess, name := Open(cfg)
	if err := sess.Start(ctx); err != nil {
		return name, "", err
	}
	if err := sess.Send(ctx, prompt); err != nil {
		return name, "", err
	}
	out, err := sess.Collect(ctx)
	return name, out, err
}

// FakeSession writes a plausible log without spawning a process.
type FakeSession struct {
	Agent  string
	prompt string
	out    string
}

func (s *FakeSession) Start(context.Context) error { return nil }

func (s *FakeSession) Send(_ context.Context, prompt string) error {
	s.prompt = prompt
	s.out = FakeLog(s.Agent, prompt)
	return nil
}

func (s *FakeSession) Collect(context.Context) (string, error) {
	if s.out == "" {
		s.out = FakeLog(s.Agent, s.prompt)
	}
	return s.out, nil
}

// CLISession runs an ACP-compatible binary with a non-interactive prompt.
type CLISession struct {
	Bin     string
	WorkDir string
	out     string
	err     error
	sent    bool
}

func (s *CLISession) Start(_ context.Context) error {
	if _, err := exec.LookPath(s.Bin); err != nil {
		return fmt.Errorf("acp: %s not on PATH", s.Bin)
	}
	return nil
}

func (s *CLISession) Send(ctx context.Context, prompt string) error {
	args := cliArgs(s.Bin, prompt)
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, s.Bin, args...)
	if s.WorkDir != "" {
		cmd.Dir = s.WorkDir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	s.err = cmd.Run()
	s.out = buf.String()
	s.sent = true
	if s.err != nil && s.out == "" {
		s.out = s.err.Error()
	}
	return nil
}

func (s *CLISession) Collect(context.Context) (string, error) {
	if !s.sent {
		return "", fmt.Errorf("acp: Send was not called")
	}
	return s.out, s.err
}

func cliArgs(bin, prompt string) []string {
	switch bin {
	case "claude":
		return []string{"-p", prompt}
	case "codex":
		return []string{"exec", prompt}
	case "opencode":
		return []string{"run", prompt}
	case "goose":
		return []string{"run", "-t", prompt}
	default:
		return []string{prompt}
	}
}
