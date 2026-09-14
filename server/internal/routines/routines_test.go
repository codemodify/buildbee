package routines

import (
	"testing"
	"time"

	"github.com/codemodify/buildbee/server/internal/models"
)

func TestDue(t *testing.T) {
	r := models.Routine{Enabled: true, Schedule: "1h"}
	if !Due(r, time.Now()) {
		t.Fatal("never-run should be due")
	}
	past := time.Now().Add(-2 * time.Hour)
	r.LastRunAt = &past
	if !Due(r, time.Now()) {
		t.Fatal("expected due")
	}
	recent := time.Now().Add(-10 * time.Minute)
	r.LastRunAt = &recent
	if Due(r, time.Now()) {
		t.Fatal("not due yet")
	}
	r.Enabled = false
	if Due(r, time.Now()) {
		t.Fatal("disabled")
	}
}

func TestParseInterval(t *testing.T) {
	if ParseInterval("daily") != 24*time.Hour {
		t.Fatal("daily")
	}
	if ParseInterval("30m") != 30*time.Minute {
		t.Fatal("30m")
	}
}
