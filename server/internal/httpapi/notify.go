package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/codemodify/buildbee/server/internal/models"
)

func (s *Server) notify(ctx context.Context, projectID, memberID, kind, title, body, href string) {
	if memberID == "" || projectID == "" {
		return
	}
	_, _ = s.store.CreateNotification(ctx, models.Notification{
		ProjectID: projectID, MemberID: memberID, Kind: kind,
		Title: title, Body: body, Href: href,
	})
}

func (s *Server) notifyHumans(ctx context.Context, projectID, kind, title, body, href string) {
	members, err := s.store.ListMembers(ctx, projectID)
	if err != nil {
		return
	}
	for _, m := range members {
		if m.Kind == "human" {
			s.notify(ctx, projectID, m.ID, kind, title, body, href)
		}
	}
}

func (s *Server) resolveNotifyMember(r *http.Request) string {
	id := strings.TrimSpace(r.URL.Query().Get("member_id"))
	if id == "" {
		id = strings.TrimSpace(r.Header.Get("X-Member-ID"))
	}
	return id
}

func (s *Server) notifyNewDecision(ctx context.Context, d *models.Decision) {
	if d == nil || d.Reused {
		return
	}
	s.notifyHumans(ctx, d.ProjectID, "decision",
		"Decision needed: "+d.Prompt, d.Prompt,
		"#/projects/"+d.ProjectID)
}

func (s *Server) maybeNotifyPipeline(ctx context.Context, p *models.Pipeline) {
	if p == nil || p.Status != "failure" {
		return
	}
	t, err := s.store.GetTask(ctx, p.TaskID)
	if err != nil || t == nil {
		return
	}
	title := "Pipeline failed: " + p.Name
	href := "#/projects/" + t.ProjectID + "/tasks/" + t.ID
	seen := map[string]bool{}
	if t.AssigneeMemberID != "" {
		s.notify(ctx, t.ProjectID, t.AssigneeMemberID, "pipeline", title, p.Name, href)
		seen[t.AssigneeMemberID] = true
	}
	members, err := s.store.ListMembers(ctx, t.ProjectID)
	if err != nil {
		return
	}
	for _, m := range members {
		if m.Kind == "human" && !seen[m.ID] {
			s.notify(ctx, t.ProjectID, m.ID, "pipeline", title, p.Name, href)
		}
	}
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	memberID := s.resolveNotifyMember(r)
	unreadOnly := r.URL.Query().Get("unread") == "1"
	items, err := s.store.ListNotifications(r.Context(), memberID, unreadOnly)
	if err != nil {
		writeError(w, err)
		return
	}
	unreadItems, err := s.store.ListNotifications(r.Context(), memberID, true)
	if err != nil {
		writeError(w, err)
		return
	}
	if items == nil {
		items = []models.Notification{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "unread": len(unreadItems)})
}

func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.MarkNotificationRead(r.Context(), r.PathValue("notificationID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	memberID := s.resolveNotifyMember(r)
	if memberID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "member_id is required"})
		return
	}
	n, err := s.store.MarkAllNotificationsRead(r.Context(), memberID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"read": n})
}
