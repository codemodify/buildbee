package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/codemodify/buildbee/internal/migrate"
	"github.com/codemodify/buildbee/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is the compose-backed Store.
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

// OpenPostgres connects with retries so compose can start the Server next to Postgres.
func OpenPostgres(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	var last error
	for i := 0; i < 30; i++ {
		pool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			last = err
		} else if err = pool.Ping(ctx); err != nil {
			pool.Close()
			last = err
		} else {
			return pool, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("postgres: %w", last)
}

// Ping reports whether Postgres answers.
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// Migrate applies SQL migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return migrate.Up(ctx, pool)
}

func (p *Postgres) CreateProject(ctx context.Context, name string, autoRun bool) (*models.ProjectBundle, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t := time.Now().UTC()
	proj := models.Project{ID: uuid.NewString(), Name: name, AutoRun: autoRun, CreatedAt: t}
	human := seedHuman(proj.ID, t)
	bots := seedBots(proj.ID, t)
	ch := models.Channel{ID: uuid.NewString(), ProjectID: proj.ID, Name: "general", CreatedAt: t}

	if _, err := tx.Exec(ctx, `INSERT INTO projects (id, name, created_at, auto_run) VALUES ($1,$2,$3,$4)`, proj.ID, proj.Name, proj.CreatedAt, proj.AutoRun); err != nil {
		return nil, err
	}
	if err := insertMember(ctx, tx, human); err != nil {
		return nil, err
	}
	members := make([]models.Member, 0, 1+len(bots))
	members = append(members, human)
	for _, bot := range bots {
		if err := insertMember(ctx, tx, bot); err != nil {
			return nil, err
		}
		members = append(members, bot)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO channels (id, project_id, name, created_at) VALUES ($1,$2,$3,$4)`, ch.ID, ch.ProjectID, ch.Name, ch.CreatedAt); err != nil {
		return nil, err
	}
	if err := insertActivity(ctx, tx, proj.ID, models.TypeProject, map[string]any{"name": name, "id": proj.ID, "auto_run": autoRun}); err != nil {
		return nil, err
	}
	if err := insertActivity(ctx, tx, proj.ID, models.TypeMember, map[string]any{"id": human.ID, "kind": "human", "role": human.Role}); err != nil {
		return nil, err
	}
	for _, bot := range bots {
		if err := insertActivity(ctx, tx, proj.ID, models.TypeMember, map[string]any{"id": bot.ID, "kind": "bot", "role": bot.Role}); err != nil {
			return nil, err
		}
	}
	if err := insertActivity(ctx, tx, proj.ID, models.TypeChannel, map[string]any{"id": ch.ID, "name": ch.Name}); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO routines (id, project_id, bot_member_id, name, schedule, enabled, created_at)
		VALUES ($1,$2,$3,'morning-digest','24h',false,$4)`, uuid.NewString(), proj.ID, pulseID(bots), t); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &models.ProjectBundle{Project: proj, Members: members, Channels: []models.Channel{ch}}, nil
}

func insertMember(ctx context.Context, tx pgx.Tx, m models.Member) error {
	_, err := tx.Exec(ctx, `INSERT INTO members (id, project_id, kind, display_name, role, instructions, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, m.ID, m.ProjectID, m.Kind, m.DisplayName, m.Role, m.Instructions, m.CreatedAt)
	return err
}

func insertActivity(ctx context.Context, tx pgx.Tx, projectID, typ string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO activity (id, project_id, type, payload, created_at) VALUES ($1,$2,$3,$4,$5)`,
		uuid.NewString(), projectID, typ, raw, time.Now().UTC())
	return err
}

func (p *Postgres) addActivity(ctx context.Context, projectID, typ string, payload map[string]any) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertActivity(ctx, tx, projectID, typ, payload); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) GetProject(ctx context.Context, id string) (*models.ProjectBundle, error) {
	var proj models.Project
	err := p.pool.QueryRow(ctx, `SELECT id, name, created_at, auto_run FROM projects WHERE id=$1`, id).
		Scan(&proj.ID, &proj.Name, &proj.CreatedAt, &proj.AutoRun)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	members, err := p.ListMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	channels, err := p.ListChannels(ctx, id)
	if err != nil {
		return nil, err
	}
	return &models.ProjectBundle{Project: proj, Members: members, Channels: channels}, nil
}

