package core

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/google/uuid"
)

// --- messages ---

// Messages returns one page of a Channel, oldest first.
func (s *Service) Messages(ctx context.Context, channelID string, p store.Page) ([]models.Message, bool, error) {
	if _, err := s.st.GetChannel(ctx, channelID); err != nil {
		return nil, false, err
	}
	return s.st.ListMessages(ctx, channelID, p)
}

// Posted is a new message plus what its @mentions set in motion.
type Posted struct {
	models.Message
	Mentions []models.Member  `json:"mentions"`
	Tasks    []models.Task    `json:"tasks"`
	Handoffs []models.Handoff `json:"handoffs"`
}

// PostMessage posts to a Channel as the acting Person. Each @Bot mentioned
// gets a Task (the message is its description) handed to it.
func (s *Service) PostMessage(ctx context.Context, a Actor, channelID, body string) (*Posted, error) {
	b, err := text("body", body, true, 32000)
	if err != nil {
		return nil, err
	}
	var out *Posted
	err = s.tx(ctx, func(w *work) error {
		ch, err := w.st.GetChannel(ctx, channelID)
		if err != nil {
			return err
		}
		if ch.ArchivedAt != nil {
			return fmt.Errorf("%w: channel #%s is archived", store.ErrConflict, ch.Name)
		}
		proj, err := w.openProject(ctx, ch.ProjectID)
		if err != nil {
			return err
		}
		author, err := w.requireMember(ctx, a, ch.ProjectID)
		if err != nil {
			return err
		}
		out, err = w.post(ctx, proj, ch, author, a, b)
		return err
	})
	return out, err
}

// post writes a message by author and acts on its @mentions.
func (w *work) post(ctx context.Context, proj *models.Project, ch *models.Channel, author *models.Member, a Actor, body string) (*Posted, error) {
	msg, err := w.st.InsertMessage(ctx, models.Message{ID: uuid.NewString(), ChannelID: ch.ID, ProjectID: ch.ProjectID,
		MemberID: author.ID, Body: body, CreatedAt: w.now})
	if err != nil {
		return nil, err
	}
	out := &Posted{Message: *msg, Mentions: []models.Member{}, Tasks: []models.Task{}, Handoffs: []models.Handoff{}}
	members, err := w.st.ListMembers(ctx, ch.ProjectID)
	if err != nil {
		return nil, err
	}
	for _, p := range models.MentionedPeople(body, members) {
		if p.ID == author.ID {
			continue
		}
		if err := w.notifyMember(ctx, &p, notice{projectID: ch.ProjectID, kind: "mention",
			title: author.DisplayName + " mentioned you in #" + ch.Name, body: truncate(body, 500),
			href: href(ch.ProjectID, "channels", ch.ID)}); err != nil {
			return nil, err
		}
	}
	if author.Kind == models.KindHuman {
		for _, bot := range models.MentionedBots(body, members) {
			bot := bot
			task, err := w.createTask(ctx, proj, author, a, newTask{title: mentionTitle(body, ch.Name), body: body})
			if err != nil {
				return nil, err
			}
			h, t, err := w.handoff(ctx, proj, task, author, &bot, a, body, false)
			if err != nil {
				return nil, err
			}
			out.Mentions = append(out.Mentions, bot)
			out.Tasks = append(out.Tasks, *t)
			out.Handoffs = append(out.Handoffs, *h)
		}
	}
	w.emit("channel:"+ch.ID, msg.Seq, "message", out)
	return out, nil
}

var leadingMentions = regexp.MustCompile(`^(?:@[\p{L}\p{N}_.-]+[\s,:;]*)+`)

