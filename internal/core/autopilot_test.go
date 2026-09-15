package core

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/models"
)

// autopilot is a Project on autopilot plus a pretend worker.
type autopilot struct {
	*fixture
	ada  Actor
	p    *models.ProjectBundle
	w    Actor
	task *models.Task
}

func newAutopilot(t *testing.T, policy string) *autopilot {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Auto")
	on, repo := true, "/srv/git/app.git"
	_, err := f.s.UpdateProject(f.ctx, ada, p.ID, ProjectPatch{AutoRun: &on, RepoURL: &repo, MergePolicy: &policy})
	f.must(err)
	return &autopilot{fixture: f, ada: ada, p: p, w: WorkerActor("w1")}
}

// next claims the queued Run, checks its kind, and reports it succeeded.
func (a *autopilot) next(kind models.RunKind, rep RunReport) *models.Run {
	a.t.Helper()
	c, err := a.s.ClaimRun(a.ctx, a.w, []string{"claude"}, 0)
	a.must(err)
	if c == nil {
		a.t.Fatalf("no Run queued, want a %s Run", kind)
	}
	if c.Run.Kind != kind {
		a.t.Fatalf("queued a %s Run, want %s", c.Run.Kind, kind)
	}
	rep.Status = "succeeded"
	r, err := a.s.ReportRun(a.ctx, a.w, c.Run.ID, rep)
	a.must(err)
	return r
}

func (a *autopilot) idle() {
	a.t.Helper()
	if c, _ := a.s.ClaimRun(a.ctx, a.w, []string{"claude"}, 0); c != nil {
		a.t.Fatalf("nothing should be queued, got a %s Run", c.Run.Kind)
	}
}

func (a *autopilot) refresh() *models.Task {
	a.t.Helper()
	t, err := a.s.Task(a.ctx, a.task.ID)
	a.must(err)
	return t
}

func (a *autopilot) start(title string) {
	a.t.Helper()
	tc, err := a.s.CreateTask(a.ctx, a.ada, a.p.ID, NewTask{Title: title})
	a.must(err)
	if tc.Run == nil || tc.Run.Kind != models.RunPlan {
		a.t.Fatalf("a new Task on autopilot starts with Scout planning: %+v", tc.Run)
	}
	a.task = &tc.Task
}

func TestAutopilotPlansBuildsReviewsAndMerges(t *testing.T) {
	a := newAutopilot(t, models.MergeAuto)
	a.start("Add caching")

	a.next(models.RunPlan, RunReport{Summary: "Cache GET /v1/projects for 5s."})
	build := a.next(models.RunBuild, RunReport{Summary: "Added a cache.", Branch: "buildbee/add-caching-1", PRURL: "https://github.com/acme/app/pull/9"})
	if !strings.Contains(build.Prompt, "Scout's plan:\n\nCache GET /v1/projects for 5s.") {
		t.Fatalf("the Builder gets Scout's plan:\n%s", build.Prompt)
	}
	if task := a.refresh(); task.Branch != "buildbee/add-caching-1" || task.PRURL == "" {
		t.Fatalf("the Task records the branch and PR: %+v", task)
	}
	review := a.next(models.RunReview, RunReport{Summary: "Tests cover it.\n\n**VERDICT: APPROVE**"})
	if review.Verdict != "approve" || !strings.Contains(review.Prompt, "branch buildbee/add-caching-1") ||
		!strings.Contains(review.Prompt, "VERDICT: APPROVE") {
		t.Fatalf("review: verdict %q, prompt:\n%s", review.Verdict, review.Prompt)
	}
	// No CI on this Task: an approved branch merges at once, on any worker.
	c, err := a.s.ClaimRun(a.ctx, WorkerActor("fake-box"), []string{"fake"}, 0)
	a.must(err)
	if c == nil || c.Run.Kind != models.RunMerge || c.Task.Branch != "buildbee/add-caching-1" {
		t.Fatalf("merge: %+v", c)
	}
	_, err = a.s.ReportRun(a.ctx, WorkerActor("fake-box"), c.Run.ID, RunReport{Status: "succeeded", Detail: "merged"})
	a.must(err)
	task := a.refresh()
	if task.Status != models.TaskDone || task.MergedAt == nil {
		t.Fatalf("merged Task: %+v", task)
	}
	hs, _ := a.s.Handoffs(a.ctx, task.ID)
	for _, h := range hs {
		if h.Status != models.HandoffComplete {
			t.Fatalf("each Bot's Handoff closes when its Run succeeds: %+v", hs)
		}
	}
	a.idle()
	var merged bool
	for _, n := range a.inbox(a.ada) {
		merged = merged || n.Title == "Merged: Add caching"
	}
	if !merged {
		t.Fatal("people hear about the merge")
	}
}

