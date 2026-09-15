package worker

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/core"
	"github.com/codemodify/buildbee/internal/httpapi"
	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/codemodify/buildbee/internal/testdb"
	"github.com/codemodify/buildbee/internal/worker/acp"
	"github.com/codemodify/buildbee/internal/ws"
)

func TestMain(m *testing.M) { testdb.Main(m) }

// stack is a Server on a fresh database with one Project.
type stack struct {
	t       *testing.T
	ctx     context.Context
	svc     *core.Service
	url     string
	ada     core.Actor
	project string
}

func newStack(t *testing.T) *stack {
	var svc *core.Service
	hub := ws.NewHub(func(ctx context.Context, topic string, after int64) ([]core.Event, error) {
		return svc.Replay(ctx, topic, after)
	}, nil)
	svc = core.New(store.New(testdb.New(t)), hub, core.Options{})
	srv := httptest.NewServer(httpapi.NewServer(svc, hub, httpapi.Options{}).Handler())
	t.Cleanup(srv.Close)
	t.Cleanup(hub.Close)
	ctx := context.Background()
	p, err := svc.Hello(ctx, "Ada")
	if err != nil {
		t.Fatal(err)
	}
	ada := core.Actor{PersonID: p.ID, Name: p.Name}
	proj, err := svc.CreateProject(ctx, ada, "Work", false)
	if err != nil {
		t.Fatal(err)
	}
	return &stack{t: t, ctx: ctx, svc: svc, url: srv.URL, ada: ada, project: proj.ID}
}

func (s *stack) queue(title string) *models.Run {
	s.t.Helper()
	tc, err := s.svc.CreateTask(s.ctx, s.ada, s.project, core.NewTask{Title: title, HandoffRole: "none"})
	if err != nil {
		s.t.Fatal(err)
	}
	r, err := s.svc.CreateRun(s.ctx, s.ada, tc.ID, core.NewRun{Agent: "fake"})
	if err != nil {
		s.t.Fatal(err)
	}
	return r
}

// start runs a worker until the test ends (or stop is called).
func (s *stack) start(cfg Config) (stop func()) {
	s.t.Helper()
	cfg.Server, cfg.Agents = s.url, []string{"fake"}
	if cfg.Name == "" {
		cfg.Name = "w1"
	}
	if cfg.Slots == 0 {
		cfg.Slots = 1
	}
	w, err := New(cfg)
	if err != nil {
		s.t.Fatal(err)
	}
	w.heartbeat = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	var once sync.Once
	stop = func() { once.Do(func() { cancel(); <-done }) }
	s.t.Cleanup(stop)
	return stop
}

// wait polls until the Run satisfies ok.
func (s *stack) wait(id string, ok func(*models.Run) bool) *models.Run {
	s.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r, err := s.svc.Run(s.ctx, id)
		if err != nil {
			s.t.Fatal(err)
		}
		if ok(r) {
			return r
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("run never got there: %+v", r)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func finished(r *models.Run) bool { return r.Status.Terminal() }

// blockingExec runs until its context ends, reporting when it starts.
func blockingExec(started chan<- string) Exec {
	return func(ctx context.Context, _, _, prompt string, emit acp.Handler) (string, error) {
		_ = emit(acp.Event{Kind: "token", Payload: map[string]any{"text": "working"}})
		started <- prompt
		<-ctx.Done()
		return "partial transcript", ctx.Err()
	}
}

func TestNewRefusesRealAgentsUnlessAllowed(t *testing.T) {
	if _, err := New(Config{Name: "w", Slots: 1, Agents: []string{"claude"}}); !errors.Is(err, ErrHostAgentsDisabled) {
		t.Fatalf("got %v", err)
	}
	if _, err := New(Config{Name: "w", Slots: 1, Agents: []string{"claude"}, AllowHostAgents: true}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Config{{Slots: 1, Agents: []string{"fake"}}, {Name: "w", Agents: []string{"fake"}}, {Name: "w", Slots: 1}, {Name: "w", Slots: 1, Agents: []string{"hal"}}} {
		if _, err := New(bad); err == nil {
			t.Fatalf("expected an error for %+v", bad)
		}
	}
}

func TestWorkerRunsTheFakeAgent(t *testing.T) {
	s := newStack(t)
	r := s.queue("Ship it")
	s.start(Config{})
	got := s.wait(r.ID, finished)
	if got.Status != models.RunSucceeded || got.Worker != "w1" {
		t.Fatalf("run: %+v", got)
	}
	arts, err := s.svc.Artifacts(s.ctx, r.TaskID)
	if err != nil || len(arts) != 1 || arts[0].Name != "fake.log" || arts[0].RunID != r.ID {
		t.Fatalf("transcript: %+v %v", arts, err)
	}
}

func TestAgentFailureFailsTheRun(t *testing.T) {
	s := newStack(t)
	r := s.queue("Break it")
	s.start(Config{Exec: func(context.Context, string, string, string, acp.Handler) (string, error) {
		return "", errors.New("agent crashed: exit 2")
	}})
	got := s.wait(r.ID, finished)
	if got.Status != models.RunFailed || !strings.Contains(got.Detail, "exit 2") {
		t.Fatalf("run: %+v", got)
	}
}

func TestCancelStopsTheAgent(t *testing.T) {
	s := newStack(t)
	r := s.queue("Cancel me")
	started := make(chan string, 1)
	s.start(Config{Exec: blockingExec(started)})
	select {
	case prompt := <-started:
		if !strings.Contains(prompt, "Task: Cancel me") {
			t.Fatalf("prompt: %q", prompt)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("agent never started")
	}
	if _, err := s.svc.UpdateRun(s.ctx, s.ada, r.ID, "canceled", "changed my mind"); err != nil {
		t.Fatal(err)
	}
	// The worker notices on its next heartbeat, stops the agent and keeps
	// the transcript; the Run stays canceled.
	deadline := time.Now().Add(10 * time.Second)
	for {
		arts, _ := s.svc.Artifacts(s.ctx, r.TaskID)
		if len(arts) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stopped agent's transcript was never uploaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, _ := s.svc.Run(s.ctx, r.ID)
	if got.Status != models.RunCanceled || got.Detail != "changed my mind" {
		t.Fatalf("run: %+v", got)
	}
}

func TestStoppingTheWorkerFailsItsRuns(t *testing.T) {
	s := newStack(t)
	r := s.queue("Long job")
	started := make(chan string, 1)
	stop := s.start(Config{Name: "laptop", Exec: blockingExec(started)})
	<-started
	stop()
	got := s.wait(r.ID, finished)
	if got.Status != models.RunFailed || !strings.Contains(got.Detail, "worker laptop stopped") {
		t.Fatalf("run: %+v", got)
	}
}

func TestSlotsBoundParallelRuns(t *testing.T) {
	s := newStack(t)
	const slots, runs = 3, 7
	var running, peak atomic.Int32
	release := make(chan struct{})
	var ids []string
	for i := range runs {
		ids = append(ids, s.queue("job "+string(rune('a'+i))).ID)
	}
	s.start(Config{Slots: slots, Exec: func(ctx context.Context, _, _, _ string, _ acp.Handler) (string, error) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		defer running.Add(-1)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "ok", nil
	}})
	deadline := time.Now().Add(10 * time.Second)
	for peak.Load() < slots && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	for _, id := range ids {
		if got := s.wait(id, finished); got.Status != models.RunSucceeded {
			t.Fatalf("run: %+v", got)
		}
	}
	if p := peak.Load(); p != slots {
		t.Fatalf("peak parallel runs %d, want %d", p, slots)
	}
}
