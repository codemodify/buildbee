package config

import (
	"strings"
	"testing"
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

func TestLoadAgent(t *testing.T) {
	_, err := LoadAgent(env(map[string]string{}))
	if err == nil || !strings.Contains(err.Error(), "BUILDBEE_BOT") || !strings.Contains(err.Error(), "BUILDBEE_AI") {
		t.Fatalf("a Bot and an AI are required: %v", err)
	}
	c, err := LoadAgent(env(map[string]string{
		"BUILDBEE_URL": "http://buildbee.lan:8080/", "BUILDBEE_BOT": "bot-1", "BUILDBEE_AI": "Claude",
		"BUILDBEE_AGENT_HOST": "nc-laptop", "BUILDBEE_SLOTS": "2", "BUILDBEE_AI_CODEX": "npx codex-acp --stdio",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerURL != "http://buildbee.lan:8080" || c.BotID != "bot-1" || c.AI != "claude" || c.Slots != 2 {
		t.Fatalf("agent: %+v", c)
	}
	if c.Name != "bot-1@nc-laptop" {
		t.Fatalf("it names itself for people to recognise: %q", c.Name)
	}
	if got := strings.Join(c.AICommands["codex"], " "); got != "npx codex-acp --stdio" {
		t.Fatalf("AI command override: %q", got)
	}
	if c.Isolation != "container" || c.Sandbox != "auto" || !c.OpenPRs {
		t.Fatalf("defaults: %+v", c)
	}
	for _, bad := range []map[string]string{
		{"BUILDBEE_BOT": "b", "BUILDBEE_AI": "claude", "BUILDBEE_SLOTS": "0"},
		{"BUILDBEE_BOT": "b", "BUILDBEE_AI": "claude", "BUILDBEE_ISOLATION": "vm"},
		{"BUILDBEE_BOT": "b", "BUILDBEE_AI": "claude", "BUILDBEE_SANDBOX": "maybe"},
	} {
		if _, err := LoadAgent(env(bad)); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}
