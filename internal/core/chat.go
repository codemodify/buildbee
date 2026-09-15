package core

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/google/uuid"
)

// Chat: Channels hold root messages; a root can start a thread of replies.
// Every Task has a thread, where its Bots report as they plan, build,
// review and merge, and where people talk to the agent at work: a reply
// while a Run is going reaches the agent, and @Bot hands the Task on.
// DMs are Channels between chosen Members; a DM with a Bot asks it to work.

// Messages returns one page of a Channel's root messages, oldest first.
func (s *Service) Messages(ctx context.Context, a Actor, channelID string, p store.Page) ([]models.Message, bool, error) {
	ch, err := s.st.GetChannel(ctx, channelID)
	if err != nil {
		return nil, false, err
	}
	if err := s.canSee(ctx, a, ch); err != nil {
		return nil, false, err
	}
	return s.st.ListMessages(ctx, channelID, p)
}

// Thread is a root message and one page of its replies.
type Thread struct {
	Root    models.Message   `json:"root"`
	Replies []models.Message `json:"replies"`
	HasMore bool             `json:"has_more"`
}

// Thread returns a thread by its root message (or any reply in it).
func (s *Service) Thread(ctx context.Context, a Actor, messageID string, p store.Page) (*Thread, error) {
	root, err := s.st.GetMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	if root.ThreadID != "" {
		if root, err = s.st.GetMessage(ctx, root.ThreadID); err != nil {
			return nil, err
		}
	}
	ch, err := s.st.GetChannel(ctx, root.ChannelID)
	if err != nil {
		return nil, err
	}
	if err := s.canSee(ctx, a, ch); err != nil {
		return nil, err
	}
	replies, more, err := s.st.ListReplies(ctx, root.ID, p)
	if err != nil {
		return nil, err
	}
	return &Thread{Root: *root, Replies: replies, HasMore: more}, nil
}

// canSee hides DMs from people who are not in them. On a LAN without
// logins this keeps conversations tidy; it is not access control.
func (s *Service) canSee(ctx context.Context, a Actor, ch *models.Channel) error {
	if ch.Kind != models.ChannelDM {
		return nil
	}
	if a.IsPerson() {
		if m, err := s.st.MemberForPerson(ctx, ch.ProjectID, a.PersonID); err == nil && ch.Allows(m.ID) {
			return nil
		}
	}
	return store.ErrNotFound
}

// Posted is a new message plus what its @mentions set in motion.
type Posted struct {
	models.Message
	Mentions []models.Member  `json:"mentions"`
	Tasks    []models.Task    `json:"tasks"`
	Handoffs []models.Handoff `json:"handoffs"`
	// Root is a reply's thread root as it stands now (reply count
	// included), so clients need not count replies themselves. Live
	// events only.
	Root *models.Message `json:"root,omitempty"`
}

// PostMessage posts a root message to a Channel as the acting Person.
func (s *Service) PostMessage(ctx context.Context, a Actor, channelID, body string) (*Posted, error) {
	return s.send(ctx, a, channelID, "", body)
}

// Reply posts to the thread of messageID (or of the thread it is in).
func (s *Service) Reply(ctx context.Context, a Actor, messageID, body string) (*Posted, error) {
	m, err := s.st.GetMessage(ctx, messageID)
	if err != nil {
		return nil, err
	}
	root := m.ID
	if m.ThreadID != "" {
		root = m.ThreadID
	}
	return s.send(ctx, a, m.ChannelID, root, body)
}

func (s *Service) send(ctx context.Context, a Actor, channelID, threadID, body string) (*Posted, error) {
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
		if !ch.Allows(author.ID) {
			return store.ErrNotFound
		}
		out, err = w.post(ctx, proj, ch, author, a, b, threadID)
		return err
	})
	return out, err
}

