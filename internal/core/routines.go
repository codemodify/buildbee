package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codemodify/buildbee/internal/githubconn"
	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/google/uuid"
)

// --- issues ---

// SyncIssues imports the Repo's open Issues as Tasks. fake imports two
// sample Issues instead (tests and demos only).
func (s *Service) SyncIssues(ctx context.Context, a Actor, projectID, repo string, fake bool) ([]models.Task, error) {
	if repo = strings.TrimSpace(repo); repo == "" {
		repo = s.github.Repo
	}
	var issues []githubconn.Issue
	switch {
	case fake:
		base := "https://github.com/example/buildbee/issues/"
		issues = []githubconn.Issue{{Number: 1, Title: "Sample Issue: welcome", HTMLURL: base + "1"},
			{Number: 2, Title: "Sample Issue: follow-up", HTMLURL: base + "2"}}
	case s.github.Token == "" || repo == "":
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, githubconn.ErrNotConfigured)
	default:
		var err error
		if issues, err = githubconn.ListOpenIssues(ctx, s.github.Token, repo); err != nil {
			return nil, fmt.Errorf("%w: github: %v", ErrUnavailable, err)
		}
	}
	out := []models.Task{}
	err := s.tx(ctx, func(w *work) error {
		if _, err := w.openProject(ctx, projectID); err != nil {
			return err
		}
		m, err := w.member(ctx, a, projectID, true)
		if err != nil {
			return err
		}
		for _, is := range issues {
			t, err := w.upsertIssue(ctx, projectID, whoOf(m, a), is.Number, is.Title, is.HTMLURL)
			if err != nil {
				return err
			}
			out = append(out, *t)
		}
		return nil
	})
	return out, err
}

// IssueWebhook applies a Repo "issues" event (opened, edited, reopened).
func (s *Service) IssueWebhook(ctx context.Context, projectID string, number int, title, url string) (*models.Task, error) {
	if number <= 0 {
		return nil, invalid("issue number must be positive")
	}
	t, err := text("title", title, true, 300)
	if err != nil {
		return nil, err
	}
	var out *models.Task
	err = s.tx(ctx, func(w *work) error {
		if _, err := w.openProject(ctx, projectID); err != nil {
			return err
		}
		out, err = w.upsertIssue(ctx, projectID, who{name: "github"}, number, t, url)
		return err
	})
	return out, err
}

func (w *work) upsertIssue(ctx context.Context, projectID string, by who, number int, title, url string) (*models.Task, error) {
	t, created, err := w.st.UpsertIssueTask(ctx, projectID, number, truncate(title, 300), url, w.now)
	if err != nil {
		return nil, err
	}
	action := "updated"
	if created {
		action = "created"
	}
	return t, w.activity(ctx, projectID, by, models.TypeIssue, action, t.ID, map[string]any{"issue_number": number, "title": t.Title})
}

// --- routines ---

