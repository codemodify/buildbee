// Package worker executes queued Runs.
//
// A worker pulls Runs from the Server (POST /v1/worker/claim), heartbeats
// while its agent works, streams the agent's output as RunEvents, uploads
// the transcript as an Artifact and reports the outcome. Workers open no
// port, so any LAN machine with agent logins can join by pointing at the
// Server.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/codemodify/buildbee/internal/agent/acp"
	"github.com/codemodify/buildbee/internal/models"
)

// Job is one agent session for a Run.
type Job struct {
	RunID string
	Agent string
	Image string // the Project's agent image; "" = the worker's
	Dir   string // where the agent works; "" for the fake agent without a repo
	// Objects is the mirror object store Dir's repo borrows from, if any;
	// a container mounts it read-only.
	Objects string
	Prompt  string
	// Files are what people attached to the Task, downloaded by the worker;
	// each Exec puts them where its agent can read them.
	Files []JobFile
	Steer <-chan acp.Steer // people's messages while the agent works
	// Ask, when the Project's agents ask before acting, turns each request
	// into a Decision and waits for its answer.
	Ask func(ctx context.Context, p acp.Permission) (string, error)
}

// JobFile is one attachment, already on the worker's disk.
type JobFile struct {
	Name     string
	MimeType string
	Path     string // on the worker
}

// Exec runs a Job, calling emit for each piece of output, and returns the
// transcript.
type Exec func(ctx context.Context, job Job, emit acp.Handler) (string, error)

