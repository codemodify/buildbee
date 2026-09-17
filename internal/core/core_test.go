package core

import (
	"context"
	"errors"
	"github.com/codemodify/buildbee/internal/blob"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/codemodify/buildbee/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

// recorder is a Publisher that keeps what was published.
type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) Publish(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recorder) topic(prefix string) []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Event
	for _, e := range r.events {
		if strings.HasPrefix(e.Topic, prefix) {
			out = append(out, e)
		}
	}
	return out
}

type fixture struct {
	t     *testing.T
	ctx   context.Context
	s     *Service
	pub   *recorder
	blobs blob.Dir
}

func newFixture(t *testing.T) *fixture {
	pub := &recorder{}
	blobs := blob.Dir{Root: t.TempDir()}
	return &fixture{t: t, ctx: context.Background(), s: New(store.New(testdb.New(t)), pub, Options{Blobs: blobs, MaxUploadBytes: 1 << 20}),
		pub: pub, blobs: blobs}
}

// agentOf is the actor for the agent process running bot.
func (f *fixture) agentOf(botID string) Actor { return AgentActor("agent-"+botID[:4]+"@test", botID) }

// claimFor claims one Run as the agent of bot, running ai.
func (f *fixture) claimFor(botID, ai string) (*models.Claim, Actor) {
	f.t.Helper()
	a := f.agentOf(botID)
	c, err := f.s.ClaimRun(f.ctx, a, Connect{AI: ai, Host: "test", Slots: 4}, 0)
	f.must(err)
	return c, a
}

func (f *fixture) person(name string) Actor {
	f.t.Helper()
	p, err := f.s.Hello(f.ctx, name)
	if err != nil {
		f.t.Fatal(err)
	}
	return Actor{PersonID: p.ID, Name: p.Name}
}

func (f *fixture) project(a Actor, name string) *models.ProjectBundle {
	f.t.Helper()
	p, err := f.s.CreateProject(f.ctx, a, NewProject{Name: name, DefaultBots: true})
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func bot(p *models.ProjectBundle, role string) *models.Member {
	return models.MemberByRole(p.Members, role)
}

func (f *fixture) memberOf(projectID string, a Actor) *models.Member {
	f.t.Helper()
	m, err := f.s.st.MemberForPerson(f.ctx, projectID, a.PersonID)
	f.must(err)
	return m
}

func (f *fixture) inbox(a Actor) []models.Notification {
	f.t.Helper()
	in, err := f.s.Notifications(f.ctx, a, false, store.Page{})
	f.must(err)
	return in.Items
}

func (f *fixture) activity(projectID string) []models.Activity {
	f.t.Helper()
	items, _, err := f.s.Activity(f.ctx, projectID, "", store.Page{Limit: 1000})
	f.must(err)
	return items
}

// --- people and projects ---

func TestHelloIsCaseInsensitiveAndValidated(t *testing.T) {
	f := newFixture(t)
	a, _ := f.s.Hello(f.ctx, "Ada")
	b, _ := f.s.Hello(f.ctx, "  ada ")
	if a.ID != b.ID || b.Name != "Ada" {
		t.Fatalf("same person expected: %+v %+v", a, b)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("x", 61), "tab\there"} {
		if _, err := f.s.Hello(f.ctx, bad); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("%q: got %v", bad, err)
		}
	}
}

func TestNewProjectsHaveNoBotsUnlessAsked(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p, err := f.s.CreateProject(f.ctx, ada, NewProject{Name: "Bare"})
	f.must(err)
	if len(p.Members) != 1 || p.Members[0].Kind != models.KindHuman || len(p.Channels) != 1 {
		t.Fatalf("bare project: %+v", p)
	}
}

