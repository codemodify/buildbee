package httpapi

import (
	"net/http"
	"strings"

	"github.com/codemodify/buildbee/internal/store"
)

// prefsMember is the Member whose notification preferences a request targets.
func (s *Server) prefsMember(r *http.Request) (string, error) {
	id := strings.TrimSpace(s.resolveNotifyMember(r))
	if id == "" {
		return "", store.ErrNotFound
	}
	if _, err := s.store.GetMember(r.Context(), id); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Server) getPreferences(w http.ResponseWriter, r *http.Request) {
	memberID, err := s.prefsMember(r)
	if err != nil {
		writeError(w, err)
		return
	}
	p, err := s.store.GetPreferences(r.Context(), memberID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) patchPreferences(w http.ResponseWriter, r *http.Request) {
	memberID, err := s.prefsMember(r)
	if err != nil {
		writeError(w, err)
		return
	}
	cur, err := s.store.GetPreferences(r.Context(), memberID)
	if err != nil {
		writeError(w, err)
		return
	}
	var in struct {
		MuteMentions *bool `json:"mute_mentions"`
		MuteRoutines *bool `json:"mute_routines"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if in.MuteMentions != nil {
		cur.MuteMentions = *in.MuteMentions
	}
	if in.MuteRoutines != nil {
		cur.MuteRoutines = *in.MuteRoutines
	}
	cur.MemberID = memberID
	out, err := s.store.SetPreferences(r.Context(), *cur)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
