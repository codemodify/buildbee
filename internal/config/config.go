// Package config loads and validates process configuration from the
// environment. Each binary calls its Load function once at startup and
// refuses to start on invalid input.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
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
	// Blobs is where attachments and large Artifacts go: BlobDir, or an
	// S3-compatible bucket when S3.Endpoint is set.
	BlobDir        string // BUILDBEE_BLOB_DIR (default ~/.local/share/buildbee/blobs)
	S3             S3
	MaxUploadBytes int64 // BUILDBEE_MAX_UPLOAD_BYTES: one attachment (default 25 MiB)
	RetentionDays  int   // BUILDBEE_RETENTION_DAYS: keep Run event streams and read notifications this long (0 = forever)
	// LocalWorker is whether the Server runs agents itself
	// (BUILDBEE_LOCAL_WORKER): auto (default) when this machine can,
	// on (refuse to start if it cannot) or off.
	LocalWorker string
}

// S3 is an S3-compatible bucket for blobs.
type S3 struct {
	Endpoint  string // BUILDBEE_S3_ENDPOINT, e.g. http://minio.lan:9000
	Bucket    string // BUILDBEE_S3_BUCKET (default buildbee)
	Region    string // BUILDBEE_S3_REGION (default us-east-1)
	AccessKey string // BUILDBEE_S3_ACCESS_KEY
	SecretKey string // BUILDBEE_S3_SECRET_KEY
}

// Local worker modes.
const (
	LocalAuto = "auto"
	LocalOn   = "on"
	LocalOff  = "off"
)

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
		LocalWorker:  strings.ToLower(value(getenv, "BUILDBEE_LOCAL_WORKER", LocalAuto)),
		BlobDir:      value(getenv, "BUILDBEE_BLOB_DIR", defaultBlobDir()),
		S3: S3{
			Endpoint:  strings.TrimRight(value(getenv, "BUILDBEE_S3_ENDPOINT", ""), "/"),
			Bucket:    value(getenv, "BUILDBEE_S3_BUCKET", "buildbee"),
			Region:    value(getenv, "BUILDBEE_S3_REGION", "us-east-1"),
			AccessKey: value(getenv, "BUILDBEE_S3_ACCESS_KEY", ""),
			SecretKey: value(getenv, "BUILDBEE_S3_SECRET_KEY", ""),
		},
		MaxUploadBytes: 25 << 20,
		RetentionDays:  90,
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
	if v := value(getenv, "BUILDBEE_MAX_UPLOAD_BYTES", ""); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1<<10 {
			errs = append(errs, fmt.Errorf("BUILDBEE_MAX_UPLOAD_BYTES must be an integer >= 1024, got %q", v))
		} else {
			c.MaxUploadBytes = n
		}
	}
	if c.MaxUploadBytes > c.MaxBodyBytes {
		errs = append(errs, fmt.Errorf("BUILDBEE_MAX_UPLOAD_BYTES (%d) must not exceed BUILDBEE_MAX_BODY_BYTES (%d)", c.MaxUploadBytes, c.MaxBodyBytes))
	}
	if v := value(getenv, "BUILDBEE_RETENTION_DAYS", ""); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			errs = append(errs, fmt.Errorf("BUILDBEE_RETENTION_DAYS must be 0 or more days, got %q", v))
		} else {
			c.RetentionDays = n
		}
	}
	if c.S3.Endpoint != "" {
		if u, err := url.Parse(c.S3.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			errs = append(errs, fmt.Errorf("BUILDBEE_S3_ENDPOINT must be an http(s) URL, got %q", c.S3.Endpoint))
		}
		if c.S3.AccessKey == "" || c.S3.SecretKey == "" {
			errs = append(errs, errors.New("BUILDBEE_S3_ACCESS_KEY and BUILDBEE_S3_SECRET_KEY are required with BUILDBEE_S3_ENDPOINT"))
		}
	} else if c.BlobDir == "" {
		errs = append(errs, errors.New("BUILDBEE_BLOB_DIR is required (no home directory to default to)"))
	}
	if !slices.Contains([]string{LocalAuto, LocalOn, LocalOff}, c.LocalWorker) {
		errs = append(errs, fmt.Errorf("BUILDBEE_LOCAL_WORKER must be auto, on or off, got %q", c.LocalWorker))
	}
	return c, errors.Join(errs...)
}

