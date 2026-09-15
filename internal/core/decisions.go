package core

import (
	"context"
	"fmt"

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
}

// openDecision records a Decision, reusing a remembered answer when there is
// one; otherwise it notifies the assignee, or every Person but the asker.
func (w *work) openDecision(ctx context.Context, projectID string, by who, asker *models.Member, in newDecision) (*models.Decision, error) {
	d := models.Decision{ID: uuid.NewString(), ProjectID: projectID, TaskID: in.taskID, Prompt: in.prompt,
		Options: in.options, Recommendation: in.recommendation, Fingerprint: models.DecisionFingerprint(in.prompt),
		AssigneeMemberID: in.assignee, Action: in.action, Commit: in.commit, CreatedAt: w.now}
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
