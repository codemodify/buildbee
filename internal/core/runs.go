package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/google/uuid"
)

// --- runs ---

// Runs lists a Task's Runs, newest first.
func (s *Service) Runs(ctx context.Context, taskID string) ([]models.Run, error) {
	if _, err := s.st.GetTask(ctx, taskID, false); err != nil {
		return nil, err
	}
	return s.st.ListRuns(ctx, taskID)
}

// Run returns one Run.
func (s *Service) Run(ctx context.Context, id string) (*models.Run, error) {
	return s.st.GetRun(ctx, id, false)
}

// NewRun describes a Run to queue. BotMemberID defaults to the Task's
// assignee if that is a Bot; Agent defaults to the Bot's agent, and empty
// means any agent the claiming worker offers.
type NewRun struct {
	BotMemberID string `json:"bot_member_id"`
	Agent       string `json:"agent"`
	// Kind is plan, build or review; empty means what the Bot's role does.
	Kind string `json:"kind"`
}

// CreateRun queues a Run of a Task for a worker to claim.
func (s *Service) CreateRun(ctx context.Context, a Actor, taskID string, in NewRun) (*models.Run, error) {
	botMemberID := strings.TrimSpace(in.BotMemberID)
	agent := strings.ToLower(strings.TrimSpace(in.Agent))
	if !models.ValidAgent(agent) {
		return nil, invalid("agent must be one of %s", strings.Join(models.Agents, ", "))
	}
	kind := models.RunKind(strings.ToLower(strings.TrimSpace(in.Kind)))
	switch kind {
	case "", models.RunPlan, models.RunBuild, models.RunReview:
	default:
		return nil, invalid("kind must be plan, build or review")
	}
	var out *models.Run
	err := s.tx(ctx, func(w *work) error {
		task, err := w.st.GetTask(ctx, taskID, false)
		if err != nil {
			return err
		}
		proj, err := w.openProject(ctx, task.ProjectID)
		if err != nil {
			return err
		}
		m, err := w.member(ctx, a, task.ProjectID, true)
		if err != nil {
			return err
		}
		if botMemberID == "" && task.AssigneeMemberID != "" {
			if assignee, err := w.st.GetMember(ctx, task.AssigneeMemberID); err == nil && assignee.Kind == models.KindBot {
				botMemberID = assignee.ID
			}
		}
		var bot *models.Member
		if botMemberID != "" {
			bot, err = w.st.GetMember(ctx, botMemberID)
			if err != nil || bot.ProjectID != task.ProjectID || bot.Kind != models.KindBot {
				return invalid("bot_member_id is not a bot in this project")
			}
		}
		if kind == models.RunReview && task.Branch == "" {
			return invalid("the Task has no branch to review yet")
		}
		out, err = w.createRun(ctx, proj, task, bot, agent, kind, whoOf(m, a))
		return err
	})
	return out, err
}

