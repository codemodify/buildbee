package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/codemodify/buildbee/server/internal/migrate"
	"github.com/codemodify/buildbee/server/internal/models"
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

// Migrate applies SQL migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return migrate.Up(ctx, pool)
}

func (p *Postgres) CreateProject(ctx context.Context, name string) (*models.ProjectBundle, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	t := time.Now().UTC()
	proj := models.Project{ID: uuid.NewString(), Name: name, CreatedAt: t}
	human := models.Member{
		ID: uuid.NewString(), ProjectID: proj.ID, Kind: "human",
		DisplayName: "You", Role: "owner", Identity: "human:stub", CreatedAt: t,
	}
	bot := models.Member{
		ID: uuid.NewString(), ProjectID: proj.ID, Kind: "bot",
		DisplayName: "BuildBee Bot", Role: "bot", Identity: "bot:" + uuid.NewString(), CreatedAt: t,
	}
	ch := models.Channel{ID: uuid.NewString(), ProjectID: proj.ID, Name: "general", CreatedAt: t}

	if _, err := tx.Exec(ctx, `INSERT INTO projects (id, name, created_at) VALUES ($1,$2,$3)`, proj.ID, proj.Name, proj.CreatedAt); err != nil {
		return nil, err
	}
	if err := insertMember(ctx, tx, human); err != nil {
		return nil, err
	}
	if err := insertMember(ctx, tx, bot); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO channels (id, project_id, name, created_at) VALUES ($1,$2,$3,$4)`, ch.ID, ch.ProjectID, ch.Name, ch.CreatedAt); err != nil {
		return nil, err
	}
	if err := insertActivity(ctx, tx, proj.ID, models.TypeProject, map[string]any{"name": name, "id": proj.ID}); err != nil {
		return nil, err
	}
	if err := insertActivity(ctx, tx, proj.ID, models.TypeMember, map[string]any{"id": human.ID, "kind": "human"}); err != nil {
		return nil, err
	}
	if err := insertActivity(ctx, tx, proj.ID, models.TypeMember, map[string]any{"id": bot.ID, "kind": "bot"}); err != nil {
		return nil, err
	}
	if err := insertActivity(ctx, tx, proj.ID, models.TypeChannel, map[string]any{"id": ch.ID, "name": ch.Name}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &models.ProjectBundle{Project: proj, Members: []models.Member{human, bot}, Channels: []models.Channel{ch}}, nil
}

func insertMember(ctx context.Context, tx pgx.Tx, m models.Member) error {
	_, err := tx.Exec(ctx, `INSERT INTO members (id, project_id, kind, display_name, role, identity, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, m.ID, m.ProjectID, m.Kind, m.DisplayName, m.Role, m.Identity, m.CreatedAt)
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
	err := p.pool.QueryRow(ctx, `SELECT id, name, created_at FROM projects WHERE id=$1`, id).
		Scan(&proj.ID, &proj.Name, &proj.CreatedAt)
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

func (p *Postgres) ListProjects(ctx context.Context) ([]models.Project, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, name, created_at FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Project{}
	for rows.Next() {
		var proj models.Project
		if err := rows.Scan(&proj.ID, &proj.Name, &proj.CreatedAt); err != nil {
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
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, kind, display_name, role, identity, created_at
		FROM members WHERE project_id=$1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Member{}
	for rows.Next() {
		var m models.Member
		if err := rows.Scan(&m.ID, &m.ProjectID, &m.Kind, &m.DisplayName, &m.Role, &m.Identity, &m.CreatedAt); err != nil {
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
	if in.Identity == "" {
		if in.Kind == "bot" {
			in.Identity = "bot:" + uuid.NewString()
		} else {
			in.Identity = "human:stub"
		}
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO members (id, project_id, kind, display_name, role, identity, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, in.ID, in.ProjectID, in.Kind, in.DisplayName, in.Role, in.Identity, in.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, in.ProjectID, models.TypeMember, map[string]any{"id": in.ID, "kind": in.Kind})
	return &in, nil
}

func (p *Postgres) GetMember(ctx context.Context, id string) (*models.Member, error) {
	var m models.Member
	err := p.pool.QueryRow(ctx, `SELECT id, project_id, kind, display_name, role, identity, created_at FROM members WHERE id=$1`, id).
		Scan(&m.ID, &m.ProjectID, &m.Kind, &m.DisplayName, &m.Role, &m.Identity, &m.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &m, err
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
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, title, status, COALESCE(assignee_member_id::text, ''), created_at, updated_at
		FROM tasks WHERE project_id=$1 ORDER BY created_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Task{}
	for rows.Next() {
		var t models.Task
		if err := rows.Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.AssigneeMemberID, &t.CreatedAt, &t.UpdatedAt); err != nil {
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
	err := p.pool.QueryRow(ctx, `SELECT id, project_id, title, status, COALESCE(assignee_member_id::text, ''), created_at, updated_at FROM tasks WHERE id=$1`, id).
		Scan(&t.ID, &t.ProjectID, &t.Title, &t.Status, &t.AssigneeMemberID, &t.CreatedAt, &t.UpdatedAt)
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
	rows, err := p.pool.Query(ctx, `SELECT id, project_id, prompt, options, recommendation, COALESCE(answer, ''), created_at, answered_at
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

func (p *Postgres) CreateDecision(ctx context.Context, projectID, prompt, recommendation string, options []string) (*models.Decision, error) {
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
		Options: options, Recommendation: recommendation, CreatedAt: time.Now().UTC(),
	}
	if _, err := p.pool.Exec(ctx, `INSERT INTO decisions (id, project_id, prompt, options, recommendation, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`, d.ID, d.ProjectID, d.Prompt, raw, d.Recommendation, d.CreatedAt); err != nil {
		return nil, err
	}
	_ = p.addActivity(ctx, projectID, models.TypeDecision, map[string]any{"id": d.ID, "action": "create"})
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
	_ = p.addActivity(ctx, d.ProjectID, models.TypeDecision, map[string]any{"id": d.ID, "action": "answer", "answer": answer})
	return d, nil
}

func (p *Postgres) getDecision(ctx context.Context, id string) (*models.Decision, error) {
	row := p.pool.QueryRow(ctx, `SELECT id, project_id, prompt, options, recommendation, COALESCE(answer, ''), created_at, answered_at FROM decisions WHERE id=$1`, id)
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
	err := row.Scan(&d.ID, &d.ProjectID, &d.Prompt, &raw, &d.Recommendation, &d.Answer, &d.CreatedAt, &answered)
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