// post writes a message by author (a reply when threadID is set) and acts
// on it: @people are notified; a person's @Bot (or a message in a DM with
// a Bot) opens a Task handed to it, whose thread lives in #tasks; in a
// thread about a Task, @Bot hands that Task on, and any other reply
// reaches the agent working on it.
func (w *work) post(ctx context.Context, proj *models.Project, ch *models.Channel, author *models.Member, a Actor,
	body, threadID string) (*Posted, error) {
	var root *models.Message
	if threadID != "" {
		r, err := w.st.GetMessage(ctx, threadID)
		if err != nil || r.ChannelID != ch.ID || r.ThreadID != "" {
			return nil, invalid("thread_id is not a thread of this channel")
		}
		root = r
	}
	msg, err := w.st.InsertMessage(ctx, models.Message{ID: uuid.NewString(), ChannelID: ch.ID, ProjectID: ch.ProjectID,
		MemberID: author.ID, Body: body, ThreadID: threadID, CreatedAt: w.now})
	if err != nil {
		return nil, err
	}
	out := &Posted{Message: *msg, Mentions: []models.Member{}, Tasks: []models.Task{}, Handoffs: []models.Handoff{}}
	members, err := w.st.ListMembers(ctx, ch.ProjectID)
	if err != nil {
		return nil, err
	}
	where := "#" + ch.Name
	if ch.Kind == models.ChannelDM {
		where = "a direct message"
	}
	link := href(ch.ProjectID, "channels", ch.ID)
	if threadID != "" {
		link = href(ch.ProjectID, "threads", threadID)
	}
	for _, p := range models.MentionedPeople(body, members) {
		if p.ID == author.ID {
			continue
		}
		if err := w.notifyMember(ctx, &p, notice{projectID: ch.ProjectID, kind: "mention",
			title: author.DisplayName + " mentioned you in " + where, body: truncate(body, 500), href: link}); err != nil {
			return nil, err
		}
	}
	if author.Kind == models.KindHuman {
		bots := models.MentionedBots(body, members)
		if root == nil && ch.Kind == models.ChannelDM { // talking to a Bot directly
			for i := range members {
				if members[i].Kind == models.KindBot && ch.Allows(members[i].ID) &&
					!slices.ContainsFunc(bots, func(b models.Member) bool { return b.ID == members[i].ID }) {
					bots = append(bots, members[i])
				}
			}
		}
		var task *models.Task
		if root != nil && root.TaskID != "" {
			if task, err = w.st.GetTask(ctx, root.TaskID, true); err != nil {
				return nil, err
			}
		}
		switch {
		case task != nil && len(bots) == 0:
			if err := w.steerTask(ctx, task, author, a, body); err != nil {
				return nil, err
			}
		case len(bots) > 0:
			if task == nil {
				if task, err = w.createTask(ctx, proj, author, a, newTask{title: mentionTitle(body, ch.Name), body: body}); err != nil {
					return nil, err
				}
				if ch.Locked && root == nil {
					// Asked in #tasks: this message is the Task's thread.
					if err := w.linkThread(ctx, task, msg.ID); err != nil {
						return nil, err
					}
				} else {
					// Asked elsewhere: the thread opens in #tasks and this
					// message links to the Task.
					if err := w.openThread(ctx, proj, task, author, "Task: "+task.Title, ""); err != nil {
						return nil, err
					}
					if err := w.st.SetMessageTask(ctx, msg.ID, task.ID); err != nil {
						return nil, err
					}
				}
				out.Message.TaskID = task.ID
			}
			for i := range bots {
				h, t, err := w.handoff(ctx, proj, task, author, &bots[i], a, body, true)
				if err != nil {
					return nil, err
				}
				task = t
				out.Mentions = append(out.Mentions, bots[i])
				out.Handoffs = append(out.Handoffs, *h)
			}
			out.Tasks = append(out.Tasks, *task)
		}
	}
	if root != nil {
		if r, err := w.st.GetMessage(ctx, root.ID); err == nil {
			out.Root = r
		}
	}
	w.emit("channel:"+ch.ID, msg.Seq, "message", out)
	return out, nil
}

// linkThread makes message the thread of task.
func (w *work) linkThread(ctx context.Context, task *models.Task, messageID string) error {
	if err := w.st.SetMessageTask(ctx, messageID, task.ID); err != nil {
		return err
	}
	task.ThreadID, task.UpdatedAt = messageID, w.now
	return w.st.UpdateTask(ctx, *task)
}

// openThread starts a Task's thread in channelID (default: the Project's
// #tasks) with a message by author.
func (w *work) openThread(ctx context.Context, proj *models.Project, task *models.Task, author *models.Member, body, channelID string) error {
	if author == nil {
		return nil
	}
	var ch *models.Channel
	if channelID != "" {
		c, err := w.st.GetChannel(ctx, channelID)
		if err != nil || c.ProjectID != proj.ID || c.ArchivedAt != nil || !c.Allows(author.ID) {
			return invalid("channel_id is not an open channel of this project")
		}
		ch = c
	} else {
		chs, err := w.st.ListChannels(ctx, proj.ID, false)
		if err != nil || len(chs) == 0 {
			return err
		}
		ch = &chs[0]
	}
	msg, err := w.st.InsertMessage(ctx, models.Message{ID: uuid.NewString(), ChannelID: ch.ID, ProjectID: proj.ID,
		MemberID: author.ID, Body: body, TaskID: task.ID, CreatedAt: w.now})
	if err != nil {
		return err
	}
	task.ThreadID, task.UpdatedAt = msg.ID, w.now
	if err := w.st.UpdateTask(ctx, *task); err != nil {
		return err
	}
	w.emit("channel:"+ch.ID, msg.Seq, "message", &Posted{Message: *msg, Mentions: []models.Member{}, Tasks: []models.Task{}, Handoffs: []models.Handoff{}})
	return nil
}

