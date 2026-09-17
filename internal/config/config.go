// Package config loads and validates process configuration from the
// environment. Each binary calls its Load function once at startup and
// refuses to start on invalid input.
package config

import (
	"errors"
	"fmt"
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
}

// S3 is an S3-compatible bucket for blobs.
type S3 struct {
	Endpoint  string // BUILDBEE_S3_ENDPOINT, e.g. http://minio.lan:9000
	Bucket    string // BUILDBEE_S3_BUCKET (default buildbee)
	Region    string // BUILDBEE_S3_REGION (default us-east-1)
	AccessKey string // BUILDBEE_S3_ACCESS_KEY
	SecretKey string // BUILDBEE_S3_SECRET_KEY
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

// Agent is the buildbee-agent configuration: the process that plugs one
// Bot's AI into a Server.
type Agent struct {
	ServerURL    string // BUILDBEE_URL
	BotID        string // BUILDBEE_BOT: the Bot this agent runs
	AI           string // BUILDBEE_AI: claude, codex, grok, opencode, goose or fake
	Name         string // BUILDBEE_AGENT_NAME: how this process names itself (default <bot>@<host>)
	Host         string // BUILDBEE_AGENT_HOST: the machine, for people to see (default the hostname)
	Slots        int    // BUILDBEE_SLOTS: Runs this Bot takes at once (default 4)
	Isolation    string // BUILDBEE_ISOLATION: container (default) or host
	Sandbox      string // BUILDBEE_SANDBOX: auto (default), require or off; host isolation only
	Image        string // BUILDBEE_IMAGE: the image container isolation starts
	Memory, CPUs string // BUILDBEE_MEMORY, BUILDBEE_CPUS: per-Run container limits
	Network      string // BUILDBEE_NETWORK: per-Run container network
	// AICommands overrides how an AI is started, from BUILDBEE_AI_<NAME>,
	// e.g. BUILDBEE_AI_CLAUDE="npx -y @agentclientprotocol/claude-agent-acp".
	AICommands map[string][]string
	RunTimeout time.Duration // BUILDBEE_RUN_TIMEOUT (default 2h)
	Dir        string        // BUILDBEE_DIR: repo mirrors and Run checkouts
	OpenPRs    bool          // BUILDBEE_OPEN_PRS=0 pushes branches without opening pull requests
}

// aiNames are the AIs a BUILDBEE_AI_<NAME> override may name.
var aiNames = []string{"claude", "codex", "grok", "opencode", "goose"}

// LoadAgent reads the agent configuration. BotID and AI are required: an
// agent is one Bot running one AI.
func LoadAgent(getenv Getenv) (Agent, error) {
	host, _ := os.Hostname()
	c := Agent{
		ServerURL:  strings.TrimRight(value(getenv, "BUILDBEE_URL", "http://127.0.0.1:8080"), "/"),
		BotID:      value(getenv, "BUILDBEE_BOT", ""),
		AI:         strings.ToLower(value(getenv, "BUILDBEE_AI", "")),
		Name:       value(getenv, "BUILDBEE_AGENT_NAME", ""),
		Host:       value(getenv, "BUILDBEE_AGENT_HOST", host),
		Slots:      4,
		Isolation:  value(getenv, "BUILDBEE_ISOLATION", "container"),
		Sandbox:    strings.ToLower(value(getenv, "BUILDBEE_SANDBOX", "auto")),
		Image:      value(getenv, "BUILDBEE_IMAGE", "buildbee-agents"),
		Memory:     value(getenv, "BUILDBEE_MEMORY", "8g"),
		CPUs:       value(getenv, "BUILDBEE_CPUS", "4"),
		Network:    value(getenv, "BUILDBEE_NETWORK", ""),
		RunTimeout: 2 * time.Hour,
		Dir:        value(getenv, "BUILDBEE_DIR", ""),
		OpenPRs:    value(getenv, "BUILDBEE_OPEN_PRS", "1") != "0",
	}
	for _, a := range aiNames {
		if argv := strings.Fields(getenv("BUILDBEE_AI_" + strings.ToUpper(a))); len(argv) > 0 {
			if c.AICommands == nil {
				c.AICommands = map[string][]string{}
			}
			c.AICommands[a] = argv
		}
	}
	var errs []error
	if u, err := url.Parse(c.ServerURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("BUILDBEE_URL must be an http(s) URL, got %q", c.ServerURL))
	}
	if c.BotID == "" {
		errs = append(errs, errors.New("BUILDBEE_BOT (or --bot) is required: the Bot this agent runs, from Add bot in # status"))
	}
	if c.AI == "" {
		errs = append(errs, errors.New("BUILDBEE_AI (or --ai) is required: claude, codex, grok, opencode, goose or fake"))
	}
	if c.Isolation != "container" && c.Isolation != "host" {
		errs = append(errs, fmt.Errorf("BUILDBEE_ISOLATION must be container or host, got %q", c.Isolation))
	}
	if !slices.Contains([]string{"auto", "require", "off"}, c.Sandbox) {
		errs = append(errs, fmt.Errorf("BUILDBEE_SANDBOX must be auto, require or off, got %q", c.Sandbox))
	}
	if v := value(getenv, "BUILDBEE_SLOTS", ""); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 256 {
			errs = append(errs, fmt.Errorf("BUILDBEE_SLOTS must be 1-256, got %q", v))
		} else {
			c.Slots = n
		}
	}
	if v := value(getenv, "BUILDBEE_RUN_TIMEOUT", ""); v != "" {
		if d, err := time.ParseDuration(v); err != nil || d < time.Minute {
			errs = append(errs, fmt.Errorf("BUILDBEE_RUN_TIMEOUT must be a duration of at least 1m, got %q", v))
		} else {
			c.RunTimeout = d
		}
	}
	if c.Name == "" && c.BotID != "" {
		c.Name = c.BotID
		if c.Host != "" {
			c.Name += "@" + c.Host
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
