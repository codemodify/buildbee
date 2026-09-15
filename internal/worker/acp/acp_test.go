package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The test binary doubles as an ACP agent process: BUILDBEE_ACP_HELPER
// selects how it behaves.
func TestMain(m *testing.M) {
	switch os.Getenv("BUILDBEE_ACP_HELPER") {
	case "":
		os.Exit(m.Run())
	case "fake":
		fmt.Fprintln(os.Stderr, "fake agent starting")
		ServeFake(os.Stdin, os.Stdout)
	case "crash":
		fmt.Fprintln(os.Stderr, "boom: cannot reach the model")
		os.Exit(3)
	case "auth":
		c := newConn(os.Stdout, func(method string, _ json.RawMessage) (any, *RPCError) {
			if method == "initialize" {
				return map[string]any{"protocolVersion": protocolVersion}, nil
			}
			return nil, &RPCError{Code: codeAuthRequired, Message: "Authentication required"}
		})
		<-c.listen(os.Stdin).done
	case "stuck":
		// Starts a child, answers the prompt with nothing, ignores cancel and EOF.
		child := exec.Command("sleep", "300")
		if err := child.Start(); err != nil {
			os.Exit(4)
		}
		_ = os.WriteFile(os.Getenv("BUILDBEE_ACP_PIDFILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o644)
		newConn(os.Stdout, func(method string, _ json.RawMessage) (any, *RPCError) {
			switch method {
			case "initialize":
				return map[string]any{"protocolVersion": protocolVersion}, nil
			case "session/new":
				return map[string]any{"sessionId": "s"}, nil
			}
			select {} // never answer the prompt
		}).listen(os.Stdin)
		select {}
	}
	os.Exit(0)
}

// helper returns the command that runs this test binary as an agent.
func helper(t *testing.T, mode string) []string {
	t.Setenv("BUILDBEE_ACP_HELPER", mode)
	return []string{os.Args[0], "-test.run=^$"}
}

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) emit(ev Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return nil
}

func (r *recorder) kinds() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, ev := range r.events {
		out = append(out, ev.Kind)
	}
	return out
}

func (r *recorder) find(kind string, match func(map[string]any) bool) map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ev := range r.events {
		if ev.Kind == kind && (match == nil || match(ev.Payload)) {
			return ev.Payload
		}
	}
	return nil
}

func hasText(sub string) func(map[string]any) bool {
	return func(p map[string]any) bool { s, _ := p["text"].(string); return strings.Contains(s, sub) }
}

func TestFakeAgentOverACP(t *testing.T) {
	var rec recorder
	out, err := Run(context.Background(), Config{Agent: "fake", WorkDir: t.TempDir()}, "Do this.\n\nTask: Add caching\n", rec.emit)
	if err != nil {
		t.Fatal(err)
	}
	if rec.find("token", hasText("Plan: read the Task, then report.")) == nil {
		t.Fatalf("reply chunks are merged into one event: %v", rec.events)
	}
	call := rec.find("tool_call", nil)
	if call == nil || call["name"] != "Read the Task" || call["kind"] != "read" || call["input"].(map[string]any)["task"] != "Add caching" {
		t.Fatalf("tool call: %v", call)
	}
	if res := rec.find("tool_result", nil); res == nil || res["ok"] != true || res["summary"] != "Task context loaded" {
		t.Fatalf("tool result: %v", res)
	}
	for _, kind := range []string{"thought", "plan", "usage"} {
		if rec.find(kind, nil) == nil {
			t.Fatalf("no %s event in %v", kind, rec.kinds())
		}
	}
	if rec.find("log", hasText("approved")) != nil {
		t.Fatal("the session switched to bypassPermissions, so nothing needed approval")
	}
	if !strings.Contains(out, "Plan: read the Task") || !strings.Contains(out, "[tool] Read the Task") || !strings.HasSuffix(out, "Done.\n") {
		t.Fatalf("transcript: %q", out)
	}
}

func TestPermissionRequestsAreApproved(t *testing.T) {
	fakeOptions.noBypass = true
	t.Cleanup(func() { fakeOptions.noBypass = false })
	var rec recorder
	if _, err := Run(context.Background(), Config{Agent: "fake"}, "Task: x", rec.emit); err != nil {
		t.Fatal(err)
	}
	if rec.find("log", hasText("approved: Read the Task")) == nil {
		t.Fatalf("no approval logged: %v", rec.events)
	}
	if res := rec.find("tool_result", nil); res["ok"] != true {
		t.Fatalf("approved tool must succeed: %v", res)
	}
}