// createRun queues a Run executed by bot (optional) with agent ("" = the
// Bot's agent, else any) and kind ("" = what the Bot's role does),
// prompted with the Project's guidance, the Task and its handoff notes.
func (w *work) createRun(ctx context.Context, proj *models.Project, task *models.Task, bot *models.Member, agent string,
	kind models.RunKind, by who) (*models.Run, error) {
	notes, err := w.st.ListHandoffs(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if kind == "" {
		kind = models.RunBuild
		if bot != nil {
			kind = models.RunKindForRole(bot.Role)
		}
	}
	r := models.Run{ID: uuid.NewString(), TaskID: task.ID, ProjectID: task.ProjectID, Kind: kind,
		Status: models.RunPending, Detail: "queued", Agent: agent, Prompt: runPrompt(proj, bot, task, notes, kind),
		CreatedAt: w.now, UpdatedAt: w.now}
	if bot != nil {
		r.BotMemberID = bot.ID
		if r.Agent == "" {
			r.Agent = bot.Agent
		}
	}
	if err := w.st.InsertRun(ctx, r); err != nil {
		return nil, err
	}
	w.queued = true
	if err := w.runEvent(ctx, r.ID, models.RunEventStatus, map[string]any{"status": r.Status, "detail": r.Detail}); err != nil {
		return nil, err
	}
	if err := w.note(ctx, task, bot, startNote(r)); err != nil {
		return nil, err
	}
	return &r, w.activity(ctx, task.ProjectID, by, models.TypeRun, "created", r.ID,
		map[string]any{"task_id": task.ID, "bot_member_id": r.BotMemberID, "agent": r.Agent, "kind": r.Kind})
}

// startNote is what a Bot says in the Task's thread when its Run is queued.
func startNote(r models.Run) string {
	with := ""
	if r.Agent != "" {
		with = " with " + r.Agent
	}
	switch r.Kind {
	case models.RunPlan:
		return "I'll look into this and write a plan" + with + "."
	case models.RunReview:
		return "Reviewing the changes" + with + "."
	default:
		return "Working on this" + with + "."
	}
}

// RunReport is a worker's report on a Run. Summary, Branch and PRURL are
// the outcome of a finished Run.
type RunReport struct {
	Status  string `json:"status"`
	Detail  string `json:"detail"`
	Summary string `json:"summary"`
	Branch  string `json:"branch"`
	Commit  string `json:"commit"`
	PRURL   string `json:"pr_url"`
}

// UpdateRun changes a Run's status and detail; see ReportRun.
func (s *Service) UpdateRun(ctx context.Context, a Actor, id, status, detail string) (*models.Run, error) {
	return s.ReportRun(ctx, a, id, RunReport{Status: status, Detail: detail})
}

// ReportRun moves a Run through its lifecycle: pending → running →
// succeeded | failed | canceled (or straight to failed/canceled). Finished
// Runs cannot change. Repeating the current status only updates the detail.
// Workers report progress and outcomes; people can only cancel. When a Run
// finishes, autopilot moves its Task on (see advance).
func (s *Service) ReportRun(ctx context.Context, a Actor, id string, rep RunReport) (*models.Run, error) {
	next, ok := models.ParseRunStatus(rep.Status)
	if !ok {
		return nil, invalid("status must be pending, running, succeeded, failed or canceled")
	}
	if a.Worker == "" && (next != models.RunCanceled || rep.Summary != "" || rep.Branch != "" || rep.PRURL != "" || rep.Commit != "") {
		return nil, invalid("a Run's progress is reported by the worker running it; people can only cancel it")
	}
	d, err := text("detail", rep.Detail, false, 2000)
	if err != nil {
		return nil, err
	}
	summary, err := text("summary", rep.Summary, false, 16000)
	if err != nil {
		return nil, err
	}
	branch, err := text("branch", rep.Branch, false, 250)
	if err != nil {
		return nil, err
	}
	prURL, err := text("pr_url", rep.PRURL, false, 500)
	if err != nil {
		return nil, err
	}
	commit := strings.TrimSpace(rep.Commit)
	if !validCommit(commit) {
		return nil, invalid("commit must be a hex commit id")
	}
	var out *models.Run
	err = s.tx(ctx, func(w *work) error {
		r, err := w.st.GetRun(ctx, id, true)
		if err != nil {
			return err
		}
		if err := ownRun(a, r); err != nil {
			return err
		}
		if next != r.Status && !r.Status.CanBecome(next) {
			return fmt.Errorf("%w: run is %s and cannot become %s", store.ErrConflict, r.Status, next)
		}
		if r.Status.Terminal() {
			return fmt.Errorf("%w: run already finished (%s)", store.ErrConflict, r.Status)
		}
		changed := next != r.Status
		r.Status, r.Detail, r.UpdatedAt = next, d, w.now
		if summary != "" {
			r.Summary = summary
		}
		if branch != "" {
			r.Branch = branch
		}
		if prURL != "" {
			r.PRURL = prURL
		}
		if commit != "" && r.Kind != models.RunMerge {
			r.Commit = commit
		}
		if r.Kind == models.RunReview && next == models.RunSucceeded {
			r.Verdict = models.ParseVerdict(r.Summary)
		}
		if next == models.RunRunning && r.StartedAt == nil {
			t := w.now
			r.StartedAt = &t
		}
		if next.Terminal() {
			t := w.now
			r.FinishedAt, r.LeaseUntil = &t, nil
		}
		if err := w.st.UpdateRun(ctx, *r); err != nil {
			return err
		}
		out = r
		if err := w.runEvent(ctx, r.ID, models.RunEventStatus, map[string]any{"status": r.Status, "detail": r.Detail}); err != nil {
			return err
		}
		if !changed {
			return nil
		}
		by := w.runActor(ctx, a, r)
		payload := map[string]any{"task_id": r.TaskID, "detail": d, "kind": r.Kind}
		if r.Verdict != "" {
			payload["verdict"] = r.Verdict
		}
		if err := w.activity(ctx, r.ProjectID, by, models.TypeRun, string(next), r.ID, payload); err != nil {
			return err
		}
		if next.Terminal() {
			return w.advance(ctx, r)
		}
		return nil
	})
	return out, err
}

// runActor attributes worker-side changes to the Run's Bot.
func (w *work) runActor(ctx context.Context, a Actor, r *models.Run) who {
	if a.Worker != "" && r.BotMemberID != "" {
		if bot, err := w.st.GetMember(ctx, r.BotMemberID); err == nil {
			return whoOf(bot, a)
		}
	}
	m, _ := w.member(ctx, a, r.ProjectID, false)
	return whoOf(m, a)
}

func (w *work) runEvent(ctx context.Context, runID, kind string, payload map[string]any) error {
	ev, err := w.st.AppendRunEvent(ctx, runID, kind, payload, w.now)
	if err != nil {
		return err
	}
	w.emit("run:"+runID, int64(ev.Seq), "run_event", ev)
	return nil
}

// AppendRunEvent records one event of a running Run (agent tokens, tool
// calls, logs). Finished Runs accept no more events.
func (s *Service) AppendRunEvent(ctx context.Context, a Actor, runID, kind string, payload map[string]any) (*models.RunEvent, error) {
	k := models.NormalizeRunEventKind(kind)
	if k == "" {
		return nil, invalid("kind must be token, thought, plan, tool_call, tool_result, usage, status or log")
	}
	var out *models.RunEvent
	err := s.tx(ctx, func(w *work) error {
		r, err := w.st.GetRun(ctx, runID, false)
		if err != nil {
			return err
		}
		if err := ownRun(a, r); err != nil {
			return err
		}
		if r.Status.Terminal() {
			return fmt.Errorf("%w: run already finished (%s)", store.ErrConflict, r.Status)
		}
		ev, err := w.st.AppendRunEvent(ctx, runID, k, payload, w.now)
		if err != nil {
			return err
		}
		out = ev
		w.emit("run:"+runID, int64(ev.Seq), "run_event", ev)
		if k == models.RunEventUsage {
			used, size, cost, currency := usageOf(payload)
			return w.st.RecordUsage(ctx, runID, used, size, cost, currency)
		}
		return nil
	})
	return out, err
}

// SteerRun passes a person's message to the agent working on a Run. A
// queued Run gets it in its prompt; a running one from its worker, after
// the agent's current turn, or at once with interrupt (the agent stops
// what it is doing and reads the message).
func (s *Service) SteerRun(ctx context.Context, a Actor, runID, message string, interrupt bool) (*models.RunEvent, error) {
	msg, err := text("text", message, true, 8000)
	if err != nil {
		return nil, err
	}
	var out *models.RunEvent
	err = s.tx(ctx, func(w *work) error {
		r, err := w.st.GetRun(ctx, runID, true)
		if err != nil {
			return err
		}
		m, err := w.requireMember(ctx, a, r.ProjectID)
		if err != nil {
			return err
		}
		if r.Status.Terminal() {
			return fmt.Errorf("%w: run already finished (%s)", store.ErrConflict, r.Status)
		}
		if r.Kind == models.RunMerge {
			return invalid("a merge Run has no agent to talk to")
		}
		out, err = w.steer(ctx, r, m, a, msg, interrupt)
		return err
	})
	return out, err
}

// steer records a person's message for the agent on r: in the prompt of a
// queued Run, as a steer event its worker delivers for a running one.
func (w *work) steer(ctx context.Context, r *models.Run, m *models.Member, a Actor, msg string, interrupt bool) (*models.RunEvent, error) {
	if r.Status == models.RunPending {
		if err := w.st.UpdateRunPrompt(ctx, r.ID, r.Prompt+"\n\nMessage from "+m.DisplayName+": "+msg+"\n"); err != nil {
			return nil, err
		}
	}
	ev, err := w.st.AppendRunEvent(ctx, r.ID, models.RunEventSteer,
		map[string]any{"text": msg, "by": m.DisplayName, "interrupt": interrupt, "queued": r.Status == models.RunPending}, w.now)
	if err != nil {
		return nil, err
	}
	w.emit("run:"+r.ID, int64(ev.Seq), "run_event", ev)
	return ev, w.activity(ctx, r.ProjectID, whoOf(m, a), models.TypeRun, "steered", r.ID,
		map[string]any{"task_id": r.TaskID, "text": truncate(msg, 300), "interrupt": interrupt})
}

// usageOf reads an ACP usage report: {used, size, cost: {amount, currency}}.
func usageOf(p map[string]any) (used, size int64, cost float64, currency string) {
	num := func(v any) float64 { f, _ := v.(float64); return max(f, 0) }
	used, size = int64(num(p["used"])), int64(num(p["size"]))
	if c, ok := p["cost"].(map[string]any); ok {
		cost = num(c["amount"])
		if cur, _ := c["currency"].(string); len(cur) <= 8 {
			currency = strings.ToUpper(cur)
		}
	}
	return used, size, cost, currency
}

// Usage sums what Runs used in the last days, for one Project or ("") all.
func (s *Service) Usage(ctx context.Context, projectID string, days int) (*models.Usage, error) {
	if days <= 0 || days > 3660 {
		days = 30
	}
	if projectID != "" {
		if _, err := s.st.GetProject(ctx, projectID); err != nil {
			return nil, err
		}
	}
	return s.st.Usage(ctx, projectID, s.now().AddDate(0, 0, -days))
}

// RunEvents returns a Run's events after seq `after`.
func (s *Service) RunEvents(ctx context.Context, runID string, after, limit int) ([]models.RunEvent, bool, error) {
	if _, err := s.st.GetRun(ctx, runID, false); err != nil {
		return nil, false, err
	}
	return s.st.ListRunEvents(ctx, runID, after, limit)
}

// --- artifacts ---

// NewArtifact describes an output of a Task or Run.
type NewArtifact struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	RunID string `json:"run_id"`
}

