package store

import (
	"context"

	"github.com/codemodify/buildbee/internal/models"
)

const projectCols = `id, name, auto_run, archived_at, created_at`

func scanProject(row interface{ Scan(...any) error }, p *models.Project) error {
	return row.Scan(&p.ID, &p.Name, &p.AutoRun, &p.ArchivedAt, &p.CreatedAt)
}

func (s *Store) InsertProject(ctx context.Context, p models.Project) error {
	_, err := s.q.Exec(ctx, `INSERT INTO projects (id, name, auto_run, created_at) VALUES ($1, $2, $3, $4)`,
		p.ID, p.Name, p.AutoRun, p.CreatedAt)
	return mapErr(err)
}

func (s *Store) GetProject(ctx context.Context, id string) (*models.Project, error) {
	var p models.Project
	err := scanProject(s.q.QueryRow(ctx, `SELECT `+projectCols+` FROM projects WHERE id=$1`, id), &p)
	return &p, mapErr(err)
}

// ListProjects returns Projects oldest first; archived ones only on request.
func (s *Store) ListProjects(ctx context.Context, includeArchived bool) ([]models.Project, error) {
	rows, err := s.q.Query(ctx, `SELECT `+projectCols+` FROM projects
		WHERE $1 OR archived_at IS NULL ORDER BY created_at, id`, includeArchived)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []models.Project{}
	for rows.Next() {
		var p models.Project
		if err := scanProject(rows, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProject writes name, auto_run and archived_at.
func (s *Store) UpdateProject(ctx context.Context, p models.Project) error {
	return one(s.q.Exec(ctx, `UPDATE projects SET name=$2, auto_run=$3, archived_at=$4 WHERE id=$1`,
		p.ID, p.Name, p.AutoRun, p.ArchivedAt))
}

// --- members ---

const memberCols = `id, project_id, COALESCE(person_id::text, ''), kind, display_name, role, instructions, created_at`

func scanMember(row interface{ Scan(...any) error }, m *models.Member) error {
	return row.Scan(&m.ID, &m.ProjectID, &m.PersonID, &m.Kind, &m.DisplayName, &m.Role, &m.Instructions, &m.CreatedAt)
}

func (s *Store) InsertMember(ctx context.Context, m models.Member) error {
	_, err := s.q.Exec(ctx, `INSERT INTO members (id, project_id, person_id, kind, display_name, role, instructions, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		m.ID, m.ProjectID, nullID(m.PersonID), m.Kind, m.DisplayName, m.Role, m.Instructions, m.CreatedAt)
	return mapErr(err)
}

func (s *Store) GetMember(ctx context.Context, id string) (*models.Member, error) {
	var m models.Member
	err := scanMember(s.q.QueryRow(ctx, `SELECT `+memberCols+` FROM members WHERE id=$1`, id), &m)
	return &m, mapErr(err)
}

// MemberForPerson returns the Person's Member in a Project.
func (s *Store) MemberForPerson(ctx context.Context, projectID, personID string) (*models.Member, error) {
	var m models.Member
	err := scanMember(s.q.QueryRow(ctx, `SELECT `+memberCols+` FROM members WHERE project_id=$1 AND person_id=$2`,
		projectID, personID), &m)
	return &m, mapErr(err)
}

// ListMembers returns a Project's Members, oldest first.
func (s *Store) ListMembers(ctx context.Context, projectID string) ([]models.Member, error) {
	rows, err := s.q.Query(ctx, `SELECT `+memberCols+` FROM members WHERE project_id=$1 ORDER BY created_at, id`, projectID)
	if err != nil {
		return nil, mapErr(err)
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

// --- channels ---

const channelCols = `id, project_id, name, archived_at, created_at`

func scanChannel(row interface{ Scan(...any) error }, c *models.Channel) error {
	return row.Scan(&c.ID, &c.ProjectID, &c.Name, &c.ArchivedAt, &c.CreatedAt)
}

func (s *Store) InsertChannel(ctx context.Context, c models.Channel) error {
	_, err := s.q.Exec(ctx, `INSERT INTO channels (id, project_id, name, created_at) VALUES ($1, $2, $3, $4)`,
		c.ID, c.ProjectID, c.Name, c.CreatedAt)
	return mapErr(err)
}

func (s *Store) GetChannel(ctx context.Context, id string) (*models.Channel, error) {
	var c models.Channel
	err := scanChannel(s.q.QueryRow(ctx, `SELECT `+channelCols+` FROM channels WHERE id=$1`, id), &c)
	return &c, mapErr(err)
}

// ListChannels returns a Project's Channels, oldest first; archived on request.
func (s *Store) ListChannels(ctx context.Context, projectID string, includeArchived bool) ([]models.Channel, error) {
	rows, err := s.q.Query(ctx, `SELECT `+channelCols+` FROM channels
		WHERE project_id=$1 AND ($2 OR archived_at IS NULL) ORDER BY created_at, id`, projectID, includeArchived)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []models.Channel{}
	for rows.Next() {
		var c models.Channel
		if err := scanChannel(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateChannel writes name and archived_at.
func (s *Store) UpdateChannel(ctx context.Context, c models.Channel) error {
	return one(s.q.Exec(ctx, `UPDATE channels SET name=$2, archived_at=$3 WHERE id=$1`, c.ID, c.Name, c.ArchivedAt))
}

// JoinPerson makes m (a human Member) part of its Project unless the Person
// already is, and returns the Person's Member either way. Safe under races.
func (s *Store) JoinPerson(ctx context.Context, m models.Member) (*models.Member, bool, error) {
	tag, err := s.q.Exec(ctx, `INSERT INTO members (id, project_id, person_id, kind, display_name, role, instructions, created_at)
		VALUES ($1, $2, $3, 'human', $4, $5, '', $6)
		ON CONFLICT (project_id, person_id) WHERE person_id IS NOT NULL DO NOTHING`,
		m.ID, m.ProjectID, m.PersonID, m.DisplayName, m.Role, m.CreatedAt)
	if err != nil {
		return nil, false, mapErr(err)
	}
	got, err := s.MemberForPerson(ctx, m.ProjectID, m.PersonID)
	return got, tag.RowsAffected() == 1, err
}
