package worker

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/blob"
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
	svc = core.New(store.New(testdb.New(t)), hub, core.Options{Blobs: blob.Dir{Root: t.TempDir()}})
	srv := httptest.NewServer(httpapi.NewServer(svc, hub, httpapi.Options{}).Handler())
	t.Cleanup(srv.Close)
	t.Cleanup(hub.Close)
	ctx := context.Background()
	p, err := svc.Hello(ctx, "Ada")
	if err != nil {
		t.Fatal(err)
	}
	ada := core.Actor{PersonID: p.ID, Name: p.Name}
	proj, err := svc.CreateProject(ctx, ada, core.NewProject{Name: "Work", DefaultBots: true})
	if err != nil {
		t.Fatal(err)
	}
	return &stack{t: t, ctx: ctx, svc: svc, url: srv.URL, ada: ada, project: proj.ID}
}

func (s *stack) queue(title string) *models.Run {
	s.t.Helper()
	return s.queueFor(title, "fake")
}

func (s *stack) queueFor(title, agent string) *models.Run {
	s.t.Helper()
	tc, err := s.svc.CreateTask(s.ctx, s.ada, s.project, core.NewTask{Title: title, HandoffRole: "none"})
	if err != nil {
		s.t.Fatal(err)
	}
	r, err := s.svc.CreateRun(s.ctx, s.ada, tc.ID, core.NewRun{Agent: agent})
	if err != nil {
		s.t.Fatal(err)
	}
	return r
}

// start runs a worker until the test ends (or stop is called).
func (s *stack) start(cfg Config) (stop func()) {
	s.t.Helper()
	cfg.Server = s.url
	if cfg.Agents == nil {
		cfg.Agents = []string{"fake"}
	}
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
	return func(ctx context.Context, job Job, emit acp.Handler) (string, error) {
		_ = emit(acp.Event{Kind: "token", Payload: map[string]any{"text": "working"}})
		started <- job.Prompt
		<-ctx.Done()
		return "partial transcript", ctx.Err()
	}
}

