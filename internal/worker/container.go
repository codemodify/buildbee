package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codemodify/buildbee/internal/worker/acp"
)

// Container runs each Run's agent in a container of its own. It sees the
// Run's work repo, the mirror's objects (read-only), and a scratch home
// holding copies of its agent's login files; nothing else of the worker
// host. The agent speaks ACP over the container's stdin and stdout.
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

// loginFile is a file under the worker user's home an agent needs.
type loginFile struct {
	path string
	// token files hold credentials the agent may refresh; a refreshed
	// token is written back to the host. Other files are copies the
	// container may change without effect.
	token bool
}

// loginFiles are the files each agent needs from the worker user's home.
// They are copied into the Run's scratch home, never mounted: a container
// must not be able to leave hooks or settings behind in the host's agent
// configuration.
var loginFiles = map[string][]loginFile{
	"claude":   {{".claude/.credentials.json", true}, {".claude.json", false}},
	"codex":    {{".codex/auth.json", true}, {".codex/config.toml", false}},
	"grok":     {{".grok/auth.json", true}, {".grok/config.toml", false}},
	"opencode": {{".local/share/opencode/auth.json", true}, {".config/opencode/opencode.json", false}},
	"goose":    {{".config/goose/config.yaml", false}},
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
func (c *Container) args(worker string, job Job, home string, argv []string, image string) []string {
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
	if job.Objects != "" {
		// The work repo borrows these objects (git alternates, same path).
		args = append(args, "--volume", job.Objects+":"+job.Objects+":ro")
	}
	for _, m := range c.Mounts {
		args = append(args, "--volume", m)
	}
	if c.Network != "" {
		args = append(args, "--network", c.Network)
	}
	return append(append(args, image), argv...)
}

func containerName(runID string) string { return "buildbee-run-" + runID }

// exec returns an Exec running each Job in a container. The fake agent
// needs no container and runs in-process.
func (c *Container) exec(worker string, commands map[string][]string) Exec {
	return func(ctx context.Context, job Job, emit acp.Handler) (string, error) {
		if job.Agent == "fake" {
			return acp.Run(ctx, acp.Config{Agent: "fake", WorkDir: job.Dir, Steer: job.Steer, Ask: job.Ask}, job.Prompt, emit)
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
		logins, err := c.copyLogins(job.Agent, home)
		if err != nil {
			return "", err
		}
		defer logins.writeBack()
		if job.Dir == "" { // no repo: an empty directory to work in
			if job.Dir, err = os.MkdirTemp("", "buildbee-run-*"); err != nil {
				return "", err
			}
			defer os.RemoveAll(job.Dir)
		}
		image := c.Image
		if job.Image != "" && job.Image != c.Image {
			if err := c.ready(ctx, job.Image, job.Agent, argv); err != nil {
				return "", err
			}
			image = job.Image
		}
		// Stopping the docker client does not stop the container; remove it.
		defer c.remove(containerName(job.RunID))
		cmd := append([]string{c.Docker}, c.args(worker, job, home, argv, image)...)
		return acp.Run(ctx, acp.Config{Agent: job.Agent, Command: cmd, WorkDir: job.Dir, Steer: job.Steer, Ask: job.Ask}, job.Prompt, emit)
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

// ready makes sure a Project's image is here (pulling it if not) and has
// the agent's command. A good answer is remembered for the worker's life.
func (c *Container) ready(ctx context.Context, image, agent string, argv []string) error {
	if strings.HasPrefix(image, "-") { // never a docker option, whatever the Server sent
		return fmt.Errorf("bad agent image %q", image)
	}
	key := image + "\x00" + agent
	if _, ok := readyImages.Load(key); ok {
		return nil
	}
	if err := c.defaults(); err != nil {
		return err
	}
	if exec.CommandContext(ctx, c.Docker, "image", "inspect", "--format", "{{.Id}}", image).Run() != nil {
		pullCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		out, err := exec.CommandContext(pullCtx, c.Docker, "pull", "--quiet", image).CombinedOutput()
		cancel()
		if err != nil {
			return fmt.Errorf("the Project's agent image %s could not be pulled: %s", image, strings.TrimSpace(string(out)))
		}
	}
	found, err := c.probe(ctx, image, []string{agent}, map[string][]string{agent: argv})
	if err != nil {
		return err
	}
	if !slices.Contains(found, agent) {
		return fmt.Errorf("%w: the Project's agent image %s has no %s command for %s", acp.ErrNoAgent, image, argv[0], agent)
	}
	readyImages.Store(key, true)
	return nil
}

// readyImages remembers Project images known to carry an agent.
var readyImages sync.Map

// Probe reports which agents' commands exist in the image.
func (c *Container) Probe(ctx context.Context, agents []string, commands map[string][]string) ([]string, error) {
	if err := c.defaults(); err != nil {
		return nil, err
	}
	return c.probe(ctx, c.Image, agents, commands)
}

func (c *Container) probe(ctx context.Context, image string, agents []string, commands map[string][]string) ([]string, error) {
	if _, err := exec.LookPath(c.Docker); err != nil {
		return nil, fmt.Errorf("%w: %s is not installed; install Docker or set BUILDBEE_WORKER_ISOLATION=host", ErrNoDocker, c.Docker)
	}
	if out, err := exec.CommandContext(ctx, c.Docker, "image", "inspect", "--format", "{{.Id}}", image).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("%w: image %s is not available (%s); build it with: docker build -f Dockerfile.agents -t %s .",
			ErrNoImage, image, strings.TrimSpace(string(out)), image)
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
	cmd := exec.CommandContext(ctx, c.Docker, "run", "--rm", "--entrypoint", "sh", image, "-c", script.String()+"true")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("probing image %s: %v: %s", image, err, strings.TrimSpace(stderr.String()))
	}
	return strings.Fields(stdout.String()), nil
}

// Why container isolation is unavailable.
var (
	ErrNoDocker = errors.New("no docker")
	ErrNoImage  = errors.New("no agent image")
)

// LoggedIn returns the agents whose login files are in the worker user's
// home: in a container, an agent has no other way to sign in. The fake
// agent needs none.
func (c *Container) LoggedIn(agents []string) ([]string, error) {
	if err := c.defaults(); err != nil {
		return nil, err
	}
	var out []string
	for _, a := range agents {
		if a == "fake" {
			out = append(out, a)
			continue
		}
		for _, f := range loginFiles[a] {
			if f.token || len(loginFiles[a]) == 1 {
				if _, err := os.Stat(filepath.Join(c.Home, f.path)); err == nil {
					out = append(out, a)
					break
				}
			}
		}
	}
	return out, nil
}

// copiedLogins are an agent's login files copied into a Run's home.
type copiedLogins struct {
	files []copiedLogin
}

type copiedLogin struct {
	host, copy string
	token      bool
	orig       []byte
}

// tokenMu serializes write-backs of one host token file across Runs.
var tokenMu sync.Map // host path -> *sync.Mutex

func (c *Container) copyLogins(agent, home string) (*copiedLogins, error) {
	return copyLogins(c.Home, agent, home)
}

// copyLogins copies agent's login files from the user's realHome into a
// Run's scratch home.
func copyLogins(realHome, agent, home string) (*copiedLogins, error) {
	out := &copiedLogins{}
	for _, f := range loginFiles[agent] {
		host := filepath.Join(realHome, f.path)
		data, err := os.ReadFile(host)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s's login: %w", agent, err)
		}
		dst := filepath.Join(home, f.path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return nil, err
		}
		out.files = append(out.files, copiedLogin{host: host, copy: dst, token: f.token, orig: data})
	}
	return out, nil
}

// writeBack returns refreshed tokens to the host, if the agent refreshed
// one, it is well-formed, and nobody changed the host file meanwhile.
func (l *copiedLogins) writeBack() {
	for _, f := range l.files {
		if !f.token {
			continue
		}
		now, err := os.ReadFile(f.copy)
		if err != nil || len(now) == 0 || bytes.Equal(now, f.orig) {
			continue
		}
		if strings.HasSuffix(f.host, ".json") && !json.Valid(now) {
			continue
		}
		mu, _ := tokenMu.LoadOrStore(f.host, &sync.Mutex{})
		mu.(*sync.Mutex).Lock()
		if cur, err := os.ReadFile(f.host); err == nil && bytes.Equal(cur, f.orig) {
			tmp := f.host + ".buildbee-tmp"
			if os.WriteFile(tmp, now, 0o600) == nil {
				_ = os.Rename(tmp, f.host)
			}
		}
		mu.(*sync.Mutex).Unlock()
	}
}
