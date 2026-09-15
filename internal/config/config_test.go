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

func TestLoadWorker(t *testing.T) {
	c, err := LoadWorker(env(map[string]string{"BUILDBEE_URL": "http://buildbee.lan:8080/", "BUILDBEE_FAKE_SANDBOX": "1"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerURL != "http://buildbee.lan:8080" || !c.FakeSandbox || c.Addr != "127.0.0.1:8090" {
		t.Fatalf("worker: %+v", c)
	}
	if _, err := LoadWorker(env(map[string]string{"BUILDBEE_URL": "buildbee.lan"})); err == nil {
		t.Fatal("expected error for URL without scheme")
	}
}
