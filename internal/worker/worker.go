// Package worker executes queued Runs.
//
// A worker pulls Runs from the Server (POST /v1/worker/claim), heartbeats
// while its agent works, streams the agent's output as RunEvents, uploads
// the transcript as an Artifact and reports the outcome. Workers open no
// port, so any LAN machine with agent logins can join by pointing at the
// Server.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/worker/acp"
)

// Job is one agent session for a Run.
type Job struct {
	RunID  string
	Agent  string
	Dir    string // where the agent works; "" for the fake agent without a repo
	GitDir string // the repo mirror Dir's checkout belongs to, if any
	Prompt string
	Steer  <-chan acp.Steer // people's messages while the agent works
}

// Exec runs a Job, calling emit for each piece of output, and returns the
// transcript.
type Exec func(ctx context.Context, job Job, emit acp.Handler) (string, error)

// Config describes one worker process.
type Config struct {
	Server string   // Server base URL
	Name   string   // unique on the LAN; owns the Runs this worker claims
	Agents []string // agents this worker offers, e.g. claude, codex, fake
	Slots  int      // Runs executed at once
	// Isolation is where agents run: IsolationContainer (default), a
	// container per Run, or IsolationHost, directly on this machine as the
	// worker's user. The fake agent always runs in-process.
	Isolation string
	Container Container // settings for IsolationContainer
	// Commands overrides how agents are started (see acp.Launch).
	Commands map[string][]string
	// RunTimeout stops a Run that takes longer (default 2h).
	RunTimeout time.Duration
	// Dir holds repo mirrors and Run checkouts (default: the user cache dir).
	Dir string
	// OpenPRs opens a pull request with the gh CLI after pushing to GitHub.
	OpenPRs bool
	Exec    Exec // nil runs the agent over ACP
	Log     *slog.Logger
}

// Isolation modes.
const (
	IsolationContainer = "container"
	IsolationHost      = "host"
)

const (
	claimWait = 25 * time.Second // long-poll per claim request
	// heartbeatEvery renews a claim well inside the Server's 60s lease.
	heartbeatEvery = 15 * time.Second
	reportTimeout  = 30 * time.Second
	maxBackoff     = 30 * time.Second
)

// Worker claims and executes Runs until its context ends.
type Worker struct {
	cfg       Config
	api       *api
	log       *slog.Logger
	heartbeat time.Duration
	repos     *repos
}

// New checks cfg and returns a Worker.
func New(cfg Config) (*Worker, error) {
	if cfg.Name == "" {
		return nil, errors.New("worker: a name is required")
	}
	if cfg.Slots < 1 {
		return nil, errors.New("worker: slots must be at least 1")
	}
	if len(cfg.Agents) == 0 {
		return nil, errors.New("worker: offer at least one agent")
	}
	if cfg.Isolation == "" {
		cfg.Isolation = IsolationContainer
	}
	if cfg.Isolation != IsolationContainer && cfg.Isolation != IsolationHost {
		return nil, fmt.Errorf("worker: isolation must be %s or %s, got %q", IsolationContainer, IsolationHost, cfg.Isolation)
	}
	real := false
	for _, a := range cfg.Agents {
		if a == "" || !models.ValidAgent(a) {
			return nil, fmt.Errorf("worker: unknown agent %q (known: %v)", a, models.Agents)
		}
		if a == "fake" {
			continue
		}
		real = true
		if cfg.Exec == nil && cfg.Isolation == IsolationHost {
			if _, err := acp.Command(a, cfg.Commands); err != nil {
				return nil, fmt.Errorf("worker: %w", err)
			}
		}
	}
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = 2 * time.Hour
	}
	if cfg.Exec == nil && real && cfg.Isolation == IsolationContainer {
		if err := cfg.Container.defaults(); err != nil {
			return nil, fmt.Errorf("worker: %w", err)
		}
		cfg.Exec = cfg.Container.exec(cfg.Name, cfg.Commands)
	}
	if cfg.Exec == nil {
		commands := cfg.Commands
		cfg.Exec = func(ctx context.Context, job Job, emit acp.Handler) (string, error) {
			var argv []string
			if job.Agent != "fake" {
				var err error
				if argv, err = acp.Command(job.Agent, commands); err != nil {
					return "", err
				}
			}
			return acp.Run(ctx, acp.Config{Agent: job.Agent, Command: argv, WorkDir: job.Dir, Steer: job.Steer}, job.Prompt, emit)
		}
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("worker: no directory for checkouts: %w", err)
		}
		cfg.Dir = filepath.Join(cache, "buildbee-worker")
	}
	return &Worker{cfg: cfg, api: newAPI(cfg.Server, cfg.Name), heartbeat: heartbeatEvery, repos: newRepos(cfg.Dir),
		log: cfg.Log.With("worker", cfg.Name)}, nil
}