// Config describes one worker process.
type Config struct {
	Server string // Server base URL
	Name   string // how this process names itself, e.g. Docs@nc-laptop
	BotID  string // the Bot this agent runs; its Runs come here
	AI     string // the AI it runs: claude, codex, grok, opencode, goose, fake
	Host   string // the machine it runs on, for people to see
	Slots  int    // Runs it takes at once
	// Isolation is where agents run: IsolationContainer (default), a
	// container per Run, or IsolationHost, directly on this machine as the
	// worker's user. The fake agent always runs in-process.
	Isolation string
	Container Container // settings for IsolationContainer
	// Sandbox, in host isolation, runs agents under bubblewrap; nil runs
	// them with the worker user's full access.
	Sandbox *Sandbox
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

// Agent claims and executes its Bot's Runs until its context ends.
type Agent struct {
	cfg       Config
	api       *api
	log       *slog.Logger
	heartbeat time.Duration
	repos     *repos
}

// New checks cfg and returns an Agent.
func New(cfg Config) (*Agent, error) {
	if cfg.Name == "" || cfg.BotID == "" {
		return nil, errors.New("agent: a name and the Bot it runs are required")
	}
	if cfg.Slots < 1 {
		return nil, errors.New("agent: slots must be at least 1")
	}
	if cfg.AI == "" || !models.ValidAgent(cfg.AI) {
		return nil, fmt.Errorf("agent: unknown AI %q (known: %v)", cfg.AI, models.Agents)
	}
	if cfg.Isolation == "" {
		cfg.Isolation = IsolationContainer
	}
	if cfg.Isolation != IsolationContainer && cfg.Isolation != IsolationHost {
		return nil, fmt.Errorf("agent: isolation must be %s or %s, got %q", IsolationContainer, IsolationHost, cfg.Isolation)
	}
	if cfg.Exec == nil && cfg.AI != "fake" && cfg.Isolation == IsolationHost {
		if _, err := acp.Command(cfg.AI, cfg.Commands); err != nil {
			return nil, fmt.Errorf("agent: %w", err)
		}
	}
	if cfg.RunTimeout <= 0 {
		cfg.RunTimeout = 2 * time.Hour
	}
	if cfg.Exec == nil && cfg.AI != "fake" && cfg.Isolation == IsolationContainer {
		if err := cfg.Container.defaults(); err != nil {
			return nil, fmt.Errorf("worker: %w", err)
		}
		cfg.Exec = cfg.Container.exec(cfg.Name, cfg.Commands)
	}
	if cfg.Exec == nil && cfg.Sandbox != nil {
		cfg.Exec = cfg.Sandbox.exec(cfg.Commands)
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
			dir, err := os.MkdirTemp("", "buildbee-attachments-*")
			if err != nil {
				return "", err
			}
			defer os.RemoveAll(dir)
			attached, err := placeFiles(job.Files, dir, dir)
			if err != nil {
				return "", err
			}
			return acp.Run(ctx, acp.Config{Agent: job.Agent, Command: argv, WorkDir: job.Dir, Steer: job.Steer, Ask: job.Ask,
				Attachments: attached}, job.Prompt, emit)
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
	return &Agent{cfg: cfg, api: newAPI(cfg), heartbeat: heartbeatEvery, repos: newRepos(cfg.Dir),
		log: cfg.Log.With("worker", cfg.Name)}, nil
}

// Run executes Runs in cfg.Slots parallel loops until ctx ends. A Run in
// progress when ctx ends is stopped and reported failed.
func (w *Agent) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for range w.cfg.Slots {
		wg.Go(func() { w.loop(ctx) })
	}
	wg.Wait()
}

func (w *Agent) loop(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		c, err := w.api.claim(ctx, w.cfg.Slots, claimWait)
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

func (w *Agent) execute(ctx context.Context, c *models.Claim) {
	run := c.Run
	agent := w.cfg.AI // this agent runs one AI; a merge needs none
	log := w.log.With("run", run.ID, "task", run.TaskID, "kind", run.Kind, "ai", agent)
	log.Info("run claimed")

	runCtx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	runCtx, cancelTimeout := context.WithTimeoutCause(runCtx, w.cfg.RunTimeout, errTimeout{w.cfg.RunTimeout})
	defer cancelTimeout()
	go w.keepAlive(runCtx, run.ID, stop, log)

	emit := w.emitter(runCtx, run.ID, log)
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
	if uerr := w.reportOutcome(rctx, run.ID, status, rep); uerr != nil {
		log.Error("reporting the outcome failed", "err", uerr, "status", status)
		return
	}
	log.Info("run finished", "status", status)
}

// reportOutcome reports a finished Run, retrying while the Server is
// unreachable. The Run's heartbeats keep its claim meanwhile.
func (w *Agent) reportOutcome(ctx context.Context, runID string, status models.RunStatus, rep outcome) error {
	var err error
	for attempt := range 6 {
		if err = w.api.report(ctx, runID, status, rep); err == nil || errors.Is(err, errConflict) || errors.Is(err, errRejected) {
			return err
		}
		if attempt < 5 {
			sleep(ctx, time.Duration(1<<attempt)*time.Second)
		}
	}
	return err
}

// emitter posts agent output as RunEvents. A Server that is briefly
// unreachable costs a few events, not the Run; only a Run the Server says
// is finished or not ours (409) stops the agent.
func (w *Agent) emitter(ctx context.Context, runID string, log *slog.Logger) acp.Handler {
	dropped := 0
	return func(ev acp.Event) error {
		var err error
		for attempt := range 3 {
			if err = w.api.event(ctx, runID, ev.Kind, ev.Payload); err == nil || errors.Is(err, errConflict) || ctx.Err() != nil {
				return err
			}
			if attempt < 2 {
				sleep(ctx, time.Duration(attempt+1)*500*time.Millisecond)
			}
		}
		if dropped++; dropped == 1 || dropped%100 == 0 {
			log.Warn("dropping run events the server did not accept", "err", err, "dropped", dropped)
		}
		return nil
	}
}

// outcome is what a Run produced, for the Server.
type outcome struct {
	detail, summary, branch, commit, prURL string
}

// work runs the agent on the Run's checkout (the Project's repo, when it
// has one) and delivers what a build produced.
func (w *Agent) work(ctx context.Context, c *models.Claim, agent string, emit acp.Handler, say func(string, ...any)) (string, outcome, error) {
	run := c.Run
	rep := outcome{detail: "agent " + agent + " finished"}
	say("worker %s starting agent %s for a %s Run", w.cfg.Name, agent, run.Kind)
	var ws *workspace
	workDir := ""
	switch {
	case c.Project.RepoURL != "":
		from := c.Branch // carry on this Bot's own branch, if it has one
		if run.Kind == models.RunReview && from == "" {
			return "", rep, errors.New("the Task has no branch to review")
		}
		name := from
		if name == "" && run.Kind == models.RunBuild {
			name = branchName(c.Task.Title, run.ID)
		}
		say("checking out %s", c.Project.RepoURL)
		var err error
		if ws, err = w.repos.prepare(ctx, c.Project, from, name, run.ID, true); err != nil {
			return "", rep, err
		}
		say("on %s at %.12s", ws.start, ws.base)
		rep.commit = ws.base // what a plan or review looked at
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
	files, cleanFiles, err := w.fetchFiles(ctx, c, say)
	if err != nil {
		return "", rep, err
	}
	defer cleanFiles()
	steer := make(chan acp.Steer, 16)
	listenCtx, stopListening := context.WithCancel(ctx)
	defer stopListening()
	go w.listen(listenCtx, run.ID, c.Seq, steer)
	job := Job{RunID: run.ID, Agent: agent, Image: c.Project.AgentImage, Dir: workDir, Prompt: run.Prompt, Files: files, Steer: steer}
	if c.Project.AgentPermissions == models.PermissionsAsk {
		job.Ask = w.asker(run.ID)
	}
	if ws != nil {
		job.Objects = ws.objects
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
func (w *Agent) deliver(ctx context.Context, c *models.Claim, ws *workspace, agent string, rep outcome, say func(string, ...any)) (outcome, bool, error) {
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
	// Push back to the branch this Run started from, or open a new one of
	// this Bot's own: two Bots on one ask never push to the same branch.
	target := c.Branch
	if target == "" {
		target = branchName(c.Task.Title, c.Run.ID)
	}
	head, err := ws.head(ctx)
	if err != nil {
		return rep, false, err
	}
	if err := ws.push(ctx, target); err != nil {
		return rep, true, fmt.Errorf("pushing %s failed (the work stays on worker %s): %w", target, w.cfg.Name, err)
	}
	say("pushed %s at %.12s", target, head)
	rep.branch, rep.commit, rep.detail = target, head, "pushed "+target
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
func (w *Agent) merge(ctx context.Context, c *models.Claim, say func(string, ...any)) (outcome, error) {
	p, t := c.Project, c.Task
	if p.RepoURL == "" || t.Branch == "" {
		return outcome{}, errors.New("nothing to merge: the Task has no branch")
	}
	if repo := githubRepo(p.RepoURL); repo != "" && t.PRURL != "" && w.cfg.OpenPRs {
		say("merging %s at %.12s", t.PRURL, c.Run.Commit)
		if err := mergePR(ctx, t.PRURL, c.Run.Commit); err != nil {
			return outcome{}, err
		}
		return outcome{detail: "merged " + t.PRURL}, nil
	}
	ws, err := w.repos.prepare(ctx, p, "", "", c.Run.ID, false)
	if err != nil {
		return outcome{}, err
	}
	defer ws.close(context.WithoutCancel(ctx), false)
	say("merging %s at %.12s into %s", t.Branch, c.Run.Commit, ws.start)
	if err := ws.merge(ctx, t.Branch, c.Run.Commit, fmt.Sprintf("Merge %s: %s\n\nBuildBee Task %s", t.Branch, t.Title, t.ID)); err != nil {
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
func (w *Agent) keepAlive(ctx context.Context, runID string, stop context.CancelCauseFunc, log *slog.Logger) {
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

// answerPoll is how often a worker looks for the answer to a permission
// question.
var answerPoll = 2 * time.Second

// asker relays an agent's permission requests for a Run as Decisions and
// waits for each answer, until the Run's context ends. An answer that is
// none of the agent's options is refused.
func (w *Agent) asker(runID string) func(ctx context.Context, p acp.Permission) (string, error) {
	return func(ctx context.Context, p acp.Permission) (string, error) {
		body := map[string]any{"title": p.Title}
		opts := make([]map[string]string, 0, len(p.Options))
		for _, o := range p.Options {
			opts = append(opts, map[string]string{"id": o.ID, "name": o.Name, "kind": o.Kind})
		}
		body["options"] = opts
		var d models.Decision
		if _, err := w.api.do(ctx, http.MethodPost, "/v1/runs/"+runID+"/permission", body, &d); err != nil {
			return "", err
		}
		for {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(answerPoll):
			}
			var got models.Decision
			if _, err := w.api.do(ctx, http.MethodGet, "/v1/decisions/"+d.ID, nil, &got); err != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				continue // the Server may be restarting; keep waiting
			}
			if got.Answer == "" {
				continue
			}
			for _, o := range p.Options {
				if got.Answer == o.Name || got.Answer == o.ID {
					return o.ID, nil
				}
			}
			return "", fmt.Errorf("answered %q, which is none of the options", got.Answer)
		}
	}
}

// maxAttachmentBytes is how much of a Task's attachments one Run is given.
const maxAttachmentBytes = 64 << 20

// fetchFiles downloads what people attached to the Task into a directory of
// the worker's. The Run goes on without a file it cannot fetch; the agent
// is told what it got.
func (w *Agent) fetchFiles(ctx context.Context, c *models.Claim, say func(string, ...any)) ([]JobFile, func(), error) {
	if len(c.Files) == 0 {
		return nil, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "buildbee-files-*")
	if err != nil {
		return nil, func() {}, err
	}
	clean := func() { os.RemoveAll(dir) }
	var out []JobFile
	var total int64
	for _, f := range c.Files {
		if total+f.Size > maxAttachmentBytes {
			say("attachment %s skipped: the Task's files are over %d MB", f.Name, maxAttachmentBytes>>20)
			continue
		}
		name := uniqueName(filepath.Base(f.Name), out)
		path := filepath.Join(dir, name)
		if err := w.api.file(ctx, c.Run.ID, f.ID, path); err != nil {
			say("attachment %s could not be fetched: %v", f.Name, err)
			continue
		}
		total += f.Size
		out = append(out, JobFile{Name: name, MimeType: f.ContentType, Path: path})
	}
	if len(out) > 0 {
		say("%d attached file(s) for the agent", len(out))
	}
	return out, clean, nil
}

// uniqueName keeps two attachments called the same apart.
func uniqueName(name string, taken []JobFile) string {
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "file"
	}
	base, ext := strings.TrimSuffix(name, filepath.Ext(name)), filepath.Ext(name)
	out := name
	for i := 2; slices.ContainsFunc(taken, func(f JobFile) bool { return f.Name == out }); i++ {
		out = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
	return out
}

// placeFiles copies a Job's files into dir (the agent's home or a scratch
// directory) and says where the agent finds them, loading images small
// enough to send in the prompt.
func placeFiles(files []JobFile, dir, agentDir string) ([]acp.Attachment, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	const maxInlineImage = 5 << 20
	var out []acp.Attachment
	for _, f := range files {
		data, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(dir, f.Name), data, 0o600); err != nil {
			return nil, err
		}
		a := acp.Attachment{Name: f.Name, MimeType: f.MimeType, Path: path.Join(agentDir, f.Name)}
		if len(data) <= maxInlineImage {
			a.Data = data
		}
		out = append(out, a)
	}
	return out, nil
}