// maxArtifactBody caps Artifacts stored in Postgres; larger outputs belong in
// object storage (roadmap).
const maxArtifactBody = 16 << 20

// CreateArtifact attaches an output to a Task (and optionally one of its Runs).
func (s *Service) CreateArtifact(ctx context.Context, a Actor, taskID string, in NewArtifact) (*models.Artifact, error) {
	n, err := text("name", in.Name, true, 200)
	if err != nil {
		return nil, err
	}
	if len(in.Body) > maxArtifactBody {
		return nil, invalid("body is larger than %d bytes", maxArtifactBody)
	}
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind == "" {
		kind = "file"
	}
	var out *models.Artifact
	err = s.tx(ctx, func(w *work) error {
		task, err := w.st.GetTask(ctx, taskID, false)
		if err != nil {
			return err
		}
		if in.RunID != "" {
			r, err := w.st.GetRun(ctx, in.RunID, false)
			if err != nil || r.TaskID != taskID {
				return invalid("run_id is not a run of this task")
			}
			if err := ownRun(a, r); err != nil {
				return err
			}
		}
		out = &models.Artifact{ID: uuid.NewString(), ProjectID: task.ProjectID, TaskID: taskID, RunID: in.RunID,
			Kind: kind, Name: n, Body: in.Body, Size: len(in.Body), URL: strings.TrimSpace(in.URL), CreatedAt: w.now}
		if err := w.st.InsertArtifact(ctx, *out); err != nil {
			return err
		}
		by := w.artifactActor(ctx, a, task, in.RunID)
		return w.activity(ctx, task.ProjectID, by, models.TypeArtifact, "created", out.ID,
			map[string]any{"task_id": taskID, "run_id": in.RunID, "kind": kind, "name": n})
	})
	return out, err
}

