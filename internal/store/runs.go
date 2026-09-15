package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const runCols = `id, task_id, project_id, COALESCE(bot_member_id::text, ''), status, detail, agent, prompt, worker,
	lease_until, attempts, created_at, updated_at, started_at, finished_at`

func scanRun(row interface{ Scan(...any) error }, r *models.Run) error {
	return row.Scan(&r.ID, &r.TaskID, &r.ProjectID, &r.BotMemberID, &r.Status, &r.Detail, &r.Agent, &r.Prompt, &r.Worker,
		&r.LeaseUntil, &r.Attempts, &r.CreatedAt, &r.UpdatedAt, &r.StartedAt, &r.FinishedAt)
}

func (s *Store) InsertRun(ctx context.Context, r models.Run) error {
	_, err := s.q.Exec(ctx, `INSERT INTO runs (id, task_id, project_id, bot_member_id, status, detail, agent, prompt, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)`,
		r.ID, r.TaskID, r.ProjectID, nullID(r.BotMemberID), r.Status, r.Detail, r.Agent, r.Prompt, r.CreatedAt)
	return mapErr(err)
}

// ClaimRun hands the oldest queued Run a worker can execute to that worker
// and leases it until `until`. Concurrent workers never get the same Run.
// A Run for any agent (agent ”) needs a worker offering a real one: the
// fake agent only takes Runs that ask for it. ErrNotFound means the queue
// has nothing for this worker.
func (s *Store) ClaimRun(ctx context.Context, worker string, agents []string, now, until time.Time) (*models.Run, error) {
	var r models.Run
	err := scanRun(s.q.QueryRow(ctx, `UPDATE runs SET status='running', worker=$1, lease_until=$4, attempts=attempts+1,
			started_at=COALESCE(started_at, $3), updated_at=$3
		WHERE id = (
			SELECT r.id FROM runs r JOIN projects p ON p.id = r.project_id
			WHERE r.status = 'pending' AND p.archived_at IS NULL AND (r.agent = ANY($2) OR (r.agent = '' AND EXISTS (SELECT 1 FROM unnest($2::text[]) a WHERE a <> 'fake')))
			ORDER BY r.created_at, r.id
			FOR UPDATE OF r SKIP LOCKED
			LIMIT 1)
		RETURNING `+runCols, worker, agents, now, until), &r)
	return &r, mapErr(err)
}

// ExtendLease renews the claim of the worker running a Run. It returns the
// Run as it stands; ErrConflict when another worker owns it.
func (s *Store) ExtendLease(ctx context.Context, id, worker string, until time.Time) (*models.Run, error) {
	r, err := s.GetRun(ctx, id, true)
	if err != nil {
		return nil, err
	}
	if r.Worker != worker {
		return nil, ErrConflict
	}
	if r.Status == models.RunRunning {
		if _, err := s.q.Exec(ctx, `UPDATE runs SET lease_until=$2 WHERE id=$1`, id, until); err != nil {
			return nil, mapErr(err)
		}
		r.LeaseUntil = &until
	}
	return r, nil
}

