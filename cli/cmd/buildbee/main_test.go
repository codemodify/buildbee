package main

import "testing"

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must be set")
	}
}

func TestRunVersion(t *testing.T) {
	if err := run([]string{"version"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunUnknown(t *testing.T) {
	if err := run([]string{"not-a-command"}); err == nil {
		t.Fatal("expected error")
	}
}