func TestCreateProjectSeedsOwnerBotsAndChannel(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Rocket")
	if len(p.Members) != 5 || p.Members[0].PersonID != ada.PersonID || p.Members[0].Role != models.RoleOwner {
		t.Fatalf("members: %+v", p.Members)
	}
	for _, role := range []string{models.RoleScout, models.RoleBuilder, models.RoleSentry, models.RolePulse} {
		if b := bot(p, role); b == nil || b.Kind != models.KindBot {
			t.Fatalf("missing bot %s", role)
		}
	}
	if len(p.Channels) != 1 || p.Channels[0].Name != "tasks" || !p.Channels[0].Locked {
		t.Fatalf("channels: %+v", p.Channels)
	}
	name, yes := "lobby", true
	if _, err := f.s.UpdateChannel(f.ctx, ada, p.Channels[0].ID, ChannelPatch{Name: &name}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("#tasks cannot be renamed: %v", err)
	}
	if _, err := f.s.UpdateChannel(f.ctx, ada, p.Channels[0].ID, ChannelPatch{Archived: &yes}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("#tasks cannot be archived: %v", err)
	}
	if rs, err := f.s.Routines(f.ctx, p.ID); err != nil || len(rs) != 0 {
		t.Fatalf("no Routines until someone schedules one: %+v %v", rs, err)
	}
	acts := f.activity(p.ID)
	if len(acts) != 1 || acts[0].Action != "created" || acts[0].ActorMemberID != p.Members[0].ID || acts[0].Actor != "Ada" {
		t.Fatalf("activity: %+v", acts)
	}
}

func TestSecondPersonIsAttributedToThemselves(t *testing.T) {
	f := newFixture(t)
	ada, bob := f.person("Ada"), f.person("Bob")
	p := f.project(ada, "Shared")
	posted, err := f.s.PostMessage(f.ctx, bob, p.Channels[0].ID, "hello from bob")
	f.must(err)
	bobMember := f.memberOf(p.ID, bob)
	if posted.MemberID != bobMember.ID || bobMember.Role != models.RoleMember {
		t.Fatalf("message by %s, bob is %+v", posted.MemberID, bobMember)
	}
	acts := f.activity(p.ID)
	if acts[0].Type != models.TypeMember || acts[0].Action != "joined" || acts[0].ActorMemberID != bobMember.ID {
		t.Fatalf("bob joining must be recorded as bob: %+v", acts[0])
	}
}

func TestAnonymousCannotPost(t *testing.T) {
	f := newFixture(t)
	p := f.project(f.person("Ada"), "P")
	if _, err := f.s.PostMessage(f.ctx, System("anonymous"), p.Channels[0].ID, "hi"); !errors.Is(err, ErrNoActor) {
		t.Fatalf("got %v", err)
	}
}

func TestArchivedProjectIsReadOnly(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Old")
	yes, no := true, false
	_, err := f.s.UpdateProject(f.ctx, ada, p.ID, ProjectPatch{Archived: &yes})
	f.must(err)
	if _, err := f.s.PostMessage(f.ctx, ada, p.Channels[0].ID, "hi"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("post to archived: %v", err)
	}
	if _, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "x"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("task in archived: %v", err)
	}
	list, _ := f.s.Projects(f.ctx, false)
	if len(list) != 0 {
		t.Fatalf("archived project listed: %+v", list)
	}
	_, err = f.s.UpdateProject(f.ctx, ada, p.ID, ProjectPatch{Archived: &no})
	f.must(err)
	if _, err := f.s.PostMessage(f.ctx, ada, p.Channels[0].ID, "back"); err != nil {
		t.Fatalf("unarchived project must accept writes: %v", err)
	}
}

// --- messages, mentions, tasks, handoffs ---

func TestMentioningABotCreatesATaskHandedToIt(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "M")
	before := len(f.pub.topic("channel:" + p.Channels[0].ID))
	posted, err := f.s.PostMessage(f.ctx, ada, p.Channels[0].ID, "@Builder add rate limiting to /login\nuse 10 req/min")
	f.must(err)
	builder := bot(p, models.RoleBuilder)
	if len(posted.Tasks) != 1 || len(posted.Handoffs) != 1 {
		t.Fatalf("posted: %+v", posted)
	}
	task := posted.Tasks[0]
	if task.AssigneeMemberID != builder.ID || task.Status != models.TaskInProgress ||
		!strings.Contains(task.Body, "10 req/min") || task.Title != "add rate limiting to /login" {
		t.Fatalf("task: %+v", task)
	}
	if h := posted.Handoffs[0]; h.FromMemberID != posted.MemberID || h.ToMemberID != builder.ID {
		t.Fatalf("handoff: %+v", h)
	}
	if task.ThreadID != posted.ID || posted.TaskID != task.ID {
		t.Fatalf("the message is the Task's thread: task %+v, message %+v", task, posted.Message)
	}
	runs, _ := f.s.Runs(f.ctx, task.ID)
	if len(runs) != 1 || runs[0].Kind != models.RunBuild {
		t.Fatalf("asking a Bot starts its Run: %+v", runs)
	}
	evs := f.pub.topic("channel:" + p.Channels[0].ID)[before:]
	if len(evs) != 2 || evs[0].Cursor != posted.Seq || evs[1].Cursor <= posted.Seq {
		t.Fatalf("the message, then the Builder's note in its thread, in order: %+v", evs)
	}
	th, err := f.s.Thread(f.ctx, ada, posted.ID, store.Page{})
	f.must(err)
	if len(th.Replies) != 1 || th.Replies[0].MemberID != builder.ID || th.Root.ReplyCount != 1 {
		t.Fatalf("thread: %+v", th)
	}
}

