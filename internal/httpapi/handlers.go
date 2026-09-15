package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/codemodify/buildbee/internal/models"
)

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListProjects(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		AutoRun bool   `json:"auto_run"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	p, err := s.store.CreateProject(r.Context(), strings.TrimSpace(in.Name), in.AutoRun)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetProject(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AutoRun *bool `json:"auto_run"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	p, err := s.store.UpdateProject(r.Context(), r.PathValue("projectID"), in.AutoRun)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListMembers(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}

func (s *Server) addMember(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DisplayName  string `json:"display_name"`
		Kind         string `json:"kind"`
		Role         string `json:"role"`
		Instructions string `json:"instructions"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.DisplayName) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "display_name is required"})
		return
	}
	kind := strings.ToLower(in.Kind)
	if kind != "bot" {
		kind = "human"
	}
	role := strings.ToLower(strings.TrimSpace(in.Role))
	if role == "" {
		if kind == "bot" {
			role = "bot"
		} else {
			role = "member"
		}
	}
	m, err := s.store.AddMember(r.Context(), models.Member{
		ProjectID:    r.PathValue("projectID"),
		DisplayName:  strings.TrimSpace(in.DisplayName),
		Kind:         kind,
		Role:         role,
		Instructions: strings.TrimSpace(in.Instructions),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListChannels(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	ch, err := s.store.CreateChannel(r.Context(), r.PathValue("projectID"), strings.TrimSpace(in.Name))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListMessages(r.Context(), r.PathValue("channelID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body     string `json:"body"`
		MemberID string `json:"member_id"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Body) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is required"})
		return
	}
	channelID := r.PathValue("channelID")
	memberID := strings.TrimSpace(in.MemberID)
	if memberID == "" {
		memberID = r.Header.Get("X-Member-ID")
	}
	if memberID == "" {
		ch, err := s.store.GetChannel(r.Context(), channelID)
		if err != nil {
			writeError(w, err)
			return
		}
		members, err := s.store.ListMembers(r.Context(), ch.ProjectID)
		if err != nil {
			writeError(w, err)
			return
		}
		for _, m := range members {
			if m.Kind == "human" {
				memberID = m.ID
				break
			}
		}
	}
	if memberID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "member_id is required"})
		return
	}
	body := strings.TrimSpace(in.Body)
	msg, err := s.store.PostMessage(r.Context(), channelID, memberID, body)
	if err != nil {
		writeError(w, err)
		return
	}
	ch, err := s.store.GetChannel(r.Context(), channelID)
	if err != nil {
		writeError(w, err)
		return
	}
	members, err := s.store.ListMembers(r.Context(), ch.ProjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	mentioned := models.MentionedBots(body, members)
	var mentionTasks []models.Task
	var mentionHandoffs []models.Handoff
	for _, bot := range mentioned {
		title := "Mention @" + bot.DisplayName + ": " + body
		if len(title) > 120 {
			title = title[:120]
		}
		task, err := s.store.CreateTask(r.Context(), ch.ProjectID, title, bot.ID)
		if err != nil {
			continue
		}
		mentionTasks = append(mentionTasks, *task)
		if h, err := s.store.CreateHandoff(r.Context(), task.ID, memberID, bot.ID, body); err == nil {
			mentionHandoffs = append(mentionHandoffs, *h)
		}
	}
	if len(mentionTasks) > 0 {
		s.notify(r.Context(), ch.ProjectID, memberID, "mention",
			"@mention created a Task", body,
			"#/projects/"+ch.ProjectID+"/tasks/"+mentionTasks[0].ID)
	}
	if len(mentioned) > 0 {
		ids := make([]string, 0, len(mentioned))
		roles := make([]string, 0, len(mentioned))
		for _, bot := range mentioned {
			ids = append(ids, bot.ID)
			roles = append(roles, bot.Role)
		}
		_ = s.store.AppendActivity(r.Context(), ch.ProjectID, models.TypeMention, map[string]any{
			"message_id": msg.ID, "channel_id": channelID, "member_ids": ids, "roles": roles,
		})
	}
	s.hub.Publish(channelID, map[string]any{"type": "message", "message": msg, "mentions": mentioned})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": msg.ID, "channel_id": msg.ChannelID, "member_id": msg.MemberID,
		"body": msg.Body, "created_at": msg.CreatedAt,
		"mentions": mentioned, "tasks": mentionTasks, "handoffs": mentionHandoffs,
	})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListTasks(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title            string `json:"title"`
		AssigneeMemberID string `json:"assignee_member_id"`
		HandoffRole      string `json:"handoff_role"`
		HandoffNote      string `json:"handoff_note"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Title) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
		return
	}
	projectID := r.PathValue("projectID")
	t, err := s.store.CreateTask(r.Context(), projectID, strings.TrimSpace(in.Title), in.AssigneeMemberID)
	if err != nil {
		writeError(w, err)
		return
	}
	role := strings.ToLower(strings.TrimSpace(in.HandoffRole))
	if q := r.URL.Query().Get("handoff"); q != "" {
		role = strings.ToLower(strings.TrimSpace(q))
	}
	if role == "" {
		role = models.RoleScout
	}
	if role == "0" || role == "none" || role == "off" {
		writeJSON(w, http.StatusCreated, t)
		return
	}
	members, err := s.store.ListMembers(r.Context(), projectID)
	if err != nil {
		writeError(w, err)
		return
	}
	to := models.MemberByRole(members, role)
	from := models.MemberByRole(members, models.RoleOwner)
	if from == nil {
		for i := range members {
			if members[i].Kind == "human" {
				from = &members[i]
				break
			}
		}
	}
	if to == nil || from == nil {
		writeJSON(w, http.StatusCreated, t)
		return
	}
	note := strings.TrimSpace(in.HandoffNote)
	if note == "" {
		note = "auto-Handoff to " + to.Role
	}
	h, err := s.store.CreateHandoff(r.Context(), t.ID, from.ID, to.ID, note)
	if err != nil {
		writeError(w, err)
		return
	}
	s.notify(r.Context(), projectID, to.ID, "handoff",
		"Task handed to you: "+t.Title, note,
		"#/projects/"+projectID+"/tasks/"+t.ID)
	task, _ := s.store.GetTask(r.Context(), t.ID)
	if task == nil {
		task = t
	}
	writeJSON(w, http.StatusCreated, map[string]any{"task": task, "handoff": h,
		"id": task.ID, "project_id": task.ProjectID, "title": task.Title, "status": task.Status,
		"assignee_member_id": task.AssigneeMemberID, "created_at": task.CreatedAt, "updated_at": task.UpdatedAt,
	})
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.GetTask(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Status) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status is required"})
		return
	}
	t, err := s.store.UpdateTaskStatus(r.Context(), r.PathValue("taskID"), strings.TrimSpace(in.Status))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) createHandoff(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FromMemberID string `json:"from_member_id"`
		ToMemberID   string `json:"to_member_id"`
		ToRole       string `json:"to_role"`
		Note         string `json:"note"`
		AutoRun      bool   `json:"autorun"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	taskID := r.PathValue("taskID")
	task, err := s.store.GetTask(r.Context(), taskID)
	if err != nil {
		writeError(w, err)
		return
	}
	members, err := s.store.ListMembers(r.Context(), task.ProjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	toID := strings.TrimSpace(in.ToMemberID)
	toRole := strings.ToLower(strings.TrimSpace(in.ToRole))
	if q := r.URL.Query().Get("to_role"); q != "" {
		toRole = strings.ToLower(strings.TrimSpace(q))
	}
	if toID == "" && toRole != "" {
		if m := models.MemberByRole(members, toRole); m != nil {
			toID = m.ID
		}
	}
	fromID := strings.TrimSpace(in.FromMemberID)
	if fromID == "" {
		if m := models.MemberByRole(members, models.RoleOwner); m != nil {
			fromID = m.ID
		} else {
			for _, mem := range members {
				if mem.Kind == "human" {
					fromID = mem.ID
					break
				}
			}
		}
	}
	if fromID == "" || toID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from_member_id and to_member_id or to_role are required"})
		return
	}
	h, err := s.store.CreateHandoff(r.Context(), taskID, fromID, toID, in.Note)
	if err != nil {
		writeError(w, err)
		return
	}
	s.notify(r.Context(), task.ProjectID, toID, "handoff",
		"Task handed to you: "+task.Title, in.Note,
		"#/projects/"+task.ProjectID+"/tasks/"+taskID)
	autorun := in.AutoRun || r.URL.Query().Get("autorun") == "1"
	if !autorun {
		if proj, err := s.store.GetProject(r.Context(), task.ProjectID); err == nil && proj.AutoRun {
			autorun = true
		}
	}
	var run *models.Run
	if autorun {
		if to, err := s.store.GetMember(r.Context(), toID); err == nil && strings.EqualFold(to.Role, models.RoleBuilder) {
			run, _ = s.store.CreateRun(r.Context(), taskID)
		}
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": h.ID, "task_id": h.TaskID, "from_member_id": h.FromMemberID, "to_member_id": h.ToMemberID,
		"note": h.Note, "status": h.Status, "created_at": h.CreatedAt, "run": run,
	})
}

