package httpapi

import (
	"net/http"
	"strings"

	"github.com/codemodify/buildbee/server/internal/models"
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
	msg, err := s.store.PostMessage(r.Context(), channelID, memberID, strings.TrimSpace(in.Body))
	if err != nil {
		writeError(w, err)
		return
	}
	s.hub.Publish(channelID, map[string]any{"type": "message", "message": msg})
	writeJSON(w, http.StatusCreated, msg)
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
			decision, _ = s.store.CreateDecision(r.Context(), task.ProjectID,
				"Scout: is Task \""+task.Title+"\" ready for Builder?",
				"handoff to Builder",
				[]string{"handoff to Builder", "needs more info"},
			)
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
	writeItems(w, items)
}

func (s *Server) createDecision(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prompt         string   `json:"prompt"`
		Options        []string `json:"options"`
		Recommendation string   `json:"recommendation"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Prompt) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt is required"})
		return
	}
	d, err := s.store.CreateDecision(r.Context(), r.PathValue("projectID"), strings.TrimSpace(in.Prompt), in.Recommendation, in.Options)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
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
	writeJSON(w, http.StatusOK, run)
}