func TestMentionTitle(t *testing.T) {
	for in, want := range map[string]string{
		"@Builder add caching":                "add caching",
		"@Scout, @Builder: triage this\nmore": "triage this",
		"@Builder":                            "Request from #general",
		"please @Builder look":                "please @Builder look",
	} {
		if got := mentionTitle(in, "general"); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestMentioningAPersonNotifiesThemUnlessMuted(t *testing.T) {
	f := newFixture(t)
	ada, grace := f.person("Ada"), f.person("Grace")
	p := f.project(ada, "M")
	_, err := f.s.Join(f.ctx, grace, p.ID)
	f.must(err)
	_, err = f.s.PostMessage(f.ctx, ada, p.Channels[0].ID, "@Grace can you look?")
	f.must(err)
	if n := f.inbox(grace); len(n) != 1 || n[0].Kind != "mention" || !strings.Contains(n[0].Title, "Ada mentioned you") {
		t.Fatalf("grace inbox: %+v", n)
	}
	mute := true
	_, err = f.s.SetPreferences(f.ctx, grace, PreferencesPatch{MuteMentions: &mute})
	f.must(err)
	_, err = f.s.PostMessage(f.ctx, ada, p.Channels[0].ID, "@Grace again")
	f.must(err)
	if n := f.inbox(grace); len(n) != 1 {
		t.Fatalf("muted mention delivered: %+v", n)
	}
	if n := f.inbox(ada); len(n) != 0 {
		t.Fatalf("author notified: %+v", n)
	}
}

func TestCreateTaskHandsToScoutByDefault(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "T")
	tc, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "Ship it", Body: "details"})
	f.must(err)
	if tc.Handoff == nil || tc.Handoff.ToMemberID != bot(p, models.RoleScout).ID || tc.Status != models.TaskInProgress || tc.Run != nil {
		t.Fatalf("task: %+v", tc)
	}
	now, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "Plan it now", AutoRun: true})
	f.must(err)
	if now.Run == nil || now.Run.Kind != models.RunPlan {
		t.Fatalf("autorun starts Scout's Run: %+v", now.Run)
	}
	none, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "Later", HandoffRole: "none"})
	f.must(err)
	if none.Handoff != nil || none.Status != models.TaskOpen {
		t.Fatalf("handoff=none: %+v", none)
	}
	if _, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "x", HandoffRole: "astronaut"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown role: %v", err)
	}
}

func TestHandoffToBuilderWithAutorunStartsARun(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "R")
	tc, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "Build", HandoffRole: "none"})
	f.must(err)
	h, err := f.s.CreateHandoff(f.ctx, ada, tc.ID, NewHandoff{ToRole: "builder", Note: "go", AutoRun: true})
	f.must(err)
	if h.Run == nil || h.Run.Status != models.RunPending || h.Run.BotMemberID != bot(p, models.RoleBuilder).ID {
		t.Fatalf("run: %+v", h.Run)
	}
	evs, _, err := f.s.RunEvents(f.ctx, h.Run.ID, 0, 0)
	f.must(err)
	if len(evs) != 1 || evs[0].Kind != models.RunEventStatus {
		t.Fatalf("a queued Run starts with a status event: %+v", evs)
	}
	noRun, err := f.s.CreateHandoff(f.ctx, ada, tc.ID, NewHandoff{ToRole: "builder"})
	f.must(err)
	if noRun.Run != nil {
		t.Fatal("no autorun, no Run")
	}
}

