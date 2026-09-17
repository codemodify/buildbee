package agent

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

// workspace is a Run's checkout, in two parts that never share git
// metadata:
//
//   - work is where the agent works: a repo of its own that borrows the
//     mirror's objects (git alternates, mounted read-only in a container).
//     The agent may change anything in it, its .git included.
//   - host is a worktree of the mirror that only the worker touches. The
//     worker records the agent's files from work into host with
//     `git --work-tree`, so no git config, hook or gitdir the agent wrote is
//     ever used by the worker. Commits, diffs and pushes happen in host.
type workspace struct {
	dir     string // work: where the agent works
	host    string // the worker's own worktree of the mirror
	mirror  string
	objects string // the mirror's object store, which work borrows
	repo    string // the Project's repo URL
	branch  string // the Run's local branch in the mirror
	start   string // the branch it started from
	base    string // the commit it started from
	// defaultBranch is the repo's default branch, a PR's base.
	defaultBranch string
	continues     bool // started from an existing branch of the Task
	lock          *sync.Mutex
}

// prepare fetches the Project's repo and checks out the tip of from (a
// branch name), or of the default branch, on a branch of the Run's own.
// name is the branch name the agent sees. forAgent adds the agent's work
// repo; without it (merges) the host worktree is fully checked out.
func (r *repos) prepare(ctx context.Context, p models.Project, from, name, runID string, forAgent bool) (*workspace, error) {
	if runID == "" || filepath.Base(runID) != runID || strings.HasPrefix(runID, ".") {
		return nil, fmt.Errorf("unexpected run id %q", runID)
	}
	sum := sha256.Sum256([]byte(p.RepoURL))
	mirror := filepath.Join(r.root, "mirrors", hex.EncodeToString(sum[:8])+".git")
	runDir := filepath.Join(r.root, "runs", runID)
	ws := &workspace{host: filepath.Join(runDir, "host"), mirror: mirror, objects: filepath.Join(mirror, "objects"),
		repo: p.RepoURL, branch: "buildbee-run/" + runID, lock: r.lock(mirror)}
	if forAgent {
		ws.dir = filepath.Join(runDir, "work")
	}
	if err := ws.fetchAndAdd(ctx, p, from, !forAgent); err != nil {
		return nil, err
	}
	if !forAgent {
		return ws, nil
	}
	if name == "" {
		name = ws.start
	}
	// The agent's repo: fresh, so running git in it here is still safe.
	steps := [][]string{
		{"init", "--quiet", ws.dir},
		{"-C", ws.dir, "fetch", "--quiet", "--no-tags", "--", mirror, "+refs/remotes/origin/*:refs/remotes/origin/*"},
		{"-C", ws.dir, "checkout", "--quiet", "-B", name, ws.base},
	}
	for i, step := range steps {
		if _, err := git(ctx, "", step...); err != nil {
			ws.close(context.WithoutCancel(ctx), false)
			return nil, fmt.Errorf("preparing the checkout: %w", err)
		}
		if i == 0 { // borrow the mirror's objects instead of copying them
			alt := filepath.Join(ws.dir, ".git", "objects", "info", "alternates")
			if err := os.WriteFile(alt, []byte(ws.objects+"\n"), 0o644); err != nil {
				ws.close(context.WithoutCancel(ctx), false)
				return nil, err
			}
		}
	}
	return ws, nil
}

// fetchAndAdd refreshes the mirror and adds the host worktree.
func (ws *workspace) fetchAndAdd(ctx context.Context, p models.Project, from string, checkout bool) error {
	ws.lock.Lock()
	defer ws.lock.Unlock()
	if _, err := os.Stat(ws.mirror); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(ws.mirror), 0o755); err != nil {
			return err
		}
		if _, err := git(ctx, "", "clone", "--bare", "--quiet", "--", p.RepoURL, ws.mirror); err != nil {
			_ = os.RemoveAll(ws.mirror)
			return fmt.Errorf("cloning %s: %w", p.RepoURL, err)
		}
	}
	// Remote branches live under refs/remotes/origin; refs/heads holds only
	// the Runs' branches, which a fetch must never touch.
	if _, err := git(ctx, ws.mirror, "fetch", "--quiet", "--prune", "--", p.RepoURL, "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return fmt.Errorf("fetching %s: %w", p.RepoURL, err)
	}
	def := p.DefaultBranch
	if def == "" { // the remote's default, recorded by the clone
		head, err := git(ctx, ws.mirror, "symbolic-ref", "--short", "HEAD")
		if err != nil {
			return fmt.Errorf("the repo has no default branch; set the Project's default_branch")
		}
		def = head
	}
	start := def
	if from != "" {
		start = from
	}
	base, err := git(ctx, ws.mirror, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+start+"^{commit}")
	if err != nil {
		return fmt.Errorf("the repo has no branch %q to start from", start)
	}
	ws.start, ws.base, ws.defaultBranch, ws.continues = start, base, def, from != ""
	if err := os.MkdirAll(filepath.Dir(ws.host), 0o755); err != nil {
		return err
	}
	args := []string{"worktree", "add", "--quiet", "-B", ws.branch, ws.host, base}
	if !checkout {
		args = []string{"worktree", "add", "--quiet", "--no-checkout", "-B", ws.branch, ws.host, base}
	}
	if _, err := git(ctx, ws.mirror, args...); err != nil {
		return fmt.Errorf("checking out %s: %w", start, err)
	}
	if !checkout {
		if _, err := git(ctx, ws.host, "read-tree", "HEAD"); err != nil {
			return err
		}
	}
	return nil
}

