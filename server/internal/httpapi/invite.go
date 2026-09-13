package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/codemodify/buildbee/server/internal/models"
	"github.com/codemodify/buildbee/server/internal/store"
)

func inviteToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func inviteJSON(inv models.Invite, projectName string) map[string]any {
	return map[string]any{
		"id":                   inv.ID,
		"project_id":           inv.ProjectID,
		"project_name":         projectName,
		"email":                inv.Email,
		"github_login":         inv.GitHubLogin,
		"role":                 inv.Role,
		"token":                inv.Token,
		"path":                 models.InvitePath(inv.Token),
		"invited_by_member_id": inv.InvitedByMemberID,
		"accepted_member_id":   inv.AcceptedMemberID,
		"status":               inv.Status(),
		"created_at":           inv.CreatedAt,
		"accepted_at":          inv.AcceptedAt,
		"revoked_at":           inv.RevokedAt,
	}
}

func (s *Server) actorOnProject(r *http.Request, projectID string) (*models.Member, error) {
	members, err := s.store.ListMembers(r.Context(), projectID)
	if err != nil {
		return nil, err
	}
	if id := strings.TrimSpace(r.Header.Get("X-Member-ID")); id != "" {
		for i := range members {
			if members[i].ID == id {
				return &members[i], nil
			}
		}
		return nil, store.ErrForbidden
	}
	ident, _, ok := s.auth.Me(r)
	if ok && ident.GitHubLogin != "" {
		for i := range members {
			if strings.EqualFold(members[i].GitHubLogin, ident.GitHubLogin) {
				return &members[i], nil
			}
		}
	}
	if s.auth.Dev() {
		if m := models.MemberByRole(members, models.RoleOwner); m != nil {
			return m, nil
		}
		for i := range members {
			if members[i].Kind == "human" {
				return &members[i], nil
			}
		}
	}
	return nil, store.ErrForbidden
}

func (s *Server) requireInviteManager(w http.ResponseWriter, r *http.Request, projectID string) *models.Member {
	actor, err := s.actorOnProject(r, projectID)
	if err != nil {
		writeError(w, err)
		return nil
	}
	if !models.CanManageInvites(actor.Role) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "owner or admin can invite"})
		return nil
	}
	return actor
}

func (s *Server) createInvite(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	actor := s.requireInviteManager(w, r, projectID)
	if actor == nil {
		return
	}
	var in struct {
		Email       string `json:"email"`
		GitHubLogin string `json:"github_login"`
		Role        string `json:"role"`
	}
	_ = decodeJSON(r, &in)
	email := strings.TrimSpace(in.Email)
	login := strings.TrimSpace(in.GitHubLogin)
	if email == "" && login == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email or github_login is required"})
		return
	}
	role := strings.ToLower(strings.TrimSpace(in.Role))
	if role == "" {
		role = models.RoleMember
	}
	if role != models.RoleMember && role != models.RoleAdmin {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be member or admin"})
		return
	}
	inv, err := s.store.CreateInvite(r.Context(), models.Invite{
		ProjectID:         projectID,
		Email:             email,
		GitHubLogin:       login,
		Role:              role,
		Token:             inviteToken(),
		InvitedByMemberID: actor.ID,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	name := ""
	if p, err := s.store.GetProject(r.Context(), projectID); err == nil {
		name = p.Name
	}
	writeJSON(w, http.StatusCreated, inviteJSON(*inv, name))
}

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectID")
	actor, err := s.actorOnProject(r, projectID)
	if err != nil {
		writeError(w, err)
		return
	}
	if actor.Kind != "human" && !models.CanManageInvites(actor.Role) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "members can read invites"})
		return
	}
	pendingOnly := r.URL.Query().Get("all") != "1"
	items, err := s.store.ListInvites(r.Context(), projectID, pendingOnly)
	if err != nil {
		writeError(w, err)
		return
	}
	name := ""
	if p, err := s.store.GetProject(r.Context(), projectID); err == nil {
		name = p.Name
	}
	out := make([]map[string]any, 0, len(items))
	for _, inv := range items {
		out = append(out, inviteJSON(inv, name))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) getInvitePreview(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.GetInviteByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		writeError(w, err)
		return
	}
	name := ""
	if p, err := s.store.GetProject(r.Context(), inv.ProjectID); err == nil {
		name = p.Name
	}
	writeJSON(w, http.StatusOK, inviteJSON(*inv, name))
}

