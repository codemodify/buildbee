// Package store persists BuildBee records in Postgres.
//
// Store methods are plain reads and writes with no side effects: business
// rules, Activity and notifications live in package core, which runs each
// operation inside Tx so a change and its Activity commit together.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/codemodify/buildbee/internal/migrate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound: the record does not exist (or is not visible to the caller).
	ErrNotFound = errors.New("not found")
	// ErrConflict: the change conflicts with the record's current state.
	ErrConflict = errors.New("conflict")
	// ErrInvalid: the input breaks a schema constraint.
	ErrInvalid = errors.New("invalid")
)

type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store reads and writes records through a pool or, inside Tx, a transaction.
type Store struct {
	pool *pgxpool.Pool
	q    querier
}

// New returns a Store on pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: pool} }

// Tx runs fn in one transaction. Inside fn, use the Store it is given.
// Calling Tx on a Store that is already inside a transaction runs fn in it.
func (s *Store) Tx(ctx context.Context, fn func(tx *Store) error) error {
	if s.pool == nil {
		return fn(s)
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{q: tx})
	})
}

// Ping reports whether Postgres answers.
func (s *Store) Ping(ctx context.Context) error {
	if s.pool == nil {
		return nil
	}
	return s.pool.Ping(ctx)
}

// Open connects with retries so compose can start the Server next to Postgres.
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	var last error
	for {
		pool, err := pgxpool.New(ctx, databaseURL)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return pool, nil
			}
			pool.Close()
		}
		last = err
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("postgres: %w", last)
		case <-time.After(time.Second):
		}
	}
}

// Migrate applies the embedded migrations.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error { return migrate.Up(ctx, pool) }

// Page selects a window of a seq-ordered list. After returns records newer
// than the cursor, Before records older than it; with neither, the newest.
type Page struct {
	Before int64
	After  int64
	Limit  int
}

const (
	defaultLimit = 100
	maxLimit     = 1000
)

func (p Page) limit() int {
	switch {
	case p.Limit <= 0:
		return defaultLimit
	case p.Limit > maxLimit:
		return maxLimit
	}
	return p.Limit
}

// mapErr turns Postgres errors into the package's sentinel errors.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", ErrConflict, pg.ConstraintName)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: %s", ErrNotFound, pg.ConstraintName)
		case "23514", "22P02", "22021", "22P05": // check, bad uuid, bad encoding
			return fmt.Errorf("%w: %s", ErrInvalid, pg.Message)
		}
	}
	return err
}

// nullID stores "" as NULL for optional references.
func nullID(id string) any {
	if id == "" {
		return nil
	}
	return id
}

// one maps a single-row write that must hit exactly one row.
func one(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
