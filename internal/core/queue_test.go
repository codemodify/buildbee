package core

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
)

func (f *fixture) queue(a Actor, projectID, title string, in NewRun) *models.Run {
	f.t.Helper()
	tc, err := f.s.CreateTask(f.ctx, a, projectID, NewTask{Title: title, HandoffRole: "none"})
	f.must(err)
	r, err := f.s.CreateRun(f.ctx, a, tc.ID, in)
	f.must(err)
	return r
}

func TestClaimMatchesAgents(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Q")
	anyAgent := f.queue(ada, p.ID, "any", NewRun{})
	claude := f.queue(ada, p.ID, "claude", NewRun{Agent: "claude"})
	fake := f.queue(ada, p.ID, "fake", NewRun{Agent: "fake"})

	fakeWorker := WorkerActor("fake-only")
	c, err := f.s.ClaimRun(f.ctx, fakeWorker, []string{"fake"}, 0)
	f.must(err)
	if c == nil || c.Run.ID != fake.ID {
		t.Fatalf("the fake agent takes only Runs asking for it, got %+v", c)
	}
	if c, _ := f.s.ClaimRun(f.ctx, fakeWorker, []string{"fake"}, 0); c != nil {
		t.Fatalf("nothing else is for a fake-only worker: %+v", c.Run)
	}
	codex := WorkerActor("codex-box")
	c, err = f.s.ClaimRun(f.ctx, codex, []string{"codex"}, 0)
	f.must(err)
	if c == nil || c.Run.ID != anyAgent.ID {
		t.Fatalf("a Run for any agent goes to the first real worker: %+v", c)
	}
	if c, _ := f.s.ClaimRun(f.ctx, codex, []string{"codex"}, 0); c != nil {
		t.Fatalf("a claude Run is not for codex: %+v", c.Run)
	}
	c, err = f.s.ClaimRun(f.ctx, WorkerActor("claude-box"), []string{"claude"}, 0)
	f.must(err)
	if c == nil || c.Run.ID != claude.ID {
		t.Fatalf("claude: %+v", c)
	}

	if _, err := f.s.ClaimRun(f.ctx, ada, []string{"fake"}, 0); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("people cannot claim: %v", err)
	}
	if _, err := f.s.ClaimRun(f.ctx, codex, []string{"hal9000"}, 0); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown agent: %v", err)
	}
	if _, err := f.s.CreateRun(f.ctx, ada, anyAgent.TaskID, NewRun{Agent: "hal9000"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("runs need a known agent: %v", err)
	}
}

func TestRunsUseTheBotsAgentAndPrompt(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Q")
	builder := bot(p, models.RoleBuilder)
	instr := "Write tests first."
	agent := "codex"
	_, err := f.s.UpdateMember(f.ctx, ada, builder.ID, MemberPatch{Agent: &agent, Instructions: &instr})
	f.must(err)
	tc, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "Add caching", Body: "GET /v1/projects", HandoffRole: "none"})
	f.must(err)
	h, err := f.s.CreateHandoff(f.ctx, ada, tc.ID, NewHandoff{ToRole: "builder", Note: "keep it small", AutoRun: true})
	f.must(err)
	r := h.Run
	for _, want := range []string{instr, "Task: Add caching", "GET /v1/projects", "Handoff note: keep it small"} {
		if !strings.Contains(r.Prompt, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, r.Prompt)
		}
	}
	if r.Agent != "codex" {
		t.Fatalf("run uses the Bot's agent: %+v", r)
	}
	human := f.memberOf(p.ID, ada)
	if _, err := f.s.UpdateMember(f.ctx, ada, human.ID, MemberPatch{Agent: &agent}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("people have no agent: %v", err)
	}
	bad := "skynet"
	if _, err := f.s.UpdateMember(f.ctx, ada, builder.ID, MemberPatch{Agent: &bad}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown agent: %v", err)
	}
}

