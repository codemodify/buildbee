package store

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/codemodify/buildbee/internal/models"
)

const projectCols = `id, name, auto_run, merge_policy, max_runs, instructions, repo_url, default_branch, kind, archived_at, created_at`

func scanProject(row interface{ Scan(...any) error }, p *models.Project) error {
	return row.Scan(&p.ID, &p.Name, &p.AutoRun, &p.MergePolicy, &p.MaxRuns, &p.Instructions, &p.RepoURL, &p.DefaultBranch, &p.Kind, &p.ArchivedAt, &p.CreatedAt)
}

// DirectSpace returns the hidden Project for DMs between people, creating
// it the first time.
func (s *Store) DirectSpace(ctx context.Context, id string, now time.Time) (*models.Project, error) {
	if _, err := s.q.Exec(ctx, `INSERT INTO projects (id, name, kind, created_at) VALUES ($1, 'Direct messages', 'direct', $2)
		ON CONFLICT (kind) WHERE kind = 'direct' DO NOTHING`, id, now); err != nil {
		return nil, mapErr(err)
	}
	var p models.Project
	err := scanProject(s.q.QueryRow(ctx, `SELECT `+projectCols+` FROM projects WHERE kind = 'direct'`), &p)
	return &p, mapErr(err)
}

func (s *Store) InsertProject(ctx context.Context, p models.Project) error {
	if p.MergePolicy == "" {
		p.MergePolicy = models.MergeAuto
	}
	_, err := s.q.Exec(ctx, `INSERT INTO projects (id, name, auto_run, merge_policy, instructions, repo_url, default_branch, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		p.ID, p.Name, p.AutoRun, p.MergePolicy, p.Instructions, p.RepoURL, p.DefaultBranch, p.CreatedAt)
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
		WHERE kind = 'project' AND ($1 OR archived_at IS NULL) ORDER BY created_at, id`, includeArchived)
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

// UpdateProject writes the Project's settings and archived_at.
func (s *Store) UpdateProject(ctx context.Context, p models.Project) error {
	return one(s.q.Exec(ctx, `UPDATE projects SET name=$2, auto_run=$3, merge_policy=$4, instructions=$5, repo_url=$6,
		default_branch=$7, archived_at=$8, max_runs=$9 WHERE id=$1`,
		p.ID, p.Name, p.AutoRun, p.MergePolicy, p.Instructions, p.RepoURL, p.DefaultBranch, p.ArchivedAt, p.MaxRuns))
}

// --- members ---

const memberCols = `id, project_id, COALESCE(person_id::text, ''), kind, display_name, role, instructions, agent, left_at, created_at`

func scanMember(row interface{ Scan(...any) error }, m *models.Member) error {
	return row.Scan(&m.ID, &m.ProjectID, &m.PersonID, &m.Kind, &m.DisplayName, &m.Role, &m.Instructions, &m.Agent, &m.LeftAt, &m.CreatedAt)
}

func (s *Store) InsertMember(ctx context.Context, m models.Member) error {
	_, err := s.q.Exec(ctx, `INSERT INTO members (id, project_id, person_id, kind, display_name, role, instructions, agent, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		m.ID, m.ProjectID, nullID(m.PersonID), m.Kind, m.DisplayName, m.Role, m.Instructions, m.Agent, m.CreatedAt)
	return mapErr(err)
}

// UpdateMember writes display_name, role, instructions and agent.
func (s *Store) UpdateMember(ctx context.Context, m models.Member) error {
	return one(s.q.Exec(ctx, `UPDATE members SET display_name=$2, role=$3, instructions=$4, agent=$5 WHERE id=$1`,
		m.ID, m.DisplayName, m.Role, m.Instructions, m.Agent))
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

const channelCols = `id, project_id, name, kind, locked, archived_at, created_at,
	COALESCE((SELECT array_agg(member_id::text ORDER BY member_id) FROM channel_members cm WHERE cm.channel_id = channels.id), '{}')`

func scanChannel(row interface{ Scan(...any) error }, c *models.Channel) error {
	if err := row.Scan(&c.ID, &c.ProjectID, &c.Name, &c.Kind, &c.Locked, &c.ArchivedAt, &c.CreatedAt, &c.Members); err != nil {
		return err
	}
	if len(c.Members) == 0 {
		c.Members = nil
	}
	return nil
}

func (s *Store) InsertChannel(ctx context.Context, c models.Channel) error {
	if c.Kind == "" {
		c.Kind = models.ChannelOpen
	}
	_, err := s.q.Exec(ctx, `INSERT INTO channels (id, project_id, name, kind, locked, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		c.ID, c.ProjectID, c.Name, c.Kind, c.Locked, c.CreatedAt)
	return mapErr(err)
}

func (s *Store) GetChannel(ctx context.Context, id string) (*models.Channel, error) {
	var c models.Channel
	err := scanChannel(s.q.QueryRow(ctx, `SELECT `+channelCols+` FROM channels WHERE id=$1`, id), &c)
	return &c, mapErr(err)
}

// ListChannels returns a Project's open Channels, oldest first; archived on request.
func (s *Store) ListChannels(ctx context.Context, projectID string, includeArchived bool) ([]models.Channel, error) {
	return s.channels(ctx, `SELECT `+channelCols+` FROM channels
		WHERE project_id=$1 AND kind='channel' AND ($2 OR archived_at IS NULL) ORDER BY locked DESC, created_at, id`, projectID, includeArchived)
}

// ListDMs returns the DMs of a Project that memberID is in, newest first.
func (s *Store) ListDMs(ctx context.Context, projectID, memberID string) ([]models.Channel, error) {
	return s.channels(ctx, `SELECT `+channelCols+` FROM channels WHERE project_id=$1 AND kind='dm'
		AND EXISTS (SELECT 1 FROM channel_members cm WHERE cm.channel_id = channels.id AND cm.member_id = $2)
		ORDER BY created_at DESC, id`, projectID, memberID)
}

// PersonDMs lists every DM a Person is in, across Projects and the direct
// space, most recently active first.
func (s *Store) PersonDMs(ctx context.Context, personID string) ([]models.DM, error) {
	rows, err := s.q.Query(ctx, `SELECT `+prefixedChannelCols+`, pr.kind, pr.name,
			(SELECT count(*) FROM messages m WHERE m.channel_id = c.id AND m.seq > COALESCE(rm.last_seq, 0) AND m.member_id <> me.id),
			COALESCE((SELECT max(m.seq) FROM messages m WHERE m.channel_id = c.id), 0),
			COALESCE((SELECT max(m.created_at) FROM messages m WHERE m.channel_id = c.id), c.created_at) AS last_at
		FROM channels c
		JOIN projects pr ON pr.id = c.project_id AND pr.archived_at IS NULL
		JOIN members me ON me.project_id = c.project_id AND me.person_id = $1
		JOIN channel_members mine ON mine.channel_id = c.id AND mine.member_id = me.id
		LEFT JOIN read_markers rm ON rm.channel_id = c.id AND rm.person_id = $1
		WHERE c.kind = 'dm' AND c.archived_at IS NULL
		ORDER BY last_at DESC, c.id`, personID)
	if err != nil {
		return nil, mapErr(err)
	}
	out := []models.DM{}
	var ids []string
	for rows.Next() {
		var d models.DM
		var kind string
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Name, &d.Kind, &d.Locked, &d.ArchivedAt, &d.CreatedAt, &d.Members,
			&kind, &d.ProjectName, &d.Unread, &d.LastSeq, &d.LastAt); err != nil {
			rows.Close()
			return nil, err
		}
		if kind == models.DirectSpace {
			d.ProjectName = ""
		}
		d.With = []models.DMPeer{}
		out = append(out, d)
		ids = append(ids, d.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}
	peers, err := s.q.Query(ctx, `SELECT cm.channel_id, m.id, COALESCE(m.person_id::text, ''), m.display_name, m.kind, m.role
		FROM channel_members cm JOIN members m ON m.id = cm.member_id
		WHERE cm.channel_id = ANY($1::uuid[]) AND (m.person_id IS NULL OR m.person_id <> $2)
		ORDER BY m.display_name`, ids, personID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer peers.Close()
	at := map[string]int{}
	for i, d := range out {
		at[d.ID] = i
	}
	for peers.Next() {
		var ch string
		var p models.DMPeer
		if err := peers.Scan(&ch, &p.MemberID, &p.PersonID, &p.Name, &p.Kind, &p.Role); err != nil {
			return nil, err
		}
		out[at[ch]].With = append(out[at[ch]].With, p)
	}
	return out, peers.Err()
}

// prefixedChannelCols are channelCols for a query that names channels c.
const prefixedChannelCols = `c.id, c.project_id, c.name, c.kind, c.locked, c.archived_at, c.created_at,
	COALESCE((SELECT array_agg(member_id::text ORDER BY member_id) FROM channel_members cm WHERE cm.channel_id = c.id), '{}')`

// OpenDM returns the DM between exactly memberIDs, creating it if needed.
// Safe under races: the sorted member list is unique.
func (s *Store) OpenDM(ctx context.Context, c models.Channel, memberIDs []string) (*models.Channel, error) {
	ids := slices.Clone(memberIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	key := c.ProjectID + ":" + strings.Join(ids, ",")
	var id string
	err := s.q.QueryRow(ctx, `INSERT INTO channels (id, project_id, name, kind, dm_key, created_at) VALUES ($1, $2, $3, 'dm', $4, $5)
		ON CONFLICT (dm_key) DO UPDATE SET dm_key = EXCLUDED.dm_key RETURNING id`, c.ID, c.ProjectID, c.Name, key, c.CreatedAt).Scan(&id)
	if err != nil {
		return nil, mapErr(err)
	}
	for _, m := range ids {
		if _, err := s.q.Exec(ctx, `INSERT INTO channel_members (channel_id, member_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, m); err != nil {
			return nil, mapErr(err)
		}
	}
	return s.GetChannel(ctx, id)
}

func (s *Store) channels(ctx context.Context, sql string, args ...any) ([]models.Channel, error) {
	rows, err := s.q.Query(ctx, sql, args...)
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

// LeaveProject marks a Person's Member as gone; false if already gone.
func (s *Store) LeaveProject(ctx context.Context, memberID string, at time.Time) (bool, error) {
	tag, err := s.q.Exec(ctx, `UPDATE members SET left_at=$2 WHERE id=$1 AND left_at IS NULL AND kind='human'`, memberID, at)
	if err != nil {
		return false, mapErr(err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *Store) UpdateChannel(ctx context.Context, c models.Channel) error {
	return one(s.q.Exec(ctx, `UPDATE channels SET name=$2, archived_at=$3 WHERE id=$1`, c.ID, c.Name, c.ArchivedAt))
}

// JoinPerson makes m (a human Member) part of its Project unless the Person
// already is, bringing back one who left, and returns the Person's Member
// either way; joined reports a join or return. Safe under races.
func (s *Store) JoinPerson(ctx context.Context, m models.Member) (*models.Member, bool, error) {
	tag, err := s.q.Exec(ctx, `INSERT INTO members (id, project_id, person_id, kind, display_name, role, instructions, created_at)
		VALUES ($1, $2, $3, 'human', $4, $5, '', $6)
		ON CONFLICT (project_id, person_id) WHERE person_id IS NOT NULL
		DO UPDATE SET left_at = NULL WHERE members.left_at IS NOT NULL`,
		m.ID, m.ProjectID, m.PersonID, m.DisplayName, m.Role, m.CreatedAt)
	if err != nil {
		return nil, false, mapErr(err)
	}
	got, err := s.MemberForPerson(ctx, m.ProjectID, m.PersonID)
	return got, tag.RowsAffected() == 1, err
}
