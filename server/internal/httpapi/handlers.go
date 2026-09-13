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
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	p, err := s.store.CreateProject(r.Context(), strings.TrimSpace(in.Name))
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
		DisplayName string `json:"display_name"`
		Kind        string `json:"kind"`
		Role        string `json:"role"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.DisplayName) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "display_name is required"})
		return
	}
	kind := strings.ToLower(in.Kind)
	if kind != "bot" {
		kind = "human"
	}
	role := in.Role
	if role == "" {
		if kind == "bot" {
			role = "bot"
		} else {
			role = "member"
		}
	}
	m, err := s.store.AddMember(r.Context(), models.Member{
		ProjectID:   r.PathValue("projectID"),
		DisplayName: strings.TrimSpace(in.DisplayName),
		Kind:        kind,
		Role:        role,
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
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Title) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
		return
	}
	t, err := s.store.CreateTask(r.Context(), r.PathValue("projectID"), strings.TrimSpace(in.Title), in.AssigneeMemberID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
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
		Note         string `json:"note"`
	}
	if err := decodeJSON(r, &in); err != nil || in.FromMemberID == "" || in.ToMemberID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "from_member_id and to_member_id are required"})
		return
	}
	h, err := s.store.CreateHandoff(r.Context(), r.PathValue("taskID"), in.FromMemberID, in.ToMemberID, in.Note)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, h)
}

func (s *Server) completeHandoff(w http.ResponseWriter, r *http.Request) {
	h, err := s.store.CompleteHandoff(r.Context(), r.PathValue("handoffID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
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