func TestReviewChangesGoBackToTheBuilderThenToAPerson(t *testing.T) {
	a := newAutopilot(t, models.MergeAuto)
	a.start("Tricky change")
	a.next(models.RunPlan, RunReport{Summary: "plan"})
	for round := 1; round <= maxRounds; round++ {
		b := a.next(models.RunBuild, RunReport{Summary: "try", Branch: "buildbee/tricky-1"})
		if round > 1 && !strings.Contains(b.Prompt, "Sentry asked for changes") || round > 1 && !strings.Contains(b.Prompt, "continuing branch buildbee/tricky-1") {
			t.Fatalf("round %d: the Builder gets the review and its branch:\n%s", round, b.Prompt)
		}
		a.next(models.RunReview, RunReport{Summary: "Missing tests.\nVERDICT: REQUEST_CHANGES"})
	}
	a.idle() // three builds: a person decides now
	ds, err := a.s.Decisions(a.ctx, a.ada, a.p.ID, DecisionFilter{Open: true})
	a.must(err)
	if len(ds) != 1 || ds[0].Action != "merge" || ds[0].TaskID != a.task.ID {
		t.Fatalf("decisions: %+v", ds)
	}
	_, err = a.s.AnswerDecision(a.ctx, a.ada, ds[0].ID, "merge")
	a.must(err)
	a.next(models.RunMerge, RunReport{})
	if a.refresh().MergedAt == nil {
		t.Fatal("answering merge merges")
	}
}

func TestApprovalPolicyAsksBeforeMerging(t *testing.T) {
	a := newAutopilot(t, models.MergeApproval)
	a.start("Careful change")
	a.next(models.RunPlan, RunReport{Summary: "plan"})
	a.next(models.RunBuild, RunReport{Branch: "buildbee/careful-1"})
	a.next(models.RunReview, RunReport{Summary: "VERDICT: APPROVE"})
	a.idle()
	ds, _ := a.s.Decisions(a.ctx, a.ada, a.p.ID, DecisionFilter{Open: true})
	if len(ds) != 1 || ds[0].Action != "merge" || !strings.Contains(ds[0].Prompt, "Merge it?") {
		t.Fatalf("decisions: %+v", ds)
	}
	_, err := a.s.AnswerDecision(a.ctx, a.ada, ds[0].ID, "not yet")
	a.must(err)
	a.idle()
	if mem, _ := a.s.DecisionMemories(a.ctx, a.p.ID); len(mem) != 0 {
		t.Fatalf("merge answers are never remembered: %+v", mem)
	}
}