func TestClaimLongPollWakesOnNewRun(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Q")
	got := make(chan *models.Claim, 1)
	go func() {
		c, err := f.s.ClaimRun(f.ctx, WorkerActor("w1"), []string{"fake"}, 10*time.Second)
		if err != nil {
			t.Error(err)
		}
		got <- c
	}()
	time.Sleep(50 * time.Millisecond) // let the claim start waiting
	start := time.Now()
	r := f.queue(ada, p.ID, "wake", NewRun{Agent: "fake"})
	select {
	case c := <-got:
		if c == nil || c.Run.ID != r.ID {
			t.Fatalf("claim: %+v", c)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("woke after %v; queuing must wake waiting workers", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiting claim never woke")
	}

	// An empty queue answers nil after the wait, and StopWaiting ends it early.
	done := make(chan *models.Claim, 1)
	go func() {
		c, _ := f.s.ClaimRun(f.ctx, WorkerActor("w1"), []string{"fake"}, time.Minute)
		done <- c
	}()
	time.Sleep(50 * time.Millisecond)
	f.s.StopWaiting()
	select {
	case c := <-done:
		if c != nil {
			t.Fatalf("nothing queued, got %+v", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StopWaiting did not release the claim")
	}
}

func TestConcurrentClaimsTakeEachRunOnce(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Q")
	const runs, workers = 30, 8
	for i := range runs {
		f.queue(ada, p.ID, "t"+string(rune('a'+i%26)), NewRun{Agent: "fake"})
	}
	var mu sync.Mutex
	seen := map[string]string{}
	var wg sync.WaitGroup
	for w := range workers {
		name := "w" + string(rune('0'+w))
		wg.Go(func() {
			for {
				c, err := f.s.ClaimRun(f.ctx, WorkerActor(name), []string{"fake"}, 0)
				if err != nil {
					t.Error(err)
					return
				}
				if c == nil {
					return
				}
				mu.Lock()
				if prev, dup := seen[c.Run.ID]; dup {
					t.Errorf("run %s claimed by %s and %s", c.Run.ID, prev, name)
				}
				seen[c.Run.ID] = name
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(seen) != runs {
		t.Fatalf("claimed %d of %d runs", len(seen), runs)
	}
}

func TestHeartbeatSeesCancelAndReaperFailsSilentWorkers(t *testing.T) {
	f := newFixture(t)
	clock := time.Now().UTC()
	var mu sync.Mutex
	f.s.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	advance := func(d time.Duration) { mu.Lock(); clock = clock.Add(d); mu.Unlock() }

	ada := f.person("Ada")
	p := f.project(ada, "Q")
	w1 := WorkerActor("w1")
	canceled := f.queue(ada, p.ID, "cancel me", NewRun{Agent: "fake"})
	silent := f.queue(ada, p.ID, "silent", NewRun{Agent: "fake"})
	for range 2 {
		c, err := f.s.ClaimRun(f.ctx, w1, []string{"fake"}, 0)
		f.must(err)
		if c == nil {
			t.Fatal("expected a claim")
		}
	}

	// A person cancels; the worker learns it from its next heartbeat.
	_, err := f.s.UpdateRun(f.ctx, ada, canceled.ID, "canceled", "not needed")
	f.must(err)
	r, err := f.s.Heartbeat(f.ctx, w1, canceled.ID)
	f.must(err)
	if r.Status != models.RunCanceled {
		t.Fatalf("heartbeat must report the cancel: %+v", r)
	}
	if _, err := f.s.Heartbeat(f.ctx, WorkerActor("w2"), silent.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("only the owner heartbeats: %v", err)
	}

	// Heartbeats keep a Run alive past the lease; silence fails it.
	advance(LeaseTTL - time.Second)
	_, err = f.s.Heartbeat(f.ctx, w1, silent.ID)
	f.must(err)
	advance(LeaseTTL - time.Second)
	n, err := f.s.ReapRuns(f.ctx)
	f.must(err)
	if n != 0 {
		t.Fatalf("a heartbeating Run was reaped")
	}
	advance(2 * time.Second)
	n, err = f.s.ReapRuns(f.ctx)
	f.must(err)
	if n != 1 {
		t.Fatalf("reaped %d, want the silent Run", n)
	}
	got, err := f.s.Run(f.ctx, silent.ID)
	f.must(err)
	if got.Status != models.RunFailed || !strings.Contains(got.Detail, "w1 stopped heartbeating") || got.LeaseUntil != nil {
		t.Fatalf("reaped run: %+v", got)
	}
	if _, err := f.s.UpdateRun(f.ctx, w1, silent.ID, "succeeded", "late"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a reaped Run stays failed: %v", err)
	}
}

func TestRepoURLValidation(t *testing.T) {
	for u, ok := range map[string]bool{
		"https://github.com/acme/app.git": true,
		"git@github.com:acme/app.git":     true,
		"ssh://git@host:2222/acme/app":    true,
		"/srv/git/app.git":                true,
		"file:///srv/git/app.git":         true,
		"-oProxyCommand=touch@x:y":        false,
		"ext::sh -c touch% /tmp/pwned":    false,
		"fd::17":                          false,
		"git@host:app;rm":                 false,
		"https://":                        false,
		"app":                             false,
	} {
		if validRepoURL(u) != ok {
			t.Errorf("validRepoURL(%q) = %v, want %v", u, !ok, ok)
		}
	}
}

func TestClaimsShareWorkersAcrossProjects(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	busy, quiet := f.project(ada, "Busy"), f.project(ada, "Quiet")
	one := 1
	_, err := f.s.UpdateProject(f.ctx, ada, busy.ID, ProjectPatch{MaxRuns: &one})
	f.must(err)
	for i := range 3 {
		f.queue(ada, busy.ID, "busy "+string(rune('a'+i)), NewRun{Agent: "fake"})
	}
	late := f.queue(ada, quiet.ID, "quiet", NewRun{Agent: "fake"})

	w := WorkerActor("w1")
	first, err := f.s.ClaimRun(f.ctx, w, []string{"fake"}, 0)
	f.must(err)
	if first == nil || first.Run.ProjectID != busy.ID {
		t.Fatalf("the oldest Run goes first: %+v", first)
	}
	second, err := f.s.ClaimRun(f.ctx, w, []string{"fake"}, 0)
	f.must(err)
	if second == nil || second.Run.ID != late.ID {
		t.Fatalf("a Project at its max_runs waits, so the other Project's Run goes next: %+v", second)
	}
	if c, _ := f.s.ClaimRun(f.ctx, w, []string{"fake"}, 0); c != nil {
		t.Fatalf("Busy is at max_runs=1: %+v", c.Run)
	}
	_, err = f.s.ReportRun(f.ctx, w, first.Run.ID, RunReport{Status: "succeeded"})
	f.must(err)
	if c, _ := f.s.ClaimRun(f.ctx, w, []string{"fake"}, 0); c == nil || c.Run.ProjectID != busy.ID {
		t.Fatalf("a finished Run frees Busy's slot: %+v", c)
	}
	bad := -1
	if _, err := f.s.UpdateProject(f.ctx, ada, busy.ID, ProjectPatch{MaxRuns: &bad}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("negative max_runs: %v", err)
	}
}

func TestUsageIsTrackedPerRunAndSummed(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Spend")
	w := WorkerActor("w1")
	for i, reports := range [][]map[string]any{
		{{"used": 5000.0, "size": 200000.0, "cost": map[string]any{"amount": 0.10, "currency": "usd"}},
			{"used": 30000.0, "size": 200000.0, "cost": map[string]any{"amount": 0.42, "currency": "usd"}},
			{"used": 12000.0, "size": 200000.0}}, // context shrank after compaction; no cost this time
		{{"used": 1000.0, "cost": map[string]any{"amount": 0.08, "currency": "USD"}}},
	} {
		r := f.queue(ada, p.ID, "spend "+string(rune('a'+i)), NewRun{Agent: "fake"})
		_, err := f.s.ClaimRun(f.ctx, w, []string{"fake"}, 0)
		f.must(err)
		for _, u := range reports {
			_, err := f.s.AppendRunEvent(f.ctx, w, r.ID, "usage", u)
			f.must(err)
		}
		if i == 0 {
			got, _ := f.s.Run(f.ctx, r.ID)
			if got.ContextTokens != 30000 || got.ContextSize != 200000 || got.Cost != 0.42 || got.CostCurrency != "USD" {
				t.Fatalf("run usage: %+v", got)
			}
		}
	}
	u, err := f.s.Usage(f.ctx, p.ID, 7)
	f.must(err)
	if u.Runs != 2 || fmt.Sprintf("%.2f", u.Cost["USD"]) != "0.50" || len(u.ByAgent) != 1 || u.ByAgent[0].ContextTokens != 31000 {
		t.Fatalf("usage: %+v", u)
	}
	all, err := f.s.Usage(f.ctx, "", 0)
	f.must(err)
	if len(all.ByProject) != 1 || all.ByProject[0].Name != "Spend" {
		t.Fatalf("server usage: %+v", all)
	}
}

func TestPresenceShowsEachAgentsLoad(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Load")
	f.queue(ada, p.ID, "one", NewRun{Agent: "claude"})
	f.queue(ada, p.ID, "two", NewRun{Agent: "claude"})
	f.queue(ada, p.ID, "three", NewRun{Agent: "codex"})
	f.queue(ada, p.ID, "four", NewRun{})
	f.s.WorkerSlots("box", 3)
	before := len(f.pub.topic("presence:server"))
	c, err := f.s.ClaimRun(f.ctx, WorkerActor("box"), []string{"claude"}, 0)
	f.must(err)
	if c == nil || c.Run.Agent != "claude" {
		t.Fatalf("claimed %+v", c)
	}
	if len(f.pub.topic("presence:server")) <= before {
		t.Fatal("a claim changes the load; subscribers are told")
	}
	f.s.SetLocalWorker(LocalWorker{State: "running", Name: "box", Isolation: "host"})
	pr, err := f.s.Presence(f.ctx)
	f.must(err)
	got := map[string]AgentLoad{}
	for _, a := range pr.Agents {
		got[a.Agent] = a
	}
	want := map[string]AgentLoad{
		"claude": {Agent: "claude", Workers: 1, Running: 1, Queued: 1},
		"codex":  {Agent: "codex", Queued: 1}, // nobody offers it
		"any":    {Agent: "any", Queued: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("agents: %+v", pr.Agents)
	}
	for k, w := range want {
		if got[k] != w {
			t.Fatalf("%s: %+v want %+v", k, got[k], w)
		}
	}
	if len(pr.Workers) != 1 || pr.Workers[0].Slots != 3 || pr.Workers[0].Running != 1 || !pr.Workers[0].Local || pr.Slots != 3 || pr.Running != 1 {
		t.Fatalf("workers: %+v slots %d running %d", pr.Workers, pr.Slots, pr.Running)
	}
	if pr.Local.Isolation != "host" {
		t.Fatalf("local: %+v", pr.Local)
	}
	_, err = f.s.ReportRun(f.ctx, WorkerActor("box"), c.Run.ID, RunReport{Status: "succeeded"})
	f.must(err)
	if pr, _ = f.s.Presence(f.ctx); pr.Running != 0 {
		t.Fatalf("finished Runs free the slot: %+v", pr)
	}
}

func TestNewProjectsBotsUseTheChosenAgent(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p, err := f.s.CreateProject(f.ctx, ada, NewProject{Name: "Codexed", DefaultBots: true, Agent: "Codex"})
	f.must(err)
	for _, m := range p.Members {
		if m.Kind == models.KindBot && m.Agent != "codex" {
			t.Fatalf("%s runs %q", m.DisplayName, m.Agent)
		}
	}
	if _, err := f.s.CreateProject(f.ctx, ada, NewProject{Name: "Bad", Agent: "gpt"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown agent: %v", err)
	}
}
