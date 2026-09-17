package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxConfinesTheAgent(t *testing.T) {
	sb, err := FindSandbox(context.Background())
	if err != nil {
		t.Skip(err)
	}
	// Outside /tmp, which the sandbox replaces anyway: these must be
	// hidden or read-only because of the sandbox's own rules.
	home, outside := dirHere(t), dirHere(t)
	if err := os.WriteFile(filepath.Join(home, "id_ed25519"), []byte("secret key"), 0o600); err != nil {
		t.Fatal(err)
	}
	sb.Home = home
	work, scratch := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(scratch, "tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	script := `echo made > written
test -e ` + home + `/id_ed25519 && echo LEAK
echo x > ` + outside + `/x 2>/dev/null && echo ESCAPE
touch /etc/buildbee-sandbox-test 2>/dev/null && echo ETC
echo HOME=$HOME
echo PWD=$(pwd)
echo PID1=$(cat /proc/1/comm)`
	argv := sb.command(Job{Dir: work}, scratch, []string{"sh", "-c", script})
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	got := string(out)
	for _, bad := range []string{"LEAK", "ESCAPE", "ETC"} {
		if strings.Contains(got, bad) {
			t.Errorf("sandbox let through %s:\n%s", bad, got)
		}
	}
	for _, want := range []string{"HOME=" + scratch, "PWD=" + work} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "PID1=systemd") || strings.Contains(got, "PID1=init") {
		t.Errorf("the agent sees the host's processes:\n%s", got)
	}
	if b, err := os.ReadFile(filepath.Join(work, "written")); err != nil || string(b) != "made\n" {
		t.Fatalf("the checkout is writable: %q %v", b, err)
	}
}

func TestSandboxKeepsAgentsInstalledUnderHome(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(bin, "my-agent-acp")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	sb := &Sandbox{Bwrap: "bwrap", Home: home}
	dirs := sb.installDirs([]string{"my-agent-acp"})
	if len(dirs) != 1 || dirs[0] != filepath.Join(home, ".local") {
		t.Fatalf("install dirs: %v", dirs)
	}
	args := strings.Join(sb.command(Job{Dir: "/w/r1"}, "/tmp/h", []string{"my-agent-acp"}), " ")
	if !strings.Contains(args, "--tmpfs "+home+" --ro-bind "+home+"/.local "+home+"/.local") {
		t.Fatalf("home hidden, install dir back read-only:\n%s", args)
	}
}

// dirHere makes a directory under the package directory, not /tmp.
func dirHere(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp(".", ".sandbox-test-")
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(abs) })
	return abs
}