func TestCIGatesTheMergeAndFailuresGoBack(t *testing.T) {
	a := newAutopilot(t, models.MergeAuto)
	a.start("Needs CI")
	a.next(models.RunPlan, RunReport{Summary: "plan"})
	a.next(models.RunBuild, RunReport{Branch: "buildbee/ci-1"})
	_, err := a.s.RecordPipeline(a.ctx, System("github"), a.task.ID, NewPipeline{Name: "test", Status: "in_progress"})
	a.must(err)
	a.next(models.RunReview, RunReport{Summary: "VERDICT: APPROVE"})
	a.idle() // CI still running

	// CI fails: back to the Builder, which fixes it; CI passes; merge.
	byBranch, err := a.s.TaskByBranch(a.ctx, "buildbee/ci-1")
	a.must(err)
	_, err = a.s.RecordPipeline(a.ctx, System("github"), byBranch.ID, NewPipeline{Name: "test", Status: "failure", ExternalURL: "https://ci/1"})
	a.must(err)
	fix := a.next(models.RunBuild, RunReport{Branch: "buildbee/ci-1"})
	if !strings.Contains(fix.Prompt, "CI failed: test https://ci/1") {
		t.Fatalf("the Builder learns what failed:\n%s", fix.Prompt)
	}
	a.next(models.RunReview, RunReport{Summary: "VERDICT: APPROVE"})
	a.idle() // the old failure is about an earlier push: wait for CI on this one
	_, err = a.s.RecordPipeline(a.ctx, System("github"), a.task.ID, NewPipeline{Name: "test", Status: "success"})
	a.must(err)
	a.next(models.RunMerge, RunReport{})
	if a.refresh().Status != models.TaskDone {
		t.Fatal("merged after CI passed")
	}
}

func TestWithoutAutopilotRunsStopAfterThemselves(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Manual")
	tc, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "By hand"})
	f.must(err)
	if tc.Run != nil {
		t.Fatal("without autopilot, handing a Task to Scout starts nothing")
	}
	r, err := f.s.CreateRun(f.ctx, ada, tc.ID, NewRun{BotMemberID: bot(p, models.RoleBuilder).ID})
	f.must(err)
	if r.Kind != models.RunBuild {
		t.Fatalf("a Builder Run builds: %+v", r)
	}
	w := WorkerActor("w1")
	_, err = f.s.ClaimRun(f.ctx, w, []string{"claude"}, 0)
	f.must(err)
	_, err = f.s.ReportRun(f.ctx, w, r.ID, RunReport{Status: "succeeded", Branch: "buildbee/by-hand-1"})
	f.must(err)
	if c, _ := f.s.ClaimRun(f.ctx, w, []string{"claude"}, 0); c != nil {
		t.Fatalf("nothing follows without autopilot: %+v", c.Run)
	}
	if task, _ := f.s.Task(f.ctx, tc.ID); task.Branch != "buildbee/by-hand-1" {
		t.Fatalf("the branch is still recorded: %+v", task)
	}
	if _, err := f.s.ReportRun(f.ctx, ada, r.ID, RunReport{Status: "canceled", Summary: "x"}); err == nil {
		t.Fatal("people cannot write a Run's outcome")
	}
}

func TestParseVerdict(t *testing.T) {
	for in, want := range map[string]string{
		"ok\nVERDICT: APPROVE":                             "approve",
		"**Verdict: Request_Changes**":                     "changes",
		"VERDICT: APPROVE\nwait\nVERDICT: REQUEST_CHANGES": "changes",
		"I approve":      "",
		"VERDICT: maybe": "",
	} {
		if got := models.ParseVerdict(in); got != want {
			t.Errorf("ParseVerdict(%q) = %q, want %q", in, got, want)
		}
	}
}

