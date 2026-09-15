package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/testdb"
	"github.com/google/uuid"
)

func TestMain(m *testing.M) { testdb.Main(m) }

type seeded struct {
	project models.Project
	person  *models.Person
	human   models.Member
	bot     models.Member
	channel models.Channel
}

func seed(t *testing.T, s *Store) seeded {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	var out seeded
	out.project = models.Project{ID: uuid.NewString(), Name: "P", CreatedAt: now}
	must(t, s.InsertProject(ctx, out.project))
	p, err := s.UpsertPerson(ctx, "Ada")
	must(t, err)
	out.person = p
	out.human = models.Member{ID: uuid.NewString(), ProjectID: out.project.ID, PersonID: p.ID, Kind: models.KindHuman,
		DisplayName: "Ada", Role: models.RoleOwner, CreatedAt: now}
	must(t, s.InsertMember(ctx, out.human))
	out.bot = models.Member{ID: uuid.NewString(), ProjectID: out.project.ID, Kind: models.KindBot,
		DisplayName: "Scout", Role: models.RoleScout, CreatedAt: now}
	must(t, s.InsertMember(ctx, out.bot))
	out.channel = models.Channel{ID: uuid.NewString(), ProjectID: out.project.ID, Name: "general", CreatedAt: now}
	must(t, s.InsertChannel(ctx, out.channel))
	return out
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestUpsertIssueTaskIsAtomic(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.New(t))
	sd := seed(t, s)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.UpsertIssueTask(ctx, sd.project.ID, 7, "Issue seven", "https://x/7", time.Now()); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	tasks, err := s.ListTasks(ctx, sd.project.ID)
	must(t, err)
	if len(tasks) != 1 || tasks[0].IssueNumber != 7 {
		t.Fatalf("concurrent upserts produced %d Tasks", len(tasks))
	}
	if _, _, err := s.UpsertIssueTask(ctx, sd.project.ID, 0, "zero", "", time.Now()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("issue number 0: %v", err)
	}
}

