package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/codemodify/buildbee/internal/models"
)

// repos keeps one bare mirror per repository under root and gives each Run
// its own git worktree of it, on a branch of its own.
type repos struct {
	root  string
	mu    sync.Mutex
	locks map[string]*sync.Mutex // one per mirror: fetches and worktree changes
}

func newRepos(root string) *repos {
	return &repos{root: root, locks: map[string]*sync.Mutex{}}
}

func (r *repos) lock(mirror string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.locks[mirror] == nil {
		r.locks[mirror] = &sync.Mutex{}
	}
	return r.locks[mirror]
}

// workspace is a Run's checkout.
type workspace struct {
	dir    string // the worktree the agent works in
	mirror string
	repo   string // the Project's repo URL
	branch string // the Run's local branch
	start  string // the branch it started from
	base   string // the commit it started from
	// defaultBranch is the repo's default branch, a PR's base.
	defaultBranch string
	continues     bool // started from an existing branch of the Task
	lock          *sync.Mutex
}

// prepare fetches the Project's repo and checks out a branch of the Run's
// own at the tip of from (a branch name), or of the default branch.
func (r *repos) prepare(ctx context.Context, p models.Project, from, runID string) (*workspace, error) {
	if runID == "" || filepath.Base(runID) != runID || strings.HasPrefix(runID, ".") {
		return nil, fmt.Errorf("unexpected run id %q", runID)
	}
	sum := sha256.Sum256([]byte(p.RepoURL))
	mirror := filepath.Join(r.root, "mirrors", hex.EncodeToString(sum[:8])+".git")
	ws := &workspace{dir: filepath.Join(r.root, "runs", runID), mirror: mirror, repo: p.RepoURL, branch: "buildbee-run/" + runID,
		lock: r.lock(mirror)}
	ws.lock.Lock()
	defer ws.lock.Unlock()

	if _, err := os.Stat(mirror); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(mirror), 0o755); err != nil {
			return nil, err
		}
		if _, err := git(ctx, "", "clone", "--bare", "--quiet", "--", p.RepoURL, mirror); err != nil {
			_ = os.RemoveAll(mirror)
			return nil, fmt.Errorf("cloning %s: %w", p.RepoURL, err)
		}
	}
	// Remote branches live under refs/remotes/origin; refs/heads holds only
	// the Runs' branches, which a fetch must never touch.
	if _, err := git(ctx, mirror, "fetch", "--quiet", "--prune", "--", p.RepoURL, "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return nil, fmt.Errorf("fetching %s: %w", p.RepoURL, err)
	}
	def := p.DefaultBranch
	if def == "" { // the remote's default, recorded by the clone
		head, err := git(ctx, mirror, "symbolic-ref", "--short", "HEAD")
		if err != nil {
			return nil, fmt.Errorf("the repo has no default branch; set the Project's default_branch")
		}
		def = head
	}
	start := def
	if from != "" {
		start = from
	}
	base, err := git(ctx, mirror, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+start+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("the repo has no branch %q to start from", start)
	}
	ws.start, ws.base, ws.defaultBranch, ws.continues = start, base, def, from != ""
	if err := os.MkdirAll(filepath.Dir(ws.dir), 0o755); err != nil {
		return nil, err
	}
	if _, err := git(ctx, mirror, "worktree", "add", "--quiet", "-B", ws.branch, ws.dir, base); err != nil {
		return nil, fmt.Errorf("checking out %s: %w", start, err)
	}
	return ws, nil
}

