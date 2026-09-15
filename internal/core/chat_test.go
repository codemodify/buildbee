package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
)

func TestThreadsKeepRepliesOutOfTheChannel(t *testing.T) {
	f := newFixture(t)
	ada, bob := f.person("Ada"), f.person("Bob")
	p := f.project(ada, "Chat")
	lunch, err := f.s.CreateChannel(f.ctx, ada, p.ID, "lunch")
	f.must(err)
	general := lunch.ID
	root, err := f.s.PostMessage(f.ctx, ada, general, "Lunch?")
	f.must(err)
	_, err = f.s.Reply(f.ctx, bob, root.ID, "Sure")
	f.must(err)
	second, err := f.s.Reply(f.ctx, ada, root.ID, "Noon")
	f.must(err)
	if _, err := f.s.Reply(f.ctx, bob, second.ID, "OK"); err != nil { // replying to a reply stays in the thread
		t.Fatal(err)
	}
	msgs, _, err := f.s.Messages(f.ctx, ada, general, store.Page{})
	f.must(err)
	if len(msgs) != 1 || msgs[0].ReplyCount != 3 || msgs[0].LastReplyAt == nil {
		t.Fatalf("the channel shows the root with its reply count: %+v", msgs)
	}
	th, err := f.s.Thread(f.ctx, bob, second.ID, store.Page{})
	f.must(err)
	if th.Root.ID != root.ID || len(th.Replies) != 3 || th.Replies[2].Body != "OK" {
		t.Fatalf("thread: %+v", th)
	}
	if _, err := f.s.PostMessage(f.ctx, ada, "00000000-0000-0000-0000-000000000000", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown channel: %v", err)
	}
}

func TestTaskThreadsCarryTheWork(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Work")
	tc, err := f.s.CreateTask(f.ctx, ada, p.ID, NewTask{Title: "Add caching", HandoffRole: "none"})
	f.must(err)
	if tc.ThreadID == "" {
		t.Fatal("every Task gets a thread")
	}
	h, err := f.s.CreateHandoff(f.ctx, ada, tc.ID, NewHandoff{ToRole: "builder", AutoRun: true})
	f.must(err)
	// A reply in the thread reaches the agent: in the prompt while queued...
	_, err = f.s.Reply(f.ctx, ada, tc.ThreadID, "Use Redis, not memory.")
	f.must(err)
	r, _ := f.s.Run(f.ctx, h.Run.ID)
	if !strings.Contains(r.Prompt, "Message from Ada: Use Redis, not memory.") {
		t.Fatalf("prompt: %s", r.Prompt)
	}
	// ...as a steer event once it runs.
	w := WorkerActor("w1")
	_, err = f.s.ClaimRun(f.ctx, w, []string{"claude"}, 0)
	f.must(err)
	_, err = f.s.Reply(f.ctx, ada, tc.ThreadID, "And add a TTL.")
	f.must(err)
	evs, _, _ := f.s.RunEvents(f.ctx, r.ID, 0, 0)
	if last := evs[len(evs)-1]; last.Kind != models.RunEventSteer || last.Payload["text"] != "And add a TTL." {
		t.Fatalf("events: %+v", evs)
	}
	_, err = f.s.ReportRun(f.ctx, w, r.ID, RunReport{Status: "succeeded", Summary: "Added a Redis cache with a TTL.", Branch: "buildbee/cache-1"})
	f.must(err)
	// @Sentry in the thread hands this Task on; it does not open another.
	posted, err := f.s.Reply(f.ctx, ada, tc.ThreadID, "@Sentry please review")
	f.must(err)
	if len(posted.Tasks) != 1 || posted.Tasks[0].ID != tc.ID || posted.Tasks[0].AssigneeMemberID != bot(p, models.RoleSentry).ID {
		t.Fatalf("posted: %+v", posted)
	}
	if tasks, _ := f.s.Tasks(f.ctx, p.ID); len(tasks) != 1 {
		t.Fatalf("still one Task: %d", len(tasks))
	}
	th, err := f.s.Thread(f.ctx, ada, tc.ThreadID, store.Page{Limit: 100})
	f.must(err)
	var said []string
	for _, m := range th.Replies {
		said = append(said, m.Body)
	}
	all := strings.Join(said, "\n")
	for _, want := range []string{"Building.", "Pushed buildbee/cache-1.", "Added a Redis cache", "Reviewing."} {
		if !strings.Contains(all, want) {
			t.Fatalf("thread lacks %q:\n%s", want, all)
		}
	}
}

