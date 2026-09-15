package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/codemodify/buildbee/internal/worker/acp"
)

// Sandbox modes for agents that run on the machine (host isolation).
const (
	SandboxAuto    = "auto"    // bubblewrap when it works here, else none
	SandboxRequire = "require" // refuse to start without it
	SandboxOff     = "off"
)

// ErrNoSandbox means bubblewrap cannot run here.
var ErrNoSandbox = errors.New("no sandbox")

// Sandbox runs a host agent under bubblewrap (Linux): the system is
// read-only, the user's home is hidden, and the agent writes only its Run's
// checkout and a scratch home holding copies of its login files. It keeps
// the network, which agents need to reach their model.
type Sandbox struct {
	Bwrap string // path to bwrap
	Home  string // the worker user's real home: hidden, logins copied out of it
}

// FindSandbox returns a working Sandbox, or ErrNoSandbox saying why not.
func FindSandbox(ctx context.Context) (*Sandbox, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("%w: bubblewrap is Linux only", ErrNoSandbox)
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("%w: bubblewrap (bwrap) is not installed", ErrNoSandbox)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("%w: no home directory", ErrNoSandbox)
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(pctx, bwrap, "--ro-bind", "/", "/", "--unshare-pid", "--dev", "/dev", "true").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%w: bwrap cannot create namespaces here (%s)", ErrNoSandbox, strings.TrimSpace(string(out)))
	}
	return &Sandbox{Bwrap: bwrap, Home: home}, nil
}

// command wraps argv so it runs sandboxed for job, with scratch as HOME.
func (s *Sandbox) command(job Job, scratch string, argv []string) []string {
	args := []string{s.Bwrap,
		"--die-with-parent", "--new-session", "--unshare-pid", "--unshare-ipc", "--unshare-uts",
		"--ro-bind", "/", "/",
		"--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp",
		"--tmpfs", s.Home, // the user's files, keys and other repos are not there
	}
	// Agents installed under the home (npm, nvm, bun, ~/.local) still start.
	for _, dir := range s.installDirs(argv) {
		args = append(args, "--ro-bind", dir, dir)
	}
	args = append(args, "--bind", scratch, scratch, "--bind", job.Dir, job.Dir)
	if job.Objects != "" {
		args = append(args, "--ro-bind", job.Objects, job.Objects)
	}
	args = append(args,
		"--setenv", "HOME", scratch, "--setenv", "BUILDBEE", "1",
		"--setenv", "TMPDIR", filepath.Join(scratch, "tmp"), "--setenv", "CLAUDE_CODE_TMPDIR", filepath.Join(scratch, "tmp"),
		"--setenv", "GIT_AUTHOR_NAME", "BuildBee agent", "--setenv", "GIT_AUTHOR_EMAIL", "buildbee@localhost",
		"--setenv", "GIT_COMMITTER_NAME", "BuildBee agent", "--setenv", "GIT_COMMITTER_EMAIL", "buildbee@localhost",
		"--chdir", job.Dir, "--")
	return append(args, argv...)
}

// installDirs are the top-level entries of the home that hold the agent's
// command, node, and the home's PATH entries, so they stay visible.
func (s *Sandbox) installDirs(argv []string) []string {
	var paths []string
	for _, name := range []string{argv[0], "node"} {
		if p, err := exec.LookPath(name); err == nil {
			paths = append(paths, p)
			if real, err := filepath.EvalSymlinks(p); err == nil {
				paths = append(paths, real)
			}
		}
	}
	paths = append(paths, filepath.SplitList(os.Getenv("PATH"))...)
	var out []string
	for _, p := range paths {
		rel, err := filepath.Rel(s.Home, p)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		top := filepath.Join(s.Home, strings.Split(rel, string(filepath.Separator))[0])
		if st, err := os.Stat(top); err == nil && st.IsDir() && !slices.Contains(out, top) {
			out = append(out, top)
		}
	}
	slices.Sort(out)
	return out
}

// exec runs host agents sandboxed; the fake agent runs in-process.
func (s *Sandbox) exec(commands map[string][]string) Exec {
	return func(ctx context.Context, job Job, emit acp.Handler) (string, error) {
		if job.Agent == "fake" {
			return acp.Run(ctx, acp.Config{Agent: "fake", WorkDir: job.Dir, Steer: job.Steer, Ask: job.Ask}, job.Prompt, emit)
		}
		argv, err := acp.Command(job.Agent, commands)
		if err != nil {
			return "", err
		}
		scratch, err := os.MkdirTemp("", "buildbee-home-*")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(scratch)
		if err := os.Mkdir(filepath.Join(scratch, "tmp"), 0o700); err != nil {
			return "", err
		}
		logins, err := copyLogins(s.Home, job.Agent, scratch)
		if err != nil {
			return "", err
		}
		defer logins.writeBack()
		if job.Dir == "" {
			if job.Dir, err = os.MkdirTemp("", "buildbee-run-*"); err != nil {
				return "", err
			}
			defer os.RemoveAll(job.Dir)
		}
		cmd := s.command(job, scratch, argv)
		return acp.Run(ctx, acp.Config{Agent: job.Agent, Command: cmd, WorkDir: job.Dir, Steer: job.Steer, Ask: job.Ask}, job.Prompt, emit)
	}
}