func TestHandoffReopensAClosedTask(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "R")
	tc, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "Done thing", HandoffRole: "none"})
	f.must(err)
	done := "done"
	_, err = f.s.UpdateTask(f.ctx, ada, tc.ID, TaskPatch{Status: &done})
	f.must(err)
	h, err := f.s.CreateHandoff(f.ctx, ada, tc.ID, NewHandoff{ToRole: "scout"})
	f.must(err)
	if h.Task.Status != models.TaskInProgress {
		t.Fatalf("status %s", h.Task.Status)
	}
	acts := f.activity(p.ID)
	if acts[0].Type != models.TypeHandoff || acts[0].Payload["reopened_from"] != "done" {
		t.Fatalf("reopening must be visible in Activity: %+v", acts[0])
	}
}

func TestTaskStatusVocabulary(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "S")
	tc, _ := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "x", HandoffRole: "none"})
	bad := "Done "
	if _, err := f.s.UpdateTask(f.ctx, ada, tc.ID, TaskPatch{Status: &bad}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestCompleteHandoffOnceAndScoutDecision(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "D")
	tc, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "Is caching worth it?"})
	f.must(err)
	done, err := f.s.CompleteHandoff(f.ctx, ada, tc.Handoff.ID, false)
	f.must(err)
	if done.Decision == nil || done.Decision.TaskID != tc.ID || len(done.Decision.Options) != 2 {
		t.Fatalf("ambiguous Task after Scout triage opens a linked Decision: %+v", done.Decision)
	}
	if _, err := f.s.CompleteHandoff(f.ctx, ada, tc.Handoff.ID, false); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second completion: %v", err)
	}
	hs, _ := f.s.Handoffs(f.ctx, tc.ID)
	if len(hs) != 1 || hs[0].Status != models.HandoffComplete || hs[0].CompletedAt == nil {
		t.Fatalf("handoffs: %+v", hs)
	}
}

// --- decisions ---

func TestDecisionMemoryAndNotifications(t *testing.T) {
	f := newFixture(t)
	ada, bob := f.person("Ada"), f.person("Bob")
	p := f.project(ada, "Mem")
	_, err := f.s.Join(f.ctx, bob, p.ID)
	f.must(err)

	d, err := f.s.CreateDecision(f.ctx, ada, p.ID, NewDecision{Prompt: "Use Postgres?", Options: []string{"yes", "no"}})
	f.must(err)
	if n := f.inbox(bob); len(n) != 1 || n[0].Kind != "decision" {
		t.Fatalf("bob should be asked: %+v", n)
	}
	if n := f.inbox(ada); len(n) != 0 {
		t.Fatalf("the asker is not notified: %+v", n)
	}
	answered, err := f.s.AnswerDecision(f.ctx, bob, d.ID, "yes")
	f.must(err)
	if answered.AnsweredByMemberID != f.memberOf(p.ID, bob).ID {
		t.Fatalf("answered by: %+v", answered)
	}
	if _, err := f.s.AnswerDecision(f.ctx, ada, d.ID, "no"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("re-answer: %v", err)
	}
	again, err := f.s.CreateDecision(f.ctx, ada, p.ID, NewDecision{Prompt: "use postgres"})
	f.must(err)
	if !again.Reused || again.Answer != "yes" || again.Fingerprint != "use postgres" {
		t.Fatalf("memory not reused: %+v", again)
	}
	if n := f.inbox(bob); len(n) != 1 {
		t.Fatalf("a remembered answer must not ask anyone: %+v", n)
	}
	open, err := f.s.Decisions(f.ctx, ada, p.ID, DecisionFilter{Open: true})
	f.must(err)
	if len(open) != 0 {
		t.Fatalf("open decisions: %+v", open)
	}
}

func TestDecisionForAnAssigneeNotifiesOnlyThem(t *testing.T) {
	f := newFixture(t)
	ada, bob, cy := f.person("Ada"), f.person("Bob"), f.person("Cy")
	p := f.project(ada, "A")
	f.s.Join(f.ctx, bob, p.ID)
	f.s.Join(f.ctx, cy, p.ID)
	_, err := f.s.CreateDecision(f.ctx, ada, p.ID, NewDecision{Prompt: "Bob, merge?", AssigneeMemberID: f.memberOf(p.ID, bob).ID})
	f.must(err)
	if len(f.inbox(bob)) != 1 || len(f.inbox(cy)) != 0 {
		t.Fatal("only the assignee is notified")
	}
	mine, _ := f.s.Decisions(f.ctx, cy, p.ID, DecisionFilter{Mine: true})
	if len(mine) != 0 {
		t.Fatalf("cy's decisions: %+v", mine)
	}
}