// commit records the agent's files as one commit on the Run's branch and
// reports whether the branch now differs from where it started. Only the
// files are taken from the agent's repo; its commits and git settings are
// not.
func (ws *workspace) commit(ctx context.Context, author, message string) (bool, error) {
	if _, err := git(ctx, ws.host, "--work-tree="+ws.dir, "add", "--all"); err != nil {
		return false, err
	}
	staged, err := git(ctx, ws.host, "diff", "--cached", "--name-only")
	if err != nil {
		return false, err
	}
	if staged != "" {
		if _, err := git(ctx, ws.host, "-c", "user.name="+author, "-c", "user.email=buildbee@localhost",
			"commit", "--quiet", "--no-verify", "-m", message); err != nil {
			return false, err
		}
	}
	ahead, err := git(ctx, ws.host, "rev-list", "--count", ws.base+"..HEAD")
	return ahead != "0", err
}

// head is the commit the Run's branch points at.
func (ws *workspace) head(ctx context.Context) (string, error) {
	return git(ctx, ws.host, "rev-parse", "HEAD")
}

// maxDiff caps the diff kept as an Artifact.
const maxDiff = 8 << 20

func (ws *workspace) diff(ctx context.Context) (string, error) {
	out, err := git(ctx, ws.host, "diff", "--stat", "--patch", ws.base+"..HEAD")
	if len(out) > maxDiff {
		out = out[:maxDiff] + "\n… diff truncated\n"
	}
	return out, err
}

// push publishes the work as branch target with this machine's git
// credentials. The lease makes the push fail rather than overwrite commits
// someone else pushed to target meanwhile (a new branch must not exist).
func (ws *workspace) push(ctx context.Context, target string) error {
	if target == ws.defaultBranch {
		return fmt.Errorf("refusing to push the Run's work to the default branch %s", target)
	}
	expect := ""
	if ws.continues && target == ws.start {
		expect = ws.base
	}
	_, err := git(ctx, ws.host, "push", "--quiet", "--force-with-lease=refs/heads/"+target+":"+expect,
		"--", ws.repo, "HEAD:refs/heads/"+target)
	return err
}

// merge merges commit, the approved tip of branch, into the checkout (the
// default branch), pushes the result, then deletes branch if it still
// points at commit. It fails if branch moved since it was approved.
func (ws *workspace) merge(ctx context.Context, branch, commit, message string) error {
	if branch == ws.start {
		return fmt.Errorf("%s is the default branch", branch)
	}
	tip, err := git(ctx, ws.mirror, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch+"^{commit}")
	if err != nil {
		return fmt.Errorf("branch %s is gone", branch)
	}
	if commit != "" && tip != commit {
		return fmt.Errorf("branch %s moved since it was approved (approved %.12s, now %.12s)", branch, commit, tip)
	}
	if _, err := git(ctx, ws.host, "-c", "user.name=BuildBee", "-c", "user.email=buildbee@localhost",
		"merge", "--no-ff", "--no-edit", "-m", message, tip); err != nil {
		_, _ = git(ctx, ws.host, "merge", "--abort")
		return fmt.Errorf("%s does not merge cleanly into %s: %w", branch, ws.start, err)
	}
	if _, err := git(ctx, ws.host, "push", "--quiet", "--", ws.repo, "HEAD:refs/heads/"+ws.start); err != nil {
		return fmt.Errorf("pushing %s: %w", ws.start, err)
	}
	_, _ = git(ctx, ws.host, "push", "--quiet", "--force-with-lease=refs/heads/"+branch+":"+tip, "--", ws.repo, ":refs/heads/"+branch)
	return nil
}

// close removes the checkout. The branch stays in the mirror when keep is
// set, so work that could not be pushed is not lost.
func (ws *workspace) close(ctx context.Context, keep bool) {
	ws.lock.Lock()
	defer ws.lock.Unlock()
	_, _ = git(ctx, ws.mirror, "worktree", "remove", "--force", ws.host)
	_ = os.RemoveAll(filepath.Dir(ws.host))
	_, _ = git(ctx, ws.mirror, "worktree", "prune")
	if !keep {
		_, _ = git(ctx, ws.mirror, "branch", "--quiet", "-D", ws.branch)
	}
}

// git runs git without prompting, over its common transports only, and
// never runs hooks or a file-system monitor.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	args = append([]string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}, args...)
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
		return "", fmt.Errorf("git %s: %s", gitVerb(args), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// gitVerb names the git command in args for error messages.
func gitVerb(args []string) string {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-c" || a == "-C":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a
		}
	}
	return "command"
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

// mergePR squash-merges a pull request with the GitHub CLI and deletes its
// branch, provided its head is still commit (the one that was approved).
func mergePR(ctx context.Context, url, commit string) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("the GitHub CLI (gh) is not installed on this worker")
	}
	args := []string{"pr", "merge", url, "--squash", "--delete-branch"}
	if commit != "" {
		args = append(args, "--match-head-commit", commit)
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
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
