package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const taskCols = `id, project_id, title, body, status, COALESCE(assignee_member_id::text, ''),
	COALESCE(created_by_member_id::text, ''), COALESCE(issue_number, 0), issue_url, created_at, updated_at`

func scanTask(row interface{ Scan(...any) error }, t *models.Task) error {
	return row.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Body, &t.Status, &t.AssigneeMemberID,
		&t.CreatedByMemberID, &t.IssueNumber, &t.IssueURL, &t.CreatedAt, &t.UpdatedAt)
}

func (s *Store) InsertTask(ctx context.Context, t models.Task) error {
	_, err := s.q.Exec(ctx, `INSERT INTO tasks (id, project_id, title, body, status, assignee_member_id, created_by_member_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
		t.ID, t.ProjectID, t.Title, t.Body, t.Status, nullID(t.AssigneeMemberID), nullID(t.CreatedByMemberID), t.CreatedAt)
	return mapErr(err)
}

// GetTask returns one Task. forUpdate locks the row until the transaction ends.
func (s *Store) GetTask(ctx context.Context, id string, forUpdate bool) (*models.Task, error) {
	sql := `SELECT ` + taskCols + ` FROM tasks WHERE id=$1`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	var t models.Task
	err := scanTask(s.q.QueryRow(ctx, sql, id), &t)
	return &t, mapErr(err)
}

// ListTasks returns a Project's Tasks, newest first.
func (s *Store) ListTasks(ctx context.Context, projectID string) ([]models.Task, error) {
	rows, err := s.q.Query(ctx, `SELECT `+taskCols+` FROM tasks WHERE project_id=$1 ORDER BY created_at DESC, id`, projectID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []models.Task{}
	for rows.Next() {
		var t models.Task
		if err := scanTask(rows, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpdateTask writes title, body, status, assignee and updated_at.
func (s *Store) UpdateTask(ctx context.Context, t models.Task) error {
	return one(s.q.Exec(ctx, `UPDATE tasks SET title=$2, body=$3, status=$4, assignee_member_id=$5, updated_at=$6 WHERE id=$1`,
		t.ID, t.Title, t.Body, t.Status, nullID(t.AssigneeMemberID), t.UpdatedAt))
}

// UpsertIssueTask creates or refreshes the Task for a Repo Issue in one
// statement, so concurrent syncs and webhooks cannot duplicate it.
func (s *Store) UpsertIssueTask(ctx context.Context, projectID string, number int, title, url string, at time.Time) (*models.Task, bool, error) {
	var id string
	var inserted bool
	err := s.q.QueryRow(ctx, `INSERT INTO tasks (id, project_id, title, status, issue_number, issue_url, created_at, updated_at)
		VALUES ($1, $2, $3, 'open', $4, $5, $6, $6)
		ON CONFLICT (project_id, issue_number) WHERE issue_number IS NOT NULL
		DO UPDATE SET title=EXCLUDED.title, issue_url=EXCLUDED.issue_url, updated_at=EXCLUDED.updated_at
		RETURNING id, (xmax = 0)`, uuid.NewString(), projectID, title, number, url, at).Scan(&id, &inserted)
	if err != nil {
		return nil, false, mapErr(err)
	}
	t, err := s.GetTask(ctx, id, false)
	return t, inserted, err
}

// --- handoffs ---

const handoffCols = `id, task_id, project_id, from_member_id, to_member_id, note, status, created_at, completed_at`

func scanHandoff(row interface{ Scan(...any) error }, h *models.Handoff) error {
	return row.Scan(&h.ID, &h.TaskID, &h.ProjectID, &h.FromMemberID, &h.ToMemberID, &h.Note, &h.Status, &h.CreatedAt, &h.CompletedAt)
}

func (s *Store) InsertHandoff(ctx context.Context, h models.Handoff) error {
	_, err := s.q.Exec(ctx, `INSERT INTO handoffs (id, task_id, project_id, from_member_id, to_member_id, note, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		h.ID, h.TaskID, h.ProjectID, h.FromMemberID, h.ToMemberID, h.Note, h.Status, h.CreatedAt)
	return mapErr(err)
}

func (s *Store) GetHandoff(ctx context.Context, id string) (*models.Handoff, error) {
	var h models.Handoff
	err := scanHandoff(s.q.QueryRow(ctx, `SELECT `+handoffCols+` FROM handoffs WHERE id=$1`, id), &h)
	return &h, mapErr(err)
}

// ListHandoffs returns a Task's Handoffs, oldest first.
func (s *Store) ListHandoffs(ctx context.Context, taskID string) ([]models.Handoff, error) {
	rows, err := s.q.Query(ctx, `SELECT `+handoffCols+` FROM handoffs WHERE task_id=$1 ORDER BY created_at, id`, taskID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []models.Handoff{}
	for rows.Next() {
		var h models.Handoff
		if err := scanHandoff(rows, &h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// CompleteHandoff closes an open Handoff; ErrConflict if it was not open.
func (s *Store) CompleteHandoff(ctx context.Context, id string, at time.Time) (*models.Handoff, error) {
	var h models.Handoff
	err := scanHandoff(s.q.QueryRow(ctx, `UPDATE handoffs SET status='complete', completed_at=$2
		WHERE id=$1 AND status='open' RETURNING `+handoffCols, id, at), &h)
	if err = mapErr(err); err == ErrNotFound {
		if _, gerr := s.GetHandoff(ctx, id); gerr == nil {
			return nil, ErrConflict
		}
	}
	return &h, err
}

// --- decisions ---

const decisionCols = `id, project_id, COALESCE(task_id::text, ''), prompt, options, recommendation, COALESCE(answer, ''),
	COALESCE(answered_by_member_id::text, ''), reused, fingerprint, COALESCE(assignee_member_id::text, ''), created_at, answered_at`

func scanDecision(row interface{ Scan(...any) error }, d *models.Decision) error {
	var raw []byte
	if err := row.Scan(&d.ID, &d.ProjectID, &d.TaskID, &d.Prompt, &raw, &d.Recommendation, &d.Answer,
		&d.AnsweredByMemberID, &d.Reused, &d.Fingerprint, &d.AssigneeMemberID, &d.CreatedAt, &d.AnsweredAt); err != nil {
		return err
	}
	_ = json.Unmarshal(raw, &d.Options)
	if d.Options == nil {
		d.Options = []string{}
	}
	return nil
}

func (s *Store) InsertDecision(ctx context.Context, d models.Decision) error {
	if d.Options == nil {
		d.Options = []string{}
	}
	raw, err := json.Marshal(d.Options)
	if err != nil {
		return err
	}
	var answer any
	if d.Answer != "" {
		answer = d.Answer
	}
	_, err = s.q.Exec(ctx, `INSERT INTO decisions (id, project_id, task_id, prompt, options, recommendation, answer,
		answered_by_member_id, reused, fingerprint, assignee_member_id, created_at, answered_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		d.ID, d.ProjectID, nullID(d.TaskID), d.Prompt, raw, d.Recommendation, answer,
		nullID(d.AnsweredByMemberID), d.Reused, d.Fingerprint, nullID(d.AssigneeMemberID), d.CreatedAt, d.AnsweredAt)
	return mapErr(err)
}

func (s *Store) GetDecision(ctx context.Context, id string) (*models.Decision, error) {
	var d models.Decision
	err := scanDecision(s.q.QueryRow(ctx, `SELECT `+decisionCols+` FROM decisions WHERE id=$1`, id), &d)
	return &d, mapErr(err)
}

// ListDecisions returns a Project's Decisions, newest first.
func (s *Store) ListDecisions(ctx context.Context, projectID string) ([]models.Decision, error) {
	rows, err := s.q.Query(ctx, `SELECT `+decisionCols+` FROM decisions WHERE project_id=$1 ORDER BY created_at DESC, id`, projectID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []models.Decision{}
	for rows.Next() {
		var d models.Decision
		if err := scanDecision(rows, &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AnswerDecision records the answer once; ErrConflict if already answered.
func (s *Store) AnswerDecision(ctx context.Context, id, answer, memberID string, at time.Time) (*models.Decision, error) {
	var d models.Decision
	err := scanDecision(s.q.QueryRow(ctx, `UPDATE decisions SET answer=$2, answered_by_member_id=$3, answered_at=$4
		WHERE id=$1 AND answer IS NULL RETURNING `+decisionCols, id, answer, nullID(memberID), at), &d)
	if err = mapErr(err); err == ErrNotFound {
		if _, gerr := s.GetDecision(ctx, id); gerr == nil {
			return nil, ErrConflict
		}
	}
	return &d, err
}

// GetDecisionMemory returns the remembered answer for a fingerprint.
func (s *Store) GetDecisionMemory(ctx context.Context, projectID, fingerprint string) (*models.DecisionMemory, error) {
	var m models.DecisionMemory
	err := s.q.QueryRow(ctx, `SELECT id, project_id, fingerprint, prompt, answer, COALESCE(decision_id::text, ''), created_at, updated_at
		FROM decision_memories WHERE project_id=$1 AND fingerprint=$2`, projectID, fingerprint).
		Scan(&m.ID, &m.ProjectID, &m.Fingerprint, &m.Prompt, &m.Answer, &m.DecisionID, &m.CreatedAt, &m.UpdatedAt)
	return &m, mapErr(err)
}

// UpsertDecisionMemory remembers (or replaces) the answer for a fingerprint.
func (s *Store) UpsertDecisionMemory(ctx context.Context, m models.DecisionMemory) error {
	_, err := s.q.Exec(ctx, `INSERT INTO decision_memories (id, project_id, fingerprint, prompt, answer, decision_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (project_id, fingerprint) DO UPDATE
		SET prompt=EXCLUDED.prompt, answer=EXCLUDED.answer, decision_id=EXCLUDED.decision_id, updated_at=EXCLUDED.updated_at`,
		uuid.NewString(), m.ProjectID, m.Fingerprint, m.Prompt, m.Answer, nullID(m.DecisionID), m.UpdatedAt)
	return mapErr(err)
}

// ListDecisionMemories returns a Project's remembered answers, newest first.
func (s *Store) ListDecisionMemories(ctx context.Context, projectID string) ([]models.DecisionMemory, error) {
	rows, err := s.q.Query(ctx, `SELECT id, project_id, fingerprint, prompt, answer, COALESCE(decision_id::text, ''), created_at, updated_at
		FROM decision_memories WHERE project_id=$1 ORDER BY updated_at DESC, id`, projectID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (models.DecisionMemory, error) {
		var m models.DecisionMemory
		err := r.Scan(&m.ID, &m.ProjectID, &m.Fingerprint, &m.Prompt, &m.Answer, &m.DecisionID, &m.CreatedAt, &m.UpdatedAt)
		return m, err
	})
}
