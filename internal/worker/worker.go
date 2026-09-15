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
	"slices"
	"sync"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/worker/acp"
)

// Exec runs agent on prompt in workDir, calling emit for each piece of
// output, and returns the whole transcript.
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
	Exec            Exec // nil runs the agent CLI (acp.Stream)
	Log             *slog.Logger
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
		if a != "fake" && !cfg.AllowHostAgents {
			return nil, fmt.Errorf("worker: agent %s: %w", a, ErrHostAgentsDisabled)
		}
	}
	if cfg.Exec == nil {
		cfg.Exec = hostExec
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &Worker{cfg: cfg, api: newAPI(cfg.Server, cfg.Name), heartbeat: heartbeatEvery,
		log: cfg.Log.With("worker", cfg.Name)}, nil
}

func hostExec(ctx context.Context, agent, workDir, prompt string, emit acp.Handler) (string, error) {
	_, out, err := acp.Stream(ctx, acp.Config{Agent: agent, WorkDir: workDir}, prompt, emit)
	return out, err
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
	go w.keepAlive(runCtx, run.ID, stop, log)

	out, err := w.runAgent(runCtx, run, agent)

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
	status, detail := models.RunSucceeded, "agent "+agent+" finished"
	switch {
	case ctx.Err() != nil:
		status, detail = models.RunFailed, "worker "+w.cfg.Name+" stopped during the run"
	case err != nil:
		status, detail = models.RunFailed, err.Error()
	}
	if uerr := w.api.updateRun(rctx, run.ID, status, detail); uerr != nil {
		log.Error("reporting the outcome failed", "err", uerr, "status", status)
		return
	}
	log.Info("run finished", "status", status)
}

func (w *Worker) runAgent(ctx context.Context, run models.Run, agent string) (string, error) {
	workDir := ""
	if agent != "fake" {
		// An empty directory until Runs get a checkout of the Project's repo.
		dir, err := os.MkdirTemp("", "buildbee-run-*")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(dir)
		workDir = dir
	}
	emit := func(ev acp.Event) error { return w.api.event(ctx, run.ID, ev.Kind, ev.Payload) }
	if err := emit(acp.Event{Kind: models.RunEventLog, Payload: map[string]any{
		"text": fmt.Sprintf("worker %s starting agent %s\n", w.cfg.Name, agent)}}); err != nil {
		return "", err
	}
	return w.cfg.Exec(ctx, agent, workDir, run.Prompt, emit)
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
