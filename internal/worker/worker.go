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

// Exec runs agent on prompt in workDir, calling emit for each piece of
// output, and returns the transcript.
type Exec func(ctx context.Context, agent, workDir, prompt string, emit acp.Handler) (string, error)

// Config describes one worker process.
type Config struct {
	Server string   // Server base URL
	Name   string   // unique on the LAN; owns the Runs this worker claims
	Agents []string // agents this worker offers, e.g. claude, codex, fake
	Slots  int      // Runs executed at once
	// AllowHostAgents lets real agent CLIs run directly on this host, with
	// its files and logins. Off by default; the fake agent needs nothing.
	AllowHostAgents bool
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

// ErrHostAgentsDisabled refuses real agents on a worker that has not opted in.
var ErrHostAgentsDisabled = errors.New("real agents would run directly on this worker host; " +
	"set BUILDBEE_WORKER_ALLOW_HOST_AGENTS=1 to allow it, or offer only the fake agent")

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
	for _, a := range cfg.Agents {
		if a == "" || !models.ValidAgent(a) {
			return nil, fmt.Errorf("worker: unknown agent %q (known: %v)", a, models.Agents)
		}
		if a == "fake" {
			continue
		}
		if !cfg.AllowHostAgents {
			return nil, fmt.Errorf("worker: agent %s: %w", a, ErrHostAgentsDisabled)
		}
		if cfg.Exec == nil {
			if _, err := acp.Command(a, cfg.Commands); err != nil {
				return nil, fmt.Errorf("worker: %w", err)
			}
		}
	}
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = 2 * time.Hour
	}
	if cfg.Exec == nil {
		commands := cfg.Commands
		cfg.Exec = func(ctx context.Context, agent, workDir, prompt string, emit acp.Handler) (string, error) {
			var argv []string
			if agent != "fake" {
				var err error
				if argv, err = acp.Command(agent, commands); err != nil {
					return "", err
				}
			}
			return acp.Run(ctx, acp.Config{Agent: agent, Command: argv, WorkDir: workDir}, prompt, emit)
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
	if agent == "" { // any agent: prefer a real one
		agent = w.cfg.Agents[0]
		if i := slices.IndexFunc(w.cfg.Agents, func(a string) bool { return a != "fake" }); i >= 0 {
			agent = w.cfg.Agents[i]
		}
	}
	log := w.log.With("run", run.ID, "task", run.TaskID, "agent", agent)
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
	out, ws, err := w.runAgent(runCtx, c, agent, emit, say)
	detail := "agent " + agent + " finished"
	keep := false
	if ws != nil {
		if err == nil && runCtx.Err() == nil {
			detail, keep, err = w.deliver(runCtx, c, ws, agent, say)
		}
		defer ws.close(context.WithoutCancel(ctx), keep)
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
		status, detail = models.RunFailed, "worker "+w.cfg.Name+" stopped during the run"
	case errors.As(context.Cause(runCtx), &timeout):
		status, detail = models.RunFailed, timeout.Error()
	case err != nil:
		status, detail = models.RunFailed, err.Error()
	}
	if uerr := w.api.updateRun(rctx, run.ID, status, detail); uerr != nil {
		log.Error("reporting the outcome failed", "err", uerr, "status", status)
		return
	}
	log.Info("run finished", "status", status)
}

// runAgent prepares the Run's directory (a checkout of the Project's repo
// when it has one) and runs the agent in it.
func (w *Worker) runAgent(ctx context.Context, c *models.Claim, agent string, emit acp.Handler, say func(string, ...any)) (string, *workspace, error) {
	say("worker %s starting agent %s", w.cfg.Name, agent)
	var ws *workspace
	workDir := ""
	switch {
	case c.Project.RepoURL != "":
		say("checking out %s", c.Project.RepoURL)
		var err error
		if ws, err = w.repos.prepare(ctx, c.Project, branchName(c.Task.Title, c.Run.ID), c.Run.ID); err != nil {
			return "", nil, err
		}
		say("working on branch %s, from %s at %.12s", ws.branch, ws.start, ws.base)
		workDir = ws.dir
	case agent != "fake":
		// The Project has no repo: the agent gets an empty directory.
		dir, err := os.MkdirTemp("", "buildbee-run-*")
		if err != nil {
			return "", nil, err
		}
		defer os.RemoveAll(dir)
		workDir = dir
	}
	out, err := w.cfg.Exec(ctx, agent, workDir, c.Run.Prompt, emit)
	return out, ws, err
}

// deliver turns the agent's work into a pushed branch and, for GitHub
// repos, a pull request. keep reports that the branch must stay on this
// worker because it could not be pushed.
func (w *Worker) deliver(ctx context.Context, c *models.Claim, ws *workspace, agent string, say func(string, ...any)) (detail string, keep bool, err error) {
	author := "BuildBee"
	if c.Bot != nil {
		author = c.Bot.DisplayName + " (BuildBee)"
	}
	changed, err := ws.commit(ctx, author, c.Task.Title+"\n\nBuildBee Run "+c.Run.ID)
	if err != nil {
		return "", false, fmt.Errorf("committing the agent's work: %w", err)
	}
	if !changed {
		return "agent " + agent + " finished without changing the repo", false, nil
	}
	if diff, err := ws.diff(ctx); err == nil {
		if err := w.api.artifact(ctx, c.Task.ID, newArtifact{Kind: "diff", Name: "changes.diff", Body: diff, RunID: c.Run.ID}); err != nil {
			say("uploading the diff failed: %v", err)
		}
	}
	if err := ws.push(ctx); err != nil {
		return "", true, fmt.Errorf("pushing %s failed (the work stays on worker %s): %w", ws.branch, w.cfg.Name, err)
	}
	say("pushed %s", ws.branch)
	repo := githubRepo(ws.repo)
	if repo == "" || !w.cfg.OpenPRs {
		return "pushed " + ws.branch, false, nil
	}
	body := fmt.Sprintf("%s\n\nOpened by BuildBee: Task %s, Run %s, agent %s.", c.Task.Body, c.Task.ID, c.Run.ID, agent)
	url, err := openPR(ctx, repo, ws.branch, ws.start, c.Task.Title, strings.TrimSpace(body))
	if err != nil {
		say("could not open a pull request: %v", err)
		return "pushed " + ws.branch + "; no pull request: " + err.Error(), false, nil
	}
	if err := w.api.artifact(ctx, c.Task.ID, newArtifact{Kind: "pr", Name: "Pull request", URL: url, RunID: c.Run.ID}); err != nil {
		say("recording the pull request failed: %v", err)
	}
	return "opened " + url, false, nil
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
