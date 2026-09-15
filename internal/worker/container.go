package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/codemodify/buildbee/internal/worker/acp"
)

// Container runs each Run's agent in a container of its own: the Run's
// checkout and that agent's login are all it sees of the worker host. The
// agent speaks ACP over the container's stdin and stdout.
type Container struct {
	Image   string   // image with the agents' ACP commands (see Dockerfile.agents)
	Docker  string   // docker-compatible CLI (default "docker")
	Memory  string   // --memory (default "8g")
	CPUs    string   // --cpus (default "4")
	Pids    int      // --pids-limit (default 1024)
	Network string   // --network (default: Docker's default)
	Mounts  []string // extra -v specs, e.g. a shared package cache
	Home    string   // the worker user's home, where agent logins live
}

// loginPaths are the files under the worker user's home that hold each
// agent's login. A Run's container gets only its own agent's.
var loginPaths = map[string][]string{
	"claude":   {".claude", ".claude.json"},
	"codex":    {".codex"},
	"grok":     {".grok"},
	"opencode": {".local/share/opencode", ".config/opencode"},
	"goose":    {".config/goose", ".local/share/goose"},
}

// containerHome is HOME inside a Run's container.
const containerHome = "/home/agent"

func (c *Container) defaults() error {
	if c.Image == "" {
		return errors.New("container isolation needs an image (BUILDBEE_WORKER_IMAGE)")
	}
	if c.Docker == "" {
		c.Docker = "docker"
	}
	if c.Memory == "" {
		c.Memory = "8g"
	}
	if c.CPUs == "" {
		c.CPUs = "4"
	}
	if c.Pids == 0 {
		c.Pids = 1024
	}
	if c.Home == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("no home directory for agent logins: %w", err)
		}
		c.Home = home
	}
	return nil
}

// args builds the docker run arguments for job, whose agent is started
// with argv inside the container. home is the Run's scratch HOME.
func (c *Container) args(worker string, job Job, home string, argv []string) []string {
	args := []string{"run", "--rm", "-i", "--init",
		"--name", containerName(job.RunID),
		"--label", "buildbee.worker=" + worker, "--label", "buildbee.run=" + job.RunID,
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--memory", c.Memory, "--cpus", c.CPUs, "--pids-limit", strconv.Itoa(c.Pids),
		"--env", "HOME=" + containerHome, "--env", "BUILDBEE=1",
		// Temp files go under the Run's home: directories Docker creates
		// for bind mounts (say under /tmp) belong to root, and agents such
		// as Claude Code refuse temp directories another user owns.
		"--env", "TMPDIR=" + containerHome + "/tmp", "--env", "CLAUDE_CODE_TMPDIR=" + containerHome + "/tmp",
		"--env", "GIT_AUTHOR_NAME=BuildBee agent", "--env", "GIT_AUTHOR_EMAIL=buildbee@localhost",
		"--env", "GIT_COMMITTER_NAME=BuildBee agent", "--env", "GIT_COMMITTER_EMAIL=buildbee@localhost",
		"--volume", home + ":" + containerHome,
		// The checkout keeps its host path: its .git file points into the mirror.
		"--volume", job.Dir + ":" + job.Dir, "--workdir", job.Dir,
	}
	if job.GitDir != "" {
		args = append(args, "--volume", job.GitDir+":"+job.GitDir)
	}
	for _, p := range loginPaths[job.Agent] {
		if _, err := os.Stat(filepath.Join(c.Home, p)); err == nil {
			args = append(args, "--volume", filepath.Join(c.Home, p)+":"+containerHome+"/"+p)
		}
	}
	for _, m := range c.Mounts {
		args = append(args, "--volume", m)
	}
	if c.Network != "" {
		args = append(args, "--network", c.Network)
	}
	args = append(args, c.Image)
	return append(args, argv...)
}

func containerName(runID string) string { return "buildbee-run-" + runID }

// exec returns an Exec running each Job in a container. The fake agent
// needs no container and runs in-process.
func (c *Container) exec(worker string, commands map[string][]string) Exec {
	return func(ctx context.Context, job Job, emit acp.Handler) (string, error) {
		if job.Agent == "fake" {
			return acp.Run(ctx, acp.Config{Agent: "fake", WorkDir: job.Dir, Steer: job.Steer}, job.Prompt, emit)
		}
		argv := commands[job.Agent]
		if len(argv) == 0 {
			argv = acp.Launch[job.Agent]
		}
		if len(argv) == 0 {
			return "", fmt.Errorf("%w: no launch command for agent %q", acp.ErrNoAgent, job.Agent)
		}
		home, err := os.MkdirTemp("", "buildbee-home-*")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(home)
		if err := os.Mkdir(filepath.Join(home, "tmp"), 0o700); err != nil {
			return "", err
		}
		if job.Dir == "" { // no repo: an empty directory to work in
			if job.Dir, err = os.MkdirTemp("", "buildbee-run-*"); err != nil {
				return "", err
			}
			defer os.RemoveAll(job.Dir)
		}
		// Stopping the docker client does not stop the container; remove it.
		defer c.remove(containerName(job.RunID))
		cmd := append([]string{c.Docker}, c.args(worker, job, home, argv)...)
		return acp.Run(ctx, acp.Config{Agent: job.Agent, Command: cmd, WorkDir: job.Dir, Steer: job.Steer}, job.Prompt, emit)
	}
}

func (c *Container) remove(names ...string) {
	if len(names) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, c.Docker, append([]string{"rm", "--force"}, names...)...).Run()
}

// RemoveLeftovers removes containers a previous run of this worker left
// behind, for example after a crash.
func (c *Container) RemoveLeftovers(ctx context.Context, worker string) error {
	if err := c.defaults(); err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, c.Docker, "ps", "--all", "--quiet", "--filter", "label=buildbee.worker="+worker).Output()
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}
	c.remove(strings.Fields(string(out))...)
	return nil
}

// safeCommand matches command names that can be probed in a shell as is.
var safeCommand = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// Probe reports which agents' commands exist in the image.
func (c *Container) Probe(ctx context.Context, agents []string, commands map[string][]string) ([]string, error) {
	if err := c.defaults(); err != nil {
		return nil, err
	}
	if _, err := exec.LookPath(c.Docker); err != nil {
		return nil, fmt.Errorf("%s is not installed; install Docker or set BUILDBEE_WORKER_ISOLATION=host", c.Docker)
	}
	if out, err := exec.CommandContext(ctx, c.Docker, "image", "inspect", "--format", "{{.Id}}", c.Image).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("image %s is not available (%s); build it with: docker build -f Dockerfile.agents -t %s .",
			c.Image, strings.TrimSpace(string(out)), c.Image)
	}
	var script strings.Builder
	for _, a := range agents {
		argv := commands[a]
		if len(argv) == 0 {
			argv = acp.Launch[a]
		}
		if len(argv) > 0 && safeCommand.MatchString(argv[0]) && safeCommand.MatchString(a) {
			fmt.Fprintf(&script, "command -v %s >/dev/null 2>&1 && echo %s\n", argv[0], a)
		}
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, c.Docker, "run", "--rm", "--entrypoint", "sh", c.Image, "-c", script.String()+"true")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("probing image %s: %v: %s", c.Image, err, strings.TrimSpace(stderr.String()))
	}
	return strings.Fields(stdout.String()), nil
}
