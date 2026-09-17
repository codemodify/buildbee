package core

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/google/uuid"
)

// DecisionFilter narrows a Decision listing.
type DecisionFilter struct {
	Open bool // unanswered only
	Mine bool // assigned to the acting Person, or to nobody
}

// Decisions lists a Project's Decisions, newest first.
func (s *Service) Decisions(ctx context.Context, a Actor, projectID string, f DecisionFilter) ([]models.Decision, error) {
	if _, err := s.st.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	items, err := s.st.ListDecisions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var mine string
	if f.Mine && a.IsPerson() {
		if m, err := s.st.MemberForPerson(ctx, projectID, a.PersonID); err == nil {
			mine = m.ID
		}
	}
	out := items[:0]
	for _, d := range items {
		if f.Open && d.Answer != "" {
			continue
		}
		if f.Mine && d.AssigneeMemberID != "" && d.AssigneeMemberID != mine {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

// DecisionMemories lists a Project's remembered answers.
func (s *Service) DecisionMemories(ctx context.Context, projectID string) ([]models.DecisionMemory, error) {
	if _, err := s.st.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.st.ListDecisionMemories(ctx, projectID)
}

// NewDecision describes a question for the Project.
type NewDecision struct {
	Prompt           string   `json:"prompt"`
	Options          []string `json:"options"`
	Recommendation   string   `json:"recommendation"`
	AssigneeMemberID string   `json:"assignee_id"`
	TaskID           string   `json:"task_id"`
}

// CreateDecision asks a question. If the same question (after normalizing)
// was answered before, the remembered answer applies and nobody is asked.
func (s *Service) CreateDecision(ctx context.Context, a Actor, projectID string, in NewDecision) (*models.Decision, error) {
	prompt, err := text("prompt", in.Prompt, true, 2000)
	if err != nil {
		return nil, err
	}
	rec, err := text("recommendation", in.Recommendation, false, 500)
	if err != nil {
		return nil, err
	}
	var options []string
	for _, o := range in.Options {
		o, err := text("option", o, false, 200)
		if err != nil {
			return nil, err
		}
		if o != "" {
			options = append(options, o)
		}
	}
	if len(options) > 20 {
		return nil, invalid("at most 20 options")
	}
	var out *models.Decision
	err = s.tx(ctx, func(w *work) error {
		if _, err := w.openProject(ctx, projectID); err != nil {
			return err
		}
		m, err := w.member(ctx, a, projectID, true)
		if err != nil {
			return err
		}
		if in.AssigneeMemberID != "" {
			if err := w.sameProject(ctx, projectID, in.AssigneeMemberID); err != nil {
				return err
			}
		}
		if in.TaskID != "" {
			t, err := w.st.GetTask(ctx, in.TaskID, false)
			if err != nil || t.ProjectID != projectID {
				return invalid("task_id is not a task in this project")
			}
		}
		out, err = w.openDecision(ctx, projectID, whoOf(m, a), m, newDecision{
			prompt: prompt, recommendation: rec, options: options, assignee: in.AssigneeMemberID, taskID: in.TaskID,
		})
		return err
	})
	return out, err
}

type newDecision struct {
	prompt, recommendation, assignee, taskID string
	options                                  []string
	action                                   string // e.g. "merge"; never answered from memory
	commit                                   string // for merge: the commit asked about
	runID                                    string // for permission: the Run waiting on it
}

// openDecision records a Decision, reusing a remembered answer when there is
// one; otherwise it notifies the assignee, or every Person but the asker.
func (w *work) openDecision(ctx context.Context, projectID string, by who, asker *models.Member, in newDecision) (*models.Decision, error) {
	d := models.Decision{ID: uuid.NewString(), ProjectID: projectID, TaskID: in.taskID, Prompt: in.prompt,
		Options: in.options, Recommendation: in.recommendation, Fingerprint: models.DecisionFingerprint(in.prompt),
		AssigneeMemberID: in.assignee, Action: in.action, Commit: in.commit, RunID: in.runID, CreatedAt: w.now}
	if d.Options == nil {
		d.Options = []string{}
	}
	mem, err := w.st.GetDecisionMemory(ctx, projectID, d.Fingerprint)
	if err != nil && err != store.ErrNotFound {
		return nil, err
	}
	if err == nil && d.Action == "" {
		t := w.now
		d.Answer, d.Reused, d.AnsweredAt = mem.Answer, true, &t
	}
	if err := w.st.InsertDecision(ctx, d); err != nil {
		return nil, err
	}
	if d.Reused {
		return &d, w.activity(ctx, projectID, by, models.TypeDecision, "reused", d.ID,
			map[string]any{"prompt": truncate(d.Prompt, 300), "answer": d.Answer, "from_decision_id": mem.DecisionID})
	}
	if err := w.activity(ctx, projectID, by, models.TypeDecision, "created", d.ID,
		map[string]any{"prompt": truncate(d.Prompt, 300), "task_id": d.TaskID}); err != nil {
		return nil, err
	}
	if d.TaskID != "" {
		if task, err := w.st.GetTask(ctx, d.TaskID, false); err == nil {
			if err := w.note(ctx, task, w.voice(ctx, projectID), "Decision: "+d.Prompt); err != nil {
				return nil, err
			}
		}
	}
	n := notice{projectID: projectID, kind: "decision", title: "Decision needed: " + d.Prompt, body: d.Prompt, href: href(projectID)}
	if d.AssigneeMemberID != "" {
		assignee, err := w.st.GetMember(ctx, d.AssigneeMemberID)
		if err != nil {
			return nil, err
		}
		return &d, w.notifyMember(ctx, assignee, n)
	}
	return &d, w.notifyPeople(ctx, projectID, asker, n)
}

// PermissionAsk is an agent asking before it acts, as its worker relays it.
type PermissionAsk struct {
	Title   string             `json:"title"`
	Options []PermissionChoice `json:"options"`
}

// PermissionChoice is one answer the agent offers.
type PermissionChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// AskPermission turns an agent's permission request into a Decision on its
// Task, for a Project whose agents ask (agent_permissions "ask"). The
// worker running the Run polls the Decision and relays the answer.
func (s *Service) AskPermission(ctx context.Context, a Actor, runID string, in PermissionAsk) (*models.Decision, error) {
	if a.Agent == "" {
		return nil, invalid("only the worker running the Run asks for permission")
	}
	title, err := text("title", in.Title, true, 500)
	if err != nil {
		return nil, err
	}
	var names []string
	recommend := ""
	for _, o := range in.Options {
		n := strings.TrimSpace(o.Name)
		if n == "" {
			n = o.ID
		}
		if n == "" || slices.Contains(names, n) {
			continue
		}
		names = append(names, truncate(n, 100))
		if recommend == "" && o.Kind == "allow_once" {
			recommend = n
		}
	}
	if len(names) == 0 {
		return nil, invalid("a permission request needs options")
	}
	var out *models.Decision
	err = s.tx(ctx, func(w *work) error {
		r, err := w.st.GetRun(ctx, runID, true)
		if err != nil {
			return err
		}
		if r.Worker != a.Agent || r.Status != models.RunRunning {
			return fmt.Errorf("%w: run is not running on worker %s", store.ErrConflict, a.Agent)
		}
		var bot *models.Member // the Run's own Bot; not the Project's voice
		who := "The agent"
		if r.BotMemberID != "" {
			if bot, err = w.st.GetMember(ctx, r.BotMemberID); err != nil {
				return err
			}
			who = bot.DisplayName
		}
		out, err = w.openDecision(ctx, r.ProjectID, w.runActor(ctx, a, r), bot, newDecision{
			prompt: who + " asks to: " + title, options: names, recommendation: recommend,
			taskID: r.TaskID, action: "permission", runID: r.ID})
		return err
	})
	return out, err
}

// Decision returns one Decision.
func (s *Service) Decision(ctx context.Context, id string) (*models.Decision, error) {
	return s.st.GetDecision(ctx, id)
}

// runEnded closes the permission questions a finished Run left open:
// nobody waits for their answers any more.
func (w *work) runEnded(ctx context.Context, a Actor, r *models.Run) error {
	closed, err := w.st.CloseRunDecisions(ctx, r.ID, "not needed: the run ended", w.now)
	if err != nil {
		return err
	}
	for _, d := range closed {
		if err := w.activity(ctx, d.ProjectID, w.runActor(ctx, a, r), models.TypeDecision, "answered", d.ID,
			map[string]any{"answer": d.Answer}); err != nil {
			return err
		}
	}
	return nil
}

// AnswerDecision records the answer (once) and remembers it for the same
// question in the future.
func (s *Service) AnswerDecision(ctx context.Context, a Actor, id, answer string) (*models.Decision, error) {
	ans, err := text("answer", answer, true, 2000)
	if err != nil {
		return nil, err
	}
	var out *models.Decision
	err = s.tx(ctx, func(w *work) error {
		d, err := w.st.GetDecision(ctx, id)
		if err != nil {
			return err
		}
		if _, err := w.openProject(ctx, d.ProjectID); err != nil {
			return err
		}
		m, err := w.member(ctx, a, d.ProjectID, true)
		if err != nil {
			return err
		}
		var memberID string
		if m != nil {
			memberID = m.ID
		}
		out, err = w.st.AnswerDecision(ctx, id, ans, memberID, w.now)
		if err == store.ErrConflict {
			return fmt.Errorf("%w: decision was already answered", store.ErrConflict)
		}
		if err != nil {
			return err
		}
		if out.Action == "" { // actions are one-off; remembering them would repeat them
			if err := w.st.UpsertDecisionMemory(ctx, models.DecisionMemory{ProjectID: d.ProjectID, Fingerprint: out.Fingerprint,
				Prompt: out.Prompt, Answer: ans, DecisionID: out.ID, UpdatedAt: w.now}); err != nil {
				return err
			}
		}
		by := whoOf(m, a)
		if err := w.activity(ctx, d.ProjectID, by, models.TypeDecision, "answered", d.ID,
			map[string]any{"prompt": truncate(d.Prompt, 300), "answer": ans}); err != nil {
			return err
		}
		return w.onDecision(ctx, out, by)
	})
	return out, err
}