func (s *Server) completeHandoff(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.CompleteHandoff(r.Context(), r.PathValue("handoffID"))
	if err != nil {
		writeError(w, err)
		return
	}
	var decision *models.Decision
	from, _ := s.store.GetMember(r.Context(), h.FromMemberID)
	to, _ := s.store.GetMember(r.Context(), h.ToMemberID)
	scoutSide := (from != nil && strings.EqualFold(from.Role, models.RoleScout)) ||
		(to != nil && strings.EqualFold(to.Role, models.RoleScout))
	if scoutSide {
		task, err := s.store.GetTask(r.Context(), h.TaskID)
		if err == nil && (models.TaskLooksAmbiguous(task.Title, h.Note) || r.URL.Query().Get("decision") == "1") {
			decision, _ = s.openDecision(r.Context(), task.ProjectID,
				"Scout: is Task \""+task.Title+"\" ready for Builder?",
				"handoff to Builder",
				[]string{"handoff to Builder", "needs more info"},
				"",
			)
			s.notifyNewDecision(r.Context(), decision)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": h.ID, "task_id": h.TaskID, "from_member_id": h.FromMemberID, "to_member_id": h.ToMemberID,
		"note": h.Note, "status": h.Status, "created_at": h.CreatedAt, "completed_at": h.CompletedAt,
		"decision": decision,
	})
}

