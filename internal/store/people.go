package store

import (
	"context"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/google/uuid"
)

// UpsertPerson returns the Person with this name (case-insensitive),
// creating them on first use.
func (s *Store) UpsertPerson(ctx context.Context, name string) (*models.Person, error) {
	var p models.Person
	err := s.q.QueryRow(ctx, `INSERT INTO people (id, name, created_at) VALUES ($1, $2, $3)
		ON CONFLICT ((lower(name))) DO UPDATE SET name = people.name
		RETURNING id, name, created_at`, uuid.NewString(), name, time.Now().UTC()).
		Scan(&p.ID, &p.Name, &p.CreatedAt)
	return &p, mapErr(err)
}

// RenamePerson changes a Person's name and their name in every Project,
// returning the Members renamed. ErrConflict if someone has the name.
func (s *Store) RenamePerson(ctx context.Context, id, name string) ([]models.Member, error) {
	if err := one(s.q.Exec(ctx, `UPDATE people SET name=$2 WHERE id=$1`, id, name)); err != nil {
		return nil, err
	}
	rows, err := s.q.Query(ctx, `UPDATE members SET display_name=$2 WHERE person_id=$1 RETURNING `+memberCols, id, name)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []models.Member
	for rows.Next() {
		var m models.Member
		if err := scanMember(rows, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetPerson returns one Person.
func (s *Store) GetPerson(ctx context.Context, id string) (*models.Person, error) {
	var p models.Person
	err := s.q.QueryRow(ctx, `SELECT id, name, created_at FROM people WHERE id=$1`, id).
		Scan(&p.ID, &p.Name, &p.CreatedAt)
	return &p, mapErr(err)
}

// GetPreferences returns a Person's preferences (defaults when never set).
func (s *Store) GetPreferences(ctx context.Context, personID string) (*models.Preferences, error) {
	p := models.Preferences{PersonID: personID}
	err := s.q.QueryRow(ctx, `SELECT mute_mentions, mute_routines, updated_at FROM person_preferences WHERE person_id=$1`, personID).
		Scan(&p.MuteMentions, &p.MuteRoutines, &p.UpdatedAt)
	if err = mapErr(err); err == ErrNotFound {
		return &p, nil
	}
	return &p, err
}

// SetPreferences stores a Person's preferences.
func (s *Store) SetPreferences(ctx context.Context, p models.Preferences) error {
	_, err := s.q.Exec(ctx, `INSERT INTO person_preferences (person_id, mute_mentions, mute_routines, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (person_id) DO UPDATE SET mute_mentions=$2, mute_routines=$3, updated_at=$4`,
		p.PersonID, p.MuteMentions, p.MuteRoutines, p.UpdatedAt)
	return mapErr(err)
}

// Roster returns everyone in the Server's open Projects.
func (s *Store) Roster(ctx context.Context, events int) (*models.Roster, error) {
	out := &models.Roster{People: []models.RosterPerson{}, Bots: []models.RosterBot{}, Events: []models.RosterEvent{}}
	rows, err := s.q.Query(ctx, `SELECT p.id, p.name, p.created_at, m.project_id, pr.name, m.id, m.role, m.created_at, m.left_at
		FROM people p
		LEFT JOIN members m ON m.person_id = p.id
			AND EXISTS (SELECT 1 FROM projects x WHERE x.id = m.project_id AND x.archived_at IS NULL AND x.kind = 'project')
		LEFT JOIN projects pr ON pr.id = m.project_id
		ORDER BY lower(p.name), pr.name`)
	if err != nil {
		return nil, mapErr(err)
	}
	byID := map[string]int{}
	for rows.Next() {
		var p models.Person
		var projectID, projectName, memberID, role *string
		var joined *time.Time
		var left *time.Time
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &projectID, &projectName, &memberID, &role, &joined, &left); err != nil {
			rows.Close()
			return nil, err
		}
		i, ok := byID[p.ID]
		if !ok {
			i = len(out.People)
			byID[p.ID] = i
			out.People = append(out.People, models.RosterPerson{Person: p, Projects: []models.Membership{}})
		}
		if projectID != nil {
			out.People[i].Projects = append(out.People[i].Projects, models.Membership{ProjectID: *projectID, ProjectName: *projectName,
				MemberID: *memberID, Role: *role, JoinedAt: *joined, LeftAt: left})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	bots, err := s.q.Query(ctx, `SELECT m.id, m.project_id, '', m.kind, m.display_name, m.role, m.instructions, m.agent, m.left_at,
		m.created_at, pr.name FROM members m JOIN projects pr ON pr.id = m.project_id
		WHERE m.kind = 'bot' AND pr.archived_at IS NULL ORDER BY pr.name, m.created_at`)
	if err != nil {
		return nil, mapErr(err)
	}
	for bots.Next() {
		var b models.RosterBot
		if err := bots.Scan(&b.ID, &b.ProjectID, &b.PersonID, &b.Kind, &b.DisplayName, &b.Role, &b.Instructions, &b.Agent,
			&b.LeftAt, &b.CreatedAt, &b.ProjectName); err != nil {
			bots.Close()
			return nil, err
		}
		out.Bots = append(out.Bots, b)
	}
	bots.Close()
	if err := bots.Err(); err != nil {
		return nil, err
	}
	evs, err := s.q.Query(ctx, `SELECT CASE WHEN a.type = 'project' THEN a.actor ELSE COALESCE(a.payload->>'name', a.actor) END, a.action, a.project_id, pr.name, a.created_at
		FROM activity a JOIN projects pr ON pr.id = a.project_id
		WHERE pr.kind = 'project' AND ((a.type = 'member' AND a.action IN ('joined', 'left', 'added')) OR (a.type = 'project' AND a.action = 'created'))
		ORDER BY a.created_at DESC, a.seq DESC LIMIT $1`, events)
	if err != nil {
		return nil, mapErr(err)
	}
	defer evs.Close()
	for evs.Next() {
		var e models.RosterEvent
		if err := evs.Scan(&e.Name, &e.Action, &e.ProjectID, &e.ProjectName, &e.At); err != nil {
			return nil, err
		}
		out.Events = append(out.Events, e)
	}
	return out, evs.Err()
}