// ExpiredRuns locks running Runs whose lease ran out before now.
func (s *Store) ExpiredRuns(ctx context.Context, now time.Time, limit int) ([]models.Run, error) {
	rows, err := s.q.Query(ctx, `SELECT `+runCols+` FROM runs
		WHERE status = 'running' AND lease_until IS NOT NULL AND lease_until < $1
		ORDER BY lease_until LIMIT $2 FOR UPDATE SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []models.Run{}
	for rows.Next() {
		var r models.Run
		if err := scanRun(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRun returns one Run. forUpdate locks the row until the transaction ends.
func (s *Store) GetRun(ctx context.Context, id string, forUpdate bool) (*models.Run, error) {
	sql := `SELECT ` + runCols + ` FROM runs WHERE id=$1`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	var r models.Run
	err := scanRun(s.q.QueryRow(ctx, sql, id), &r)
	return &r, mapErr(err)
}

// ListRuns returns a Task's Runs, newest first.
func (s *Store) ListRuns(ctx context.Context, taskID string) ([]models.Run, error) {
	rows, err := s.q.Query(ctx, `SELECT `+runCols+` FROM runs WHERE task_id=$1 ORDER BY created_at DESC, id`, taskID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []models.Run{}
	for rows.Next() {
		var r models.Run
		if err := scanRun(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateRun writes status, detail, lease and the lifecycle timestamps.
func (s *Store) UpdateRun(ctx context.Context, r models.Run) error {
	return one(s.q.Exec(ctx, `UPDATE runs SET status=$2, detail=$3, updated_at=$4, started_at=$5, finished_at=$6, lease_until=$7 WHERE id=$1`,
		r.ID, r.Status, r.Detail, r.UpdatedAt, r.StartedAt, r.FinishedAt, r.LeaseUntil))
}

// AppendRunEvent stores the next event of a Run. The per-Run counter makes
// seq allocation O(1) and serializes appenders on the Run row.
func (s *Store) AppendRunEvent(ctx context.Context, runID, kind string, payload map[string]any, at time.Time) (*models.RunEvent, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ev := models.RunEvent{ID: uuid.NewString(), RunID: runID, Kind: kind, Payload: payload, CreatedAt: at}
	if err := s.q.QueryRow(ctx, `UPDATE runs SET next_seq = next_seq + 1 WHERE id=$1 RETURNING next_seq`, runID).Scan(&ev.Seq); err != nil {
		return nil, mapErr(err)
	}
	_, err = s.q.Exec(ctx, `INSERT INTO run_events (id, run_id, seq, kind, payload, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		ev.ID, ev.RunID, ev.Seq, ev.Kind, raw, ev.CreatedAt)
	return &ev, mapErr(err)
}

// ListRunEvents returns events after seq `after`, in order.
func (s *Store) ListRunEvents(ctx context.Context, runID string, after, limit int) ([]models.RunEvent, bool, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	rows, err := s.q.Query(ctx, `SELECT id, run_id, seq, kind, payload, created_at FROM run_events
		WHERE run_id=$1 AND seq > $2 ORDER BY seq LIMIT $3`, runID, after, limit+1)
	if err != nil {
		return nil, false, mapErr(err)
	}
	defer rows.Close()
	out := []models.RunEvent{}
	for rows.Next() {
		var ev models.RunEvent
		var raw []byte
		if err := rows.Scan(&ev.ID, &ev.RunID, &ev.Seq, &ev.Kind, &raw, &ev.CreatedAt); err != nil {
			return nil, false, err
		}
		_ = json.Unmarshal(raw, &ev.Payload)
		if ev.Payload == nil {
			ev.Payload = map[string]any{}
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}

// --- artifacts ---

func (s *Store) InsertArtifact(ctx context.Context, a models.Artifact) error {
	_, err := s.q.Exec(ctx, `INSERT INTO artifacts (id, project_id, task_id, run_id, kind, name, body, url, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		a.ID, a.ProjectID, a.TaskID, nullID(a.RunID), a.Kind, a.Name, a.Body, a.URL, a.CreatedAt)
	return mapErr(err)
}

// GetArtifact returns one Artifact including its Body.
func (s *Store) GetArtifact(ctx context.Context, id string) (*models.Artifact, error) {
	var a models.Artifact
	err := s.q.QueryRow(ctx, `SELECT id, project_id, task_id, COALESCE(run_id::text, ''), kind, name, body, octet_length(body), url, created_at
		FROM artifacts WHERE id=$1`, id).
		Scan(&a.ID, &a.ProjectID, &a.TaskID, &a.RunID, &a.Kind, &a.Name, &a.Body, &a.Size, &a.URL, &a.CreatedAt)
	return &a, mapErr(err)
}

// ListArtifacts returns a Task's Artifacts without their bodies, newest first.
func (s *Store) ListArtifacts(ctx context.Context, taskID string) ([]models.Artifact, error) {
	rows, err := s.q.Query(ctx, `SELECT id, project_id, task_id, COALESCE(run_id::text, ''), kind, name, octet_length(body), url, created_at
		FROM artifacts WHERE task_id=$1 ORDER BY created_at DESC, id`, taskID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (models.Artifact, error) {
		var a models.Artifact
		err := r.Scan(&a.ID, &a.ProjectID, &a.TaskID, &a.RunID, &a.Kind, &a.Name, &a.Size, &a.URL, &a.CreatedAt)
		return a, err
	})
}

// --- pipelines ---

const pipelineCols = `id, project_id, task_id, COALESCE(artifact_id::text, ''), name, status, external_url, created_at, updated_at`

func scanPipeline(row interface{ Scan(...any) error }, p *models.Pipeline) error {
	return row.Scan(&p.ID, &p.ProjectID, &p.TaskID, &p.ArtifactID, &p.Name, &p.Status, &p.ExternalURL, &p.CreatedAt, &p.UpdatedAt)
}

func (s *Store) InsertPipeline(ctx context.Context, p models.Pipeline) error {
	_, err := s.q.Exec(ctx, `INSERT INTO pipelines (id, project_id, task_id, artifact_id, name, status, external_url, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
		p.ID, p.ProjectID, p.TaskID, nullID(p.ArtifactID), p.Name, p.Status, p.ExternalURL, p.CreatedAt)
	return mapErr(err)
}

func (s *Store) GetPipeline(ctx context.Context, id string) (*models.Pipeline, error) {
	var p models.Pipeline
	err := scanPipeline(s.q.QueryRow(ctx, `SELECT `+pipelineCols+` FROM pipelines WHERE id=$1`, id), &p)
	return &p, mapErr(err)
}

// UpdatePipeline writes status, external_url and updated_at.
func (s *Store) UpdatePipeline(ctx context.Context, p models.Pipeline) error {
	return one(s.q.Exec(ctx, `UPDATE pipelines SET status=$2, external_url=$3, updated_at=$4 WHERE id=$1`,
		p.ID, p.Status, p.ExternalURL, p.UpdatedAt))
}

// ListPipelines returns a Task's Pipelines, newest first.
func (s *Store) ListPipelines(ctx context.Context, taskID string) ([]models.Pipeline, error) {
	rows, err := s.q.Query(ctx, `SELECT `+pipelineCols+` FROM pipelines WHERE task_id=$1 ORDER BY created_at DESC, id`, taskID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []models.Pipeline{}
	for rows.Next() {
		var p models.Pipeline
		if err := scanPipeline(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// --- routines ---

const routineCols = `id, project_id, COALESCE(bot_member_id::text, ''), name, schedule, enabled, last_run_at, created_at`

func scanRoutine(row interface{ Scan(...any) error }, r *models.Routine) error {
	return row.Scan(&r.ID, &r.ProjectID, &r.BotMemberID, &r.Name, &r.Schedule, &r.Enabled, &r.LastRunAt, &r.CreatedAt)
}

func (s *Store) InsertRoutine(ctx context.Context, r models.Routine) error {
	_, err := s.q.Exec(ctx, `INSERT INTO routines (id, project_id, bot_member_id, name, schedule, enabled, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		r.ID, r.ProjectID, nullID(r.BotMemberID), r.Name, r.Schedule, r.Enabled, r.CreatedAt)
	return mapErr(err)
}

func (s *Store) GetRoutine(ctx context.Context, id string) (*models.Routine, error) {
	var r models.Routine
	err := scanRoutine(s.q.QueryRow(ctx, `SELECT `+routineCols+` FROM routines WHERE id=$1`, id), &r)
	return &r, mapErr(err)
}

// ListRoutines returns a Project's Routines, oldest first.
func (s *Store) ListRoutines(ctx context.Context, projectID string) ([]models.Routine, error) {
	return s.routines(ctx, `SELECT `+routineCols+` FROM routines WHERE project_id=$1 ORDER BY created_at, id`, projectID)
}

// ListEnabledRoutines returns enabled Routines of Projects that are not archived.
func (s *Store) ListEnabledRoutines(ctx context.Context) ([]models.Routine, error) {
	return s.routines(ctx, `SELECT r.id, r.project_id, COALESCE(r.bot_member_id::text, ''), r.name, r.schedule, r.enabled, r.last_run_at, r.created_at
		FROM routines r JOIN projects p ON p.id = r.project_id
		WHERE r.enabled AND p.archived_at IS NULL ORDER BY r.created_at, r.id`)
}

func (s *Store) routines(ctx context.Context, sql string, args ...any) ([]models.Routine, error) {
	rows, err := s.q.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []models.Routine{}
	for rows.Next() {
		var r models.Routine
		if err := scanRoutine(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpdateRoutine writes name, schedule, enabled and bot.
func (s *Store) UpdateRoutine(ctx context.Context, r models.Routine) error {
	return one(s.q.Exec(ctx, `UPDATE routines SET name=$2, schedule=$3, enabled=$4, bot_member_id=$5 WHERE id=$1`,
		r.ID, r.Name, r.Schedule, r.Enabled, nullID(r.BotMemberID)))
}

// ClaimRoutine moves last_run_at from prev to at if nobody else has fired
// the Routine since prev. Only the caller that gets true may fire it.
func (s *Store) ClaimRoutine(ctx context.Context, id string, prev *time.Time, at time.Time) (bool, error) {
	tag, err := s.q.Exec(ctx, `UPDATE routines SET last_run_at=$3 WHERE id=$1 AND last_run_at IS NOT DISTINCT FROM $2`, id, prev, at)
	if err != nil {
		return false, mapErr(err)
	}
	return tag.RowsAffected() == 1, nil
}