// Commits as a worker reports them.
const (
	commitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	commitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestOnlyTheApprovedCommitMerges(t *testing.T) {
	a := newAutopilot(t, models.MergeAuto)
	a.start("Exact")
	a.next(models.RunPlan, RunReport{Summary: "plan"})
	a.next(models.RunBuild, RunReport{Branch: "buildbee/exact-1", Commit: commitA})
	// A person queues another build while Sentry reviews commit A.
	review, err := a.s.ClaimRun(a.ctx, a.w, []string{"claude"}, 0)
	a.must(err)
	_, err = a.s.CreateRun(a.ctx, a.ada, a.task.ID, NewRun{BotMemberID: bot(a.p, models.RoleBuilder).ID})
	a.must(err)
	a.next(models.RunBuild, RunReport{Branch: "buildbee/exact-1", Commit: commitB})
	_, err = a.s.ReportRun(a.ctx, a.w, review.Run.ID, RunReport{Status: "succeeded", Summary: "VERDICT: APPROVE", Commit: commitA})
	a.must(err)
	for {
		c, _ := a.s.ClaimRun(a.ctx, a.w, []string{"claude"}, 0)
		if c == nil {
			break
		}
		if c.Run.Kind == models.RunMerge {
			t.Fatal("approving commit A must not merge commit B")
		}
		a.must(func() error { _, err := a.s.ReportRun(a.ctx, a.w, c.Run.ID, RunReport{Status: "canceled"}); return err }())
	}
}

func TestAMergeDecisionIsAboutOneCommit(t *testing.T) {
	a := newAutopilot(t, models.MergeApproval)
	a.start("Asked")
	a.next(models.RunPlan, RunReport{Summary: "plan"})
	a.next(models.RunBuild, RunReport{Branch: "buildbee/asked-1", Commit: commitA})
	a.next(models.RunReview, RunReport{Summary: "VERDICT: APPROVE", Commit: commitA})
	ds, _ := a.s.Decisions(a.ctx, a.ada, a.p.ID, DecisionFilter{Open: true})
	if len(ds) != 1 || ds[0].Commit != commitA {
		t.Fatalf("decision: %+v", ds)
	}
	// New work lands before anyone answers.
	_, err := a.s.CreateRun(a.ctx, a.ada, a.task.ID, NewRun{BotMemberID: bot(a.p, models.RoleBuilder).ID})
	a.must(err)
	a.next(models.RunBuild, RunReport{Branch: "buildbee/asked-1", Commit: commitB})
	a.next(models.RunReview, RunReport{Summary: "VERDICT: REQUEST_CHANGES", Commit: commitB})
	a.next(models.RunBuild, RunReport{Branch: "buildbee/asked-1", Commit: commitB}) // no new commit this round
	a.next(models.RunReview, RunReport{Summary: "hmm", Commit: commitB})            // no verdict: stops
	_, err = a.s.AnswerDecision(a.ctx, a.ada, ds[0].ID, "merge")
	a.must(err)
	a.idle()
	var told bool
	for _, n := range a.inbox(a.ada) {
		told = told || strings.HasPrefix(n.Title, "Not merged: Asked changed")
	}
	if !told {
		t.Fatal("people learn why their answer did not merge")
	}
}

func TestCIIsMatchedByCommit(t *testing.T) {
	a := newAutopilot(t, models.MergeAuto)
	a.start("Fast CI")
	a.next(models.RunPlan, RunReport{Summary: "plan"})
	// A fast check reports before the build does.
	build, err := a.s.ClaimRun(a.ctx, a.w, []string{"claude"}, 0)
	a.must(err)
	_, err = a.s.RecordPipeline(a.ctx, System("github"), a.task.ID, NewPipeline{Name: "lint", Status: "failure", Commit: commitA})
	a.must(err)
	_, err = a.s.RecordPipeline(a.ctx, System("github"), a.task.ID, NewPipeline{Name: "old", Status: "failure", Commit: commitB})
	a.must(err)
	_, err = a.s.ReportRun(a.ctx, a.w, build.Run.ID, RunReport{Status: "succeeded", Branch: "buildbee/fast-1", Commit: commitA})
	a.must(err)
	a.next(models.RunReview, RunReport{Summary: "VERDICT: APPROVE", Commit: commitA})
	fix := a.next(models.RunBuild, RunReport{Branch: "buildbee/fast-1", Commit: commitB})
	if !strings.Contains(fix.Prompt, "CI failed: lint") || strings.Contains(fix.Prompt, "CI failed: old") {
		t.Fatalf("the early failure on commit A counts; a result for another commit does not:\n%s", fix.Prompt)
	}
	if _, err := a.s.RecordPipeline(a.ctx, System("github"), a.task.ID, NewPipeline{Name: "lint", Status: "success", Commit: "not-a-sha"}); err == nil {
		t.Fatal("commit must be hex")
	}
}

func TestMergeAnywayIsAskedOnce(t *testing.T) {
	a := newAutopilot(t, models.MergeAuto)
	a.start("Stubborn")
	a.next(models.RunPlan, RunReport{Summary: "plan"})
	for range maxRounds {
		a.next(models.RunBuild, RunReport{Branch: "buildbee/stubborn-1", Commit: commitA})
		a.next(models.RunReview, RunReport{Summary: "VERDICT: REQUEST_CHANGES", Commit: commitA})
	}
	for range 3 { // CI keeps failing and re-running
		_, err := a.s.RecordPipeline(a.ctx, System("github"), a.task.ID, NewPipeline{Name: "test", Status: "failure", Commit: commitA})
		a.must(err)
	}
	if ds, _ := a.s.Decisions(a.ctx, a.ada, a.p.ID, DecisionFilter{Open: true}); len(ds) != 1 {
		t.Fatalf("one open question, not %d", len(ds))
	}
}

func TestReapedRunsTellPeople(t *testing.T) {
	a := newAutopilot(t, models.MergeAuto)
	clock := time.Now().UTC()
	var mu sync.Mutex
	a.s.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	a.start("Lost")
	_, err := a.s.ClaimRun(a.ctx, a.w, []string{"claude"}, 0)
	a.must(err)
	mu.Lock()
	clock = clock.Add(2 * LeaseTTL)
	mu.Unlock()
	n, err := a.s.ReapRuns(a.ctx)
	a.must(err)
	var told bool
	for _, x := range a.inbox(a.ada) {
		told = told || strings.HasPrefix(x.Title, "Plan Run failed on Lost")
	}
	if n != 1 || !told {
		t.Fatalf("reaped %d, told %v", n, told)
	}
}

func TestMaxRunsHoldsUnderConcurrentClaims(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Capped")
	one := 1
	_, err := f.s.UpdateProject(f.ctx, ada, p.ID, ProjectPatch{MaxRuns: &one})
	f.must(err)
	for i := range 6 {
		f.queue(ada, p.ID, "t"+string(rune('a'+i)), NewRun{Agent: "fake"})
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	claimed := 0
	for i := range 6 {
		wg.Go(func() {
			c, err := f.s.ClaimRun(f.ctx, WorkerActor("w"+string(rune('0'+i))), []string{"fake"}, 0)
			if err != nil {
				t.Error(err)
			}
			if c != nil {
				mu.Lock()
				claimed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if claimed != 1 {
		t.Fatalf("max_runs=1 but %d Runs started", claimed)
	}
}

func TestACanceledTaskIsNotMerged(t *testing.T) {
	a := newAutopilot(t, models.MergeApproval)
	a.start("Dropped")
	a.next(models.RunPlan, RunReport{Summary: "plan"})
	a.next(models.RunBuild, RunReport{Branch: "buildbee/dropped-1", Commit: commitA})
	a.next(models.RunReview, RunReport{Summary: "VERDICT: APPROVE", Commit: commitA})
	canceled := "canceled"
	_, err := a.s.UpdateTask(a.ctx, a.ada, a.task.ID, TaskPatch{Status: &canceled})
	a.must(err)
	ds, _ := a.s.Decisions(a.ctx, a.ada, a.p.ID, DecisionFilter{Open: true})
	_, err = a.s.AnswerDecision(a.ctx, a.ada, ds[0].ID, "merge")
	a.must(err)
	a.idle()
}
