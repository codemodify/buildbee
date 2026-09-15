package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/models"
)

func TestContainerArgs(t *testing.T) {
	home := t.TempDir()
	for _, p := range []string{".claude", ".codex"} {
		if err := os.Mkdir(filepath.Join(home, p), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	c := Container{Image: "agents:1", Home: home, Mounts: []string{"/cache:/cache:ro"}, Network: "agents"}
	if err := c.defaults(); err != nil {
		t.Fatal(err)
	}
	job := Job{RunID: "r1", Agent: "claude", Dir: "/w/runs/r1/work", Objects: "/w/mirrors/m.git/objects"}
	args := strings.Join(c.args("box", job, "/tmp/h", []string{"claude-agent-acp"}, c.Image), " ")
	for _, want := range []string{
		"run --rm -i --init --name buildbee-run-r1",
		"--label buildbee.worker=box --label buildbee.run=r1",
		"--cap-drop ALL --security-opt no-new-privileges",
		"--memory 8g --cpus 4 --pids-limit 1024",
		"--volume /tmp/h:/home/agent",
		"--env TMPDIR=/home/agent/tmp --env CLAUDE_CODE_TMPDIR=/home/agent/tmp",
		"--volume /w/runs/r1/work:/w/runs/r1/work --workdir /w/runs/r1/work",
		"--volume /w/mirrors/m.git/objects:/w/mirrors/m.git/objects:ro",
		"--volume /cache:/cache:ro",
		"--network agents agents:1 claude-agent-acp",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("missing %q in\n%s", want, args)
		}
	}
	for _, not := range []string{".claude", ".codex", "docker.sock", "--privileged", "/w/mirrors/m.git:"} {
		if strings.Contains(args, not) {
			t.Errorf("a claude Run must not get %q:\n%s", not, args)
		}
	}
}

func TestLoginsAreCopiedAndOnlyRefreshedTokensGoBack(t *testing.T) {
	home, scratch := t.TempDir(), t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".claude/.credentials.json"), `{"token":"old"}`)
	write(filepath.Join(home, ".claude.json"), `{"mcpServers":{}}`)
	write(filepath.Join(home, ".claude/settings.json"), `{}`)
	c := Container{Image: "x", Home: home}
	logins, err := c.copyLogins("claude", scratch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(scratch, ".claude/settings.json")); err == nil {
		t.Fatal("only login files are copied, not the agent's settings")
	}
	// The agent refreshes its token and tampers with its config.
	write(filepath.Join(scratch, ".claude/.credentials.json"), `{"token":"new"}`)
	write(filepath.Join(scratch, ".claude.json"), `{"mcpServers":{"evil":{"command":"sh"}}}`)
	logins.writeBack()
	if got, _ := os.ReadFile(filepath.Join(home, ".claude/.credentials.json")); string(got) != `{"token":"new"}` {
		t.Fatalf("refreshed token not kept: %s", got)
	}
	if got, _ := os.ReadFile(filepath.Join(home, ".claude.json")); string(got) != `{"mcpServers":{}}` {
		t.Fatalf("config changes must not reach the host: %s", got)
	}

	// A token written as garbage, or a host file someone else changed, is left alone.
	logins, _ = c.copyLogins("claude", t.TempDir())
	write(logins.files[0].copy, `not json`)
	logins.writeBack()
	logins2, _ := c.copyLogins("claude", t.TempDir())
	write(filepath.Join(home, ".claude/.credentials.json"), `{"token":"other run"}`)
	write(logins2.files[0].copy, `{"token":"stale"}`)
	logins2.writeBack()
	if got, _ := os.ReadFile(filepath.Join(home, ".claude/.credentials.json")); string(got) != `{"token":"other run"}` {
		t.Fatalf("got %s", got)
	}
}

// dockerImage returns a local Alpine image, skipping the test when Docker
// or the image is missing. Tests never pull images.
func dockerImage(t *testing.T) string {
	t.Helper()
	if testing.Short() || runtime.GOOS != "linux" {
		t.Skip("needs Docker on Linux")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Docker is not available")
	}
	for _, img := range []string{"alpine:3.24", "alpine:latest", "alpine:3.20"} {
		if exec.Command("docker", "image", "inspect", img).Run() == nil {
			return img
		}
	}
	t.Skip("no local alpine image")
	return ""
}

// workerBinary builds a static buildbee-worker, whose fake-agent mode
// speaks ACP inside the container.
func workerBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "buildbee-worker")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/codemodify/buildbee/cmd/buildbee-worker")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the worker: %v\n%s", err, out)
	}
	return bin
}

func leftovers(t *testing.T, runID string) []string {
	t.Helper()
	out, err := exec.Command("docker", "ps", "--all", "--quiet", "--filter", "label=buildbee.run="+runID).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(out))
}