// mentionTitle names a Task created by a mention after what was asked: the
// message's first line without the leading @mentions.
func mentionTitle(body, channel string) string {
	t := strings.TrimSpace(leadingMentions.ReplaceAllString(firstLine(body), ""))
	if t == "" {
		t = "Request from #" + channel
	}
	return truncate(t, 120)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// --- tasks ---

// Tasks lists a Project's Tasks, newest first.
func (s *Service) Tasks(ctx context.Context, projectID string) ([]models.Task, error) {
	if _, err := s.st.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.st.ListTasks(ctx, projectID)
}

// Task returns one Task.
func (s *Service) Task(ctx context.Context, id string) (*models.Task, error) {
	return s.st.GetTask(ctx, id, false)
}

// TaskDetail returns a Task with its Handoffs, Runs, Artifacts (without
// bodies) and Pipelines.
func (s *Service) TaskDetail(ctx context.Context, id string) (*models.TaskDetail, error) {
	t, err := s.st.GetTask(ctx, id, false)
	if err != nil {
		return nil, err
	}
	d := &models.TaskDetail{Task: *t}
	if d.Handoffs, err = s.st.ListHandoffs(ctx, id); err != nil {
		return nil, err
	}
	if d.Runs, err = s.st.ListRuns(ctx, id); err != nil {
		return nil, err
	}
	if d.Artifacts, err = s.st.ListArtifacts(ctx, id); err != nil {
		return nil, err
	}
	if d.Pipelines, err = s.st.ListPipelines(ctx, id); err != nil {
		return nil, err
	}
	return d, nil
}

// NewTask describes a Task to create. HandoffRole picks the Bot it is handed
// to on creation: "" means Scout, "none" means nobody.
type NewTask struct {
	Title            string `json:"title"`
	Body             string `json:"body"`
	AssigneeMemberID string `json:"assignee_member_id"`
	HandoffRole      string `json:"handoff_role"`
	HandoffNote      string `json:"handoff_note"`
}

// TaskCreated is a new Task and the Handoff made with it, if any.
type TaskCreated struct {
	models.Task
	Handoff *models.Handoff `json:"handoff,omitempty"`
	Run     *models.Run     `json:"run,omitempty"`
}

// CreateTask opens a Task in a Project and, by default, hands it to Scout.
func (s *Service) CreateTask(ctx context.Context, a Actor, projectID string, in NewTask) (*TaskCreated, error) {
	title, err := text("title", in.Title, true, 300)
	if err != nil {
		return nil, err
	}
	body, err := text("body", in.Body, false, 32000)
	if err != nil {
		return nil, err
	}
	note, err := text("handoff_note", in.HandoffNote, false, 4000)
	if err != nil {
		return nil, err
	}
	role := strings.ToLower(strings.TrimSpace(in.HandoffRole))
	if role == "" {
		role = models.RoleScout
	}
	var out *TaskCreated
	err = s.tx(ctx, func(w *work) error {
		proj, err := w.openProject(ctx, projectID)
		if err != nil {
			return err
		}
		m, err := w.member(ctx, a, projectID, true)
		if err != nil {
			return err
		}
		task, err := w.createTask(ctx, proj, m, a, newTask{title: title, body: body, assignee: in.AssigneeMemberID})
		if err != nil {
			return err
		}
		out = &TaskCreated{Task: *task}
		if role == "none" || m == nil {
			return nil // only a Person hands off; system-created Tasks wait
		}
		members, err := w.st.ListMembers(ctx, projectID)
		if err != nil {
			return err
		}
		to := models.MemberByRole(members, role)
		if to == nil {
			return invalid("no member has role %q", role)
		}
		if note == "" {
			note = "Please take this."
		}
		h, t, err := w.handoff(ctx, proj, task, m, to, a, note, false)
		if err != nil {
			return err
		}
		out.Task, out.Handoff = *t, h
		out.Run = w.lastRun
		return nil
	})
	return out, err
}

type newTask struct {
	title, body, assignee string
}

func (w *work) createTask(ctx context.Context, proj *models.Project, by *models.Member, a Actor, in newTask) (*models.Task, error) {
	if in.assignee != "" {
		if err := w.sameProject(ctx, proj.ID, in.assignee); err != nil {
			return nil, err
		}
	}
	t := models.Task{ID: uuid.NewString(), ProjectID: proj.ID, Title: in.title, Body: in.body, Status: models.TaskOpen,
		AssigneeMemberID: in.assignee, CreatedAt: w.now, UpdatedAt: w.now}
	if by != nil {
		t.CreatedByMemberID = by.ID
	}
	if err := w.st.InsertTask(ctx, t); err != nil {
		return nil, err
	}
	return &t, w.activity(ctx, proj.ID, whoOf(by, a), models.TypeTask, "created", t.ID, map[string]any{"title": t.Title})
}

// sameProject checks that memberID belongs to projectID.
func (w *work) sameProject(ctx context.Context, projectID, memberID string) error {
	m, err := w.st.GetMember(ctx, memberID)
	if err != nil {
		return invalid("member %s does not exist", memberID)
	}
	if m.ProjectID != projectID {
		return invalid("member %s is not in this project", memberID)
	}
	return nil
}

// TaskPatch changes only the fields that are set.
type TaskPatch struct {
	Title  *string `json:"title"`
	Body   *string `json:"body"`
	Status *string `json:"status"`
}

// UpdateTask edits a Task's title, description or status.
func (s *Service) UpdateTask(ctx context.Context, a Actor, id string, patch TaskPatch) (*models.Task, error) {
	var out *models.Task
	err := s.tx(ctx, func(w *work) error {
		t, err := w.st.GetTask(ctx, id, true)
		if err != nil {
			return err
		}
		if _, err := w.openProject(ctx, t.ProjectID); err != nil {
			return err
		}
		m, err := w.member(ctx, a, t.ProjectID, true)
		if err != nil {
			return err
		}
		changes := map[string]any{}
		if patch.Title != nil {
			if t.Title, err = text("title", *patch.Title, true, 300); err != nil {
				return err
			}
			changes["title"] = t.Title
		}
		if patch.Body != nil {
			if t.Body, err = text("body", *patch.Body, false, 32000); err != nil {
				return err
			}
			changes["body"] = true
		}
		if patch.Status != nil {
			st, ok := models.ParseTaskStatus(*patch.Status)
			if !ok {
				return invalid("status must be open, in_progress, done or canceled")
			}
			if st != t.Status {
				changes["from"], changes["status"] = t.Status, st
				t.Status = st
			}
		}
		if len(changes) == 0 {
			out = t
			return nil
		}
		t.UpdatedAt = w.now
		if err := w.st.UpdateTask(ctx, *t); err != nil {
			return err
		}
		out = t
		return w.activity(ctx, t.ProjectID, whoOf(m, a), models.TypeTask, "updated", t.ID, changes)
	})
	return out, err
}

// --- handoffs ---

// Handoffs lists a Task's Handoffs, oldest first.
func (s *Service) Handoffs(ctx context.Context, taskID string) ([]models.Handoff, error) {
	if _, err := s.st.GetTask(ctx, taskID, false); err != nil {
		return nil, err
	}
	return s.st.ListHandoffs(ctx, taskID)
}

// NewHandoff names the receiver by Member ID or by Role.
type NewHandoff struct {
	ToMemberID string `json:"to_member_id"`
	ToRole     string `json:"to_role"`
	Note       string `json:"note"`
	AutoRun    bool   `json:"autorun"`
}

// HandedOff is a new Handoff and the Run it started, if any.
type HandedOff struct {
	models.Handoff
	Task models.Task `json:"task"`
	Run  *models.Run `json:"run,omitempty"`
}

// CreateHandoff hands a Task from the acting Person to a Member or Role.
// Handing to Builder starts a Run when requested or when the Project
// auto-runs.
func (s *Service) CreateHandoff(ctx context.Context, a Actor, taskID string, in NewHandoff) (*HandedOff, error) {
	note, err := text("note", in.Note, false, 4000)
	if err != nil {
		return nil, err
	}
	var out *HandedOff
	err = s.tx(ctx, func(w *work) error {
		task, err := w.st.GetTask(ctx, taskID, true)
		if err != nil {
			return err
		}
		proj, err := w.openProject(ctx, task.ProjectID)
		if err != nil {
			return err
		}
		from, err := w.requireMember(ctx, a, task.ProjectID)
		if err != nil {
			return err
		}
		var to *models.Member
		switch {
		case strings.TrimSpace(in.ToMemberID) != "":
			to, err = w.st.GetMember(ctx, strings.TrimSpace(in.ToMemberID))
			if err != nil || to.ProjectID != task.ProjectID {
				return invalid("to_member_id is not a member of this project")
			}
		case strings.TrimSpace(in.ToRole) != "":
			members, err := w.st.ListMembers(ctx, task.ProjectID)
			if err != nil {
				return err
			}
			if to = models.MemberByRole(members, in.ToRole); to == nil {
				return invalid("no member has role %q", in.ToRole)
			}
		default:
			return invalid("to_member_id or to_role is required")
		}
		h, t, err := w.handoff(ctx, proj, task, from, to, a, note, in.AutoRun)
		if err != nil {
			return err
		}
		out = &HandedOff{Handoff: *h, Task: *t, Run: w.lastRun}
		return nil
	})
	return out, err
}

// handoff records a Handoff, assigns the Task to the receiver (reopening it
// if it was closed), notifies a human receiver, and starts a Run when a
// Builder receives it with autorun (or the Project auto-runs).
func (w *work) handoff(ctx context.Context, proj *models.Project, task *models.Task, from, to *models.Member, a Actor,
	note string, autorun bool) (*models.Handoff, *models.Task, error) {
	w.lastRun = nil
	h := models.Handoff{ID: uuid.NewString(), TaskID: task.ID, ProjectID: task.ProjectID, FromMemberID: from.ID,
		ToMemberID: to.ID, Note: note, Status: models.HandoffOpen, CreatedAt: w.now}
	if err := w.st.InsertHandoff(ctx, h); err != nil {
		return nil, nil, err
	}
	payload := map[string]any{"task_id": task.ID, "to_member_id": to.ID, "to": to.DisplayName, "note": truncate(note, 500)}
	if task.Status == models.TaskDone || task.Status == models.TaskCanceled {
		payload["reopened_from"] = task.Status
	}
	t := *task
	t.AssigneeMemberID = to.ID
	if t.Status != models.TaskInProgress {
		t.Status = models.TaskInProgress
	}
	t.UpdatedAt = w.now
	if err := w.st.UpdateTask(ctx, t); err != nil {
		return nil, nil, err
	}
	by := whoOf(from, a)
	if err := w.activity(ctx, task.ProjectID, by, models.TypeHandoff, "created", h.ID, payload); err != nil {
		return nil, nil, err
	}
	if err := w.notifyMember(ctx, to, notice{projectID: task.ProjectID, kind: "handoff",
		title: "Task handed to you: " + t.Title, body: noteText(note), href: href(task.ProjectID, "tasks", task.ID)}); err != nil {
		return nil, nil, err
	}
	if to.Kind == models.KindBot && strings.EqualFold(to.Role, models.RoleBuilder) && (autorun || proj.AutoRun) {
		run, err := w.createRun(ctx, &t, to, "", by)
		if err != nil {
			return nil, nil, err
		}
		w.lastRun = run
	}
	return &h, &t, nil
}

func noteText(s string) string { return truncate(s, 500) }

// CompletedHandoff is a completed Handoff and the Decision it opened, if any.
type CompletedHandoff struct {
	models.Handoff
	Decision *models.Decision `json:"decision,omitempty"`
}

// CompleteHandoff closes an open Handoff. When Scout completes triage of a
// Task that looks ambiguous (or askDecision is set), a Decision linked to
// the Task asks whether it is ready for Builder.
func (s *Service) CompleteHandoff(ctx context.Context, a Actor, id string, askDecision bool) (*CompletedHandoff, error) {
	var out *CompletedHandoff
	err := s.tx(ctx, func(w *work) error {
		h, err := w.st.GetHandoff(ctx, id)
		if err != nil {
			return err
		}
		if _, err := w.openProject(ctx, h.ProjectID); err != nil {
			return err
		}
		m, err := w.member(ctx, a, h.ProjectID, true)
		if err != nil {
			return err
		}
		done, err := w.st.CompleteHandoff(ctx, id, w.now)
		if err != nil {
			if err == store.ErrConflict {
				return fmt.Errorf("%w: handoff is already complete", store.ErrConflict)
			}
			return err
		}
		out = &CompletedHandoff{Handoff: *done}
		by := whoOf(m, a)
		if err := w.activity(ctx, h.ProjectID, by, models.TypeHandoff, "completed", h.ID, map[string]any{"task_id": h.TaskID}); err != nil {
			return err
		}
		from, err := w.st.GetMember(ctx, h.FromMemberID)
		if err != nil {
			return err
		}
		to, err := w.st.GetMember(ctx, h.ToMemberID)
		if err != nil {
			return err
		}
		if !strings.EqualFold(from.Role, models.RoleScout) && !strings.EqualFold(to.Role, models.RoleScout) {
			return nil
		}
		task, err := w.st.GetTask(ctx, h.TaskID, false)
		if err != nil {
			return err
		}
		if !askDecision && !models.TaskLooksAmbiguous(task.Title, h.Note) {
			return nil
		}
		d, err := w.openDecision(ctx, h.ProjectID, by, m, newDecision{
			prompt:         "Scout: is Task \"" + task.Title + "\" ready for Builder?",
			recommendation: "handoff to Builder",
			options:        []string{"handoff to Builder", "needs more info"},
			taskID:         task.ID,
		})
		out.Decision = d
		return err
	})
	return out, err
}