// note posts a reply in a Task's thread, as author (a Bot, usually).
func (w *work) note(ctx context.Context, task *models.Task, author *models.Member, body string) error {
	if task.ThreadID == "" || author == nil || strings.TrimSpace(body) == "" {
		return nil
	}
	root, err := w.st.GetMessage(ctx, task.ThreadID)
	if err != nil {
		return err
	}
	msg, err := w.st.InsertMessage(ctx, models.Message{ID: uuid.NewString(), ChannelID: root.ChannelID, ProjectID: root.ProjectID,
		MemberID: author.ID, Body: truncate(body, 8000), ThreadID: root.ID, CreatedAt: w.now})
	if err != nil {
		return err
	}
	if r, err := w.st.GetMessage(ctx, root.ID); err == nil {
		root = r
	}
	w.emit("channel:"+root.ChannelID, msg.Seq, "message", &Posted{Message: *msg, Mentions: []models.Member{}, Tasks: []models.Task{},
		Handoffs: []models.Handoff{}, Root: root})
	return nil
}

// voice is the Member that speaks for BuildBee itself in a Project: Pulse.
func (w *work) voice(ctx context.Context, projectID string) *models.Member {
	members, err := w.st.ListMembers(ctx, projectID)
	if err != nil {
		return nil
	}
	return models.MemberByRole(members, models.RolePulse)
}

// botOf returns the Run's Bot, or the Project's voice for Runs without one.
func (w *work) botOf(ctx context.Context, r *models.Run) *models.Member {
	if r.BotMemberID != "" {
		if m, err := w.st.GetMember(ctx, r.BotMemberID); err == nil {
			return m
		}
	}
	return w.voice(ctx, r.ProjectID)
}

// steerTask passes a person's reply in a Task's thread to the agent
// working on the Task, if one is.
func (w *work) steerTask(ctx context.Context, task *models.Task, author *models.Member, a Actor, text string) error {
	runs, err := w.st.ListRuns(ctx, task.ID)
	if err != nil {
		return err
	}
	for i := range runs {
		r := &runs[i]
		if !r.Status.Terminal() && r.Kind != models.RunMerge {
			_, err := w.steer(ctx, r, author, a, text, false)
			return err
		}
	}
	return nil
}

// --- DMs ---

// OpenDM returns the DM between the acting Person and memberIDs (people or
// Bots of the Project), creating it the first time.
func (s *Service) OpenDM(ctx context.Context, a Actor, projectID string, memberIDs []string) (*models.Channel, error) {
	if len(memberIDs) == 0 || len(memberIDs) > 20 {
		return nil, invalid("a direct message is with 1 to 20 members")
	}
	var out *models.Channel
	err := s.tx(ctx, func(w *work) error {
		if _, err := w.openProject(ctx, projectID); err != nil {
			return err
		}
		me, err := w.requireMember(ctx, a, projectID)
		if err != nil {
			return err
		}
		ids := []string{me.ID}
		var names []string
		for _, id := range memberIDs {
			m, err := w.st.GetMember(ctx, strings.TrimSpace(id))
			if err != nil || m.ProjectID != projectID {
				return invalid("member %s is not in this project", id)
			}
			if m.ID != me.ID {
				ids = append(ids, m.ID)
				names = append(names, m.DisplayName)
			}
		}
		if len(ids) < 2 {
			return invalid("a direct message needs someone besides you")
		}
		slices.Sort(names)
		out, err = w.st.OpenDM(ctx, models.Channel{ID: uuid.NewString(), ProjectID: projectID,
			Name: truncate(strings.Join(names, ", "), 100), CreatedAt: w.now}, ids)
		return err
	})
	return out, err
}

// DMs lists the acting Person's DMs in a Project.
func (s *Service) DMs(ctx context.Context, a Actor, projectID string) ([]models.Channel, error) {
	if !a.IsPerson() {
		return []models.Channel{}, nil
	}
	m, err := s.st.MemberForPerson(ctx, projectID, a.PersonID)
	if err != nil {
		return []models.Channel{}, nil
	}
	return s.st.ListDMs(ctx, projectID, m.ID)
}

// --- unread ---

// Unread counts what the acting Person has not read in each Channel and
// DM of a Project.
func (s *Service) Unread(ctx context.Context, a Actor, projectID string) ([]models.Unread, error) {
	if !a.IsPerson() {
		return []models.Unread{}, nil
	}
	return s.st.Unread(ctx, a.PersonID, projectID)
}

// MarkChannelRead records that the acting Person has read a Channel up to seq.
func (s *Service) MarkChannelRead(ctx context.Context, a Actor, channelID string, seq int64) error {
	if !a.IsPerson() {
		return ErrNoActor
	}
	ch, err := s.st.GetChannel(ctx, channelID)
	if err != nil {
		return err
	}
	if err := s.canSee(ctx, a, ch); err != nil {
		return err
	}
	return s.st.MarkRead(ctx, a.PersonID, channelID, seq)
}
