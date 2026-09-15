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
	if c.Addr != ":8080" || c.MaxBodyBytes != 32<<20 || c.WebDir != "" || c.LocalWorker != LocalAuto {
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
		"bad local worker": {map[string]string{"DATABASE_URL": "postgres://x/y", "BUILDBEE_LOCAL_WORKER": "yes"}, "auto, on or off"},
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
		!slices.Equal(c.Agents, []string{"claude", "codex"}) || c.Isolation != "container" || c.Image != "buildbee-agents" || c.RunTimeout != 45*time.Minute ||
		!slices.Equal(c.AgentCommands["claude"], []string{"npx", "-y", "@agentclientprotocol/claude-agent-acp"}) {
		t.Fatalf("worker: %+v", c)
	}
	for _, bad := range []map[string]string{
		{"BUILDBEE_URL": "buildbee.lan"},
		{"BUILDBEE_WORKER_SLOTS": "0"},
		{"BUILDBEE_WORKER_SLOTS": "many"},
		{"BUILDBEE_WORKER_RUN_TIMEOUT": "5s"},
		{"BUILDBEE_WORKER_ISOLATION": "vm"},
		{"BUILDBEE_WORKER_ALLOW_HOST_AGENTS": "1"},
	} {
		if _, err := LoadWorker(env(bad)); err == nil {
			t.Fatalf("expected an error for %v", bad)
		}
	}
	if c, err := LoadWorker(env(nil)); err != nil || c.Slots != 4 || c.Agents != nil || c.RunTimeout != 2*time.Hour {
		t.Fatalf("defaults: %+v %v", c, err)
	}
}

func TestLocalWorkerTalksOverLoopback(t *testing.T) {
	for addr, want := range map[string]string{
		":8080":          "http://127.0.0.1:8080",
		"0.0.0.0:9000":   "http://127.0.0.1:9000",
		"10.0.0.5:8080":  "http://10.0.0.5:8080",
		"[::]:8080":      "http://127.0.0.1:8080",
		"127.0.0.1:8084": "http://127.0.0.1:8084",
	} {
		c, err := LocalWorker(env(map[string]string{"BUILDBEE_URL": "http://elsewhere:1"}), addr)
		if err != nil {
			t.Fatal(err)
		}
		if c.ServerURL != want {
			t.Errorf("%s: %s, want %s", addr, c.ServerURL, want)
		}
		if !strings.HasSuffix(c.Name, "-server") || !strings.Contains(c.Dir, "buildbee-server-worker") || c.Isolation != "container" {
			t.Errorf("%s: %+v", addr, c)
		}
	}
	c, err := LocalWorker(env(map[string]string{"BUILDBEE_WORKER_NAME": "box", "BUILDBEE_WORKER_DIR": "/w"}), ":1")
	if err != nil || c.Name != "box" || c.Dir != "/w" {
		t.Fatalf("overrides: %+v %v", c, err)
	}
}