// Run executes Runs in cfg.Slots parallel loops until ctx ends. A Run in
// progress when ctx ends is stopped and reported failed.
func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range w.cfg.Slots {
		wg.Go(func() { w.loop(ctx) })
	}
	wg.Wait()
}

func (w *Worker) loop(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		c, err := w.api.claim(ctx, w.cfg.Agents, claimWait)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.log.Warn("claim failed; retrying", "err", err, "in", backoff)
			sleep(ctx, backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second
		if c != nil {
			w.execute(ctx, c)
		}
	}
}

// errStopped means the Run finished on the Server (canceled by a person,
// or failed by the reaper) while the agent was still working.
type errStopped struct{ status models.RunStatus }

func (e errStopped) Error() string { return "run was " + string(e.status) + " on the server" }

// errTimeout means the Run took longer than the worker allows.
type errTimeout struct{ limit time.Duration }

func (e errTimeout) Error() string { return "the run exceeded its time limit of " + e.limit.String() }

func (w *Worker) execute(ctx context.Context, c *models.Claim) {
	run := c.Run
	agent := run.Agent
	if agent == "" && run.Kind != models.RunMerge { // any agent: prefer a real one
		agent = w.cfg.Agents[0]
		if i := slices.IndexFunc(w.cfg.Agents, func(a string) bool { return a != "fake" }); i >= 0 {
			agent = w.cfg.Agents[i]
		}
	}
	log := w.log.With("run", run.ID, "task", run.TaskID, "kind", run.Kind, "agent", agent)
	log.Info("run claimed")

	runCtx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	runCtx, cancelTimeout := context.WithTimeoutCause(runCtx, w.cfg.RunTimeout, errTimeout{w.cfg.RunTimeout})
	defer cancelTimeout()
	go w.keepAlive(runCtx, run.ID, stop, log)

	emit := func(ev acp.Event) error { return w.api.event(runCtx, run.ID, ev.Kind, ev.Payload) }
	say := func(format string, args ...any) {
		_ = emit(acp.Event{Kind: models.RunEventLog, Payload: map[string]any{"text": fmt.Sprintf(format, args...) + "\n"}})
	}
	var (
		out string
		rep outcome
		err error
	)
	if run.Kind == models.RunMerge {
		rep, err = w.merge(runCtx, c, say)
	} else {
		out, rep, err = w.work(runCtx, c, agent, emit, say)
	}

	// Report with a fresh context so a stopping worker still records the outcome.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reportTimeout)
	defer cancel()
	if out != "" {
		if aerr := w.api.artifact(rctx, run.TaskID, newArtifact{Kind: "log", Name: agent + ".log", Body: out, RunID: run.ID}); aerr != nil {
			log.Warn("uploading transcript failed", "err", aerr)
		}
	}
	var stopped errStopped
	if errors.As(context.Cause(runCtx), &stopped) {
		log.Info("run stopped on the server", "status", stopped.status)
		return
	}
	status := models.RunSucceeded
	var timeout errTimeout
	switch {
	case ctx.Err() != nil:
		status, rep.detail = models.RunFailed, "worker "+w.cfg.Name+" stopped during the run"
	case errors.As(context.Cause(runCtx), &timeout):
		status, rep.detail = models.RunFailed, timeout.Error()
	case err != nil:
		status, rep.detail = models.RunFailed, err.Error()
	}
	if uerr := w.api.report(rctx, run.ID, status, rep); uerr != nil {
		log.Error("reporting the outcome failed", "err", uerr, "status", status)
		return
	}
	log.Info("run finished", "status", status)
}