func TestDeleteProjectCascadesThroughMembers(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	s := New(pool)
	sd := seed(t, s)
	now := time.Now().UTC()
	_, err := s.InsertMessage(ctx, models.Message{ID: uuid.NewString(), ChannelID: sd.channel.ID, ProjectID: sd.project.ID,
		MemberID: sd.human.ID, Body: "hi", CreatedAt: now})
	must(t, err)
	task := models.Task{ID: uuid.NewString(), ProjectID: sd.project.ID, Title: "t", Status: models.TaskOpen,
		AssigneeMemberID: sd.bot.ID, CreatedByMemberID: sd.human.ID, CreatedAt: now}
	must(t, s.InsertTask(ctx, task))
	must(t, s.InsertHandoff(ctx, models.Handoff{ID: uuid.NewString(), TaskID: task.ID, ProjectID: sd.project.ID,
		FromMemberID: sd.human.ID, ToMemberID: sd.bot.ID, Status: models.HandoffOpen, CreatedAt: now}))
	_, err = s.InsertActivity(ctx, models.Activity{ProjectID: sd.project.ID, ActorMemberID: sd.human.ID, Actor: "Ada",
		Type: "task", Action: "created", CreatedAt: now})
	must(t, err)
	if _, err := pool.Exec(ctx, `DELETE FROM projects WHERE id=$1`, sd.project.ID); err != nil {
		t.Fatalf("deleting a used Project: %v", err)
	}
	var left int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM members WHERE project_id=$1`, sd.project.ID).Scan(&left))
	if left != 0 {
		t.Fatalf("%d members left", left)
	}
}

func TestMessagePaging(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.New(t))
	sd := seed(t, s)
	var seqs []int64
	for i := 0; i < 7; i++ {
		m, err := s.InsertMessage(ctx, models.Message{ID: uuid.NewString(), ChannelID: sd.channel.ID, ProjectID: sd.project.ID,
			MemberID: sd.human.ID, Body: fmt.Sprint(i), CreatedAt: time.Now()})
		must(t, err)
		seqs = append(seqs, m.Seq)
	}
	latest, more, err := s.ListMessages(ctx, sd.channel.ID, Page{Limit: 3})
	must(t, err)
	if !more || len(latest) != 3 || latest[0].Body != "4" || latest[2].Body != "6" {
		t.Fatalf("latest page (oldest first): more=%v %+v", more, latest)
	}
	older, more, err := s.ListMessages(ctx, sd.channel.ID, Page{Before: latest[0].Seq, Limit: 3})
	must(t, err)
	if !more || len(older) != 3 || older[0].Body != "1" || older[2].Body != "3" {
		t.Fatalf("older page: more=%v %+v", more, older)
	}
	newer, more, err := s.ListMessages(ctx, sd.channel.ID, Page{After: seqs[4], Limit: 10})
	must(t, err)
	if more || len(newer) != 2 || newer[0].Body != "5" {
		t.Fatalf("newer page: more=%v %+v", more, newer)
	}
}

func TestActivityPagesNewestFirst(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.New(t))
	sd := seed(t, s)
	for i := 0; i < 5; i++ {
		_, err := s.InsertActivity(ctx, models.Activity{ProjectID: sd.project.ID, Actor: "system", Type: "task",
			Action: fmt.Sprint(i), CreatedAt: time.Now()})
		must(t, err)
	}
	first, more, err := s.ListActivity(ctx, sd.project.ID, "", Page{Limit: 2})
	must(t, err)
	if !more || first[0].Action != "4" || first[1].Action != "3" {
		t.Fatalf("first page: %+v", first)
	}
	next, _, err := s.ListActivity(ctx, sd.project.ID, "", Page{Before: first[1].Seq, Limit: 2})
	must(t, err)
	if next[0].Action != "2" || next[1].Action != "1" {
		t.Fatalf("next page: %+v", next)
	}
	newer, _, err := s.ListActivity(ctx, sd.project.ID, "", Page{After: next[0].Seq})
	must(t, err)
	if len(newer) != 2 || newer[0].Action != "4" {
		t.Fatalf("after cursor, newest first: %+v", newer)
	}
}

func TestJoinPersonIsRaceSafe(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.New(t))
	sd := seed(t, s)
	bob, err := s.UpsertPerson(ctx, "Bob")
	must(t, err)
	var wg sync.WaitGroup
	ids := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, _, err := s.JoinPerson(ctx, models.Member{ID: uuid.NewString(), ProjectID: sd.project.ID, PersonID: bob.ID,
				DisplayName: "Bob", Role: models.RoleMember, CreatedAt: time.Now()})
			if err != nil {
				t.Error(err)
				return
			}
			ids <- m.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Fatalf("one Person joined as %d Members", len(seen))
	}
}

func TestClaimRoutineIsCompareAndSet(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.New(t))
	sd := seed(t, s)
	r := models.Routine{ID: uuid.NewString(), ProjectID: sd.project.ID, Name: "digest", Schedule: "24h", Enabled: true, CreatedAt: time.Now()}
	must(t, s.InsertRoutine(ctx, r))
	now := time.Now().UTC()
	ok1, err := s.ClaimRoutine(ctx, r.ID, nil, now)
	must(t, err)
	ok2, err := s.ClaimRoutine(ctx, r.ID, nil, now.Add(time.Second))
	must(t, err)
	if !ok1 || ok2 {
		t.Fatalf("claims: first=%v second=%v", ok1, ok2)
	}
}

func TestConditionalUpdatesRefuseSecondAttempt(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.New(t))
	sd := seed(t, s)
	now := time.Now().UTC()
	task := models.Task{ID: uuid.NewString(), ProjectID: sd.project.ID, Title: "t", Status: models.TaskOpen, CreatedAt: now}
	must(t, s.InsertTask(ctx, task))
	h := models.Handoff{ID: uuid.NewString(), TaskID: task.ID, ProjectID: sd.project.ID, FromMemberID: sd.human.ID,
		ToMemberID: sd.bot.ID, Status: models.HandoffOpen, CreatedAt: now}
	must(t, s.InsertHandoff(ctx, h))
	_, err := s.CompleteHandoff(ctx, h.ID, now)
	must(t, err)
	if _, err := s.CompleteHandoff(ctx, h.ID, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("second completion: %v", err)
	}
	if _, err := s.CompleteHandoff(ctx, uuid.NewString(), now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown handoff: %v", err)
	}
	d := models.Decision{ID: uuid.NewString(), ProjectID: sd.project.ID, Prompt: "?", CreatedAt: now}
	must(t, s.InsertDecision(ctx, d))
	_, err = s.AnswerDecision(ctx, d.ID, "yes", sd.human.ID, now)
	must(t, err)
	if _, err := s.AnswerDecision(ctx, d.ID, "no", sd.human.ID, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("second answer: %v", err)
	}
}

func TestSchemaRejectsBadStatusAndHumansWithoutPerson(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.New(t))
	sd := seed(t, s)
	err := s.InsertTask(ctx, models.Task{ID: uuid.NewString(), ProjectID: sd.project.ID, Title: "t", Status: "Done ", CreatedAt: time.Now()})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad task status: %v", err)
	}
	err = s.InsertMember(ctx, models.Member{ID: uuid.NewString(), ProjectID: sd.project.ID, Kind: models.KindHuman,
		DisplayName: "Ghost", Role: models.RoleMember, CreatedAt: time.Now()})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("human without a Person: %v", err)
	}
}

func TestTxRollsBackEverything(t *testing.T) {
	ctx := context.Background()
	s := New(testdb.New(t))
	sd := seed(t, s)
	boom := errors.New("boom")
	err := s.Tx(ctx, func(tx *Store) error {
		if _, err := tx.InsertMessage(ctx, models.Message{ID: uuid.NewString(), ChannelID: sd.channel.ID, ProjectID: sd.project.ID,
			MemberID: sd.human.ID, Body: "lost", CreatedAt: time.Now()}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
	msgs, _, _ := s.ListMessages(ctx, sd.channel.ID, Page{})
	if len(msgs) != 0 {
		t.Fatalf("rolled-back message persisted: %+v", msgs)
	}
}