func TestNewChecksIsolationAndAgents(t *testing.T) {
	claude := []string{"claude"}
	if _, err := New(Config{Name: "w", Slots: 1, Agents: claude}); err == nil || !strings.Contains(err.Error(), "needs an image") {
		t.Fatalf("container isolation without an image: %v", err)
	}
	if _, err := New(Config{Name: "w", Slots: 1, Agents: claude, Container: Container{Image: "agents"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Name: "w", Slots: 1, Agents: []string{"fake"}}); err != nil {
		t.Fatalf("the fake agent needs no container: %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := New(Config{Name: "w", Slots: 1, Agents: claude, Isolation: IsolationHost}); !errors.Is(err, acp.ErrNoAgent) {
		t.Fatalf("a host agent whose ACP command is missing: %v", err)
	}
	if _, err := New(Config{Name: "w", Slots: 1, Agents: claude, Isolation: IsolationHost,
		Commands: map[string][]string{"claude": {"/bin/sh"}}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Config{
		{Slots: 1, Agents: []string{"fake"}},
		{Name: "w", Agents: []string{"fake"}},
		{Name: "w", Slots: 1},
		{Name: "w", Slots: 1, Agents: []string{"hal"}},
		{Name: "w", Slots: 1, Agents: []string{"fake"}, Isolation: "vm"},
	} {
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
	s.start(Config{Exec: func(context.Context, Job, acp.Handler) (string, error) {
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
	s.start(Config{Slots: slots, Exec: func(ctx context.Context, _ Job, _ acp.Handler) (string, error) {
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

func TestRunTimeoutFailsTheRun(t *testing.T) {
	s := newStack(t)
	r := s.queue("Slow job")
	s.start(Config{RunTimeout: 100 * time.Millisecond, Exec: blockingExec(make(chan string, 1))})
	got := s.wait(r.ID, finished)
	if got.Status != models.RunFailed || !strings.Contains(got.Detail, "time limit of 100ms") {
		t.Fatalf("run: %+v", got)
	}
}

func TestFinalReply(t *testing.T) {
	for in, want := range map[string]string{
		"Plan: do it.":       "Plan: do it.",
		"[tool] Read\nDone.": "Done.",
		"Thinking.\n[tool] Read\nx\n[tool] Edit\n\nAll set.\n": "All set.",
		"[tool] Edit": "",
	} {
		if got := finalReply(in); got != want {
			t.Errorf("finalReply(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSteeringReachesTheAgent(t *testing.T) {
	prev := acp.StreamStep
	acp.StreamStep = 30 * time.Millisecond
	t.Cleanup(func() { acp.StreamStep = prev })
	s := newStack(t)

	// A message to a queued Run goes into its prompt.
	early := s.queue("Early word")
	if _, err := s.svc.SteerRun(s.ctx, s.ada, early.ID, "Prefer small commits.", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.svc.SteerRun(s.ctx, core.WorkerActor("w9"), early.ID, "hi", false); err == nil {
		t.Fatal("only people steer")
	}
	s.start(Config{})
	got := s.wait(early.ID, finished)
	if !strings.Contains(got.Prompt, "Message from Ada: Prefer small commits.") {
		t.Fatalf("prompt: %q", got.Prompt)
	}
	if transcript := s.transcript(got); strings.Count(transcript, "Done.") != 1 {
		t.Fatalf("a message already in the prompt is not sent again: %q", transcript)
	}

	// A message to a running Run reaches the agent as its next turn.
	live := s.queue("Live word")
	deadline := time.Now().Add(10 * time.Second)
	for {
		evs, _, _ := s.svc.RunEvents(s.ctx, live.ID, 0, 0)
		if slices.ContainsFunc(evs, func(e models.RunEvent) bool { return e.Kind == "token" }) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the agent never replied")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := s.svc.SteerRun(s.ctx, s.ada, live.ID, "Also update the README.", false); err != nil {
		t.Fatal(err)
	}
	got = s.wait(live.ID, finished)
	transcript := s.transcript(got)
	if got.Status != models.RunSucceeded || !strings.Contains(transcript, "[Ada] Also update the README.") || strings.Count(transcript, "Done.") != 2 {
		t.Fatalf("run %s, transcript %q", got.Status, transcript)
	}
	if _, err := s.svc.SteerRun(s.ctx, s.ada, live.ID, "too late", false); err == nil {
		t.Fatal("a finished Run takes no messages")
	}
}

// transcript returns the Run's transcript Artifact.
func (s *stack) transcript(r *models.Run) string {
	s.t.Helper()
	arts, err := s.svc.Artifacts(s.ctx, r.TaskID)
	if err != nil {
		s.t.Fatal(err)
	}
	for _, a := range arts {
		if a.RunID == r.ID && a.Kind == "log" {
			full, err := s.svc.Artifact(s.ctx, a.ID)
			if err != nil {
				s.t.Fatal(err)
			}
			return full.Body
		}
	}
	s.t.Fatalf("run %s has no transcript", r.ID)
	return ""
}

func TestALongFailureIsStillReported(t *testing.T) {
	s := newStack(t)
	r := s.queue("Noisy crash")
	long := strings.Repeat("stderr line that goes on and on\n", 200) // ~6 KB
	s.start(Config{Exec: func(context.Context, Job, acp.Handler) (string, error) {
		return "", errors.New("agent crashed: " + long)
	}})
	got := s.wait(r.ID, finished)
	if got.Status != models.RunFailed || !strings.HasPrefix(got.Detail, "agent crashed: stderr line") {
		t.Fatalf("the failure is reported, truncated, not lost to the reaper: %+v", got)
	}
}

func TestAgentsAskPeopleWhenTheProjectSaysSo(t *testing.T) {
	s := newStack(t)
	ask := models.PermissionsAsk
	if _, err := s.svc.UpdateProject(s.ctx, s.ada, s.project, core.ProjectPatch{AgentPermissions: &ask}); err != nil {
		t.Fatal(err)
	}
	answerPoll = 10 * time.Millisecond
	t.Cleanup(func() { answerPoll = 2 * time.Second })
	s.start(Config{Dir: t.TempDir()})
	open := func() []models.Decision {
		ds, err := s.svc.Decisions(s.ctx, s.ada, s.project, core.DecisionFilter{Open: true})
		if err != nil {
			t.Fatal(err)
		}
		return ds
	}
	waitOpen := func() models.Decision {
		deadline := time.Now().Add(10 * time.Second)
		for len(open()) == 0 {
			if time.Now().After(deadline) {
				t.Fatal("no permission question")
			}
			time.Sleep(10 * time.Millisecond)
		}
		return open()[0]
	}

	// Answered: the agent goes ahead.
	r := s.queue("asks first")
	d := waitOpen()
	if d.Action != "permission" || d.RunID != r.ID || d.Prompt != "The agent asks to: Read the Task" ||
		!slices.Equal(d.Options, []string{"Reject", "Allow"}) || d.Recommendation != "Allow" {
		t.Fatalf("decision: %+v", d)
	}
	if got := s.wait(r.ID, func(*models.Run) bool { return true }); got.Status != models.RunRunning {
		t.Fatalf("the run waits for the answer: %s", got.Status)
	}
	if _, err := s.svc.AnswerDecision(s.ctx, s.ada, d.ID, "Allow"); err != nil {
		t.Fatal(err)
	}
	s.wait(r.ID, func(r *models.Run) bool { return r.Status == models.RunSucceeded })

	// Unanswered, then canceled: the question closes with the Run.
	r2 := s.queue("never answered")
	waitOpen()
	if _, err := s.svc.UpdateRun(s.ctx, s.ada, r2.ID, string(models.RunCanceled), "stop"); err != nil {
		t.Fatal(err)
	}
	s.wait(r2.ID, func(r *models.Run) bool { return r.Status == models.RunCanceled })
	if left := open(); len(left) != 0 {
		t.Fatalf("questions outlive their run: %+v", left)
	}
	if _, err := s.svc.AskPermission(s.ctx, core.WorkerActor("w1"), r2.ID, core.PermissionAsk{Title: "x", Options: []core.PermissionChoice{{ID: "y", Name: "Allow"}}}); err == nil {
		t.Fatal("a finished run asks nothing")
	}
	if _, err := s.svc.AskPermission(s.ctx, core.WorkerActor("other"), r.ID, core.PermissionAsk{Title: "x", Options: []core.PermissionChoice{{ID: "y"}}}); err == nil {
		t.Fatal("only the run's worker asks")
	}
}

func TestAgentsGetWhatPeopleAttached(t *testing.T) {
	s := newStack(t)
	png := []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("p", 40))
	// Ada asks in #tasks with a screenshot and a note.
	proj, err := s.svc.Project(s.ctx, s.project)
	if err != nil {
		t.Fatal(err)
	}
	tasks := proj.Channels[0].ID
	shot, err := s.svc.UploadFile(s.ctx, s.ada, tasks, "shot.png", int64(len(png)), bytes.NewReader(png))
	if err != nil {
		t.Fatal(err)
	}
	notes, err := s.svc.UploadFile(s.ctx, s.ada, tasks, "notes.md", 11, strings.NewReader("# what I want"[:11]))
	if err != nil {
		t.Fatal(err)
	}
	posted, err := s.svc.PostMessage(s.ctx, s.ada, tasks, "@Builder make it look like this", shot.ID, notes.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(posted.Tasks) != 1 {
		t.Fatalf("the mention opens a Task: %+v", posted)
	}
	task := posted.Tasks[0]
	r, err := s.svc.CreateRun(s.ctx, s.ada, task.ID, core.NewRun{Agent: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	s.start(Config{Dir: t.TempDir()})
	s.wait(r.ID, func(r *models.Run) bool { return r.Status == models.RunSucceeded })

	arts, err := s.svc.Artifacts(s.ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var log string
	for _, a := range arts {
		if a.Kind == "log" {
			full, err := s.svc.Artifact(s.ctx, a.ID)
			if err != nil {
				t.Fatal(err)
			}
			log = full.Body
		}
	}
	if !strings.Contains(log, "[fake agent received 1 image(s) and 2 file link(s): shot.png notes.md]") {
		t.Fatalf("the agent got the files:\n%s", log)
	}
	if !strings.Contains(log, "- shot.png (image/png) at ") || !strings.Contains(log, "- notes.md (text/plain") {
		t.Fatalf("the prompt says what and where:\n%s", log)
	}
}
