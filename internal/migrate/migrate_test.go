package migrate

import (
	"errors"
	"testing"
)

func TestCheck(t *testing.T) {
	migs := []migration{{name: "0001_baseline.sql", sum: "aaa"}, {name: "0002_next.sql", sum: "bbb"}}

	if err := check(migs, map[string]string{"0001_baseline.sql": "aaa"}); err != nil {
		t.Fatalf("pending migration should pass: %v", err)
	}
	if err := check(migs, map[string]string{"001_init.sql": ""}); !errors.Is(err, errRecreate) {
		t.Fatalf("pre-rebuild database: got %v", err)
	}
	if err := check(migs, map[string]string{"0001_baseline.sql": "zzz"}); !errors.Is(err, errRecreate) {
		t.Fatalf("edited migration: got %v", err)
	}
}

func TestLoadIsSorted(t *testing.T) {
	migs, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if len(migs) == 0 || migs[0].name != "0001_baseline.sql" || migs[0].sum == "" {
		t.Fatalf("migrations: %+v", migs)
	}
}
