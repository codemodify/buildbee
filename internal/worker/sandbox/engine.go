package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Spec is what the supervisor passes to a Sandbox engine.
type Spec struct {
	RunID   string
	Image   string
	WorkDir string
	RepoURL string
	Command string
}

type Result struct {
	ExitCode int
	Logs     string
}

// Engine runs a command inside a Sandbox.
type Engine interface {
	Run(ctx context.Context, spec Spec) (Result, error)
}

// FakeEngine never talks to Docker. Used by tests and --fake.
type FakeEngine struct {
	Logs     string
	ExitCode int
	Err      error
}

func (f FakeEngine) Run(_ context.Context, spec Spec) (Result, error) {
	if f.Err != nil {
		return Result{}, f.Err
	}
	logs := f.Logs
	if logs == "" {
		logs = fmt.Sprintf("fake sandbox run=%s cmd=%s repo=%s\n", spec.RunID, spec.Command, spec.RepoURL)
	}
	return Result{ExitCode: f.ExitCode, Logs: logs}, nil
}

// DockerEngine starts an ephemeral container (`docker run --rm`).
type DockerEngine struct {
	Bin string
}

func (d DockerEngine) Run(ctx context.Context, spec Spec) (Result, error) {
	bin := d.Bin
	if bin == "" {
		bin = "docker"
	}
	image := spec.Image
	if image == "" {
		image = "alpine:3.20"
	}
	work := spec.WorkDir
	if work == "" {
		work = "/work"
	}
	cmd := spec.Command
	if cmd == "" {
		cmd = "echo buildbee sandbox ok && uname -a"
	}
	script := buildScript(spec.RepoURL, cmd)
	name := "buildbee-run"
	if spec.RunID != "" {
		name = "buildbee-run-" + strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return '-'
		}, spec.RunID)
		if len(name) > 60 {
			name = name[:60]
		}
	}
	args := []string{
		"run", "--rm",
		"--name", name,
		"-w", work,
		image,
		"sh", "-c", script,
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
	}
	c := exec.CommandContext(ctx, bin, args...)
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	err := c.Run()
	res := Result{Logs: out.String()}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		return res, fmt.Errorf("docker: %w", err)
	}
	return res, nil
}

func buildScript(repoURL, command string) string {
	var b strings.Builder
	b.WriteString("set -e; mkdir -p /work; cd /work; ")
	if repoURL != "" {
		b.WriteString("apk add --no-cache git >/dev/null; git clone --depth 1 ")
		b.WriteString(shellQuote(repoURL))
		b.WriteString(" repo; cd repo; ")
	}
	b.WriteString(command)
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// Available reports whether a docker binary is on PATH.
func Available() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}