// defaultBlobDir is ~/.local/share/buildbee/blobs, or "" without a home.
func defaultBlobDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "buildbee", "blobs")
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".local", "share", "buildbee", "blobs")
	}
	return ""
}

// LocalWorker is the configuration of the Server's own worker: the
// BUILDBEE_WORKER_* settings, talking to the Server over loopback, named
// after the host with a "-server" suffix, with its own directory.
func LocalWorker(getenv Getenv, addr string) (Worker, error) {
	c, err := LoadWorker(func(k string) string {
		switch k {
		case "BUILDBEE_URL":
			return loopback(addr)
		case "BUILDBEE_WORKER_NAME":
			if v := getenv(k); v != "" {
				return v
			}
			host, _ := os.Hostname()
			return host + "-server"
		case "BUILDBEE_WORKER_DIR":
			if v := getenv(k); v != "" {
				return v
			}
			if cache, err := os.UserCacheDir(); err == nil {
				return filepath.Join(cache, "buildbee-server-worker")
			}
		}
		return getenv(k)
	})
	return c, err
}

// loopback is the URL a process on this machine reaches addr at.
func loopback(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// Worker is the buildbee-worker configuration.
type Worker struct {
	ServerURL    string   // BUILDBEE_URL
	Name         string   // BUILDBEE_WORKER_NAME, default the hostname; unique per worker
	Agents       []string // BUILDBEE_WORKER_AGENTS, comma-separated; empty = decided by the worker
	Slots        int      // BUILDBEE_WORKER_SLOTS: Runs executed at once (default 4)
	Isolation    string   // BUILDBEE_WORKER_ISOLATION: container (default) or host
	Sandbox      string   // BUILDBEE_WORKER_SANDBOX: auto (default), require or off; host isolation only
	Image        string   // BUILDBEE_WORKER_IMAGE: agent image for container isolation
	Memory, CPUs string   // BUILDBEE_WORKER_MEMORY, BUILDBEE_WORKER_CPUS: per-Run container limits
	Network      string   // BUILDBEE_WORKER_NETWORK: per-Run container network
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
		ServerURL:  strings.TrimRight(value(getenv, "BUILDBEE_URL", "http://127.0.0.1:8080"), "/"),
		Name:       value(getenv, "BUILDBEE_WORKER_NAME", host),
		Slots:      4,
		Isolation:  value(getenv, "BUILDBEE_WORKER_ISOLATION", "container"),
		Sandbox:    strings.ToLower(value(getenv, "BUILDBEE_WORKER_SANDBOX", "auto")),
		Image:      value(getenv, "BUILDBEE_WORKER_IMAGE", "buildbee-agents"),
		Memory:     value(getenv, "BUILDBEE_WORKER_MEMORY", "8g"),
		CPUs:       value(getenv, "BUILDBEE_WORKER_CPUS", "4"),
		Network:    value(getenv, "BUILDBEE_WORKER_NETWORK", ""),
		RunTimeout: 2 * time.Hour,
		Dir:        value(getenv, "BUILDBEE_WORKER_DIR", ""),
		OpenPRs:    value(getenv, "BUILDBEE_WORKER_OPEN_PRS", "1") != "0",
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
	if c.Isolation != "container" && c.Isolation != "host" {
		errs = append(errs, fmt.Errorf("BUILDBEE_WORKER_ISOLATION must be container or host, got %q", c.Isolation))
	}
	if !slices.Contains([]string{"auto", "require", "off"}, c.Sandbox) {
		errs = append(errs, fmt.Errorf("BUILDBEE_WORKER_SANDBOX must be auto, require or off, got %q", c.Sandbox))
	}
	if getenv("BUILDBEE_WORKER_ALLOW_HOST_AGENTS") != "" {
		errs = append(errs, errors.New("BUILDBEE_WORKER_ALLOW_HOST_AGENTS is gone; set BUILDBEE_WORKER_ISOLATION=host to run agents on this machine"))
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
