package store

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/codemodify/buildbee/server/internal/models"
	"github.com/google/uuid"
)

// Memory is an in-process Store used by tests and by `go run` when DATABASE_URL is unset.
type Memory struct {
	mu        sync.Mutex
	projects  map[string]models.Project
	members   map[string]models.Member
	channels  map[string]models.Channel
	messages  map[string]models.Message
	tasks     map[string]models.Task
	handoffs  map[string]models.Handoff
	decisions map[string]models.Decision
	activity  map[string]models.Activity
	runs      map[string]models.Run
}

func NewMemory() *Memory {
	return &Memory{
		projects:  map[string]models.Project{},
		members:   map[string]models.Member{},
		channels:  map[string]models.Channel{},
		messages:  map[string]models.Message{},
		tasks:     map[string]models.Task{},
		handoffs:  map[string]models.Handoff{},
		decisions: map[string]models.Decision{},
		activity:  map[string]models.Activity{},
		runs:      map[string]models.Run{},
	}
}

func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func (m *Memory) addActivity(projectID, typ string, payload map[string]any) {
	id := uuid.NewString()
	m.activity[id] = models.Activity{
		ID: id, ProjectID: projectID, Type: typ, Payload: payload, CreatedAt: now(),
	}
}

func (m *Memory) CreateProject(_ context.Context, name string) (*models.ProjectBundle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := now()
	p := models.Project{ID: uuid.NewString(), Name: name, CreatedAt: t}
	human := models.Member{
		ID: uuid.NewString(), ProjectID: p.ID, Kind: "human",
		DisplayName: "You", Role: "owner", Identity: "human:stub", CreatedAt: t,
	}
	bot := models.Member{
		ID: uuid.NewString(), ProjectID: p.ID, Kind: "bot",
		DisplayName: "BuildBee Bot", Role: "bot",
		Identity: "bot:" + uuid.NewString(), CreatedAt: t,
	}
	ch := models.Channel{ID: uuid.NewString(), ProjectID: p.ID, Name: "general", CreatedAt: t}
	m.projects[p.ID] = p
	m.members[human.ID] = human
	m.members[bot.ID] = bot
	m.channels[ch.ID] = ch
	m.addActivity(p.ID, models.TypeProject, map[string]any{"name": name, "id": p.ID})
	m.addActivity(p.ID, models.TypeMember, map[string]any{"id": human.ID, "kind": "human"})
	m.addActivity(p.ID, models.TypeMember, map[string]any{"id": bot.ID, "kind": "bot"})
	m.addActivity(p.ID, models.TypeChannel, map[string]any{"id": ch.ID, "name": ch.Name})
	return &models.ProjectBundle{Project: p, Members: []models.Member{human, bot}, Channels: []models.Channel{ch}}, nil
}

func (m *Memory) GetProject(_ context.Context, id string) (*models.ProjectBundle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &models.ProjectBundle{Project: p, Members: m.membersFor(id), Channels: m.channelsFor(id)}, nil
}