func (w *work) artifactActor(ctx context.Context, a Actor, task *models.Task, runID string) who {
	if runID != "" {
		if r, err := w.st.GetRun(ctx, runID, false); err == nil {
			return w.runActor(ctx, a, r)
		}
	}
	m, _ := w.member(ctx, a, task.ProjectID, false)
	return whoOf(m, a)
}

// Artifact returns one Artifact including its Body.
func (s *Service) Artifact(ctx context.Context, id string) (*models.Artifact, error) {
	return s.st.GetArtifact(ctx, id)
}

// Artifacts lists a Task's Artifacts without bodies.
func (s *Service) Artifacts(ctx context.Context, taskID string) ([]models.Artifact, error) {
	if _, err := s.st.GetTask(ctx, taskID, false); err != nil {
		return nil, err
	}
	return s.st.ListArtifacts(ctx, taskID)
}

// --- pipelines ---

// Pipelines lists a Task's CI checks.
func (s *Service) Pipelines(ctx context.Context, taskID string) ([]models.Pipeline, error) {
	if _, err := s.st.GetTask(ctx, taskID, false); err != nil {
		return nil, err
	}
	return s.st.ListPipelines(ctx, taskID)
}

// NewPipeline describes a CI check on a Task.
type NewPipeline struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	ExternalURL string `json:"external_url"`
	ArtifactID  string `json:"artifact_id"`
	Commit      string `json:"commit"` // the commit CI checked; lets autopilot match results to pushes exactly
}

