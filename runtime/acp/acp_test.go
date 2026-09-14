package acp

import (
	"context"
	"strings"
	"testing"
)

func TestDetectFakeWhenMissing(t *testing.T) {
	if got := Detect("fake"); got != "fake" {
		t.Fatalf("got %q", got)
	}
	if got := Detect("definitely-not-an-acp-bin"); got != "fake" {
		t.Fatalf("missing binary should fake, got %q", got)
	}
	if got := Detect("auto"); got != "fake" && !contains(KnownAgents, got) {
		t.Fatalf("auto: %q", got)
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

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
