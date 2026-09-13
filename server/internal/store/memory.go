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
	mu            sync.Mutex
	projects      map[string]models.Project
	members       map[string]models.Member
	channels      map[string]models.Channel
	messages      map[string]models.Message
	tasks         map[string]models.Task
	handoffs      map[string]models.Handoff
	decisions     map[string]models.Decision
	activity      map[string]models.Activity
	runs          map[string]models.Run
	artifacts     map[string]models.Artifact
	pipelines     map[string]models.Pipeline
	routines      map[string]models.Routine
	memories      map[string]models.DecisionMemory
	notifications map[string]models.Notification
}

func NewMemory() *Memory {
	return &Memory{
		projects:      map[string]models.Project{},
		members:       map[string]models.Member{},
		channels:      map[string]models.Channel{},
		messages:      map[string]models.Message{},
		tasks:         map[string]models.Task{},
		handoffs:      map[string]models.Handoff{},
		decisions:     map[string]models.Decision{},
		activity:      map[string]models.Activity{},
		runs:          map[string]models.Run{},
		artifacts:     map[string]models.Artifact{},
		pipelines:     map[string]models.Pipeline{},
		routines:      map[string]models.Routine{},
		memories:      map[string]models.DecisionMemory{},
		notifications: map[string]models.Notification{},
	}
}

func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func (m *Memory) addActivity(projectID, typ string, payload map[string]any) {
	id := uuid.NewString()
	m.activity[id] = models.Activity{
		ID: id, ProjectID: projectID, Type: typ, Payload: payload, CreatedAt: now(),
	}
}