// --- runs ---

func TestRunLifecycle(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Runs")
	tc, _ := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "x", HandoffRole: "builder"})
	run, err := f.s.CreateRun(f.ctx, ada, tc.ID, NewRun{Agent: "fake"})
	f.must(err)
	if run.BotMemberID != bot(p, models.RoleBuilder).ID || run.Agent != "fake" || !strings.Contains(run.Prompt, "Task: x") {
		t.Fatalf("run defaults to the assigned Bot and carries its prompt: %+v", run)
	}
	w1 := f.agentOf(bot(p, models.RoleBuilder).ID)
	w2 := AgentActor("other@test", bot(p, models.RoleSentry).ID)
	if _, err := f.s.UpdateRun(f.ctx, w1, run.ID, "running", ""); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("a worker must claim before writing: %v", err)
	}
	c, err := f.s.ClaimRun(f.ctx, w1, Connect{AI: "fake"}, 0)
	f.must(err)
	if c == nil || c.Run.ID != run.ID || c.Run.Status != models.RunRunning || c.Run.Worker != w1.Agent ||
		c.Run.StartedAt == nil || c.Run.LeaseUntil == nil || c.Bot == nil || c.Task.ID != tc.ID {
		t.Fatalf("claim: %+v", c)
	}
	if _, err := f.s.AppendRunEvent(f.ctx, w2, run.ID, "token", nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("another worker cannot write: %v", err)
	}
	_, err = f.s.AppendRunEvent(f.ctx, w1, run.ID, "token", map[string]any{"text": "hi"})
	f.must(err)
	r, err := f.s.UpdateRun(f.ctx, w1, run.ID, "succeeded", "exit 0")
	f.must(err)
	if r.FinishedAt == nil || r.LeaseUntil != nil {
		t.Fatalf("finished_at set, lease cleared: %+v", r)
	}
	if _, err := f.s.UpdateRun(f.ctx, w1, run.ID, "running", ""); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("finished runs cannot change: %v", err)
	}
	if _, err := f.s.AppendRunEvent(f.ctx, w1, run.ID, "token", nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("finished runs take no events: %v", err)
	}
	if _, err := f.s.UpdateRun(f.ctx, w1, run.ID, "finished", ""); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown status: %v", err)
	}
	other := f.queue(ada, p.ID, "y", NewRun{Agent: "fake"})
	if _, err := f.s.UpdateRun(f.ctx, ada, other.ID, "running", ""); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("people cannot start a Run: %v", err)
	}
	if r, err := f.s.UpdateRun(f.ctx, ada, other.ID, "canceled", "no"); err != nil || r.Status != models.RunCanceled {
		t.Fatalf("people can cancel a queued Run: %+v %v", r, err)
	}
	evs, _, _ := f.s.RunEvents(f.ctx, run.ID, 0, 0)
	for i, ev := range evs {
		if ev.Seq != i+1 {
			t.Fatalf("seq must be contiguous: %+v", evs)
		}
	}
	for _, a := range f.activity(p.ID) {
		if a.Type == models.TypeRun && a.Action == "succeeded" && a.ActorMemberID != bot(p, models.RoleBuilder).ID {
			t.Fatalf("worker changes are the Bot's: %+v", a)
		}
	}
}

func TestArtifactListingsOmitBodies(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "A")
	tc, _ := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "x", HandoffRole: "none"})
	a, err := f.s.CreateArtifact(f.ctx, ada, tc.ID, NewArtifact{Kind: "log", Name: "acp.log", Body: "hello world"})
	f.must(err)
	list, _ := f.s.Artifacts(f.ctx, tc.ID)
	if len(list) != 1 || list[0].Body != "" || list[0].Size != 11 {
		t.Fatalf("list: %+v", list)
	}
	full, _ := f.s.Artifact(f.ctx, a.ID)
	if full.Body != "hello world" {
		t.Fatalf("get: %+v", full)
	}
}

// --- pipelines, issues, routines ---