func (p *Postgres) UpdateProject(ctx context.Context, id string, autoRun *bool) (*models.Project, error) {
	if autoRun != nil {
		tag, err := p.pool.Exec(ctx, `UPDATE projects SET auto_run=$2 WHERE id=$1`, id, *autoRun)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, ErrNotFound
		}
		_ = p.addActivity(ctx, id, models.TypeProject, map[string]any{"id": id, "auto_run": *autoRun, "action": "update"})
	}
	bundle, err := p.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}
	return &bundle.Project, nil
}

func (p *Postgres) ListProjects(ctx context.Context) ([]models.Project, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, name, created_at, auto_run FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Project{}
	for rows.Next() {
		var proj models.Project
		if err := rows.Scan(&proj.ID, &proj.Name, &proj.CreatedAt, &proj.AutoRun); err != nil {
			return nil, err
		}
		out = append(out, proj)
	}
	return out, rows.Err()
}

func (p *Postgres) ListMembers(ctx context.Context, projectID string) ([]models.Member, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT `+memberCols+` FROM members WHERE project_id=$1 ORDER BY created_at, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Member{}
	for rows.Next() {
		var m models.Member
		if err := scanMember(rows, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (p *Postgres) AddMember(ctx context.Context, in models.Member) (*models.Member, error) {
	if err := p.mustProject(ctx, in.ProjectID); err != nil {
		return nil, err
	}
	in.ID = uuid.NewString()
	in.CreatedAt = time.Now().UTC()
	_, err := p.pool.Exec(ctx, `INSERT INTO members (id, project_id, kind, display_name, role, instructions, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, in.ID, in.ProjectID, in.Kind, in.DisplayName, in.Role, in.Instructions, in.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, in.ProjectID, models.TypeMember, map[string]any{"id": in.ID, "kind": in.Kind})
	return &in, nil
}

func (p *Postgres) GetMember(ctx context.Context, id string) (*models.Member, error) {
	var m models.Member
	err := scanMember(p.pool.QueryRow(ctx, `SELECT `+memberCols+` FROM members WHERE id=$1`, id), &m)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &m, err
}

const memberCols = `id, project_id, kind, display_name, role, instructions, created_at`

func scanMember(row scannable, m *models.Member) error {
	return row.Scan(&m.ID, &m.ProjectID, &m.Kind, &m.DisplayName, &m.Role, &m.Instructions, &m.CreatedAt)
}

func (p *Postgres) ListChannels(ctx context.Context, projectID string) ([]models.Channel, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, name, created_at FROM channels WHERE project_id=$1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Channel{}
	for rows.Next() {
		var ch models.Channel
		if err := rows.Scan(&ch.ID, &ch.ProjectID, &ch.Name, &ch.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateChannel(ctx context.Context, projectID, name string) (*models.Channel, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	ch := models.Channel{ID: uuid.NewString(), ProjectID: projectID, Name: name, CreatedAt: time.Now().UTC()}
	if _, err := p.pool.Exec(ctx, `INSERT INTO channels (id, project_id, name, created_at) VALUES ($1,$2,$3,$4)`,
		ch.ID, ch.ProjectID, ch.Name, ch.CreatedAt); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, projectID, models.TypeChannel, map[string]any{"id": ch.ID, "name": name})
	return &ch, nil
}

func (p *Postgres) GetChannel(ctx context.Context, id string) (*models.Channel, error) {
	var ch models.Channel
	err := p.pool.QueryRow(ctx, `SELECT id, project_id, name, created_at FROM channels WHERE id=$1`, id).
		Scan(&ch.ID, &ch.ProjectID, &ch.Name, &ch.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &ch, err
}

func (p *Postgres) ListMessages(ctx context.Context, channelID string) ([]models.Message, error) {
	if _, err := p.GetChannel(ctx, channelID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id, channel_id, member_id, body, created_at FROM messages WHERE channel_id=$1 ORDER BY created_at`, channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Message{}
	for rows.Next() {
		var msg models.Message
		if err := rows.Scan(&msg.ID, &msg.ChannelID, &msg.MemberID, &msg.Body, &msg.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

func (p *Postgres) PostMessage(ctx context.Context, channelID, memberID, body string) (*models.Message, error) {
	ch, err := p.GetChannel(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if _, err := p.GetMember(ctx, memberID); err != nil {
		return nil, err
	}
	msg := models.Message{ID: uuid.NewString(), ChannelID: channelID, MemberID: memberID, Body: body, CreatedAt: time.Now().UTC()}
	if _, err := p.pool.Exec(ctx, `INSERT INTO messages (id, channel_id, member_id, body, created_at) VALUES ($1,$2,$3,$4,$5)`,
		msg.ID, msg.ChannelID, msg.MemberID, msg.Body, msg.CreatedAt); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, ch.ProjectID, models.TypeMessage, map[string]any{"id": msg.ID, "channel_id": channelID})
	return &msg, nil
}

func (p *Postgres) ListTasks(ctx context.Context, projectID string) ([]models.Task, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, title, status, COALESCE(assignee_member_id::text, ''), created_at, updated_at, COALESCE(issue_number,0), COALESCE(issue_url,'')
		FROM tasks WHERE project_id=$1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Task{}
	for rows.Next() {
		var t models.Task
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.AssigneeMemberID, &t.CreatedAt, &t.UpdatedAt, &t.IssueNumber, &t.IssueURL); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateTask(ctx context.Context, projectID, title, assigneeMemberID string) (*models.Task, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	t := time.Now().UTC()
	task := models.Task{ID: uuid.NewString(), ProjectID: projectID, Title: title, Status: "open", AssigneeMemberID: assigneeMemberID, CreatedAt: t, UpdatedAt: t}
	var assignee any
	if assigneeMemberID != "" {
		assignee = assigneeMemberID
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO tasks (id, project_id, title, status, assignee_member_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, task.ID, task.ProjectID, task.Title, task.Status, assignee, task.CreatedAt, task.UpdatedAt); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, projectID, models.TypeTask, map[string]any{"id": task.ID, "title": title, "action": "create"})
	return &task, nil
}

func (p *Postgres) GetTask(ctx context.Context, id string) (*models.Task, error) {
	var t models.Task
	err := p.pool.QueryRow(ctx, `SELECT id, project_id, title, status, COALESCE(assignee_member_id::text, ''), created_at, updated_at, COALESCE(issue_number,0), COALESCE(issue_url,'') FROM tasks WHERE id=$1`, id).
		Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.AssigneeMemberID, &t.CreatedAt, &t.UpdatedAt, &t.IssueNumber, &t.IssueURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &t, err
}

func (p *Postgres) UpdateTaskStatus(ctx context.Context, id, status string) (*models.Task, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE tasks SET status=$2, updated_at=$3 WHERE id=$1`, id, status, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	t, err := p.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, t.ProjectID, models.TypeTask, map[string]any{"id": t.ID, "status": status, "action": "update"})
	return t, nil
}

func (p *Postgres) CreateHandoff(ctx context.Context, taskID, fromID, toID, note string) (*models.Handoff, error) {
	task, err := p.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if _, err := p.GetMember(ctx, fromID); err != nil {
		return nil, err
	}
	if _, err := p.GetMember(ctx, toID); err != nil {
		return nil, err
	}
	h := models.Handoff{
		ID: uuid.NewString(), TaskID: taskID, FromMemberID: fromID, ToMemberID: toID,
		Note: note, Status: "open", CreatedAt: time.Now().UTC(),
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO handoffs (id, task_id, from_member_id, to_member_id, note, status, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, h.ID, h.TaskID, h.FromMemberID, h.ToMemberID, h.Note, h.Status, h.CreatedAt); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE tasks SET assignee_member_id=$2, status='in_progress', updated_at=$3 WHERE id=$1`,
		taskID, toID, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := insertActivity(ctx, tx, task.ProjectID, models.TypeHandoff, map[string]any{"id": h.ID, "task_id": taskID, "action": "create"}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &h, nil
}

func (p *Postgres) GetHandoff(ctx context.Context, id string) (*models.Handoff, error) {
	var h models.Handoff
	var completed *time.Time
	err := p.pool.QueryRow(ctx, `SELECT id, task_id, from_member_id, to_member_id, note, status, created_at, completed_at FROM handoffs WHERE id=$1`, id).
		Scan(&h.ID, &h.TaskID, &h.FromMemberID, &h.ToMemberID, &h.Note, &h.Status, &h.CreatedAt, &completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	h.CompletedAt = completed
	return &h, nil
}

func (p *Postgres) CompleteHandoff(ctx context.Context, id string) (*models.Handoff, error) {
	h, err := p.GetHandoff(ctx, id)
	if err != nil {
		return nil, err
	}
	task, err := p.GetTask(ctx, h.TaskID)
	if err != nil {
		return nil, err
	}
	t := time.Now().UTC()
	if _, err := p.pool.Exec(ctx, `UPDATE handoffs SET status='complete', completed_at=$2 WHERE id=$1`, id, t); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, task.ProjectID, models.TypeHandoff, map[string]any{"id": h.ID, "task_id": h.TaskID, "action": "complete"})
	return p.GetHandoff(ctx, id)
}

func (p *Postgres) ListDecisions(ctx context.Context, projectID string) ([]models.Decision, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, prompt, options, recommendation, COALESCE(answer, ''), created_at, answered_at, COALESCE(assignee_member_id::text, '')
		FROM decisions WHERE project_id=$1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Decision{}
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateDecision(ctx context.Context, projectID, prompt, recommendation string, options []string, assigneeMemberID string) (*models.Decision, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	if options == nil {
		options = []string{}
	}
	raw, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	d := models.Decision{
		ID: uuid.NewString(), ProjectID: projectID, Prompt: prompt,
		Options: options, Recommendation: recommendation, AssigneeMemberID: assigneeMemberID, CreatedAt: time.Now().UTC(),
	}
	var assignee any
	if assigneeMemberID != "" {
		assignee = assigneeMemberID
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO decisions (id, project_id, prompt, options, recommendation, created_at, assignee_member_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, d.ID, d.ProjectID, d.Prompt, raw, d.Recommendation, d.CreatedAt, assignee); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, projectID, models.TypeDecision, map[string]any{"id": d.ID, "action": "create"})
	return &d, nil
}

func (p *Postgres) CreateReusedDecision(ctx context.Context, projectID, prompt, recommendation string, options []string, answer, assigneeMemberID string) (*models.Decision, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	if options == nil {
		options = []string{}
	}
	raw, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	t := time.Now().UTC()
	d := models.Decision{
		ID: uuid.NewString(), ProjectID: projectID, Prompt: prompt,
		Options: options, Recommendation: recommendation, Answer: answer,
		Reused: true, Fingerprint: models.DecisionFingerprint(prompt),
		AssigneeMemberID: assigneeMemberID, CreatedAt: t, AnsweredAt: &t,
	}
	var assignee any
	if assigneeMemberID != "" {
		assignee = assigneeMemberID
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO decisions (id, project_id, prompt, options, recommendation, answer, answered_at, created_at, assignee_member_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, d.ID, d.ProjectID, d.Prompt, raw, d.Recommendation, d.Answer, d.AnsweredAt, d.CreatedAt, assignee); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, projectID, models.TypeDecision, map[string]any{"id": d.ID, "action": "reuse", "answer": answer, "fingerprint": d.Fingerprint})
	_ = p.addActivity(ctx, projectID, models.TypeMemory, map[string]any{"fingerprint": d.Fingerprint, "action": "reuse", "answer": answer})
	return &d, nil
}

func (p *Postgres) AnswerDecision(ctx context.Context, id, answer string) (*models.Decision, error) {
	t := time.Now().UTC()
	tag, err := p.pool.Exec(ctx, `UPDATE decisions SET answer=$2, answered_at=$3 WHERE id=$1`, id, answer, t)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	d, err := p.getDecision(ctx, id)
	if err != nil {
		return nil, err
	}
	fp := models.DecisionFingerprint(d.Prompt)
	if err := p.upsertDecisionMemory(ctx, d.ProjectID, fp, d.Prompt, answer, d.ID); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, d.ProjectID, models.TypeDecision, map[string]any{"id": d.ID, "action": "answer", "answer": answer})
	return d, nil
}

func (p *Postgres) upsertDecisionMemory(ctx context.Context, projectID, fingerprint, prompt, answer, decisionID string) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO decision_memories (id, project_id, fingerprint, prompt, answer, decision_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,now(),now())
		ON CONFLICT (project_id, fingerprint) DO UPDATE SET
			prompt=EXCLUDED.prompt, answer=EXCLUDED.answer, decision_id=EXCLUDED.decision_id, updated_at=now()`,
		uuid.NewString(), projectID, fingerprint, prompt, answer, decisionID)
	return err
}

func (p *Postgres) GetDecisionMemory(ctx context.Context, projectID, fingerprint string) (*models.DecisionMemory, error) {
	var mem models.DecisionMemory
	err := p.pool.QueryRow(ctx, `SELECT id, project_id, fingerprint, prompt, answer, COALESCE(decision_id::text,''), created_at, updated_at
		FROM decision_memories WHERE project_id=$1 AND fingerprint=$2`, projectID, fingerprint).
		Scan(&mem.ID, &mem.ProjectID, &mem.Fingerprint, &mem.Prompt, &mem.Answer, &mem.DecisionID, &mem.CreatedAt, &mem.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &mem, nil
}

func (p *Postgres) ListDecisionMemories(ctx context.Context, projectID string) ([]models.DecisionMemory, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, fingerprint, prompt, answer, COALESCE(decision_id::text,''), created_at, updated_at
		FROM decision_memories WHERE project_id=$1 ORDER BY updated_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.DecisionMemory{}
	for rows.Next() {
		var mem models.DecisionMemory
		if err := rows.Scan(&mem.ID, &mem.ProjectID, &mem.Fingerprint, &mem.Prompt, &mem.Answer, &mem.DecisionID, &mem.CreatedAt, &mem.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, mem)
	}
	return out, rows.Err()
}

func (p *Postgres) getDecision(ctx context.Context, id string) (*models.Decision, error) {
	row := p.pool.QueryRow(ctx, `SELECT id, project_id, prompt, options, recommendation, COALESCE(answer, ''), created_at, answered_at, COALESCE(assignee_member_id::text, '') FROM decisions WHERE id=$1`, id)
	d, err := scanDecision(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanDecision(row scannable) (models.Decision, error) {
	var d models.Decision
	var raw []byte
	var answered *time.Time
	err := row.Scan(&d.ID, &d.ProjectID, &d.Prompt, &raw, &d.Recommendation, &d.Answer, &d.CreatedAt, &answered, &d.AssigneeMemberID)
	if err != nil {
		return d, err
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &d.Options)
	}
	if d.Options == nil {
		d.Options = []string{}
	}
	d.AnsweredAt = answered
	return d, nil
}

func (p *Postgres) ListActivity(ctx context.Context, projectID, typeFilter string) ([]models.Activity, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	q := `SELECT id, project_id, type, payload, created_at FROM activity WHERE project_id=$1`
	args := []any{projectID}
	if typeFilter != "" {
		q += ` AND type=$2`
		args = append(args, typeFilter)
	}
	q += ` ORDER BY created_at DESC LIMIT 200`
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Activity{}
	for rows.Next() {
		var a models.Activity
		var raw []byte
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.Type, &raw, &a.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &a.Payload)
		if a.Payload == nil {
			a.Payload = map[string]any{}
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateRun(ctx context.Context, taskID string) (*models.Run, error) {
	task, err := p.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	t := time.Now().UTC()
	run := models.Run{ID: uuid.NewString(), TaskID: taskID, ProjectID: task.ProjectID, Status: "pending", CreatedAt: t, UpdatedAt: t}
	if _, err := p.pool.Exec(ctx, `INSERT INTO runs (id, task_id, project_id, status, detail, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, run.ID, run.TaskID, run.ProjectID, run.Status, run.Detail, run.CreatedAt, run.UpdatedAt); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, task.ProjectID, models.TypeRun, map[string]any{"id": run.ID, "task_id": taskID, "status": run.Status})
	return &run, nil
}

func (p *Postgres) GetRun(ctx context.Context, id string) (*models.Run, error) {
	var r models.Run
	err := p.pool.QueryRow(ctx, `SELECT id, task_id, project_id, status, detail, created_at, updated_at FROM runs WHERE id=$1`, id).
		Scan(&r.ID, &r.TaskID, &r.ProjectID, &r.Status, &r.Detail, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

func (p *Postgres) UpdateRun(ctx context.Context, id, status, detail string) (*models.Run, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE runs SET status=$2, detail=$3, updated_at=$4 WHERE id=$1`, id, status, detail, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	r, err := p.GetRun(ctx, id)
	if err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, r.ProjectID, models.TypeRun, map[string]any{"id": r.ID, "status": status})
	return r, nil
}

func (p *Postgres) mustProject(ctx context.Context, id string) error {
	var n int
	err := p.pool.QueryRow(ctx, `SELECT 1 FROM projects WHERE id=$1`, id).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (p *Postgres) ListRuns(ctx context.Context, taskID string) ([]models.Run, error) {
	if _, err := p.GetTask(ctx, taskID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id, task_id, project_id, status, detail, created_at, updated_at FROM runs WHERE task_id=$1 ORDER BY created_at DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Run{}
	for rows.Next() {
		var r models.Run
		if err := rows.Scan(&r.ID, &r.TaskID, &r.ProjectID, &r.Status, &r.Detail, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) AppendRunEvent(ctx context.Context, runID, kind string, payload map[string]any) (*models.RunEvent, error) {
	k := models.NormalizeRunEventKind(kind)
	if k == "" {
		return nil, fmt.Errorf("invalid run event kind")
	}
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists string
	if err := tx.QueryRow(ctx, `SELECT id FROM runs WHERE id=$1 FOR UPDATE`, runID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var seq int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(seq), 0) FROM run_events WHERE run_id=$1`, runID).Scan(&seq); err != nil {
		return nil, err
	}
	ev := models.RunEvent{
		ID: uuid.NewString(), RunID: runID, Seq: seq + 1, Kind: k,
		Payload: payload, CreatedAt: time.Now().UTC(),
	}
	if _, err := tx.Exec(ctx, `INSERT INTO run_events (id, run_id, seq, kind, payload, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`, ev.ID, ev.RunID, ev.Seq, ev.Kind, raw, ev.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &ev, nil
}

func (p *Postgres) ListRunEvents(ctx context.Context, runID string, afterSeq int) ([]models.RunEvent, error) {
	if _, err := p.GetRun(ctx, runID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id, run_id, seq, kind, payload, created_at FROM run_events
		WHERE run_id=$1 AND seq>$2 ORDER BY seq ASC`, runID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.RunEvent{}
	for rows.Next() {
		var ev models.RunEvent
		var raw []byte
		if err := rows.Scan(&ev.ID, &ev.RunID, &ev.Seq, &ev.Kind, &raw, &ev.CreatedAt); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &ev.Payload)
		}
		if ev.Payload == nil {
			ev.Payload = map[string]any{}
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateArtifact(ctx context.Context, in models.Artifact) (*models.Artifact, error) {
	task, err := p.GetTask(ctx, in.TaskID)
	if err != nil {
		return nil, err
	}
	in.ID = uuid.NewString()
	in.ProjectID = task.ProjectID
	in.CreatedAt = time.Now().UTC()
	if in.Kind == "" {
		in.Kind = "file"
	}
	var runID any
	if in.RunID != "" {
		runID = in.RunID
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO artifacts (id, project_id, task_id, run_id, kind, name, body, url, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, in.ID, in.ProjectID, in.TaskID, runID, in.Kind, in.Name, in.Body, in.URL, in.CreatedAt); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, in.ProjectID, models.TypeArtifact, map[string]any{"id": in.ID, "kind": in.Kind, "name": in.Name})
	return &in, nil
}

func (p *Postgres) GetArtifact(ctx context.Context, id string) (*models.Artifact, error) {
	var a models.Artifact
	err := p.pool.QueryRow(ctx, `SELECT id, project_id, task_id, COALESCE(run_id::text, ''), kind, name, body, url, created_at FROM artifacts WHERE id=$1`, id).
		Scan(&a.ID, &a.ProjectID, &a.TaskID, &a.RunID, &a.Kind, &a.Name, &a.Body, &a.URL, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &a, err
}

func (p *Postgres) ListArtifacts(ctx context.Context, taskID string) ([]models.Artifact, error) {
	if _, err := p.GetTask(ctx, taskID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, task_id, COALESCE(run_id::text, ''), kind, name, body, url, created_at FROM artifacts WHERE task_id=$1 ORDER BY created_at DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Artifact{}
	for rows.Next() {
		var a models.Artifact
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.TaskID, &a.RunID, &a.Kind, &a.Name, &a.Body, &a.URL, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *Postgres) CreatePipeline(ctx context.Context, in models.Pipeline) (*models.Pipeline, error) {
	task, err := p.GetTask(ctx, in.TaskID)
	if err != nil {
		return nil, err
	}
	t := time.Now().UTC()
	in.ID = uuid.NewString()
	in.ProjectID = task.ProjectID
	if in.Status == "" {
		in.Status = "pending"
	}
	in.CreatedAt = t
	in.UpdatedAt = t
	var art any
	if in.ArtifactID != "" {
		art = in.ArtifactID
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO pipelines (id, project_id, task_id, artifact_id, name, status, external_url, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, in.ID, in.ProjectID, in.TaskID, art, in.Name, in.Status, in.ExternalURL, in.CreatedAt, in.UpdatedAt); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, in.ProjectID, models.TypePipeline, map[string]any{"id": in.ID, "name": in.Name, "status": in.Status})
	return &in, nil
}

func (p *Postgres) ListPipelines(ctx context.Context, taskID string) ([]models.Pipeline, error) {
	if _, err := p.GetTask(ctx, taskID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, task_id, COALESCE(artifact_id::text, ''), name, status, external_url, created_at, updated_at FROM pipelines WHERE task_id=$1 ORDER BY created_at DESC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Pipeline{}
	for rows.Next() {
		var pl models.Pipeline
		if err := rows.Scan(&pl.ID, &pl.ProjectID, &pl.TaskID, &pl.ArtifactID, &pl.Name, &pl.Status, &pl.ExternalURL, &pl.CreatedAt, &pl.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, pl)
	}
	return out, rows.Err()
}

func (p *Postgres) GetPipeline(ctx context.Context, id string) (*models.Pipeline, error) {
	var pl models.Pipeline
	err := p.pool.QueryRow(ctx, `SELECT id, project_id, task_id, COALESCE(artifact_id::text, ''), name, status, external_url, created_at, updated_at FROM pipelines WHERE id=$1`, id).
		Scan(&pl.ID, &pl.ProjectID, &pl.TaskID, &pl.ArtifactID, &pl.Name, &pl.Status, &pl.ExternalURL, &pl.CreatedAt, &pl.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &pl, err
}

func (p *Postgres) UpdatePipeline(ctx context.Context, id, status, externalURL string) (*models.Pipeline, error) {
	pl, err := p.GetPipeline(ctx, id)
	if err != nil {
		return nil, err
	}
	if externalURL == "" {
		externalURL = pl.ExternalURL
	}
	if _, err := p.pool.Exec(ctx, `UPDATE pipelines SET status=$2, external_url=$3, updated_at=$4 WHERE id=$1`, id, status, externalURL, time.Now().UTC()); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, pl.ProjectID, models.TypePipeline, map[string]any{"id": pl.ID, "status": status})
	return p.GetPipeline(ctx, id)
}

func (p *Postgres) UpsertIssueTask(ctx context.Context, projectID string, number int, title, issueURL string) (*models.Task, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	var id string
	err := p.pool.QueryRow(ctx, `SELECT id FROM tasks WHERE project_id=$1 AND issue_number=$2`, projectID, number).Scan(&id)
	if err == nil {
		if _, err := p.pool.Exec(ctx, `UPDATE tasks SET title=$2, issue_url=$3, updated_at=$4 WHERE id=$1`, id, title, issueURL, time.Now().UTC()); err != nil {
			return nil, err
		}
		_ = p.addActivity(ctx, projectID, models.TypeIssue, map[string]any{"task_id": id, "issue_number": number, "action": "update"})
		return p.GetTask(ctx, id)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	t := time.Now().UTC()
	task := models.Task{
		ID: uuid.NewString(), ProjectID: projectID, Title: title, Status: "open",
		IssueNumber: number, IssueURL: issueURL, CreatedAt: t, UpdatedAt: t,
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO tasks (id, project_id, title, status, issue_number, issue_url, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, task.ID, task.ProjectID, task.Title, task.Status, number, issueURL, t, t); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, projectID, models.TypeIssue, map[string]any{"task_id": task.ID, "issue_number": number, "action": "create"})
	return &task, nil
}

func (p *Postgres) AppendActivity(ctx context.Context, projectID, typ string, payload map[string]any) error {
	if err := p.mustProject(ctx, projectID); err != nil {
		return err
	}
	return p.addActivity(ctx, projectID, typ, payload)
}

func (p *Postgres) ListRoutines(ctx context.Context, projectID string) ([]models.Routine, error) {
	if err := p.mustProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, COALESCE(bot_member_id::text,''), name, schedule, enabled, last_run_at, created_at FROM routines WHERE project_id=$1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Routine{}
	for rows.Next() {
		r, err := scanRoutine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) ListEnabledRoutines(ctx context.Context) ([]models.Routine, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, COALESCE(bot_member_id::text,''), name, schedule, enabled, last_run_at, created_at FROM routines WHERE enabled=true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Routine{}
	for rows.Next() {
		r, err := scanRoutine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateRoutine(ctx context.Context, in models.Routine) (*models.Routine, error) {
	if err := p.mustProject(ctx, in.ProjectID); err != nil {
		return nil, err
	}
	in.ID = uuid.NewString()
	in.CreatedAt = time.Now().UTC()
	if in.Schedule == "" {
		in.Schedule = "24h"
	}
	var bot any
	if in.BotMemberID != "" {
		bot = in.BotMemberID
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO routines (id, project_id, bot_member_id, name, schedule, enabled, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, in.ID, in.ProjectID, bot, in.Name, in.Schedule, in.Enabled, in.CreatedAt); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, in.ProjectID, models.TypeRoutine, map[string]any{"id": in.ID, "name": in.Name, "action": "create"})
	return &in, nil
}

func (p *Postgres) GetRoutine(ctx context.Context, id string) (*models.Routine, error) {
	row := p.pool.QueryRow(ctx, `SELECT id, project_id, COALESCE(bot_member_id::text,''), name, schedule, enabled, last_run_at, created_at FROM routines WHERE id=$1`, id)
	r, err := scanRoutine(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (p *Postgres) UpdateRoutine(ctx context.Context, id string, enabled *bool, lastRun *time.Time) (*models.Routine, error) {
	r, err := p.GetRoutine(ctx, id)
	if err != nil {
		return nil, err
	}
	if enabled != nil {
		r.Enabled = *enabled
	}
	if lastRun != nil {
		r.LastRunAt = lastRun
	}
	if _, err := p.pool.Exec(ctx, `UPDATE routines SET enabled=$2, last_run_at=$3 WHERE id=$1`, r.ID, r.Enabled, r.LastRunAt); err != nil {
		return nil, err
	}
	return r, nil
}

func scanRoutine(row scannable) (models.Routine, error) {
	var r models.Routine
	var last *time.Time
	err := row.Scan(&r.ID, &r.ProjectID, &r.BotMemberID, &r.Name, &r.Schedule, &r.Enabled, &last, &r.CreatedAt)
	r.LastRunAt = last
	return r, err
}

func (p *Postgres) CreateNotification(ctx context.Context, in models.Notification) (*models.Notification, error) {
	if err := p.mustProject(ctx, in.ProjectID); err != nil {
		return nil, err
	}
	if _, err := p.GetMember(ctx, in.MemberID); err != nil {
		return nil, err
	}
	in.ID = uuid.NewString()
	in.CreatedAt = time.Now().UTC()
	if in.Kind == "" {
		in.Kind = "notice"
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO notifications (id, project_id, member_id, kind, title, body, href, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, in.ID, in.ProjectID, in.MemberID, in.Kind, in.Title, in.Body, in.Href, in.CreatedAt); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, in.ProjectID, models.TypeNotification, map[string]any{"id": in.ID, "kind": in.Kind, "member_id": in.MemberID})
	return &in, nil
}

func (p *Postgres) ListNotifications(ctx context.Context, memberID string, unreadOnly bool) ([]models.Notification, error) {
	q := `SELECT id, project_id, member_id, kind, title, body, href, read_at, created_at FROM notifications WHERE 1=1`
	args := []any{}
	if memberID != "" {
		args = append(args, memberID)
		q += fmt.Sprintf(` AND member_id=$%d`, len(args))
	}
	if unreadOnly {
		q += ` AND read_at IS NULL`
	}
	q += ` ORDER BY created_at DESC LIMIT 100`
	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Notification{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (p *Postgres) GetNotification(ctx context.Context, id string) (*models.Notification, error) {
	row := p.pool.QueryRow(ctx, `SELECT id, project_id, member_id, kind, title, body, href, read_at, created_at FROM notifications WHERE id=$1`, id)
	n, err := scanNotification(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (p *Postgres) MarkNotificationRead(ctx context.Context, id string) (*models.Notification, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE notifications SET read_at=$2 WHERE id=$1`, id, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return p.GetNotification(ctx, id)
}

func (p *Postgres) MarkAllNotificationsRead(ctx context.Context, memberID string) (int, error) {
	tag, err := p.pool.Exec(ctx, `UPDATE notifications SET read_at=$2 WHERE member_id=$1 AND read_at IS NULL`, memberID, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func scanNotification(row scannable) (models.Notification, error) {
	var n models.Notification
	var read *time.Time
	err := row.Scan(&n.ID, &n.ProjectID, &n.MemberID, &n.Kind, &n.Title, &n.Body, &n.Href, &read, &n.CreatedAt)
	n.ReadAt = read
	return n, err
}

func (p *Postgres) GetPreferences(ctx context.Context, memberID string) (*models.Preferences, error) {
	var pref models.Preferences
	err := p.pool.QueryRow(ctx, `SELECT member_id, mute_mentions, mute_routines, updated_at FROM member_preferences WHERE member_id=$1`, memberID).
		Scan(&pref.MemberID, &pref.MuteMentions, &pref.MuteRoutines, &pref.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return &models.Preferences{MemberID: memberID}, nil
	}
	if err != nil {
		return nil, err
	}
	return &pref, nil
}

func (p *Postgres) SetPreferences(ctx context.Context, in models.Preferences) (*models.Preferences, error) {
	in.UpdatedAt = time.Now().UTC()
	_, err := p.pool.Exec(ctx, `INSERT INTO member_preferences (member_id, mute_mentions, mute_routines, updated_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (member_id) DO UPDATE SET mute_mentions=$2, mute_routines=$3, updated_at=$4`,
		in.MemberID, in.MuteMentions, in.MuteRoutines, in.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &in, nil
}