// ParseInterval turns a Routine schedule into an interval ("daily" = 24h).
func ParseInterval(schedule string) (time.Duration, error) {
	s := strings.TrimSpace(strings.ToLower(schedule))
	if s == "" || s == "daily" {
		return 24 * time.Hour, nil
	}
	if s == "hourly" {
		return time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < time.Minute {
		return 0, invalid("schedule must be daily, hourly or a duration of at least 1m, like 6h")
	}
	return d, nil
}

// Due reports whether an enabled Routine should fire at now.
func Due(r models.Routine, now time.Time) bool {
	if !r.Enabled {
		return false
	}
	if r.LastRunAt == nil {
		return true
	}
	d, err := ParseInterval(r.Schedule)
	if err != nil {
		return false
	}
	return now.Sub(*r.LastRunAt) >= d
}

// Routines lists a Project's Routines.
func (s *Service) Routines(ctx context.Context, projectID string) ([]models.Routine, error) {
	if _, err := s.st.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.st.ListRoutines(ctx, projectID)
}

// NewRoutine describes a Routine to create. Prompt is what the Task it
// opens asks for; BotMemberID is who gets the Task (default Scout).
type NewRoutine struct {
	Name        string `json:"name"`
	Prompt      string `json:"prompt"`
	Schedule    string `json:"schedule"`
	Enabled     bool   `json:"enabled"`
	BotMemberID string `json:"bot_member_id"`
}

// CreateRoutine adds a Routine to a Project.
func (s *Service) CreateRoutine(ctx context.Context, a Actor, projectID string, in NewRoutine) (*models.Routine, error) {
	n, err := text("name", in.Name, true, 100)
	if err != nil {
		return nil, err
	}
	prompt, err := text("prompt", in.Prompt, true, 16000)
	if err != nil {
		return nil, err
	}
	if in.Schedule == "" {
		in.Schedule = "daily"
	}
	if _, err := ParseInterval(in.Schedule); err != nil {
		return nil, err
	}
	var out *models.Routine
	err = s.tx(ctx, func(w *work) error {
		if _, err := w.openProject(ctx, projectID); err != nil {
			return err
		}
		m, err := w.member(ctx, a, projectID, true)
		if err != nil {
			return err
		}
		if in.BotMemberID != "" {
			if err := w.routineBot(ctx, projectID, in.BotMemberID); err != nil {
				return err
			}
		}
		r := models.Routine{ID: uuid.NewString(), ProjectID: projectID, BotMemberID: in.BotMemberID, Name: n, Prompt: prompt,
			Schedule: strings.TrimSpace(in.Schedule), Enabled: in.Enabled, CreatedAt: w.now}
		if err := w.st.InsertRoutine(ctx, r); err != nil {
			return err
		}
		out = &r
		return w.activity(ctx, projectID, whoOf(m, a), models.TypeRoutine, "created", r.ID,
			map[string]any{"name": r.Name, "schedule": r.Schedule, "enabled": r.Enabled})
	})
	return out, err
}

// RoutinePatch changes only the fields that are set.
type RoutinePatch struct {
	Name        *string `json:"name"`
	Prompt      *string `json:"prompt"`
	Schedule    *string `json:"schedule"`
	Enabled     *bool   `json:"enabled"`
	BotMemberID *string `json:"bot_member_id"`
}

// routineBot checks that a Routine's Tasks go to a Bot of the Project
// that works on Tasks.
func (w *work) routineBot(ctx context.Context, projectID, memberID string) error {
	m, err := w.st.GetMember(ctx, memberID)
	if err != nil || m.ProjectID != projectID || m.Kind != models.KindBot || !worksOnTasks(m.Role) {
		return invalid("bot_member_id must be this Project's Scout, Builder or Sentry")
	}
	return nil
}

// UpdateRoutine renames, reschedules, enables or disables a Routine.
func (s *Service) UpdateRoutine(ctx context.Context, a Actor, id string, patch RoutinePatch) (*models.Routine, error) {
	var out *models.Routine
	err := s.tx(ctx, func(w *work) error {
		r, err := w.st.GetRoutine(ctx, id)
		if err != nil {
			return err
		}
		if _, err := w.openProject(ctx, r.ProjectID); err != nil {
			return err
		}
		m, err := w.member(ctx, a, r.ProjectID, true)
		if err != nil {
			return err
		}
		changes := map[string]any{}
		if patch.Name != nil {
			if r.Name, err = text("name", *patch.Name, true, 100); err != nil {
				return err
			}
			changes["name"] = r.Name
		}
		if patch.Prompt != nil {
			if r.Prompt, err = text("prompt", *patch.Prompt, true, 16000); err != nil {
				return err
			}
			changes["prompt"] = true
		}
		if patch.BotMemberID != nil {
			if id := strings.TrimSpace(*patch.BotMemberID); id != "" {
				if err := w.routineBot(ctx, r.ProjectID, id); err != nil {
					return err
				}
			}
			r.BotMemberID = strings.TrimSpace(*patch.BotMemberID)
			changes["bot_member_id"] = r.BotMemberID
		}
		if patch.Schedule != nil {
			if _, err := ParseInterval(*patch.Schedule); err != nil {
				return err
			}
			r.Schedule = strings.TrimSpace(*patch.Schedule)
			changes["schedule"] = r.Schedule
		}
		if patch.Enabled != nil {
			r.Enabled = *patch.Enabled
			changes["enabled"] = r.Enabled
		}
		if err := w.st.UpdateRoutine(ctx, *r); err != nil {
			return err
		}
		out = r
		return w.activity(ctx, r.ProjectID, whoOf(m, a), models.TypeRoutine, "updated", r.ID, changes)
	})
	return out, err
}

// FireRoutine runs a Routine now, whatever its schedule.
func (s *Service) FireRoutine(ctx context.Context, a Actor, id string) (*models.Routine, error) {
	r, err := s.st.GetRoutine(ctx, id)
	if err != nil {
		return nil, err
	}
	fired, err := s.fire(ctx, a, r)
	if err != nil {
		return nil, err
	}
	if !fired {
		return nil, fmt.Errorf("%w: routine fired concurrently; try again", store.ErrConflict)
	}
	return s.st.GetRoutine(ctx, id)
}

// TickRoutines fires every due Routine once. Safe to run in several Server
// processes: each firing is claimed with a compare-and-set.
func (s *Service) TickRoutines(ctx context.Context) {
	rs, err := s.st.ListEnabledRoutines(ctx)
	if err != nil {
		s.log.Error("routines: list", "err", err)
		return
	}
	now := s.now()
	for i := range rs {
		if !Due(rs[i], now) {
			continue
		}
		if _, err := s.fire(ctx, System("routines"), &rs[i]); err != nil {
			s.log.Error("routines: fire", "routine", rs[i].ID, "err", err)
		}
	}
}

// fire claims the Routine and opens its Task: Pulse hands it to the
// Routine's Bot (Scout by default), which starts a Run. While the previous
// firing's Task is still open the Routine skips, so work does not pile up.
// Returns false if another process claimed this firing first.
func (s *Service) fire(ctx context.Context, a Actor, r *models.Routine) (bool, error) {
	fired := false
	err := s.tx(ctx, func(w *work) error {
		proj, err := w.openProject(ctx, r.ProjectID)
		if err != nil {
			return err
		}
		if fired, err = w.st.ClaimRoutine(ctx, r.ID, r.LastRunAt, w.now); err != nil || !fired {
			return err
		}
		members, err := w.st.ListMembers(ctx, r.ProjectID)
		if err != nil {
			return err
		}
		pulse := models.MemberByRole(members, models.RolePulse)
		by := whoOf(pulse, a)
		if r.LastTaskID != "" {
			if prev, err := w.st.GetTask(ctx, r.LastTaskID, false); err == nil && prev.Status != models.TaskDone && prev.Status != models.TaskCanceled {
				return w.activity(ctx, r.ProjectID, by, models.TypeRoutine, "skipped", r.ID,
					map[string]any{"name": r.Name, "open_task_id": prev.ID})
			}
		}
		var to *models.Member
		for i := range members {
			if members[i].ID == r.BotMemberID {
				to = &members[i]
			}
		}
		if to == nil {
			to = models.MemberByRole(members, models.RoleScout)
		}
		from := pulse
		if from == nil {
			from = to
		}
		if to == nil {
			return invalid("the Project has no Bot to give the Routine's Task to")
		}
		title := r.Name + " (" + w.now.Format("2006-01-02") + ")"
		task, err := w.createTask(ctx, proj, from, a, newTask{title: title, body: r.Prompt})
		if err != nil {
			return err
		}
		if err := w.openThread(ctx, proj, task, from, "Routine "+r.Name+": "+title+" → "+to.DisplayName+".", ""); err != nil {
			return err
		}
		if _, _, err := w.handoff(ctx, proj, task, from, to, a, "Scheduled by Routine "+r.Name+".", true); err != nil {
			return err
		}
		r.LastTaskID = task.ID
		if err := w.st.UpdateRoutine(ctx, *r); err != nil {
			return err
		}
		if err := w.activity(ctx, r.ProjectID, by, models.TypeRoutine, "fired", r.ID,
			map[string]any{"name": r.Name, "task_id": task.ID, "by": a.Name}); err != nil {
			return err
		}
		return w.notifyPeople(ctx, r.ProjectID, nil, notice{projectID: r.ProjectID, kind: "routine",
			title: "Routine " + r.Name + " opened a Task", body: title, href: href(r.ProjectID, "tasks", task.ID)})
	})
	return fired, err
}

// --- replay ---

// Replay returns the events of a topic after cursor, for a WebSocket
// subscriber catching up. Topics are project:, channel:, run: and person:.
func (s *Service) Replay(ctx context.Context, topic string, after int64) ([]Event, error) {
	kind, id, ok := strings.Cut(topic, ":")
	if !ok || id == "" {
		return nil, invalid("unknown topic %q", topic)
	}
	var out []Event
	switch kind {
	case "run":
		evs, _, err := s.st.ListRunEvents(ctx, id, int(after), 0)
		if err != nil {
			return nil, err
		}
		for _, ev := range evs {
			out = append(out, Event{Topic: topic, Cursor: int64(ev.Seq), Type: "run_event", Data: ev})
		}
	case "channel":
		msgs, _, err := s.st.ListChannelSince(ctx, id, after, 1000)
		if err != nil {
			return nil, err
		}
		for _, m := range msgs {
			out = append(out, Event{Topic: topic, Cursor: m.Seq, Type: "message", Data: Posted{Message: m}})
		}
	case "project":
		acts, _, err := s.st.ListActivity(ctx, id, "", store.Page{After: after, Limit: 1000})
		if err != nil {
			return nil, err
		}
		for i := len(acts) - 1; i >= 0; i-- { // oldest first
			out = append(out, Event{Topic: topic, Cursor: acts[i].Seq, Type: "activity", Data: acts[i]})
		}
	case "presence": // live only: subscribers fetch GET /v1/presence
		return nil, nil
	case "person":
		ns, _, err := s.st.ListNotifications(ctx, id, false, store.Page{After: after, Limit: 1000})
		if err != nil {
			return nil, err
		}
		for i := len(ns) - 1; i >= 0; i-- {
			out = append(out, Event{Topic: topic, Cursor: ns[i].Seq, Type: "notification", Data: ns[i]})
		}
	default:
		return nil, invalid("unknown topic %q", topic)
	}
	return out, nil
}