func TestPipelineFailureNotifiesEveryone(t *testing.T) {
	f := newFixture(t)
	ada, bob := f.person("Ada"), f.person("Bob")
	p := f.project(ada, "CI")
	f.s.Join(f.ctx, bob, p.ID)
	tc, _ := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "x", HandoffRole: "none"})
	_, err := f.s.RecordPipeline(f.ctx, System("github"), tc.ID, NewPipeline{Name: "test", Status: "success"})
	f.must(err)
	if len(f.inbox(ada)) != 0 {
		t.Fatal("success notifies nobody")
	}
	_, err = f.s.RecordPipeline(f.ctx, System("github"), tc.ID, NewPipeline{Name: "test", Status: "timed_out"})
	f.must(err)
	if len(f.inbox(ada)) != 1 || len(f.inbox(bob)) != 1 {
		t.Fatal("failure notifies every Person")
	}
	if _, err := f.s.RecordPipeline(f.ctx, System("github"), tc.ID, NewPipeline{Name: "t", Status: "banana"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown status: %v", err)
	}
}

func TestIssueWebhookUpserts(t *testing.T) {
	f := newFixture(t)
	p := f.project(f.person("Ada"), "Issues")
	a, err := f.s.IssueWebhook(f.ctx, p.ID, 7, "First title", "https://x/7")
	f.must(err)
	b, err := f.s.IssueWebhook(f.ctx, p.ID, 7, "Edited title", "https://x/7")
	f.must(err)
	if a.ID != b.ID || b.Title != "Edited title" {
		t.Fatalf("%+v %+v", a, b)
	}
	if _, err := f.s.SyncIssues(f.ctx, f.person("Ada"), p.ID, "", false); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("sync without GitHub: %v", err)
	}
}

func TestRoutineOpensATaskOnceAcrossConcurrentTicks(t *testing.T) {
	f := newFixture(t)
	clock := time.Now().UTC()
	var mu sync.Mutex
	f.s.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return clock }
	ada := f.person("Ada")
	p := f.project(ada, "Pulse")
	r, err := f.s.CreateRoutine(f.ctx, ada, p.ID, NewRoutine{Name: "Update dependencies", Schedule: "24h", Enabled: true,
		Prompt: "Update dependencies to their latest compatible versions and fix what breaks."})
	f.must(err)

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() { f.s.TickRoutines(f.ctx) })
	}
	wg.Wait()
	tasks, _ := f.s.Tasks(f.ctx, p.ID)
	if len(tasks) != 1 || !strings.HasPrefix(tasks[0].Title, "Update dependencies (") ||
		!strings.Contains(tasks[0].Body, "latest compatible versions") || tasks[0].AssigneeMemberID != bot(p, models.RoleScout).ID {
		t.Fatalf("concurrent ticks opened %d Tasks: %+v", len(tasks), tasks)
	}
	runs, _ := f.s.Runs(f.ctx, tasks[0].ID)
	if len(runs) != 1 || runs[0].Kind != models.RunPlan {
		t.Fatalf("Scout starts on it: %+v", runs)
	}
	msgs, _, _ := f.s.Messages(f.ctx, ada, p.Channels[0].ID, store.Page{})
	last := msgs[len(msgs)-1]
	if last.MemberID != bot(p, models.RolePulse).ID || !strings.Contains(last.Body, "→ Scout") || last.TaskID == "" {
		t.Fatalf("Pulse opens the Task's thread in #tasks: %+v", msgs)
	}
	if n := f.inbox(ada); len(n) != 1 || n[0].Kind != "routine" {
		t.Fatalf("inbox: %+v", n)
	}

	f.s.TickRoutines(f.ctx) // not due again for 24h
	mu.Lock()
	clock = clock.Add(25 * time.Hour)
	mu.Unlock()
	f.s.TickRoutines(f.ctx) // due, but the last Task is still open
	if tasks, _ = f.s.Tasks(f.ctx, p.ID); len(tasks) != 1 {
		t.Fatalf("a Routine waits for its open Task: %d Tasks", len(tasks))
	}
	done := "done"
	_, err = f.s.UpdateTask(f.ctx, ada, tasks[0].ID, TaskPatch{Status: &done})
	f.must(err)
	mu.Lock()
	clock = clock.Add(25 * time.Hour)
	mu.Unlock()
	f.s.TickRoutines(f.ctx)
	if tasks, _ = f.s.Tasks(f.ctx, p.ID); len(tasks) != 2 {
		t.Fatalf("fires again once the Task is done: %d Tasks", len(tasks))
	}
	if _, err := f.s.CreateRoutine(f.ctx, ada, p.ID, NewRoutine{Name: "x"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a Routine needs a prompt: %v", err)
	}
	pulse := bot(p, models.RolePulse).ID
	if _, err := f.s.UpdateRoutine(f.ctx, ada, r.ID, RoutinePatch{BotMemberID: &pulse}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("Routine Tasks go to a Bot that works on Tasks: %v", err)
	}
}

