// Package config loads and validates process configuration from the
// environment. Each binary calls its Load function once at startup and
// refuses to start on invalid input.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Getenv matches os.Getenv so tests can pass a map lookup.
type Getenv func(string) string

// GitHub holds the optional Repo / Issues / Pipelines integration settings.
type GitHub struct {
	Token         string // GITHUB_TOKEN: opens PRs, syncs Issues
	Repo          string // GITHUB_REPO: owner/name
	WebhookSecret string // GITHUB_WEBHOOK_SECRET: verifies webhook signatures
}

// Server is the buildbee-server configuration.
type Server struct {
	Addr         string // BUILDBEE_ADDR
	DatabaseURL  string // DATABASE_URL
	WebDir       string // BUILDBEE_WEB_DIR: serve the UI from disk instead of the embed
	MaxBodyBytes int64  // BUILDBEE_MAX_BODY_BYTES
	GitHub       GitHub
}

const (
	defaultServerAddr   = ":8080"
	defaultMaxBodyBytes = 32 << 20
)

// LoadServer reads the Server configuration.
func LoadServer(getenv Getenv) (Server, error) {
	c := Server{
		Addr:         value(getenv, "BUILDBEE_ADDR", defaultServerAddr),
		DatabaseURL:  value(getenv, "DATABASE_URL", ""),
		WebDir:       value(getenv, "BUILDBEE_WEB_DIR", ""),
		MaxBodyBytes: defaultMaxBodyBytes,
		GitHub: GitHub{
			Token:         value(getenv, "GITHUB_TOKEN", ""),
			Repo:          value(getenv, "GITHUB_REPO", ""),
			WebhookSecret: value(getenv, "GITHUB_WEBHOOK_SECRET", ""),
		},
	}
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required (Postgres is the only store)"))
	} else if u, err := url.Parse(c.DatabaseURL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		errs = append(errs, errors.New("DATABASE_URL must be a postgres:// URL"))
	}
	if v := value(getenv, "BUILDBEE_MAX_BODY_BYTES", ""); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1<<10 {
			errs = append(errs, fmt.Errorf("BUILDBEE_MAX_BODY_BYTES must be an integer >= 1024, got %q", v))
		} else {
			c.MaxBodyBytes = n
		}
	}
	if c.GitHub.Repo != "" && !validRepo(c.GitHub.Repo) {
		errs = append(errs, fmt.Errorf("GITHUB_REPO must be owner/name, got %q", c.GitHub.Repo))
	}
	return c, errors.Join(errs...)
}

// Worker is the buildbee-worker configuration.
type Worker struct {
	Addr        string // BUILDBEE_WORKER_ADDR
	ServerURL   string // BUILDBEE_URL
	FakeSandbox bool   // BUILDBEE_FAKE_SANDBOX=1: never touch Docker (tests, demos)
}

// LoadWorker reads the worker configuration.
func LoadWorker(getenv Getenv) (Worker, error) {
	c := Worker{
		Addr:        value(getenv, "BUILDBEE_WORKER_ADDR", "127.0.0.1:8090"),
		ServerURL:   strings.TrimRight(value(getenv, "BUILDBEE_URL", "http://127.0.0.1:8080"), "/"),
		FakeSandbox: value(getenv, "BUILDBEE_FAKE_SANDBOX", "") == "1",
	}
	if u, err := url.Parse(c.ServerURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c, fmt.Errorf("BUILDBEE_URL must be an http(s) URL, got %q", c.ServerURL)
	}
	return c, nil
}

func value(getenv Getenv, key, def string) string {
	if v := strings.TrimSpace(getenv(key)); v != "" {
		return v
	}
	return def
}

func validRepo(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	return ok && owner != "" && name != "" && !strings.Contains(name, "/")
}
