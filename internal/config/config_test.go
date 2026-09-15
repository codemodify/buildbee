package config

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) Getenv { return func(k string) string { return m[k] } }

func TestLoadServerDefaults(t *testing.T) {
	c, err := LoadServer(env(map[string]string{"DATABASE_URL": "postgres://u:p@db/buildbee"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || c.MaxBodyBytes != 32<<20 || c.WebDir != "" {
		t.Fatalf("defaults: %+v", c)
	}
}

func TestLoadServerRejectsBadInput(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"missing database": {map[string]string{}, "DATABASE_URL is required"},
		"not postgres":     {map[string]string{"DATABASE_URL": "mysql://x"}, "postgres:// URL"},
		"bad body limit":   {map[string]string{"DATABASE_URL": "postgres://x/y", "BUILDBEE_MAX_BODY_BYTES": "12"}, "BUILDBEE_MAX_BODY_BYTES"},
		"bad repo":         {map[string]string{"DATABASE_URL": "postgres://x/y", "GITHUB_REPO": "justname"}, "owner/name"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadServer(env(tc.env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestLoadWorker(t *testing.T) {
	c, err := LoadWorker(env(map[string]string{"BUILDBEE_URL": "http://buildbee.lan:8080/",
		"BUILDBEE_WORKER_NAME": "gpu-box", "BUILDBEE_WORKER_AGENTS": " Claude, codex,claude ", "BUILDBEE_WORKER_SLOTS": "12",
		"BUILDBEE_AGENT_CLAUDE": "npx -y @agentclientprotocol/claude-agent-acp", "BUILDBEE_WORKER_RUN_TIMEOUT": "45m"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerURL != "http://buildbee.lan:8080" || c.Name != "gpu-box" || c.Slots != 12 ||
		!slices.Equal(c.Agents, []string{"claude", "codex"}) || c.AllowHostAgents || c.RunTimeout != 45*time.Minute ||
		!slices.Equal(c.AgentCommands["claude"], []string{"npx", "-y", "@agentclientprotocol/claude-agent-acp"}) {
		t.Fatalf("worker: %+v", c)
	}
	for _, bad := range []map[string]string{
		{"BUILDBEE_URL": "buildbee.lan"},
		{"BUILDBEE_WORKER_SLOTS": "0"},
		{"BUILDBEE_WORKER_SLOTS": "many"},
		{"BUILDBEE_WORKER_RUN_TIMEOUT": "5s"},
	} {
		if _, err := LoadWorker(env(bad)); err == nil {
			t.Fatalf("expected an error for %v", bad)
		}
	}
	if c, err := LoadWorker(env(nil)); err != nil || c.Slots != 4 || c.Agents != nil || c.RunTimeout != 2*time.Hour {
		t.Fatalf("defaults: %+v %v", c, err)
	}
}