// RecordPipeline records a CI check on a Task. A failure notifies the
// Task's assignee and every Person in the Project.
func (s *Service) RecordPipeline(ctx context.Context, a Actor, taskID string, in NewPipeline) (*models.Pipeline, error) {
	n, err := text("name", in.Name, true, 200)
	if err != nil {
		return nil, err
	}
	st, ok := models.ParsePipelineStatus(in.Status)
	if !ok {
		return nil, invalid("status %q is not a CI status", in.Status)
	}
	commit := strings.ToLower(strings.TrimSpace(in.Commit))
	if !validCommit(commit) {
		return nil, invalid("commit must be a hex commit id")
	}
	var out *models.Pipeline
	err = s.tx(ctx, func(w *work) error {
		// Locked: results arriving together are judged one after another.
		task, err := w.st.GetTask(ctx, taskID, true)
		if err != nil {
			return err
		}
		p := models.Pipeline{ID: uuid.NewString(), ProjectID: task.ProjectID, TaskID: taskID, ArtifactID: in.ArtifactID,
			Name: n, Status: st, Commit: commit, ExternalURL: strings.TrimSpace(in.ExternalURL), CreatedAt: w.now, UpdatedAt: w.now}
		if p.ArtifactID != "" {
			art, err := w.st.GetArtifact(ctx, p.ArtifactID)
			if err != nil || art.TaskID != taskID {
				return invalid("artifact_id is not an artifact of this task")
			}
		}
		if err := w.st.InsertPipeline(ctx, p); err != nil {
			return err
		}
		out = &p
		return w.pipelineRecorded(ctx, a, task, &p, "recorded")
	})
	return out, err
}

// UpdatePipeline changes a CI check's status and link.
func (s *Service) UpdatePipeline(ctx context.Context, a Actor, id, status, externalURL string) (*models.Pipeline, error) {
	st, ok := models.ParsePipelineStatus(status)
	if !ok {
		return nil, invalid("status %q is not a CI status", status)
	}
	var out *models.Pipeline
	err := s.tx(ctx, func(w *work) error {
		p, err := w.st.GetPipeline(ctx, id)
		if err != nil {
			return err
		}
		task, err := w.st.GetTask(ctx, p.TaskID, true)
		if err != nil {
			return err
		}
		p.Status, p.UpdatedAt = st, w.now
		if u := strings.TrimSpace(externalURL); u != "" {
			p.ExternalURL = u
		}
		if err := w.st.UpdatePipeline(ctx, *p); err != nil {
			return err
		}
		out = p
		return w.pipelineRecorded(ctx, a, task, p, "updated")
	})
	return out, err
}

func (w *work) pipelineRecorded(ctx context.Context, a Actor, task *models.Task, p *models.Pipeline, action string) error {
	m, err := w.member(ctx, a, task.ProjectID, false)
	if err != nil {
		return err
	}
	if err := w.activity(ctx, task.ProjectID, whoOf(m, a), models.TypePipeline, action, p.ID,
		map[string]any{"task_id": task.ID, "name": p.Name, "status": p.Status}); err != nil {
		return err
	}
	if p.Status == models.PipelineFailure {
		if err := w.notifyPeople(ctx, task.ProjectID, nil, notice{projectID: task.ProjectID, kind: "pipeline",
			title: "Pipeline failed: " + p.Name + " on " + task.Title, body: p.ExternalURL, href: href(task.ProjectID, "tasks", task.ID)}); err != nil {
			return err
		}
	}
	return w.onPipeline(ctx, task, p)
}

// validCommit accepts "" or a hex commit id.
func validCommit(c string) bool {
	if len(c) > 64 {
		return false
	}
	for _, r := range c {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
