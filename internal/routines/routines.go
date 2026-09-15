// Package routines fires Project Routines (digest stub) and checks due schedules.
package routines

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
)

func ParseInterval(schedule string) time.Duration {
	s := strings.TrimSpace(strings.ToLower(schedule))
	switch s {
	case "", "daily":
		return 24 * time.Hour
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 24 * time.Hour
	}
	return d
}

func Due(r models.Routine, now time.Time) bool {
	if !r.Enabled {
		return false
	}
	if r.LastRunAt == nil {
		return true
	}
	return now.Sub(*r.LastRunAt) >= ParseInterval(r.Schedule)
}

// Fire posts a Channel digest message, creates a Task, and records Activity.
func Fire(ctx context.Context, st store.Store, id string) (*models.Routine, error) {
	r, err := st.GetRoutine(ctx, id)
	if err != nil {
		return nil, err
	}
	channels, err := st.ListChannels(ctx, r.ProjectID)
	if err != nil {
		return nil, err
	}
	members, err := st.ListMembers(ctx, r.ProjectID)
	if err != nil {
		return nil, err
	}
	botID := r.BotMemberID
	if botID == "" {
		if pulse := models.MemberByRole(members, models.RolePulse); pulse != nil {
			botID = pulse.ID
		} else {
			for _, m := range members {
				if m.Kind == "bot" {
					botID = m.ID
					break
				}
			}
		}
	}
	if len(channels) > 0 && botID != "" {
		_, _ = st.PostMessage(ctx, channels[0].ID, botID, "Routine "+r.Name+": morning digest (Pulse stub)")
	}
	_, _ = st.CreateTask(ctx, r.ProjectID, "Routine: "+r.Name, botID)
	_ = st.AppendActivity(ctx, r.ProjectID, models.TypeRoutine, map[string]any{"id": r.ID, "name": r.Name, "action": "fire"})
	now := time.Now().UTC()
	return st.UpdateRoutine(ctx, r.ID, nil, &now)
}

func StartWorker(ctx context.Context, st store.Store, every time.Duration) {
	if every <= 0 {
		every = 15 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			rs, err := st.ListEnabledRoutines(ctx)
			if err != nil {
				slog.Error("routines worker: list routines", "err", err)
				continue
			}
			for _, r := range rs {
				if !Due(r, now.UTC()) {
					continue
				}
				if _, err := Fire(ctx, st, r.ID); err != nil {
					slog.Error("routine fire failed", "routine", r.ID, "err", err)
				}
			}
		}
	}
}