func TestRoutineScheduleValidation(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "S")
	if _, err := f.s.CreateRoutine(f.ctx, ada, p.ID, NewRoutine{Name: "x", Prompt: "y", Schedule: "5s"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("sub-minute schedule: %v", err)
	}
	if !Due(models.Routine{Enabled: true}, time.Now()) {
		t.Fatal("never fired is due")
	}
}

// --- events and replay ---

func TestEventsArePublishedOnlyAfterCommit(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "E")
	published := len(f.pub.topic("project:" + p.ID))
	// The Task and its Activity are written, then the unknown handoff role
	// fails the transaction: nothing may be published or persisted.
	_, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "doomed", HandoffRole: "astronaut"})
	if !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("got %v", err)
	}
	if got := len(f.pub.topic("project:" + p.ID)); got != published {
		t.Fatalf("a rolled-back transaction published %d events", got-published)
	}
	if tasks, _ := f.s.Tasks(f.ctx, p.ID); len(tasks) != 0 {
		t.Fatalf("rolled-back Task persisted: %+v", tasks)
	}
}

func TestReplayCatchesUpEachTopic(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Replay")
	ch := p.Channels[0].ID
	first, _ := f.s.PostMessage(f.ctx, ada, ch, "one")
	f.s.PostMessage(f.ctx, ada, ch, "two")
	f.s.PostMessage(f.ctx, ada, ch, "three")
	evs, err := f.s.Replay(f.ctx, "channel:"+ch, first.Seq)
	f.must(err)
	if len(evs) != 2 || evs[0].Data.(Posted).Body != "two" || evs[1].Cursor <= evs[0].Cursor {
		t.Fatalf("channel replay: %+v", evs)
	}
	acts, err := f.s.Replay(f.ctx, "project:"+p.ID, 0)
	f.must(err)
	for i := 1; i < len(acts); i++ {
		if acts[i].Cursor <= acts[i-1].Cursor {
			t.Fatal("project replay must be oldest first")
		}
	}
	if _, err := f.s.Replay(f.ctx, "bogus:1", 0); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("unknown topic: %v", err)
	}
}

func TestRenameChangesTheNameEverywhere(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	f.person("Bob")
	p := f.project(ada, "Names")
	got, err := f.s.Rename(f.ctx, ada, "Ada L")
	f.must(err)
	if got.Name != "Ada L" || f.memberOf(p.ID, ada).DisplayName != "Ada L" {
		t.Fatalf("renamed: %+v %+v", got, f.memberOf(p.ID, ada))
	}
	if _, err := f.s.Rename(f.ctx, ada, "bob"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("names are unique, ignoring case: %v", err)
	}
	if acts := f.activity(p.ID); acts[0].Action != "updated" {
		t.Fatalf("activity: %+v", acts[0])
	}
}

func TestAgentImageNames(t *testing.T) {
	for _, ok := range []string{"buildbee-agents", "ghcr.io/acme/agents:1.2", "registry.lan:5000/team/agents:latest",
		"agents@sha256:" + strings.Repeat("a", 64), "localhost:5000/x"} {
		if !imageRef.MatchString(ok) {
			t.Errorf("refused %q", ok)
		}
	}
	for _, bad := range []string{"-v", "--privileged", "agents x", "Agents", "a:b:c:d", "../x", ""} {
		if imageRef.MatchString(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Img")
	img := "ghcr.io/acme/agents:2"
	got, err := f.s.UpdateProject(f.ctx, ada, p.ID, ProjectPatch{AgentImage: &img})
	f.must(err)
	if got.AgentImage != img {
		t.Fatalf("saved: %+v", got)
	}
	bad := "--privileged"
	if _, err := f.s.UpdateProject(f.ctx, ada, p.ID, ProjectPatch{AgentImage: &bad}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("bad image: %v", err)
	}
}
