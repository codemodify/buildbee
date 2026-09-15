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
