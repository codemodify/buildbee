package store

import (
	"context"
	"sync"
	"testing"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/testdb"
)

func TestMain(m *testing.M) { testdb.Main(m) }

func TestUpsertIssueTaskIsAtomic(t *testing.T) {
	ctx := context.Background()
	st := NewPostgres(testdb.New(t))
	proj, err := st.CreateProject(ctx, "Issues", false)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.UpsertIssueTask(ctx, proj.ID, 7, "Issue seven", "https://example.test/7"); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	tasks, err := st.ListTasks(ctx, proj.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, tk := range tasks {
		if tk.IssueNumber == 7 {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("concurrent upserts created %d Tasks for issue #7, want 1", n)
	}
	if _, err := st.UpsertIssueTask(ctx, proj.ID, 0, "zero", ""); err == nil {
		t.Fatal("issue number 0 must be rejected")
	}
}

func TestDeleteProjectCascadesThroughMembers(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	st := NewPostgres(pool)
	proj, err := st.CreateProject(ctx, "Doomed", false)
	if err != nil {
		t.Fatal(err)
	}
	human := proj.Members[0]
	scout := models.MemberByRole(proj.Members, models.RoleScout)
	if _, err := st.PostMessage(ctx, proj.Channels[0].ID, human.ID, "hello"); err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(ctx, proj.ID, "work", scout.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateHandoff(ctx, task.ID, human.ID, scout.ID, "go"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDecision(ctx, proj.ID, "Ship?", "yes", []string{"yes", "no"}, human.ID); err != nil {
		t.Fatal(err)
	}
	// Used to fail with "violates foreign key constraint" from messages/handoffs.
	if _, err := pool.Exec(ctx, `DELETE FROM projects WHERE id=$1`, proj.ID); err != nil {
		t.Fatalf("deleting a used Project: %v", err)
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM members WHERE project_id=$1`, proj.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("members left: %d %v", left, err)
	}
}