func TestDMs(t *testing.T) {
	f := newFixture(t)
	ada, bob, cy := f.person("Ada"), f.person("Bob"), f.person("Cy")
	p := f.project(ada, "DMs")
	dm, err := f.s.OpenDirect(f.ctx, ada, NewDirect{PersonIDs: []string{bob.PersonID, bob.PersonID}})
	f.must(err)
	if dm.Kind != models.ChannelDM || len(dm.Members) != 2 || dm.Name != "Bob" {
		t.Fatalf("dm: %+v", dm)
	}
	// A DM is talk between people: mentioning a Bot there starts nothing.
	posted, err := f.s.PostMessage(f.ctx, ada, dm.ID, "@Builder bump the Go version")
	f.must(err)
	if len(posted.Tasks) != 0 {
		t.Fatalf("posted: %+v", posted)
	}
	// Others do not see it.
	if dms, _ := f.s.DirectMessages(f.ctx, cy); len(dms) != 0 {
		t.Fatalf("Cy sees %+v", dms)
	}
	if _, _, err := f.s.Messages(f.ctx, cy, dm.ID, store.Page{}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Cy reads the DM: %v", err)
	}
	if err := f.s.CloseDM(f.ctx, cy, dm.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Cy closes the DM: %v", err)
	}
	if chs, _ := f.s.Channels(f.ctx, p.ID, false); len(chs) != 1 {
		t.Fatalf("DMs are not Channels: %+v", chs)
	}
	if _, err := f.s.OpenDirect(f.ctx, ada, NewDirect{PersonIDs: []string{ada.PersonID}}); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("a DM with yourself only: %v", err)
	}
	// Closing hides it until someone writes, or you open it again.
	listed := func(a Actor) bool {
		dms, err := f.s.DirectMessages(f.ctx, a)
		f.must(err)
		return len(dms) == 1
	}
	f.must(f.s.CloseDM(f.ctx, bob, dm.ID))
	if listed(bob) || !listed(ada) {
		t.Fatal("closing is for the closer only")
	}
	_, err = f.s.PostMessage(f.ctx, ada, dm.ID, "still there?")
	f.must(err)
	if !listed(bob) {
		t.Fatal("a new message brings it back")
	}
	f.must(f.s.CloseDM(f.ctx, bob, dm.ID))
	_, err = f.s.OpenDirect(f.ctx, bob, NewDirect{PersonIDs: []string{ada.PersonID}})
	f.must(err)
	if !listed(bob) {
		t.Fatal("opening it again brings it back")
	}
	if msgs, _, _ := f.s.Messages(f.ctx, bob, dm.ID, store.Page{}); len(msgs) != 2 {
		t.Fatalf("closing deletes nothing: %+v", msgs)
	}
}