func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.GetInviteByToken(r.Context(), r.PathValue("token"))
	if err != nil {
		writeError(w, err)
		return
	}
	if inv.Status() != "pending" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "invite " + inv.Status()})
		return
	}
	var in struct {
		DisplayName string `json:"display_name"`
		GitHubLogin string `json:"github_login"`
	}
	_ = decodeJSON(r, &in)

	member, already, err := s.resolveJoiner(r, inv, strings.TrimSpace(in.DisplayName), strings.TrimSpace(in.GitHubLogin))
	if err != nil {
		writeError(w, err)
		return
	}
	if already != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"already_member": true, "member": already, "invite": inviteJSON(*inv, ""),
		})
		return
	}
	created, err := s.store.AddMember(r.Context(), *member)
	if err != nil {
		writeError(w, err)
		return
	}
	accepted, err := s.store.AcceptInvite(r.Context(), inv.ID, created.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	s.notify(r.Context(), inv.ProjectID, inv.InvitedByMemberID, "invite",
		"Invite accepted: "+created.DisplayName, created.DisplayName,
		"#/projects/"+inv.ProjectID)
	name := ""
	if p, err := s.store.GetProject(r.Context(), inv.ProjectID); err == nil {
		name = p.Name
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"already_member": false, "member": created, "invite": inviteJSON(*accepted, name),
	})
}

func (s *Server) revokeInvite(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.GetInvite(r.Context(), r.PathValue("inviteID"))
	if err != nil {
		writeError(w, err)
		return
	}
	if s.requireInviteManager(w, r, inv.ProjectID) == nil {
		return
	}
	revoked, err := s.store.RevokeInvite(r.Context(), inv.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inviteJSON(*revoked, ""))
}

func (s *Server) resolveJoiner(r *http.Request, inv *models.Invite, displayName, githubLogin string) (newMember *models.Member, already *models.Member, err error) {
	members, err := s.store.ListMembers(r.Context(), inv.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	ident, _, signedIn := s.auth.Me(r)
	if !s.auth.Dev() && !signedIn {
		return nil, nil, store.ErrForbidden
	}
	if inv.GitHubLogin != "" {
		sessionLogin := ident.GitHubLogin
		if githubLogin == "" {
			githubLogin = sessionLogin
		}
		if sessionLogin != "" && !strings.EqualFold(sessionLogin, inv.GitHubLogin) {
			return nil, nil, store.ErrForbidden
		}
		if githubLogin != "" && !strings.EqualFold(githubLogin, inv.GitHubLogin) {
			return nil, nil, store.ErrForbidden
		}
	}

	if id := strings.TrimSpace(r.Header.Get("X-Member-ID")); id != "" {
		src, err := s.store.GetMember(r.Context(), id)
		if err != nil {
			return nil, nil, err
		}
		for i := range members {
			if members[i].ID == src.ID {
				return nil, &members[i], nil
			}
			if src.GitHubLogin != "" && strings.EqualFold(members[i].GitHubLogin, src.GitHubLogin) {
				return nil, &members[i], nil
			}
		}
		copied := *src
		copied.ID = ""
		copied.ProjectID = inv.ProjectID
		copied.Kind = "human"
		copied.Role = inv.Role
		copied.Instructions = ""
		return &copied, nil, nil
	}

	login := githubLogin
	if login == "" {
		login = ident.GitHubLogin
	}
	if login != "" {
		for i := range members {
			if strings.EqualFold(members[i].GitHubLogin, login) {
				return nil, &members[i], nil
			}
		}
	}

	name := displayName
	if name == "" {
		name = ident.DisplayName
	}
	if name == "" {
		name = login
	}
	if name == "" {
		name = "Member"
	}

	// Dev session "You" already owns the Project — treat as already a Member
	// unless the accepter supplied a distinct display name or GitHub login.
	if s.auth.Dev() && displayName == "" && githubLogin == "" {
		if m := models.MemberByRole(members, models.RoleOwner); m != nil {
			return nil, m, nil
		}
	}

	identity := ident.ID
	if identity == "" || identity == "dev" {
		identity = "human:" + strings.ToLower(name)
	}
	return &models.Member{
		ProjectID:   inv.ProjectID,
		Kind:        "human",
		DisplayName: name,
		Role:        inv.Role,
		Identity:    identity,
		GitHubLogin: login,
		GitHubID:    ident.GitHubID,
	}, nil, nil
}
