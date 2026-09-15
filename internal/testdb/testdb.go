// Package testdb gives each test its own migrated Postgres database.
//
// Set BUILDBEE_TEST_DATABASE_URL to a server-level URL (any database the
// role can connect to, e.g. postgres://postgres:postgres@127.0.0.1:55432/postgres)
// to reuse a running Postgres. Otherwise the first test starts a throwaway
// postgres container through the docker CLI and Main removes it when the
// package's tests finish.
//
// Databases are cloned from a migrated template, so each test pays for one
// CREATE DATABASE rather than a full migration.
package testdb

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/migrate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvURL names the variable that points tests at an existing Postgres.
const EnvURL = "BUILDBEE_TEST_DATABASE_URL"

const image = "postgres:17-alpine"

var (
	once      sync.Once
	setupErr  error
	adminURL  string
	template  string
	container string
	counter   atomic.Int64
)

// Main runs a package's tests and removes the container testdb started, if any.
// Use it from TestMain in every package that calls New.
func Main(m *testing.M) {
	code := m.Run()
	if container != "" {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	}
	os.Exit(code)
}

// New returns a pool on a fresh database with every migration applied.
// The database is dropped when the test ends.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	once.Do(func() { setupErr = setup() })
	if setupErr != nil {
		t.Fatalf("testdb: %v", setupErr)
	}
	ctx := context.Background()
	name := fmt.Sprintf("%s_%d", template, counter.Add(1))
	if err := adminExec(ctx, fmt.Sprintf(`CREATE DATABASE %s TEMPLATE %s`, name, template)); err != nil {
		t.Fatalf("testdb: create %s: %v", name, err)
	}
	pool, err := pgxpool.New(ctx, withDatabase(adminURL, name))
	if err != nil {
		t.Fatalf("testdb: connect %s: %v", name, err)
	}
	t.Cleanup(func() {
		pool.Close()
		_ = adminExec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, name))
	})
	return pool
}

func setup() error {
	adminURL = strings.TrimSpace(os.Getenv(EnvURL))
	if adminURL == "" {
		u, err := startContainer()
		if err != nil {
			return fmt.Errorf("%s is unset and starting %s failed: %w", EnvURL, image, err)
		}
		adminURL = u
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := waitReady(ctx, adminURL); err != nil {
		return err
	}
	template = fmt.Sprintf("bbtest_%d_%d", os.Getpid(), time.Now().UnixNano()%1_000_000)
	if err := adminExec(ctx, `CREATE DATABASE `+template); err != nil {
		return fmt.Errorf("create template: %w", err)
	}
	pool, err := pgxpool.New(ctx, withDatabase(adminURL, template))
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := migrate.Up(ctx, pool); err != nil {
		return fmt.Errorf("migrate template: %w", err)
	}
	return nil
}

func startContainer() (string, error) {
	out, err := exec.Command("docker", "run", "-d", "--rm",
		"-e", "POSTGRES_PASSWORD=postgres",
		"-p", "127.0.0.1::5432",
		image, "-c", "fsync=off", "-c", "full_page_writes=off").Output()
	if err != nil {
		return "", commandError(err)
	}
	container = strings.TrimSpace(string(out))
	portOut, err := exec.Command("docker", "port", container, "5432/tcp").Output()
	if err != nil {
		return "", commandError(err)
	}
	// "127.0.0.1:49153" (possibly several lines for v4/v6)
	hostPort := strings.TrimSpace(strings.SplitN(string(portOut), "\n", 2)[0])
	return "postgres://postgres:postgres@" + hostPort + "/postgres?sslmode=disable", nil
}

func waitReady(ctx context.Context, dsn string) error {
	var last error
	for {
		conn, err := pgx.Connect(ctx, dsn)
		if err == nil {
			_, err = conn.Exec(ctx, `SELECT 1`)
			_ = conn.Close(ctx)
			if err == nil {
				return nil
			}
		}
		last = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("postgres not ready: %w", last)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func adminExec(ctx context.Context, sql string) error {
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, sql)
	return err
}

func withDatabase(dsn, name string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	u.Path = "/" + name
	return u.String()
}

func commandError(err error) error {
	if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
	}
	return err
}
