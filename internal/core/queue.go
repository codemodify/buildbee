package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
)

// LeaseTTL is how long a claim lasts without a heartbeat. Workers heartbeat
// well inside it; a Run whose worker stops is failed by ReapRuns.
const LeaseTTL = 60 * time.Second

// signal wakes goroutines waiting for new work.
type signal struct {
	mu      sync.Mutex
	ch      chan struct{}
	stopped chan struct{}
	stop    sync.Once
}

func newSignal() *signal { return &signal{ch: make(chan struct{}), stopped: make(chan struct{})} }

func (s *signal) wait() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ch
}

func (s *signal) notify() {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(s.ch)
	s.ch = make(chan struct{})
}

// StopWaiting ends every pending and future ClaimRun wait, so a shutting
// down server does not hold workers' long-polls open.
func (s *Service) StopWaiting() { s.queue.stop.Do(func() { close(s.queue.stopped) }) }

// ClaimRun gives the worker the oldest queued Run for one of its agents,
// waiting up to wait for one to appear. It returns nil, nil when there is
// none.
func (s *Service) ClaimRun(ctx context.Context, a Actor, agents []string, wait time.Duration) (*models.Claim, error) {
	if a.Worker == "" {
		return nil, invalid("only a worker (X-BuildBee-Worker) can claim Runs")
	}
	var clean []string
	for _, ag := range agents {
		ag = strings.ToLower(strings.TrimSpace(ag))
		if ag == "" {
			continue
		}
		if !models.ValidAgent(ag) {
			return nil, invalid("unknown agent %q", ag)
		}
		clean = append(clean, ag)
	}
	if len(clean) == 0 {
		return nil, invalid("a worker must offer at least one agent")
	}
	deadline := time.Now().Add(wait)
	for {
		ready := s.queue.wait() // before trying, so a Run queued meanwhile still wakes us
		c, err := s.claimOnce(ctx, a, clean)
		if c != nil || err != nil {
			return c, err
		}
		left := time.Until(deadline)
		if left <= 0 {
			return nil, nil
		}
		timer := time.NewTimer(left)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil
		case <-s.queue.stopped:
			timer.Stop()
			return nil, nil
		case <-timer.C:
			return nil, nil
		case <-ready:
			timer.Stop()
		}
	}
}

func (s *Service) claimOnce(ctx context.Context, a Actor, agents []string) (*models.Claim, error) {
	var out *models.Claim
	err := s.tx(ctx, func(w *work) error {
		r, err := w.st.ClaimRun(ctx, a.Worker, agents, w.now, w.now.Add(LeaseTTL))
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		task, err := w.st.GetTask(ctx, r.TaskID, false)
		if err != nil {
			return err
		}
		proj, err := w.st.GetProject(ctx, r.ProjectID)
		if err != nil {
			return err
		}
		notes, err := w.st.ListHandoffs(ctx, r.TaskID)
		if err != nil {
			return err
		}
		c := &models.Claim{Run: *r, Task: *task, Project: *proj, Notes: notes}
		if r.BotMemberID != "" {
			if c.Bot, err = w.st.GetMember(ctx, r.BotMemberID); err != nil {
				return err
			}
		}
		if err := w.runEvent(ctx, r.ID, models.RunEventStatus,
			map[string]any{"status": r.Status, "detail": "claimed by worker " + a.Worker}); err != nil {
			return err
		}
		by := w.runActor(ctx, a, r)
		if err := w.activity(ctx, r.ProjectID, by, models.TypeRun, "claimed", r.ID,
			map[string]any{"task_id": r.TaskID, "worker": a.Worker, "attempt": r.Attempts}); err != nil {
			return err
		}
		out = c
		return nil
	})
	return out, err
}

// Heartbeat renews the worker's lease on a Run and returns the Run, so the
// worker sees a cancellation (status canceled) and stops.
func (s *Service) Heartbeat(ctx context.Context, a Actor, runID string) (*models.Run, error) {
	if a.Worker == "" {
		return nil, invalid("only a worker (X-BuildBee-Worker) can heartbeat Runs")
	}
	var out *models.Run
	err := s.tx(ctx, func(w *work) error {
		r, err := w.st.ExtendLease(ctx, runID, a.Worker, w.now.Add(LeaseTTL))
		if errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("%w: run is claimed by another worker", store.ErrConflict)
		}
		out = r
		return err
	})
	return out, err
}

// ReapRuns fails running Runs whose worker stopped heartbeating. Agent work
// is not safe to repeat blindly, so nothing is retried automatically.
func (s *Service) ReapRuns(ctx context.Context) (int, error) {
	n := 0
	err := s.tx(ctx, func(w *work) error {
		runs, err := w.st.ExpiredRuns(ctx, w.now, 100)
		if err != nil {
			return err
		}
		for i := range runs {
			r := &runs[i]
			r.Status = models.RunFailed
			r.Detail = "worker " + r.Worker + " stopped heartbeating"
			r.UpdatedAt, r.LeaseUntil = w.now, nil
			t := w.now
			r.FinishedAt = &t
			if err := w.st.UpdateRun(ctx, *r); err != nil {
				return err
			}
			if err := w.runEvent(ctx, r.ID, models.RunEventStatus, map[string]any{"status": r.Status, "detail": r.Detail}); err != nil {
				return err
			}
			if err := w.activity(ctx, r.ProjectID, who{name: "buildbee"}, models.TypeRun, string(models.RunFailed), r.ID,
				map[string]any{"task_id": r.TaskID, "detail": r.Detail}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// ownRun lets a worker write only to Runs it claimed. People may still
// change any Run (for example, cancel it).
func ownRun(a Actor, r *models.Run) error {
	switch {
	case a.Worker == "" || r.Worker == a.Worker:
		return nil
	case r.Worker == "":
		return fmt.Errorf("%w: claim the run before writing to it", store.ErrConflict)
	default:
		return fmt.Errorf("%w: run is claimed by worker %s", store.ErrConflict, r.Worker)
	}
}

// runPrompt is what the agent is asked to do: the Bot's standing
// instructions, the Task, and what people asked for when handing it over.
func runPrompt(bot *models.Member, task *models.Task, notes []models.Handoff) string {
	var b strings.Builder
	if bot != nil && bot.Instructions != "" {
		b.WriteString(bot.Instructions)
		b.WriteString("\n\n")
	}
	b.WriteString("Task: ")
	b.WriteString(task.Title)
	b.WriteString("\n")
	if task.Body != "" {
		b.WriteString("\n")
		b.WriteString(task.Body)
		b.WriteString("\n")
	}
	for _, h := range notes {
		if h.Note == "" || (bot != nil && h.ToMemberID != bot.ID) {
			continue
		}
		b.WriteString("\nHandoff note: ")
		b.WriteString(h.Note)
		b.WriteString("\n")
	}
	return b.String()
}
