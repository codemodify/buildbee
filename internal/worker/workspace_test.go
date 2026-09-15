package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/core"
	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/worker/acp"
)

// origin creates a bare repo with one commit on main and returns its path.
func origin(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig")) // ignore the developer's config
	dir := t.TempDir()
	bare, work := filepath.Join(dir, "origin.git"), filepath.Join(dir, "seed")
	run := func(cwd string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(dir, "init", "--quiet", "--bare", "--initial-branch=main", bare)
	run(dir, "init", "--quiet", "--initial-branch=main", work)
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(work, "add", ".")
	run(work, "commit", "--quiet", "-m", "initial")
	run(work, "push", "--quiet", bare, "main")
	return bare
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(context.Background(), dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (s *stack) useRepo(url string) {
	s.t.Helper()
	if _, err := s.svc.UpdateProject(s.ctx, s.ada, s.project, core.ProjectPatch{RepoURL: &url}); err != nil {
		s.t.Fatal(err)
	}
}

// writeFile is an agent that adds a file to its checkout.
func writeFile(name, body string) Exec {
	return func(_ context.Context, job Job, _ acp.Handler) (string, error) {
		if job.Dir == "" || job.GitDir == "" {
			return "", os.ErrInvalid
		}
		return "wrote " + name, os.WriteFile(filepath.Join(job.Dir, name), []byte(body), 0o644)
	}
}

func TestRunOnARepoPushesItsBranch(t *testing.T) {
	bare := origin(t)
	s := newStack(t)
	s.useRepo(bare)
	r := s.queue("Add a greeting")
	dir := t.TempDir()
	s.start(Config{Dir: dir, Exec: writeFile("hello.txt", "hi\n")})
	got := s.wait(r.ID, finished)
	branch := "buildbee/add-a-greeting-" + r.ID[:8]
	if got.Status != models.RunSucceeded || got.Detail != "pushed "+branch {
		t.Fatalf("run: %+v", got)
	}
	if body := gitOut(t, bare, "show", branch+":hello.txt"); body != "hi" {
		t.Fatalf("pushed file: %q", body)
	}
	if subject := gitOut(t, bare, "log", "-1", "--format=%s%n%an", branch); subject != "Add a greeting\nBuildBee" {
		t.Fatalf("commit: %q", subject)
	}
	arts, _ := s.svc.Artifacts(s.ctx, r.TaskID)
	var diff *models.Artifact
	for i := range arts {
		if arts[i].Kind == "diff" {
			diff, _ = s.svc.Artifact(s.ctx, arts[i].ID)
		}
	}
	if diff == nil || !strings.Contains(diff.Body, "+hi") || !strings.Contains(diff.Body, "hello.txt") {
		t.Fatalf("diff artifact: %+v", diff)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "runs")); len(entries) != 0 {
		t.Fatalf("checkouts left behind: %v", entries)
	}
	if local := gitOut(t, filepath.Join(dir, "mirrors", mustOne(t, filepath.Join(dir, "mirrors"))), "branch", "--list", "buildbee-run/*"); local != "" {
		t.Fatalf("pushed branches are dropped from the mirror: %q", local)
	}
}

func mustOne(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("%s: %v %v", dir, entries, err)
	}
	return entries[0].Name()
}

func TestRunsStartFromTheLatestDefaultBranch(t *testing.T) {
	bare := origin(t)
	s := newStack(t)
	s.useRepo(bare)
	dir := t.TempDir()
	var mu sync.Mutex
	var seen []string
	s.start(Config{Dir: dir, Slots: 1, Exec: func(_ context.Context, job Job, _ acp.Handler) (string, error) {
		readme, err := os.ReadFile(filepath.Join(job.Dir, "README.md"))
		mu.Lock()
		seen = append(seen, string(readme))
		mu.Unlock()
		return "", err
	}})
	first := s.queue("one")
	if got := s.wait(first.ID, finished); got.Status != models.RunSucceeded || !strings.Contains(got.Detail, "without changing") {
		t.Fatalf("run: %+v", got)
	}
	// Someone lands a change on main between Runs.
	seed := filepath.Join(t.TempDir(), "seed")
	gitOut(t, "", "clone", "--quiet", bare, seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("# app v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, seed, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--quiet", "-am", "v2")
	gitOut(t, seed, "push", "--quiet", "origin", "main")
	second := s.queue("two")
	s.wait(second.ID, finished)
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || seen[0] != "# app\n" || seen[1] != "# app v2\n" {
		t.Fatalf("checkouts saw %q", seen)
	}
}

func TestFailedPushKeepsTheWork(t *testing.T) {
	bare := origin(t)
	hook := filepath.Join(bare, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho 'pushes are frozen' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := newStack(t)
	s.useRepo(bare)
	r := s.queue("Frozen")
	dir := t.TempDir()
	s.start(Config{Name: "box", Dir: dir, Exec: writeFile("x.txt", "x")})
	got := s.wait(r.ID, finished)
	if got.Status != models.RunFailed || !strings.Contains(got.Detail, "work stays on worker box") || !strings.Contains(got.Detail, "pushes are frozen") {
		t.Fatalf("run: %+v", got)
	}
	branch := "buildbee-run/" + r.ID
	mirror := filepath.Join(dir, "mirrors", mustOne(t, filepath.Join(dir, "mirrors")))
	if gitOut(t, mirror, "branch", "--list", branch) == "" {
		t.Fatal("the unpushed branch must stay in the mirror")
	}
}

func TestBadRepoFailsTheRun(t *testing.T) {
	s := newStack(t)
	s.useRepo(filepath.Join(t.TempDir(), "missing.git"))
	r := s.queue("Nowhere")
	s.start(Config{Dir: t.TempDir()})
	if got := s.wait(r.ID, finished); got.Status != models.RunFailed || !strings.Contains(got.Detail, "cloning") {
		t.Fatalf("run: %+v", got)
	}
}

func TestBranchNameAndGitHubRepo(t *testing.T) {
	if got := branchName("Fix: the login page (again!)", "0123456789ab"); got != "buildbee/fix-the-login-page-again-01234567" {
		t.Fatal(got)
	}
	if got := branchName("日本語", "abcdef012345"); got != "buildbee/run-abcdef01" {
		t.Fatal(got)
	}
	for url, want := range map[string]string{
		"https://github.com/acme/app.git": "acme/app",
		"git@github.com:acme/app.git":     "acme/app",
		"ssh://git@github.com/acme/app":   "acme/app",
		"https://gitlab.com/acme/app.git": "",
		"/srv/git/app.git":                "",
	} {
		if got := githubRepo(url); got != want {
			t.Errorf("githubRepo(%q) = %q, want %q", url, got, want)
		}
	}
}

func TestOpenPRUsesTheGitHubCLI(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\necho 'Creating pull request'\necho https://github.com/acme/app/pull/7\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	url, err := openPR(context.Background(), "acme/app", "buildbee/x-1", "main", "Title", "Body")
	if err != nil || url != "https://github.com/acme/app/pull/7" {
		t.Fatalf("%q %v", url, err)
	}
	raw, _ := os.ReadFile(argsFile)
	if got := strings.Join(strings.Fields(string(raw)), " "); got != "pr create --repo acme/app --head buildbee/x-1 --title Title --body Body --base main" {
		t.Fatalf("gh args: %s", got)
	}
}

// TestAutopilotEndToEnd drives a Task through Scout, Builder, Sentry and a
// merge on a real repo, with a scripted agent standing in for the real ones.
func TestAutopilotEndToEnd(t *testing.T) {
	bare := origin(t)
	s := newStack(t)
	s.useRepo(bare)
	on := true
	if _, err := s.svc.UpdateProject(s.ctx, s.ada, s.project, core.ProjectPatch{AutoRun: &on}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	reviews, builds := 0, 0
	agent := func(_ context.Context, job Job, _ acp.Handler) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.Contains(job.Prompt, "You are planning"):
			return "Plan: add greet.txt.", nil
		case strings.Contains(job.Prompt, "Implement the Task"):
			builds++
			name := "greet.txt"
			if builds == 2 {
				if _, err := os.Stat(filepath.Join(job.Dir, "greet.txt")); err != nil {
					return "", fmt.Errorf("round 2 must continue the branch: %w", err)
				}
				name = "greet_test.txt"
			}
			return "[tool] Write\nWrote " + name + ".", os.WriteFile(filepath.Join(job.Dir, name), []byte("hi\n"), 0o644)
		case strings.Contains(job.Prompt, "You are reviewing"):
			reviews++
			if reviews == 1 {
				return "Needs a test.\nVERDICT: REQUEST_CHANGES", nil
			}
			return "Good.\nVERDICT: APPROVE", nil
		}
		return "", fmt.Errorf("unexpected prompt: %s", job.Prompt)
	}
	s.start(Config{Agents: []string{"claude"}, Isolation: IsolationHost, Dir: t.TempDir(), Exec: agent})

	tc, err := s.svc.CreateTask(s.ctx, s.ada, s.project, core.NewTask{Title: "Greet people"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		task, _ := s.svc.Task(s.ctx, tc.ID)
		if task.MergedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			runs, _ := s.svc.Runs(s.ctx, tc.ID)
			t.Fatalf("not merged: %+v\nruns: %+v", task, runs)
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, f := range []string{"greet.txt", "greet_test.txt"} {
		if gitOut(t, bare, "show", "main:"+f) != "hi" {
			t.Fatalf("main lacks %s", f)
		}
	}
	runs, _ := s.svc.Runs(s.ctx, tc.ID)
	var kinds []string
	for i := len(runs) - 1; i >= 0; i-- {
		kinds = append(kinds, string(runs[i].Kind)+":"+string(runs[i].Status))
	}
	want := "plan:succeeded build:succeeded review:succeeded build:succeeded review:succeeded merge:succeeded"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("runs: %s\nwant: %s", got, want)
	}
	if runs[len(runs)-2].Summary != "Wrote greet.txt." {
		t.Fatalf("the summary is the agent's closing message: %q", runs[len(runs)-2].Summary)
	}
	task, _ := s.svc.Task(s.ctx, tc.ID)
	if gitOut(t, bare, "branch", "--list", task.Branch) != "" {
		t.Fatalf("the merged branch %s is deleted", task.Branch)
	}
}