func TestUnreadCounts(t *testing.T) {
	f := newFixture(t)
	ada, bob := f.person("Ada"), f.person("Bob")
	p := f.project(ada, "Unread")
	chat, err := f.s.CreateChannel(f.ctx, ada, p.ID, "chat")
	f.must(err)
	general := chat.ID
	_, err = f.s.PostMessage(f.ctx, ada, general, "mine")
	f.must(err)
	for _, m := range []string{"one", "two"} {
		_, err := f.s.PostMessage(f.ctx, bob, general, m)
		f.must(err)
	}
	unread := func() models.Unread {
		t.Helper()
		us, err := f.s.Unread(f.ctx, ada, p.ID)
		f.must(err)
		for _, u := range us {
			if u.ChannelID == general {
				return u
			}
		}
		t.Fatalf("no unread entry for the channel: %+v", us)
		return models.Unread{}
	}
	u := unread()
	if u.Unread != 2 {
		t.Fatalf("Ada has two unread from Bob: %+v", u)
	}
	f.must(f.s.MarkChannelRead(f.ctx, ada, general, u.LastSeq))
	f.must(f.s.MarkChannelRead(f.ctx, ada, general, 1)) // markers never move back
	if u := unread(); u.Unread != 0 {
		t.Fatalf("after reading: %+v", u)
	}
}

func TestEventsPublishInCursorOrderPerTopic(t *testing.T) {
	in := []Event{{Topic: "a", Cursor: 5}, {Topic: "b", Cursor: 1}, {Topic: "a", Cursor: 3}, {Topic: "b", Cursor: 2}}
	out := inTopicOrder(in)
	want := []Event{{Topic: "a", Cursor: 3}, {Topic: "b", Cursor: 1}, {Topic: "a", Cursor: 5}, {Topic: "b", Cursor: 2}}
	for i := range want {
		if out[i].Topic != want[i].Topic || out[i].Cursor != want[i].Cursor {
			t.Fatalf("got %+v", out)
		}
	}
}

func TestPresence(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	release := f.s.Online(ada.PersonID)
	second := f.s.Online(ada.PersonID) // another tab
	p, err := f.s.Presence(f.ctx)
	f.must(err)
	if len(p.People) != 1 || p.People[0].Name != "Ada" {
		t.Fatalf("presence: %+v", p)
	}
	release()
	release() // idempotent
	if p, _ := f.s.Presence(f.ctx); len(p.People) != 1 {
		t.Fatal("still online in the other tab")
	}
	second()
	if p, _ := f.s.Presence(f.ctx); len(p.People) != 0 {
		t.Fatalf("offline: %+v", p)
	}
	if _, err := f.s.ClaimRun(f.ctx, WorkerActor("gpu-box"), []string{"claude", "codex"}, 0); err != nil {
		t.Fatal(err)
	}
	p, _ = f.s.Presence(f.ctx)
	if len(p.Workers) != 1 || p.Workers[0].Name != "gpu-box" || strings.Join(p.Workers[0].Agents, ",") != "claude,codex" {
		t.Fatalf("workers: %+v", p.Workers)
	}
	if n := len(f.pub.topic("presence:server")); n < 3 {
		t.Fatalf("presence changes are published: %d events", n)
	}
}

func TestRosterShowsTheServersPeopleAndBots(t *testing.T) {
	f := newFixture(t)
	ada, bob := f.person("Ada"), f.person("Bob")
	p := f.project(ada, "People")
	f.project(bob, "Other")
	_, err := f.s.Join(f.ctx, bob, p.ID)
	f.must(err)
	f.must(f.s.Leave(f.ctx, bob, p.ID))
	f.must(f.s.Leave(f.ctx, bob, p.ID))                            // once is enough
	_, err = f.s.PostMessage(f.ctx, bob, p.Channels[0].ID, "back") // writing brings Bob back
	f.must(err)
	_, err = f.s.AddMember(f.ctx, ada, p.ID, NewMember{Kind: "bot", DisplayName: "Docs", Role: "writer"})
	f.must(err)
	r, err := f.s.Roster(f.ctx)
	f.must(err)
	if len(r.People) != 2 || r.People[0].Name != "Ada" || len(r.People[1].Projects) != 2 {
		t.Fatalf("people: %+v", r.People)
	}
	if len(r.Bots) != 9 || r.Bots[0].ProjectName != "Other" {
		t.Fatalf("bots of both Projects: %d %+v", len(r.Bots), r.Bots[0])
	}
	var log []string
	for _, e := range r.Events {
		log = append(log, e.Name+" "+e.Action)
	}
	if got := strings.Join(log, ", "); got != "Docs added, Bob joined, Bob left, Bob joined, Bob created, Ada created" {
		t.Fatalf("events, newest first: %s", got)
	}
	msgs, _, _ := f.s.Messages(f.ctx, ada, p.Channels[0].ID, store.Page{})
	if len(msgs) != 1 || msgs[0].Body != "back" {
		t.Fatalf("no status lines in chat: %+v", msgs)
	}
}