func (m *Memory) ListProjects(_ context.Context) ([]models.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]models.Project, 0, len(m.projects))
	for _, p := range m.projects {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) membersFor(projectID string) []models.Member {
	out := []models.Member{}
	for _, mem := range m.members {
		if mem.ProjectID == projectID {
			out = append(out, mem)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (m *Memory) channelsFor(projectID string) []models.Channel {
	out := []models.Channel{}
	for _, ch := range m.channels {
		if ch.ProjectID == projectID {
			out = append(out, ch)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (m *Memory) ListMembers(_ context.Context, projectID string) ([]models.Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	return m.membersFor(projectID), nil
}

func (m *Memory) AddMember(_ context.Context, in models.Member) (*models.Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[in.ProjectID]; !ok {
		return nil, ErrNotFound
	}
	in.ID = uuid.NewString()
	in.CreatedAt = now()
	if in.Identity == "" {
		if in.Kind == "bot" {
			in.Identity = "bot:" + uuid.NewString()
		} else {
			in.Identity = "human:stub"
		}
	}
	m.members[in.ID] = in
	m.addActivity(in.ProjectID, models.TypeMember, map[string]any{"id": in.ID, "kind": in.Kind})
	return &in, nil
}

func (m *Memory) GetMember(_ context.Context, id string) (*models.Member, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mem, ok := m.members[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &mem, nil
}

func (m *Memory) ListChannels(_ context.Context, projectID string) ([]models.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	return m.channelsFor(projectID), nil
}

func (m *Memory) CreateChannel(_ context.Context, projectID, name string) (*models.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	ch := models.Channel{ID: uuid.NewString(), ProjectID: projectID, Name: name, CreatedAt: now()}
	m.channels[ch.ID] = ch
	m.addActivity(projectID, models.TypeChannel, map[string]any{"id": ch.ID, "name": name})
	return &ch, nil
}

func (m *Memory) GetChannel(_ context.Context, id string) (*models.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch, ok := m.channels[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &ch, nil
}

func (m *Memory) ListMessages(_ context.Context, channelID string) ([]models.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.channels[channelID]; !ok {
		return nil, ErrNotFound
	}
	out := []models.Message{}
	for _, msg := range m.messages {
		if msg.ChannelID == channelID {
			out = append(out, msg)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) PostMessage(_ context.Context, channelID, memberID, body string) (*models.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch, ok := m.channels[channelID]
	if !ok {
		return nil, ErrNotFound
	}
	if _, ok := m.members[memberID]; !ok {
		return nil, fmt.Errorf("%w: member", ErrNotFound)
	}
	msg := models.Message{ID: uuid.NewString(), ChannelID: channelID, MemberID: memberID, Body: body, CreatedAt: now()}
	m.messages[msg.ID] = msg
	m.addActivity(ch.ProjectID, models.TypeMessage, map[string]any{"id": msg.ID, "channel_id": channelID})
	return &msg, nil
}

func (m *Memory) ListTasks(_ context.Context, projectID string) ([]models.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	out := []models.Task{}
	for _, t := range m.tasks {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) CreateTask(_ context.Context, projectID, title, assigneeMemberID string) (*models.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	t := now()
	task := models.Task{
		ID: uuid.NewString(), ProjectID: projectID, Title: title,
		Status: "open", AssigneeMemberID: assigneeMemberID, CreatedAt: t, UpdatedAt: t,
	}
	m.tasks[task.ID] = task
	m.addActivity(projectID, models.TypeTask, map[string]any{"id": task.ID, "title": title, "action": "create"})
	return &task, nil
}

func (m *Memory) GetTask(_ context.Context, id string) (*models.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &t, nil
}

func (m *Memory) UpdateTaskStatus(_ context.Context, id, status string) (*models.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, ErrNotFound
	}
	t.Status = status
	t.UpdatedAt = now()
	m.tasks[id] = t
	m.addActivity(t.ProjectID, models.TypeTask, map[string]any{"id": t.ID, "status": status, "action": "update"})
	return &t, nil
}

func (m *Memory) CreateHandoff(_ context.Context, taskID, fromID, toID, note string) (*models.Handoff, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[taskID]
	if !ok {
		return nil, ErrNotFound
	}
	if _, ok := m.members[fromID]; !ok {
		return nil, fmt.Errorf("%w: from member", ErrNotFound)
	}
	if _, ok := m.members[toID]; !ok {
		return nil, fmt.Errorf("%w: to member", ErrNotFound)
	}
	h := models.Handoff{
		ID: uuid.NewString(), TaskID: taskID, FromMemberID: fromID, ToMemberID: toID,
		Note: note, Status: "open", CreatedAt: now(),
	}
	m.handoffs[h.ID] = h
	task.AssigneeMemberID = toID
	task.Status = "in_progress"
	task.UpdatedAt = now()
	m.tasks[taskID] = task
	m.addActivity(task.ProjectID, models.TypeHandoff, map[string]any{"id": h.ID, "task_id": taskID, "action": "create"})
	return &h, nil
}

func (m *Memory) GetHandoff(_ context.Context, id string) (*models.Handoff, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.handoffs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &h, nil
}

func (m *Memory) CompleteHandoff(_ context.Context, id string) (*models.Handoff, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.handoffs[id]
	if !ok {
		return nil, ErrNotFound
	}
	t := now()
	h.Status = "complete"
	h.CompletedAt = &t
	m.handoffs[id] = h
	task := m.tasks[h.TaskID]
	m.addActivity(task.ProjectID, models.TypeHandoff, map[string]any{"id": h.ID, "task_id": h.TaskID, "action": "complete"})
	return &h, nil
}

func (m *Memory) ListDecisions(_ context.Context, projectID string) ([]models.Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	out := []models.Decision{}
	for _, d := range m.decisions {
		if d.ProjectID == projectID {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) CreateDecision(_ context.Context, projectID, prompt, recommendation string, options []string) (*models.Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	if options == nil {
		options = []string{}
	}
	d := models.Decision{
		ID: uuid.NewString(), ProjectID: projectID, Prompt: prompt,
		Options: options, Recommendation: recommendation, CreatedAt: now(),
	}
	m.decisions[d.ID] = d
	m.addActivity(projectID, models.TypeDecision, map[string]any{"id": d.ID, "action": "create"})
	return &d, nil
}

func (m *Memory) AnswerDecision(_ context.Context, id, answer string) (*models.Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.decisions[id]
	if !ok {
		return nil, ErrNotFound
	}
	t := now()
	d.Answer = answer
	d.AnsweredAt = &t
	m.decisions[id] = d
	m.addActivity(d.ProjectID, models.TypeDecision, map[string]any{"id": d.ID, "action": "answer", "answer": answer})
	return &d, nil
}

func (m *Memory) ListActivity(_ context.Context, projectID, typeFilter string) ([]models.Activity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	out := []models.Activity{}
	for _, a := range m.activity {
		if a.ProjectID == projectID && (typeFilter == "" || a.Type == typeFilter) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 200 {
		out = out[:200]
	}
	return out, nil
}

func (m *Memory) CreateRun(_ context.Context, taskID string) (*models.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[taskID]
	if !ok {
		return nil, ErrNotFound
	}
	t := now()
	run := models.Run{
		ID: uuid.NewString(), TaskID: taskID, ProjectID: task.ProjectID,
		Status: "pending", CreatedAt: t, UpdatedAt: t,
	}
	m.runs[run.ID] = run
	m.addActivity(task.ProjectID, models.TypeRun, map[string]any{"id": run.ID, "task_id": taskID, "status": run.Status})
	return &run, nil
}

func (m *Memory) GetRun(_ context.Context, id string) (*models.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &r, nil
}

func (m *Memory) UpdateRun(_ context.Context, id, status, detail string) (*models.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	r.Status = status
	r.Detail = detail
	r.UpdatedAt = now()
	m.runs[id] = r
	m.addActivity(r.ProjectID, models.TypeRun, map[string]any{"id": r.ID, "status": status})
	return &r, nil
}