func (s *Server) listDecisions(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListDecisions(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeError(w, err)
		return
	}
	if r.URL.Query().Get("inbox") == "1" {
		open := items[:0]
		for _, d := range items {
			if d.Answer == "" && !d.Reused {
				open = append(open, d)
			}
		}
		items = open
	}
	if r.URL.Query().Get("mine") == "1" {
		memberID := s.resolveNotifyMember(r)
		if memberID != "" {
			mine := items[:0]
			for _, d := range items {
				if d.AssigneeMemberID == "" || d.AssigneeMemberID == memberID {
					mine = append(mine, d)
				}
			}
			items = mine
		}
	}
	writeItems(w, items)
}

func (s *Server) listDecisionMemories(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListDecisionMemories(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}

func (s *Server) createDecision(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt           string   `json:"prompt"`
		Options          []string `json:"options"`
		Recommendation   string   `json:"recommendation"`
		AssigneeID       string   `json:"assignee_id"`
		AssigneeMemberID string   `json:"assignee_member_id"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Prompt) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt is required"})
		return
	}
	assignee := strings.TrimSpace(in.AssigneeID)
	if assignee == "" {
		assignee = strings.TrimSpace(in.AssigneeMemberID)
	}
	d, err := s.openDecision(r.Context(), r.PathValue("projectID"), strings.TrimSpace(in.Prompt), in.Recommendation, in.Options, assignee)
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyNewDecision(r.Context(), d)
	writeJSON(w, http.StatusCreated, d)
}

func (s *Server) openDecision(ctx context.Context, projectID, prompt, recommendation string, options []string, assigneeMemberID string) (*models.Decision, error) {
	fp := models.DecisionFingerprint(prompt)
	if mem, err := s.store.GetDecisionMemory(ctx, projectID, fp); err == nil && mem != nil && mem.Answer != "" {
		return s.store.CreateReusedDecision(ctx, projectID, prompt, recommendation, options, mem.Answer, assigneeMemberID)
	}
	return s.store.CreateDecision(ctx, projectID, prompt, recommendation, options, assigneeMemberID)
}

func (s *Server) answerDecision(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Answer string `json:"answer"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Answer) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "answer is required"})
		return
	}
	d, err := s.store.AnswerDecision(r.Context(), r.PathValue("decisionID"), strings.TrimSpace(in.Answer))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) listActivity(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListActivity(r.Context(), r.PathValue("projectID"), r.URL.Query().Get("type"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.CreateRun(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeError(w, err)
		return
	}
	s.recordRunEvent(r.Context(), run.ID, models.RunEventStatus, map[string]any{"status": run.Status, "detail": run.Detail})
	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetRun(r.Context(), r.PathValue("runID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) updateRun(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
		Detail string `json:"detail"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Status) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status is required"})
		return
	}
	run, err := s.store.UpdateRun(r.Context(), r.PathValue("runID"), strings.TrimSpace(in.Status), in.Detail)
	if err != nil {
		writeError(w, err)
		return
	}
	s.recordRunEvent(r.Context(), run.ID, models.RunEventStatus, map[string]any{"status": run.Status, "detail": run.Detail})
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) recordRunEvent(ctx context.Context, runID, kind string, payload map[string]any) *models.RunEvent {
	ev, err := s.store.AppendRunEvent(ctx, runID, kind, payload)
	if err != nil {
		return nil
	}
	s.hub.PublishRun(runID, ev)
	return ev
}

func (s *Server) createRunEvent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
	}
	if err := decodeJSON(r, &in); err != nil || models.NormalizeRunEventKind(in.Kind) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "kind must be token, tool_call, tool_result, status, or log"})
		return
	}
	ev := s.recordRunEvent(r.Context(), r.PathValue("runID"), in.Kind, in.Payload)
	if ev == nil {
		if _, err := s.store.GetRun(r.Context(), r.PathValue("runID")); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not append run event"})
		return
	}
	writeJSON(w, http.StatusCreated, ev)
}

func (s *Server) listRunEvents(w http.ResponseWriter, r *http.Request) {
	after := 0
	if q := strings.TrimSpace(r.URL.Query().Get("after")); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "after must be a sequence number"})
			return
		}
		after = n
	}
	items, err := s.store.ListRunEvents(r.Context(), r.PathValue("runID"), after)
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}
