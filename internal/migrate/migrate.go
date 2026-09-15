// Package migrate applies the embedded SQL migrations.
package migrate

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var files embed.FS

// lockKey serializes migrations across Servers sharing one database.
const lockKey int64 = 0x62756964_62656531 // "buildbee1"

type migration struct {
	name, body, sum string
}

// Up applies pending migrations in lexical order. Each file and its version
// row commit in one transaction under a session advisory lock, so a crash or
// a second Server starting at the same time cannot half-apply one.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	migs, err := load()
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		return fmt.Errorf("migration lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey) }()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			checksum TEXT NOT NULL DEFAULT '',
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("schema_migrations: %w", err)
	}
	applied, err := appliedVersions(ctx, conn.Conn())
	if err != nil {
		return err
	}
	if err := check(migs, applied); err != nil {
		return err
	}
	for _, m := range migs {
		if _, ok := applied[m.name]; ok {
			continue
		}
		if err := pgx.BeginFunc(ctx, conn.Conn(), func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, m.body); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)`, m.name, m.sum)
			return err
		}); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

func load() ([]migration, error) {
	entries, err := fs.ReadDir(files, "sql")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := files.ReadFile("sql/" + e.Name())
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		out = append(out, migration{name: e.Name(), body: string(raw), sum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

func appliedVersions(ctx context.Context, conn *pgx.Conn) (map[string]string, error) {
	rows, err := conn.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var v, sum string
		if err := rows.Scan(&v, &sum); err != nil {
			return nil, err
		}
		out[v] = sum
	}
	return out, rows.Err()
}

// errRecreate explains the only fix for a mismatched pre-release database.
var errRecreate = errors.New("recreate the database (docker compose down -v)")

// check refuses a database whose recorded migrations this build does not
// ship (an older or newer BuildBee created it) or whose files changed after
// they were applied.
func check(migs []migration, applied map[string]string) error {
	known := map[string]string{}
	for _, m := range migs {
		known[m.name] = m.sum
	}
	var unknown, changed []string
	for v, sum := range applied {
		want, ok := known[v]
		switch {
		case !ok:
			unknown = append(unknown, v)
		case sum != "" && sum != want:
			changed = append(changed, v)
		}
	}
	sort.Strings(unknown)
	sort.Strings(changed)
	if len(unknown) > 0 {
		return fmt.Errorf("database has migrations this build does not ship (%s): %w", strings.Join(unknown, ", "), errRecreate)
	}
	if len(changed) > 0 {
		return fmt.Errorf("migrations changed after they were applied (%s): %w", strings.Join(changed, ", "), errRecreate)
	}
	return nil
}