func TestAskingElsewhereOpensTheTaskThreadInTasks(t *testing.T) {
	f := newFixture(t)
	ada := f.person("Ada")
	p := f.project(ada, "Where")
	design, err := f.s.CreateChannel(f.ctx, ada, p.ID, "design")
	f.must(err)
	posted, err := f.s.PostMessage(f.ctx, ada, design.ID, "@Scout look at the empty states")
	f.must(err)
	task := posted.Tasks[0]
	th, err := f.s.Thread(f.ctx, ada, task.ThreadID, store.Page{})
	f.must(err)
	if th.Root.ChannelID != p.Channels[0].ID || th.Root.TaskID != task.ID || posted.TaskID != task.ID || posted.ReplyCount != 0 {
		t.Fatalf("thread root %+v, message %+v", th.Root, posted.Message)
	}
	if len(th.Replies) != 1 || th.Replies[0].Body != "Planning." {
		t.Fatalf("Scout reports in the Task's thread: %+v", th.Replies)
	}
}

func TestDirectMessagesSpanTheServer(t *testing.T) {
	f := newFixture(t)
	ada, bob, cy := f.person("Ada"), f.person("Bob"), f.person("Cy")
	f.project(ada, "Payments")
	f.project(bob, "Website")
	// Ada and Bob share no Project; a DM needs none.
	dm, err := f.s.OpenDirect(f.ctx, ada, NewDirect{PersonIDs: []string{bob.PersonID}})
	f.must(err)
	again, err := f.s.OpenDirect(f.ctx, bob, NewDirect{PersonIDs: []string{ada.PersonID}})
	f.must(err)
	if again.ID != dm.ID {
		t.Fatal("one conversation between two people, whoever opens it")
	}
	_, err = f.s.PostMessage(f.ctx, ada, dm.ID, "hi Bob")
	f.must(err)
	if n := f.inbox(bob); len(n) != 1 || n[0].Kind != "dm" || n[0].Title != "Ada messaged you" {
		t.Fatalf("bob's inbox: %+v", n)
	}
	if n := f.inbox(ada); len(n) != 0 {
		t.Fatalf("no notice for the author: %+v", n)
	}
	if _, err := f.s.PostMessage(f.ctx, cy, dm.ID, "let me in"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("others cannot write in it: %v", err)
	}
	list, err := f.s.DirectMessages(f.ctx, bob)
	f.must(err)
	if len(list) != 1 || list[0].Unread != 1 || len(list[0].With) != 1 || list[0].With[0].Name != "Ada" {
		t.Fatalf("bob's DMs: %+v", list)
	}
	// The direct space is not a Project anyone sees or changes.
	ps, err := f.s.Projects(f.ctx, false)
	f.must(err)
	if len(ps) != 2 {
		t.Fatalf("projects: %+v", ps)
	}
	if _, err := f.s.CreateChannel(f.ctx, ada, dm.ProjectID, "side"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("no channels in the direct space: %v", err)
	}
	if _, err := f.s.CreateTask(f.ctx, ada, dm.ProjectID, NewTask{Title: "x"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("no tasks in the direct space: %v", err)
	}
	r, err := f.s.Roster(f.ctx)
	f.must(err)
	for _, p := range r.People {
		for _, m := range p.Projects {
			if m.ProjectID == dm.ProjectID {
				t.Fatalf("roster shows the direct space: %+v", p)
			}
		}
	}
}
