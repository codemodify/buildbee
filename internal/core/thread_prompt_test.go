package core

import (
	"strings"
	"testing"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
)

// Asking several Bots at once gives each its own Run, and a Bot asked
// afterwards reads what the others answered. That is what "consolidate
// these into one answer" needs, and what lets Bots cross-check each other.
func TestABotReadsWhatTheOtherBotsAnswered(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Research")
	scout, builder := bot(p, models.RoleScout), bot(p, models.RoleBuilder)

	posted, err := f.s.PostMessage(f.ctx, ada, p.Channels[0].ID,
		"@Scout @Builder how should we cache sessions?")
	f.must(err)
	if len(posted.Tasks) != 1 {
		t.Fatalf("one thread for the ask: %+v", posted.Tasks)
	}
	task := posted.Tasks[0]
	runs, err := f.s.Runs(f.ctx, task.ID)
	f.must(err)
	if len(runs) != 2 {
		t.Fatalf("one Run per Bot asked, got %d: %+v", len(runs), runs)
	}
	// Each Bot is asked in its own Run, and sees the question.
	for _, r := range runs {
		if !strings.Contains(r.Prompt, "how should we cache sessions?") {
			t.Fatalf("Run %s never got the question:\n%s", r.ID, r.Prompt)
		}
	}

	// Each answers in the thread.
	answers := map[string]string{scout.ID: "Redis, with a short TTL.", builder.ID: "Signed cookies, no server state."}
	for _, r := range runs {
		_, w := f.claimFor(r.BotMemberID, "claude")
		_, err := f.s.ReportRun(f.ctx, w, r.ID, RunReport{Status: "succeeded", Summary: answers[r.BotMemberID]})
		f.must(err)
	}

	// Now one Bot is asked to consolidate: its prompt carries the thread,
	// with every other Bot's answer and its own marked "You".
	_, err = f.s.Reply(f.ctx, ada, task.ThreadID, "@Scout consolidate these into one answer")
	f.must(err)
	runs, err = f.s.Runs(f.ctx, task.ID)
	f.must(err)
	var latest *models.Run
	for i := range runs {
		if runs[i].Status == models.RunPending {
			latest = &runs[i]
		}
	}
	if latest == nil {
		t.Fatalf("Scout's second Run: %+v", runs)
	}
	for _, want := range []string{
		"Ada: @Scout @Builder how should we cache sessions?",
		"You: Redis, with a short TTL.",
		"Builder: Signed cookies, no server state.",
		"consolidate these into one answer",
	} {
		if !strings.Contains(latest.Prompt, want) {
			t.Fatalf("the consolidating Bot never saw %q:\n%s", want, latest.Prompt)
		}
	}
}

// Bots working the same ask each keep their own branch, and each is told
// where the others' work is so it can read it and argue with it.
func TestEachBotKeepsItsOwnBranch(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Adversarial")
	scout, builder := bot(p, models.RoleScout), bot(p, models.RoleBuilder)

	posted, err := f.s.PostMessage(f.ctx, ada, p.Channels[0].ID, "@Scout @Builder add a cache")
	f.must(err)
	task := posted.Tasks[0]
	runs, err := f.s.Runs(f.ctx, task.ID)
	f.must(err)
	if len(runs) != 2 {
		t.Fatalf("a Run each: %+v", runs)
	}
	// Both start fresh: neither is handed the other's branch.
	for _, r := range runs {
		c, w := f.claimFor(r.BotMemberID, "claude")
		if c.Branch != "" {
			t.Fatalf("a first Run starts fresh, got branch %q", c.Branch)
		}
		branch := map[string]string{scout.ID: "buildbee/cache-scout", builder.ID: "buildbee/cache-builder"}[r.BotMemberID]
		_, err := f.s.ReportRun(f.ctx, w, r.ID, RunReport{Status: "succeeded", Summary: "pushed " + branch, Branch: branch})
		f.must(err)
	}

	// Asked again, each Bot carries on its own branch and is told the other's.
	_, err = f.s.Reply(f.ctx, ada, task.ThreadID, "@Scout @Builder now cross-check each other")
	f.must(err)
	runs, err = f.s.Runs(f.ctx, task.ID)
	f.must(err)
	seen := map[string]bool{}
	for _, r := range runs {
		if r.Status != models.RunPending {
			continue
		}
		seen[r.BotMemberID] = true
		mine, theirs := "buildbee/cache-scout", "Builder on buildbee/cache-builder"
		if r.BotMemberID == builder.ID {
			mine, theirs = "buildbee/cache-builder", "Scout on buildbee/cache-scout"
		}
		c, _ := f.claimFor(r.BotMemberID, "claude")
		if c.Branch != mine {
			t.Fatalf("claim branch is %q, want its own %q", c.Branch, mine)
		}
		if !strings.Contains(r.Prompt, theirs) {
			t.Fatalf("a Bot is never told about %q:\n%s", theirs, r.Prompt)
		}
		if !strings.Contains(r.Prompt, mine) {
			t.Fatalf("a Bot is not told its own branch %q:\n%s", mine, r.Prompt)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("both Bots asked again: %v", seen)
	}
}

// A long thread is trimmed oldest-first, and the ask always survives.
func TestTheThreadGivenToABotIsCapped(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Long")
	posted, err := f.s.PostMessage(f.ctx, ada, p.Channels[0].ID, "@Scout the original question")
	f.must(err)
	task := posted.Tasks[0]
	filler := strings.Repeat("x", 2000)
	for i := 0; i < 20; i++ {
		_, err := f.s.Reply(f.ctx, ada, task.ThreadID, filler)
		f.must(err)
	}
	_, err = f.s.Reply(f.ctx, ada, task.ThreadID, "@Builder and now this")
	f.must(err)
	runs, err := f.s.Runs(f.ctx, task.ID)
	f.must(err)
	var last *models.Run
	for i := range runs {
		if last == nil || runs[i].CreatedAt.After(last.CreatedAt) {
			last = &runs[i]
		}
	}
	if len(last.Prompt) > convoBytes+4000 {
		t.Fatalf("prompt is %d bytes, cap is %d", len(last.Prompt), convoBytes)
	}
	if !strings.Contains(last.Prompt, "the original question") {
		t.Fatalf("the ask must survive trimming:\n%s", last.Prompt[:min(500, len(last.Prompt))])
	}
	if !strings.Contains(last.Prompt, "and now this") {
		t.Fatal("the newest reply must survive trimming")
	}
	th, err := f.s.Thread(f.ctx, ada, task.ThreadID, store.Page{Limit: 100})
	f.must(err)
	if len(th.Replies) < 21 {
		t.Fatalf("the thread itself keeps everything: %d replies", len(th.Replies))
	}
}
