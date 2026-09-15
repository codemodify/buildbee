package sandbox

import (
	"context"
	"strings"
	"testing"
)

func TestFakeEngine(t *testing.T) {
	res, err := FakeEngine{Logs: "hello"}.Run(context.Background(), Spec{RunID: "r1", Command: "true"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Logs != "hello" || res.ExitCode != 0 {
		t.Fatalf("%#v", res)
	}
}

func TestBuildScriptClone(t *testing.T) {
	s := buildScript("https://example.test/repo.git", "echo hi")
	if !strings.Contains(s, "git clone") || !strings.Contains(s, "echo hi") {
		t.Fatal(s)
	}
}