// outcome is what a Run produced, for the Server.
type outcome struct {
	detail, summary, branch, prURL string
}

// work runs the agent on the Run's checkout (the Project's repo, when it
// has one) and delivers what a build produced.
func (w *Worker) work(ctx context.Context, c *models.Claim, agent string, emit acp.Handler, say func(string, ...any)) (string, outcome, error) {
	run := c.Run
	rep := outcome{detail: "agent " + agent + " finished"}
	say("worker %s starting agent %s for a %s Run", w.cfg.Name, agent, run.Kind)
	var ws *workspace
	workDir := ""
	switch {
	case c.Project.RepoURL != "":
		from := c.Task.Branch // continue the Task's branch
		if run.Kind == models.RunReview && from == "" {
			return "", rep, errors.New("the Task has no branch to review")
		}
		say("checking out %s", c.Project.RepoURL)
		var err error
		if ws, err = w.repos.prepare(ctx, c.Project, from, run.ID); err != nil {
			return "", rep, err
		}
		say("on %s at %.12s", ws.start, ws.base)
		workDir = ws.dir
	case agent != "fake" && w.cfg.Isolation == IsolationHost:
		// The Project has no repo: the agent gets an empty directory.
		dir, err := os.MkdirTemp("", "buildbee-run-*")
		if err != nil {
			return "", rep, err
		}
		defer os.RemoveAll(dir)
		workDir = dir
	}
	steer := make(chan acp.Steer, 16)
	listenCtx, stopListening := context.WithCancel(ctx)
	defer stopListening()
	go w.listen(listenCtx, run.ID, c.Seq, steer)
	job := Job{RunID: run.ID, Agent: agent, Dir: workDir, Prompt: run.Prompt, Steer: steer}
	if ws != nil {
		job.GitDir = ws.mirror
	}
	out, err := w.cfg.Exec(ctx, job, emit)
	rep.summary = finalReply(out)
	keep := false
	if ws != nil {
		defer func() { ws.close(context.WithoutCancel(ctx), keep) }()
	}
	if err != nil || ctx.Err() != nil || ws == nil || run.Kind != models.RunBuild {
		return out, rep, err
	}
	rep, keep, err = w.deliver(ctx, c, ws, agent, rep, say)
	return out, rep, err
}