func TestRunsInAContainer(t *testing.T) {
	image := dockerImage(t)
	bin := workerBinary(t)
	bare := origin(t)
	s := newStack(t)
	s.useRepo(bare)
	box := Container{Image: image, Home: t.TempDir(), Mounts: []string{bin + ":/usr/local/bin/buildbee-worker:ro"}, Network: "none"}
	s.start(Config{Name: "box", Agents: []string{"claude"}, Slots: 2, Dir: t.TempDir(), Container: box,
		Commands: map[string][]string{"claude": {"buildbee-worker", "fake-agent"}}})

	done := s.queueFor("In a box", "claude")
	got := s.wait(done.ID, finished)
	if got.Status != models.RunSucceeded || !strings.Contains(got.Detail, "without changing") {
		t.Fatalf("run: %+v", got)
	}
	evs, _, _ := s.svc.RunEvents(s.ctx, done.ID, 0, 0)
	if !slices.ContainsFunc(evs, func(e models.RunEvent) bool { return e.Kind == "tool_call" }) {
		t.Fatalf("the agent in the container streamed no tool calls: %+v", evs)
	}
	if left := leftovers(t, done.ID); len(left) != 0 {
		t.Fatalf("containers left behind: %v", left)
	}

	// Canceling stops the agent and removes its container.
	canceled := s.queueFor("Cancel the box", "claude")
	deadline := time.Now().Add(30 * time.Second)
	for {
		evs, _, _ := s.svc.RunEvents(s.ctx, canceled.ID, 0, 0)
		if slices.ContainsFunc(evs, func(e models.RunEvent) bool { return e.Kind == "token" }) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the agent never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := s.svc.UpdateRun(s.ctx, s.ada, canceled.ID, "canceled", "stop"); err != nil {
		t.Fatal(err)
	}
	for len(leftovers(t, canceled.ID)) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the canceled Run's container is still there")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestProbeFindsAgentsInTheImage(t *testing.T) {
	image := dockerImage(t)
	c := Container{Image: image}
	// Alpine has sh and ls but no agent.
	found, err := c.Probe(context.Background(), []string{"claude", "goose"}, map[string][]string{"goose": {"ls"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(found, []string{"goose"}) {
		t.Fatalf("found %v", found)
	}
	if _, err := (&Container{Image: "buildbee-no-such-image:0"}).Probe(context.Background(), nil, nil); err == nil ||
		!strings.Contains(err.Error(), "docker build -f Dockerfile.agents") {
		t.Fatalf("missing image: %v", err)
	}
}

func TestLoggedInNeedsTheAgentsLogin(t *testing.T) {
	home := t.TempDir()
	write := func(p string) {
		t.Helper()
		full := filepath.Join(home, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".claude/.credentials.json")
	write(".codex/config.toml") // settings without a login
	write(".config/goose/config.yaml")
	box := Container{Image: "img", Home: home}
	got, err := box.LoggedIn([]string{"claude", "codex", "grok", "goose", "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "claude,goose,fake" {
		t.Fatalf("logged in: %v", got)
	}
}

// fakeDocker writes a docker stand-in that logs its arguments; images in
// have are present, pull brings any other, and run answers a probe with
// the agents named in agents.
func fakeDocker(t *testing.T, have, agents string) (docker, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls")
	script := `#!/bin/sh
echo "$*" >> ` + log + `
case "$1 $2" in
"image inspect") case " ` + have + ` " in *" $5 "*) exit 0;; esac; test -f ` + dir + `/pulled && exit 0; exit 1;;
"pull --quiet") touch ` + dir + `/pulled; exit 0;;
esac
if [ "$1" = run ]; then for a in ` + agents + `; do echo $a; done; fi
`
	docker = filepath.Join(dir, "docker")
	if err := os.WriteFile(docker, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return docker, log
}

func TestProjectImagesArePulledAndChecked(t *testing.T) {
	docker, log := fakeDocker(t, "", "claude")
	c := Container{Image: "buildbee-agents", Home: t.TempDir(), Docker: docker}
	ctx := context.Background()
	if err := c.ready(ctx, "ghcr.io/acme/agents:2", "claude", []string{"claude-agent-acp"}); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "pull --quiet ghcr.io/acme/agents:2") || !strings.Contains(string(calls), "run --rm --entrypoint sh ghcr.io/acme/agents:2") {
		t.Fatalf("pulled and probed:\n%s", calls)
	}
	before := len(calls)
	if err := c.ready(ctx, "ghcr.io/acme/agents:2", "claude", []string{"claude-agent-acp"}); err != nil {
		t.Fatal(err)
	}
	if calls, _ = os.ReadFile(log); len(calls) != before {
		t.Fatal("a checked image is remembered")
	}
	if err := c.ready(ctx, "ghcr.io/acme/agents:2", "codex", []string{"codex-acp"}); err == nil || !strings.Contains(err.Error(), "has no codex-acp") {
		t.Fatalf("an agent missing from the image: %v", err)
	}
	if err := c.ready(ctx, "--privileged", "claude", []string{"claude-agent-acp"}); err == nil {
		t.Fatal("an image that reads as an option is refused")
	}
}
