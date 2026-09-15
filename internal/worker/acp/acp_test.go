package acp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetect(t *testing.T) {
	if got, err := Detect("fake"); err != nil || got != "fake" {
		t.Fatalf("fake: %q %v", got, err)
	}
	if _, err := Detect("definitely-not-an-acp-bin"); !errors.Is(err, ErrNoAgent) {
		t.Fatalf("missing named agent must be an error, got %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Detect("auto"); !errors.Is(err, ErrNoAgent) {
		t.Fatalf("auto with no agents installed must be an error, got %v", err)
	}
}

// fakeAgent installs an executable named bin on PATH that runs script.
func fakeAgent(t *testing.T, bin, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, bin), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCLISessionKeepsOutputAfterFastExit(t *testing.T) {
	// The agent writes everything into the pipe buffer and exits at once,
	// while the handler is slow (in production each line is an HTTP POST):
	// output still in the pipe when the process exits must not be lost.
	fakeAgent(t, "bbagent", "i=0; while [ $i -lt 3000 ]; do echo line $i; i=$((i+1)); done; echo FINAL SUMMARY\n")
	var events int
	_, out, err := Stream(context.Background(), Config{Agent: "bbagent"}, "do it", func(Event) error {
		events++
		if events == 1 {
			time.Sleep(300 * time.Millisecond) // the agent exits meanwhile
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "FINAL SUMMARY") || events != 3001 {
		t.Fatalf("lost output: events=%d tail=%q", events, out[max(0, len(out)-40):])
	}
}

func TestScanStreamTruncatesHugeLinesAndKeepsReading(t *testing.T) {
	huge := strings.Repeat("x", maxLine+5000)
	var lines []string
	scanStream(strings.NewReader("a\n"+huge+"\nb\n"), func(l string) { lines = append(lines, l) })
	if len(lines) != 3 || lines[0] != "a\n" || lines[2] != "b\n" {
		t.Fatalf("lines: %d %q", len(lines), lines[len(lines)-1])
	}
	if !strings.HasSuffix(lines[1], "[line truncated]\n") || len(lines[1]) > maxLine+64 {
		t.Fatalf("huge line not truncated: len=%d", len(lines[1]))
	}
}

func TestFakeSession(t *testing.T) {
	prompt := Prompt("Ship slice", "", "please take this")
	if !strings.Contains(prompt, "Ship slice") || !strings.Contains(prompt, "please take this") {
		t.Fatalf("prompt: %s", prompt)
	}
	name, out, err := Run(context.Background(), Config{Agent: "fake"}, prompt)
	if err != nil {
		t.Fatal(err)
	}
	if name != "fake" {
		t.Fatalf("agent %q", name)
	}
	if !strings.Contains(out, "FakeACP") || !strings.Contains(out, "succeeded") {
		t.Fatalf("log: %s", out)
	}
}

func TestStreamFakeEmitsChunks(t *testing.T) {
	var evs []Event
	out, err := StreamFake(context.Background(), "fake", Prompt("Ship", "", "go"), func(ev Event) error {
		evs = append(evs, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "FakeACP") {
		t.Fatalf("transcript: %s", out)
	}
	kinds := map[string]int{}
	for _, ev := range evs {
		kinds[ev.Kind]++
	}
	if len(evs) < 5 || kinds["token"] == 0 || kinds["tool_call"] == 0 || kinds["tool_result"] == 0 {
		t.Fatalf("events %#v kinds %#v", evs, kinds)
	}
}
