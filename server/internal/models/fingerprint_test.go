package models

import "testing"

func TestDecisionFingerprint(t *testing.T) {
	a := DecisionFingerprint("  Ship today?  ")
	b := DecisionFingerprint("SHIP TODAY!!!")
	if a == "" || a != b {
		t.Fatalf("normalized mismatch: %q vs %q", a, b)
	}
	if DecisionFingerprint("Ship tomorrow?") == a {
		t.Fatal("different questions must not collide")
	}
}

func TestNormalizeRunEventKind(t *testing.T) {
	if NormalizeRunEventKind(" TOKEN ") != RunEventToken {
		t.Fatal("token")
	}
	if NormalizeRunEventKind("nope") != "" {
		t.Fatal("unknown")
	}
}
