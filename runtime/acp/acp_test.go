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

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
