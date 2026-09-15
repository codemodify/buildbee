// Package acp talks to an ACP-compatible CLI during a Run.
//
// Detection order: claude, codex, opencode, goose. If none are on PATH
// (or agent=fake), FakeACP streams plausible chunks and succeeds so e2e/CI
// work without real binaries.
package acp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// KnownAgents are ACP-oriented CLIs BuildBee will spawn when present.
var KnownAgents = []string{"claude", "codex", "opencode", "goose"}

// StreamStep is the pause between FakeACP chunks (~1–2s total outside tests).
var StreamStep = 180 * time.Millisecond

func init() {
	if testing.Testing() {
		StreamStep = 0
	}
}

// Event is one incremental ACP / log chunk posted as a RunEvent.
type Event struct {
	Kind    string
	Payload map[string]any
}

// Handler receives events as they arrive. Return an error to abort the stream.
type Handler func(Event) error

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

// ErrNoAgent means the requested agent CLI is not installed on this worker.
var ErrNoAgent = errors.New("acp: agent not installed")

// Detect resolves the agent binary to run. "fake" selects FakeACP explicitly;
// a named agent must be on PATH; "auto" (or empty) picks the first installed
// KnownAgent. A missing agent is an error — never a silent FakeACP.
func Detect(preferred string) (string, error) {
	preferred = strings.ToLower(strings.TrimSpace(preferred))
	if preferred == "fake" {
		return "fake", nil
	}
	if preferred != "" && preferred != "auto" {
		if _, err := exec.LookPath(preferred); err != nil {
			return "", fmt.Errorf("%w: %s is not on PATH", ErrNoAgent, preferred)
		}
		return preferred, nil
	}
	for _, name := range KnownAgents {
		if _, err := exec.LookPath(name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("%w: none of %s is on PATH", ErrNoAgent, strings.Join(KnownAgents, ", "))
}

// Installed lists the KnownAgents on PATH.
func Installed() []string {
	var out []string
	for _, name := range KnownAgents {
		if _, err := exec.LookPath(name); err == nil {
			out = append(out, name)
		}
	}
	return out
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

func emit(h Handler, kind string, payload map[string]any) error {
	if h == nil {
		return nil
	}
	if payload == nil {
		payload = map[string]any{}
	}
	return h(Event{Kind: kind, Payload: payload})
}

func pause(ctx context.Context) error {
	if StreamStep <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(StreamStep)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// StreamFake emits FakeACP chunks over StreamStep intervals (tokens + a tool call).
func StreamFake(ctx context.Context, agent, prompt string, h Handler) (string, error) {
	if agent == "" {
		agent = "fake"
	}
	transcript := FakeLog(agent, prompt)
	chunks := []Event{
		{Kind: "log", Payload: map[string]any{"text": "# FakeACP session\nagent: " + agent + "\n"}},
		{Kind: "token", Payload: map[string]any{"text": "Plan: "}},
		{Kind: "token", Payload: map[string]any{"text": "implement the Task in a Sandbox.\n"}},
		{Kind: "tool_call", Payload: map[string]any{"name": "read_task", "args": map[string]any{"title": firstLine(prompt)}}},
		{Kind: "tool_result", Payload: map[string]any{"name": "read_task", "ok": true, "summary": "Task context loaded"}},
		{Kind: "token", Payload: map[string]any{"text": "No ACP binary on PATH; this is the e2e FakeACP log.\n"}},
		{Kind: "token", Payload: map[string]any{"text": "Done.\n"}},
		{Kind: "log", Payload: map[string]any{"text": "status: succeeded\n"}},
	}
	for _, ev := range chunks {
		if err := ctx.Err(); err != nil {
			return transcript, err
		}
		if err := emit(h, ev.Kind, ev.Payload); err != nil {
			return transcript, err
		}
		if err := pause(ctx); err != nil {
			return transcript, err
		}
	}
	return transcript, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// Open returns a Session for the resolved agent (CLI or FakeACP).
func Open(cfg Config) (Session, string, error) {
	name, err := Detect(cfg.Agent)
	if err != nil {
		return nil, "", err
	}
	if name == "fake" {
		return &FakeSession{Agent: "fake"}, "fake", nil
	}
	return &CLISession{Bin: name, WorkDir: cfg.WorkDir}, name, nil
}

// Run starts a session, sends the prompt, and collects output (no live events).
func Run(ctx context.Context, cfg Config, prompt string) (agent, output string, err error) {
	return Stream(ctx, cfg, prompt, nil)
}

// Stream runs the agent and invokes emit for each token, tool, or log line.
func Stream(ctx context.Context, cfg Config, prompt string, emitFn Handler) (agent, output string, err error) {
	name, err := Detect(cfg.Agent)
	if err != nil {
		return "", "", err
	}
	if name == "fake" {
		out, err := StreamFake(ctx, name, prompt, emitFn)
		return name, out, err
	}
	sess := &CLISession{Bin: name, WorkDir: cfg.WorkDir, emit: emitFn}
	if err := sess.Start(ctx); err != nil {
		return name, "", err
	}
	if err := sess.Send(ctx, prompt); err != nil {
		return name, sess.out, err
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

func (s *FakeSession) Send(ctx context.Context, prompt string) error {
	s.prompt = prompt
	out, err := StreamFake(ctx, s.Agent, prompt, nil)
	s.out = out
	return err
}

func (s *FakeSession) Collect(context.Context) (string, error) {
	if s.out == "" {
		s.out = FakeLog(s.Agent, s.prompt)
	}
	return s.out, nil
}

// CLISession runs an ACP-compatible binary and streams stdout/stderr as it arrives.
type CLISession struct {
	Bin     string
	WorkDir string
	out     string
	err     error
	sent    bool
	emit    Handler
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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	var mu sync.Mutex
	writeLine := func(kind, stream, line string) {
		mu.Lock()
		buf.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			buf.WriteByte('\n')
		}
		mu.Unlock()
		payload := map[string]any{"text": line}
		if stream != "" {
			payload["stream"] = stream
		}
		_ = emit(s.emit, kind, payload)
	}
	if err := cmd.Start(); err != nil {
		s.err = err
		s.sent = true
		return nil
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanStream(stdout, func(line string) { writeLine("token", "stdout", line) })
	}()
	go func() {
		defer wg.Done()
		scanStream(stderr, func(line string) { writeLine("log", "stderr", line) })
	}()
	wg.Wait() // drain both pipes first: Wait closes them and loses unread output
	s.err = cmd.Wait()
	s.out = buf.String()
	s.sent = true
	if s.err != nil && s.out == "" {
		s.out = s.err.Error()
	}
	return nil
}

// maxLine caps one line of agent output; longer lines are truncated.
const maxLine = 1 << 20

// scanStream calls each for every line until r is exhausted. A line longer
// than maxLine is truncated with a marker and the rest of it discarded, so
// the pipe keeps draining and the agent never blocks on a full pipe.
func scanStream(r io.Reader, each func(string)) {
	br := bufio.NewReaderSize(r, 64*1024)
	var line []byte
	truncated := false
	for {
		chunk, err := br.ReadSlice('\n')
		if len(line)+len(chunk) > maxLine {
			if room := maxLine - len(line); room > 0 {
				line = append(line, chunk[:room]...)
			}
			truncated = true
		} else {
			line = append(line, chunk...)
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if len(line) > 0 {
			text := strings.TrimSuffix(string(line), "\n")
			if truncated {
				text += " …[line truncated]"
			}
			each(text + "\n")
		}
		line, truncated = line[:0], false
		if err != nil {
			return
		}
	}
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
