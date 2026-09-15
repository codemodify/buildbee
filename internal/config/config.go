// Package config loads and validates process configuration from the
// environment. Each binary calls its Load function once at startup and
// refuses to start on invalid input.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
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
	ServerURL       string   // BUILDBEE_URL
	Name            string   // BUILDBEE_WORKER_NAME, default the hostname; unique per worker
	Agents          []string // BUILDBEE_WORKER_AGENTS, comma-separated; empty = decided by the worker
	Slots           int      // BUILDBEE_WORKER_SLOTS: Runs executed at once (default 4)
	AllowHostAgents bool     // BUILDBEE_WORKER_ALLOW_HOST_AGENTS=1: run real agent CLIs on this host
	// AgentCommands overrides how an agent is started, from
	// BUILDBEE_AGENT_<NAME>, e.g. BUILDBEE_AGENT_CLAUDE="npx -y @agentclientprotocol/claude-agent-acp".
	AgentCommands map[string][]string
	RunTimeout    time.Duration // BUILDBEE_WORKER_RUN_TIMEOUT (default 2h)
	Dir           string        // BUILDBEE_WORKER_DIR: repo mirrors and Run checkouts
	OpenPRs       bool          // BUILDBEE_WORKER_OPEN_PRS=0 pushes branches without opening PRs
}

// agentNames are the agents a BUILDBEE_AGENT_<NAME> override may name.
var agentNames = []string{"claude", "codex", "grok", "opencode", "goose"}

// LoadWorker reads the worker configuration.
func LoadWorker(getenv Getenv) (Worker, error) {
	host, _ := os.Hostname()
	c := Worker{
		ServerURL:       strings.TrimRight(value(getenv, "BUILDBEE_URL", "http://127.0.0.1:8080"), "/"),
		Name:            value(getenv, "BUILDBEE_WORKER_NAME", host),
		Slots:           4,
		AllowHostAgents: value(getenv, "BUILDBEE_WORKER_ALLOW_HOST_AGENTS", "") == "1",
		RunTimeout:      2 * time.Hour,
		Dir:             value(getenv, "BUILDBEE_WORKER_DIR", ""),
		OpenPRs:         value(getenv, "BUILDBEE_WORKER_OPEN_PRS", "1") != "0",
	}
	for _, a := range agentNames {
		if argv := strings.Fields(getenv("BUILDBEE_AGENT_" + strings.ToUpper(a))); len(argv) > 0 {
			if c.AgentCommands == nil {
				c.AgentCommands = map[string][]string{}
			}
			c.AgentCommands[a] = argv
		}
	}
	var errs []error
	if u, err := url.Parse(c.ServerURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("BUILDBEE_URL must be an http(s) URL, got %q", c.ServerURL))
	}
	if c.Name == "" {
		errs = append(errs, errors.New("BUILDBEE_WORKER_NAME is required (the hostname is unknown)"))
	}
	for _, a := range strings.Split(getenv("BUILDBEE_WORKER_AGENTS"), ",") {
		if a = strings.ToLower(strings.TrimSpace(a)); a != "" && !slices.Contains(c.Agents, a) {
			c.Agents = append(c.Agents, a)
		}
	}
	if v := value(getenv, "BUILDBEE_WORKER_SLOTS", ""); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 256 {
			errs = append(errs, fmt.Errorf("BUILDBEE_WORKER_SLOTS must be 1-256, got %q", v))
		} else {
			c.Slots = n
		}
	}
	if v := value(getenv, "BUILDBEE_WORKER_RUN_TIMEOUT", ""); v != "" {
		if d, err := time.ParseDuration(v); err != nil || d < time.Minute {
			errs = append(errs, fmt.Errorf("BUILDBEE_WORKER_RUN_TIMEOUT must be a duration of at least 1m, got %q", v))
		} else {
			c.RunTimeout = d
		}
	}
	return c, errors.Join(errs...)
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