func (m *Memory) CreateProject(_ context.Context, name string, autoRun bool) (*models.ProjectBundle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := now()
	p := models.Project{ID: uuid.NewString(), Name: name, AutoRun: autoRun, CreatedAt: t}
	human := seedHuman(p.ID, t)
	bots := seedBots(p.ID, t)
	ch := models.Channel{ID: uuid.NewString(), ProjectID: p.ID, Name: "general", CreatedAt: t}
	m.projects[p.ID] = p
	m.members[human.ID] = human
	m.channels[ch.ID] = ch
	m.addActivity(p.ID, models.TypeProject, map[string]any{"name": name, "id": p.ID, "auto_run": autoRun})
	m.addActivity(p.ID, models.TypeMember, map[string]any{"id": human.ID, "kind": "human", "role": human.Role})
	members := make([]models.Member, 0, 1+len(bots))
	members = append(members, human)
	for _, bot := range bots {
		m.members[bot.ID] = bot
		members = append(members, bot)
		m.addActivity(p.ID, models.TypeMember, map[string]any{"id": bot.ID, "kind": "bot", "role": bot.Role})
	}
	m.addActivity(p.ID, models.TypeChannel, map[string]any{"id": ch.ID, "name": ch.Name})
	rid := uuid.NewString()
	m.routines[rid] = models.Routine{
		ID: rid, ProjectID: p.ID, BotMemberID: pulseID(bots),
		Name: "morning-digest", Schedule: "24h", Enabled: false, CreatedAt: t,
	}
	return &models.ProjectBundle{Project: p, Members: members, Channels: []models.Channel{ch}}, nil
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

func (m *Memory) UpdateProject(_ context.Context, id string, autoRun *bool) (*models.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.projects[id]
	if !ok {
		return nil, ErrNotFound
	}
	if autoRun != nil {
		p.AutoRun = *autoRun
		m.projects[id] = p
		m.addActivity(id, models.TypeProject, map[string]any{"id": id, "auto_run": p.AutoRun, "action": "update"})
	}
	return &p, nil
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
	d.Fingerprint = models.DecisionFingerprint(prompt)
	m.decisions[d.ID] = d
	m.addActivity(projectID, models.TypeDecision, map[string]any{"id": d.ID, "action": "create"})
	return &d, nil
}

func (m *Memory) CreateReusedDecision(_ context.Context, projectID, prompt, recommendation string, options []string, answer string) (*models.Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	if options == nil {
		options = []string{}
	}
	t := now()
	d := models.Decision{
		ID: uuid.NewString(), ProjectID: projectID, Prompt: prompt,
		Options: options, Recommendation: recommendation, Answer: answer,
		Reused: true, Fingerprint: models.DecisionFingerprint(prompt),
		CreatedAt: t, AnsweredAt: &t,
	}
	m.decisions[d.ID] = d
	m.addActivity(projectID, models.TypeDecision, map[string]any{"id": d.ID, "action": "reuse", "answer": answer, "fingerprint": d.Fingerprint})
	m.addActivity(projectID, models.TypeMemory, map[string]any{"fingerprint": d.Fingerprint, "action": "reuse", "answer": answer})
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
	if d.Fingerprint == "" {
		d.Fingerprint = models.DecisionFingerprint(d.Prompt)
	}
	m.decisions[id] = d
	m.upsertMemoryLocked(d.ProjectID, d.Fingerprint, d.Prompt, answer, d.ID, t)
	m.addActivity(d.ProjectID, models.TypeDecision, map[string]any{"id": d.ID, "action": "answer", "answer": answer})
	return &d, nil
}

func (m *Memory) upsertMemoryLocked(projectID, fingerprint, prompt, answer, decisionID string, t time.Time) {
	for id, mem := range m.memories {
		if mem.ProjectID == projectID && mem.Fingerprint == fingerprint {
			mem.Answer = answer
			mem.Prompt = prompt
			mem.DecisionID = decisionID
			mem.UpdatedAt = t
			m.memories[id] = mem
			return
		}
	}
	id := uuid.NewString()
	m.memories[id] = models.DecisionMemory{
		ID: id, ProjectID: projectID, Fingerprint: fingerprint,
		Prompt: prompt, Answer: answer, DecisionID: decisionID, CreatedAt: t, UpdatedAt: t,
	}
}

func (m *Memory) GetDecisionMemory(_ context.Context, projectID, fingerprint string) (*models.DecisionMemory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mem := range m.memories {
		if mem.ProjectID == projectID && mem.Fingerprint == fingerprint {
			return &mem, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Memory) ListDecisionMemories(_ context.Context, projectID string) ([]models.DecisionMemory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	out := []models.DecisionMemory{}
	for _, mem := range m.memories {
		if mem.ProjectID == projectID {
			out = append(out, mem)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
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

func (m *Memory) ListRuns(_ context.Context, taskID string) ([]models.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[taskID]; !ok {
		return nil, ErrNotFound
	}
	out := []models.Run{}
	for _, r := range m.runs {
		if r.TaskID == taskID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) CreateArtifact(_ context.Context, in models.Artifact) (*models.Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[in.TaskID]
	if !ok {
		return nil, ErrNotFound
	}
	in.ID = uuid.NewString()
	in.ProjectID = task.ProjectID
	in.CreatedAt = now()
	if in.Kind == "" {
		in.Kind = "file"
	}
	m.artifacts[in.ID] = in
	m.addActivity(task.ProjectID, models.TypeArtifact, map[string]any{"id": in.ID, "kind": in.Kind, "name": in.Name})
	return &in, nil
}

func (m *Memory) GetArtifact(_ context.Context, id string) (*models.Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.artifacts[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &a, nil
}

func (m *Memory) ListArtifacts(_ context.Context, taskID string) ([]models.Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[taskID]; !ok {
		return nil, ErrNotFound
	}
	out := []models.Artifact{}
	for _, a := range m.artifacts {
		if a.TaskID == taskID {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) CreatePipeline(_ context.Context, in models.Pipeline) (*models.Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[in.TaskID]
	if !ok {
		return nil, ErrNotFound
	}
	t := now()
	in.ID = uuid.NewString()
	in.ProjectID = task.ProjectID
	if in.Status == "" {
		in.Status = "pending"
	}
	in.CreatedAt = t
	in.UpdatedAt = t
	m.pipelines[in.ID] = in
	m.addActivity(task.ProjectID, models.TypePipeline, map[string]any{"id": in.ID, "name": in.Name, "status": in.Status})
	return &in, nil
}

func (m *Memory) ListPipelines(_ context.Context, taskID string) ([]models.Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[taskID]; !ok {
		return nil, ErrNotFound
	}
	out := []models.Pipeline{}
	for _, p := range m.pipelines {
		if p.TaskID == taskID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) GetPipeline(_ context.Context, id string) (*models.Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pipelines[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &p, nil
}

func (m *Memory) UpdatePipeline(_ context.Context, id, status, externalURL string) (*models.Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pipelines[id]
	if !ok {
		return nil, ErrNotFound
	}
	p.Status = status
	if externalURL != "" {
		p.ExternalURL = externalURL
	}
	p.UpdatedAt = now()
	m.pipelines[id] = p
	m.addActivity(p.ProjectID, models.TypePipeline, map[string]any{"id": p.ID, "status": status})
	return &p, nil
}

func (m *Memory) UpsertIssueTask(_ context.Context, projectID string, number int, title, issueURL string) (*models.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	for id, t := range m.tasks {
		if t.ProjectID == projectID && t.IssueNumber == number {
			t.Title = title
			t.IssueURL = issueURL
			t.UpdatedAt = now()
			m.tasks[id] = t
			m.addActivity(projectID, models.TypeIssue, map[string]any{"task_id": t.ID, "issue_number": number, "action": "update"})
			return &t, nil
		}
	}
	t := now()
	task := models.Task{
		ID: uuid.NewString(), ProjectID: projectID, Title: title, Status: "open",
		IssueNumber: number, IssueURL: issueURL, CreatedAt: t, UpdatedAt: t,
	}
	m.tasks[task.ID] = task
	m.addActivity(projectID, models.TypeIssue, map[string]any{"task_id": task.ID, "issue_number": number, "action": "create"})
	return &task, nil
}

func (m *Memory) AppendActivity(_ context.Context, projectID, typ string, payload map[string]any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return ErrNotFound
	}
	m.addActivity(projectID, typ, payload)
	return nil
}

func (m *Memory) ListRoutines(_ context.Context, projectID string) ([]models.Routine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[projectID]; !ok {
		return nil, ErrNotFound
	}
	out := []models.Routine{}
	for _, r := range m.routines {
		if r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *Memory) ListEnabledRoutines(_ context.Context) ([]models.Routine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []models.Routine{}
	for _, r := range m.routines {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *Memory) CreateRoutine(_ context.Context, in models.Routine) (*models.Routine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[in.ProjectID]; !ok {
		return nil, ErrNotFound
	}
	in.ID = uuid.NewString()
	in.CreatedAt = now()
	if in.Schedule == "" {
		in.Schedule = "24h"
	}
	m.routines[in.ID] = in
	m.addActivity(in.ProjectID, models.TypeRoutine, map[string]any{"id": in.ID, "name": in.Name, "action": "create"})
	return &in, nil
}

func (m *Memory) GetRoutine(_ context.Context, id string) (*models.Routine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.routines[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &r, nil
}

func (m *Memory) UpdateRoutine(_ context.Context, id string, enabled *bool, lastRun *time.Time) (*models.Routine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.routines[id]
	if !ok {
		return nil, ErrNotFound
	}
	if enabled != nil {
		r.Enabled = *enabled
	}
	if lastRun != nil {
		r.LastRunAt = lastRun
	}
	m.routines[id] = r
	return &r, nil
}

func (m *Memory) CreateNotification(_ context.Context, in models.Notification) (*models.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[in.ProjectID]; !ok {
		return nil, ErrNotFound
	}
	if _, ok := m.members[in.MemberID]; !ok {
		return nil, fmt.Errorf("%w: member", ErrNotFound)
	}
	in.ID = uuid.NewString()
	in.CreatedAt = now()
	if in.Kind == "" {
		in.Kind = "notice"
	}
	m.notifications[in.ID] = in
	m.addActivity(in.ProjectID, models.TypeNotification, map[string]any{"id": in.ID, "kind": in.Kind, "member_id": in.MemberID})
	return &in, nil
}

func (m *Memory) ListNotifications(_ context.Context, memberID string, unreadOnly bool) ([]models.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []models.Notification{}
	for _, n := range m.notifications {
		if memberID != "" && n.MemberID != memberID {
			continue
		}
		if unreadOnly && n.ReadAt != nil {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}

func (m *Memory) GetNotification(_ context.Context, id string) (*models.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.notifications[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &n, nil
}

func (m *Memory) MarkNotificationRead(_ context.Context, id string) (*models.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.notifications[id]
	if !ok {
		return nil, ErrNotFound
	}
	t := now()
	n.ReadAt = &t
	m.notifications[id] = n
	return &n, nil
}

func (m *Memory) MarkAllNotificationsRead(_ context.Context, memberID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := now()
	n := 0
	for id, item := range m.notifications {
		if item.MemberID == memberID && item.ReadAt == nil {
			item.ReadAt = &t
			m.notifications[id] = item
			n++
		}
	}
	return n, nil
}