// deliver turns a build's work into a pushed branch and, for GitHub repos,
// a pull request. keep reports that the work must stay on this worker
// because it could not be pushed.
func (w *Worker) deliver(ctx context.Context, c *models.Claim, ws *workspace, agent string, rep outcome, say func(string, ...any)) (outcome, bool, error) {
	author := "BuildBee"
	if c.Bot != nil {
		author = c.Bot.DisplayName + " (BuildBee)"
	}
	changed, err := ws.commit(ctx, author, c.Task.Title+"\n\nBuildBee Run "+c.Run.ID)
	if err != nil {
		return rep, false, fmt.Errorf("committing the agent's work: %w", err)
	}
	if !changed {
		rep.detail = "agent " + agent + " finished without changing the repo"
		return rep, false, nil
	}
	if diff, err := ws.diff(ctx); err == nil {
		if err := w.api.artifact(ctx, c.Task.ID, newArtifact{Kind: "diff", Name: "changes.diff", Body: diff, RunID: c.Run.ID}); err != nil {
			say("uploading the diff failed: %v", err)
		}
	}
	target := c.Task.Branch
	if target == "" {
		target = branchName(c.Task.Title, c.Run.ID)
	}
	if err := ws.push(ctx, target); err != nil {
		return rep, true, fmt.Errorf("pushing %s failed (the work stays on worker %s): %w", target, w.cfg.Name, err)
	}
	say("pushed %s", target)
	rep.branch, rep.detail = target, "pushed "+target
	repo := githubRepo(ws.repo)
	if repo == "" || !w.cfg.OpenPRs || c.Task.PRURL != "" {
		return rep, false, nil // pushing updated any existing PR
	}
	body := fmt.Sprintf("%s\n\nOpened by BuildBee: Task %s, Run %s, agent %s.", c.Task.Body, c.Task.ID, c.Run.ID, agent)
	url, err := openPR(ctx, repo, target, ws.defaultBranch, c.Task.Title, strings.TrimSpace(body))
	if err != nil {
		say("could not open a pull request: %v", err)
		rep.detail += "; no pull request: " + err.Error()
		return rep, false, nil
	}
	if err := w.api.artifact(ctx, c.Task.ID, newArtifact{Kind: "pr", Name: "Pull request", URL: url, RunID: c.Run.ID}); err != nil {
		say("recording the pull request failed: %v", err)
	}
	rep.prURL, rep.detail = url, "opened "+url
	return rep, false, nil
}

// merge lands the Task's branch on the default branch: with the GitHub CLI
// when the Task has a pull request, else with git.
func (w *Worker) merge(ctx context.Context, c *models.Claim, say func(string, ...any)) (outcome, error) {
	p, t := c.Project, c.Task
	if p.RepoURL == "" || t.Branch == "" {
		return outcome{}, errors.New("nothing to merge: the Task has no branch")
	}
	if repo := githubRepo(p.RepoURL); repo != "" && t.PRURL != "" && w.cfg.OpenPRs {
		say("merging %s", t.PRURL)
		if err := mergePR(ctx, t.PRURL); err != nil {
			return outcome{}, err
		}
		return outcome{detail: "merged " + t.PRURL}, nil
	}
	ws, err := w.repos.prepare(ctx, p, "", c.Run.ID)
	if err != nil {
		return outcome{}, err
	}
	defer ws.close(context.WithoutCancel(ctx), false)
	say("merging %s into %s", t.Branch, ws.start)
	if err := ws.merge(ctx, t.Branch, fmt.Sprintf("Merge %s: %s\n\nBuildBee Task %s", t.Branch, t.Title, t.ID)); err != nil {
		return outcome{}, err
	}
	return outcome{detail: "merged " + t.Branch + " into " + ws.start}, nil
}

// finalReply is the agent's closing message: its reply after its last tool
// call, capped to what a summary needs.
func finalReply(transcript string) string {
	if i := strings.LastIndex("\n"+transcript, "\n[tool] "); i >= 0 {
		rest := transcript[i+len("[tool] "):]
		if j := strings.IndexByte(rest, '\n'); j >= 0 {
			transcript = rest[j+1:]
		} else {
			transcript = ""
		}
	}
	transcript = strings.TrimSpace(transcript)
	if len(transcript) > 8000 {
		transcript = "…" + transcript[len(transcript)-8000:]
	}
	return transcript
}

// keepAlive renews the claim until ctx ends, and stops the Run when the
// Server says it is finished or no longer this worker's.
func (w *Worker) keepAlive(ctx context.Context, runID string, stop context.CancelCauseFunc, log *slog.Logger) {
	t := time.NewTicker(w.heartbeat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		r, err := w.api.heartbeat(ctx, runID)
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, errConflict):
			stop(errStopped{status: "claimed by another worker"})
			return
		case err != nil:
			log.Warn("heartbeat failed", "err", err) // the lease outlives a few misses
		case r.Status.Terminal():
			stop(errStopped{status: r.Status})
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