// commit records whatever the agent left uncommitted and reports whether
// the branch now differs from where it started.
func (ws *workspace) commit(ctx context.Context, author, message string) (bool, error) {
	if _, err := git(ctx, ws.dir, "add", "--all"); err != nil {
		return false, err
	}
	status, err := git(ctx, ws.dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if status != "" {
		if _, err := git(ctx, ws.dir, "-c", "user.name="+author, "-c", "user.email=buildbee@localhost",
			"commit", "--quiet", "--no-verify", "-m", message); err != nil {
			return false, err
		}
	}
	ahead, err := git(ctx, ws.dir, "rev-list", "--count", ws.base+"..HEAD")
	return ahead != "0", err
}

// maxDiff caps the diff kept as an Artifact.
const maxDiff = 8 << 20

func (ws *workspace) diff(ctx context.Context) (string, error) {
	out, err := git(ctx, ws.dir, "diff", "--stat", "--patch", ws.base+"..HEAD")
	if len(out) > maxDiff {
		out = out[:maxDiff] + "\n… diff truncated\n"
	}
	return out, err
}

// push publishes the work as branch target with this machine's git
// credentials. The lease makes the push fail rather than overwrite commits
// someone else pushed to target meanwhile (a new branch must not exist).
func (ws *workspace) push(ctx context.Context, target string) error {
	expect := ""
	if ws.continues && target == ws.start {
		expect = ws.base
	}
	_, err := git(ctx, ws.dir, "push", "--quiet", "--force-with-lease=refs/heads/"+target+":"+expect,
		"--", ws.repo, "HEAD:refs/heads/"+target)
	return err
}

// merge merges branch into the checkout (the default branch) and pushes
// the result, then deletes the merged branch.
func (ws *workspace) merge(ctx context.Context, branch, message string) error {
	if _, err := git(ctx, ws.dir, "-c", "user.name=BuildBee", "-c", "user.email=buildbee@localhost",
		"merge", "--no-ff", "--no-edit", "-m", message, "refs/remotes/origin/"+branch); err != nil {
		_, _ = git(ctx, ws.dir, "merge", "--abort")
		return fmt.Errorf("%s does not merge cleanly into %s: %w", branch, ws.start, err)
	}
	if _, err := git(ctx, ws.dir, "push", "--quiet", "--", ws.repo, "HEAD:refs/heads/"+ws.start); err != nil {
		return fmt.Errorf("pushing %s: %w", ws.start, err)
	}
	_, _ = git(ctx, ws.dir, "push", "--quiet", "--", ws.repo, ":refs/heads/"+branch)
	return nil
}

// close removes the worktree. The branch stays in the mirror when keep is
// set, so work that could not be pushed is not lost.
func (ws *workspace) close(ctx context.Context, keep bool) {
	ws.lock.Lock()
	defer ws.lock.Unlock()
	_, _ = git(ctx, ws.mirror, "worktree", "remove", "--force", ws.dir)
	_ = os.RemoveAll(ws.dir)
	_, _ = git(ctx, ws.mirror, "worktree", "prune")
	if !keep {
		_, _ = git(ctx, ws.mirror, "branch", "--quiet", "-D", ws.branch)
	}
}

// git runs git without prompting, over its common transports only.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ALLOW_PROTOCOL=file:git:http:https:ssh",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// branchName is buildbee/<task words>-<run id prefix>.
func branchName(taskTitle, runID string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(taskTitle), "-"), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		slug = "run"
	}
	return "buildbee/" + slug + "-" + runID[:min(8, len(runID))]
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// githubRepo returns owner/name for a GitHub remote, or "".
func githubRepo(url string) string {
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "ssh://git@github.com/", "git@github.com:"} {
		if rest, ok := strings.CutPrefix(url, prefix); ok {
			rest = strings.TrimSuffix(strings.TrimSuffix(rest, "/"), ".git")
			if owner, name, ok := strings.Cut(rest, "/"); ok && owner != "" && name != "" && !strings.Contains(name, "/") {
				return owner + "/" + name
			}
		}
	}
	return ""
}

// mergePR squash-merges a pull request with the GitHub CLI and deletes its branch.
func mergePR(ctx context.Context, url string) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("the GitHub CLI (gh) is not installed on this worker")
	}
	cmd := exec.CommandContext(ctx, "gh", "pr", "merge", url, "--squash", "--delete-branch")
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gh pr merge: %s", strings.TrimSpace(string(out)+" "+err.Error()))
	}
	return nil
}

// openPR opens a pull request with the GitHub CLI, using this machine's gh
// login. It returns the PR's URL.
func openPR(ctx context.Context, repo, head, base, title, body string) (string, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return "", errors.New("the GitHub CLI (gh) is not installed on this worker")
	}
	args := []string{"pr", "create", "--repo", repo, "--head", head, "--title", title, "--body", body}
	if base != "" {
		args = append(args, "--base", base)
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh pr create: %s", strings.TrimSpace(stderr.String()+" "+err.Error()))
	}
	lines := strings.Fields(stdout.String())
	if len(lines) == 0 {
		return "", errors.New("gh pr create printed no URL")
	}
	return lines[len(lines)-1], nil
}