func TestAuthenticatesWhenTheAgentAsks(t *testing.T) {
	fakeOptions.authenticate = true
	t.Cleanup(func() { fakeOptions.authenticate = false })
	if _, err := Run(context.Background(), Config{Agent: "fake"}, "Task: x", nil); err != nil {
		t.Fatalf("an agent that wants authenticate with its cached login: %v", err)
	}
}

func TestCancelStopsTheTurn(t *testing.T) {
	prev := StreamStep
	StreamStep = 50 * time.Millisecond
	t.Cleanup(func() { StreamStep = prev })
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	start := time.Now()
	_, err := Run(ctx, Config{Agent: "fake"}, "Task: x", func(Event) error {
		once.Do(cancel)
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("cancel took %v", d)
	}
}

func TestEmitFailureStopsTheRun(t *testing.T) {
	refused := errors.New("server refused the event")
	_, err := Run(context.Background(), Config{Agent: "fake"}, "Task: x", func(ev Event) error {
		if ev.Kind == "tool_call" {
			return refused
		}
		return nil
	})
	if !errors.Is(err, refused) {
		t.Fatalf("got %v", err)
	}
}

func TestAgentProcessOverStdio(t *testing.T) {
	var rec recorder
	out, err := Run(context.Background(), Config{Agent: "helper", Command: helper(t, "fake"), WorkDir: t.TempDir()}, "Task: y", rec.emit)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out, "Done.\n") || rec.find("log", hasText("fake agent starting")) == nil {
		t.Fatalf("out=%q events=%v", out, rec.events)
	}
}

func TestAgentCrashReportsItsLastOutput(t *testing.T) {
	_, err := Run(context.Background(), Config{Agent: "helper", Command: helper(t, "crash")}, "Task: y", nil)
	if err == nil || !strings.Contains(err.Error(), "boom: cannot reach the model") {
		t.Fatalf("got %v", err)
	}
}

func TestAuthRequiredIsExplained(t *testing.T) {
	_, err := Run(context.Background(), Config{Agent: "claude", Command: helper(t, "auth")}, "Task: y", nil)
	if !errors.Is(err, ErrNotLoggedIn) || !strings.Contains(err.Error(), "log in to claude") {
		t.Fatalf("got %v", err)
	}
}

func TestCancelKillsAStuckAgentAndItsChildren(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep binary")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	t.Setenv("BUILDBEE_ACP_PIDFILE", pidFile)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, Config{Agent: "helper", Command: helper(t, "stuck"), CancelGrace: 100 * time.Millisecond}, "Task: y", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("stopping a stuck agent took %v", d)
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(raw))
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("the agent's child %d outlived it", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCommandAndInstalled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "my-claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if _, err := Command("claude", nil); !errors.Is(err, ErrNoAgent) {
		t.Fatalf("claude without its adapter: %v", err)
	}
	over := map[string][]string{"claude": {"my-claude", "--acp"}}
	if argv, err := Command("claude", over); err != nil || argv[1] != "--acp" {
		t.Fatalf("override: %v %v", argv, err)
	}
	if got := strings.Join(Installed(over), ","); got != "claude,opencode" {
		t.Fatalf("installed: %s", got)
	}
}

func TestBypassOption(t *testing.T) {
	var opts []configOption
	raw := `[{"id":"model","type":"select","options":[{"value":"opus","name":"Opus"}]},
		{"id":"mode","type":"select","options":[{"value":"default","name":"Default"},{"value":"bypassPermissions","name":"Bypass"}]}]`
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		t.Fatal(err)
	}
	if id, v, ok := bypassOption(opts); !ok || id != "mode" || v != "bypassPermissions" {
		t.Fatalf("%s %s %v", id, v, ok)
	}
	grouped := `[{"id":"approval","type":"select","options":[{"group":"g","name":"G","options":[{"value":"full-access","name":"Full"}]}]}]`
	if err := json.Unmarshal([]byte(grouped), &opts); err != nil {
		t.Fatal(err)
	}
	if id, v, ok := bypassOption(opts); !ok || id != "approval" || v != "full-access" {
		t.Fatalf("grouped: %s %s %v", id, v, ok)
	}
}

func TestScanLinesTruncatesAndKeepsReading(t *testing.T) {
	huge := strings.Repeat("x", maxLine+5000)
	var lines []string
	scanLines(strings.NewReader("a\n"+huge+"\nb"), func(l string) { lines = append(lines, l) })
	if len(lines) != 3 || lines[0] != "a" || lines[2] != "b" || !strings.HasSuffix(lines[1], "[truncated]") {
		t.Fatalf("%d lines", len(lines))
	}
}
