package httpapi

import (
	"net/http"

	"github.com/codemodify/buildbee/server/internal/models"
	"github.com/codemodify/buildbee/server/internal/store"
)

func (s *Server) prefsIdentity(r *http.Request) (string, error) {
	if id := s.resolveNotifyMember(r); id != "" {
		mem, err := s.store.GetMember(r.Context(), id)
		if err != nil {
			return "", err
		}
		return models.PreferencesKey(*mem), nil
	}
	ident, _, ok := s.auth.Me(r)
	if ok && ident.GitHubLogin != "" {
		rec, err := s.store.UpsertHumanIdentity(r.Context(), ident.GitHubLogin, ident.GitHubID, ident.DisplayName)
		if err != nil {
			return "", err
		}
		return rec.ID, nil
	}
	return "", store.ErrForbidden
}

func (s *Server) getPreferences(w http.ResponseWriter, r *http.Request) {
	key, err := s.prefsIdentity(r)
	if err != nil {
		writeError(w, err)
		return
	}
	p, err := s.store.GetPreferences(r.Context(), key)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) patchPreferences(w http.ResponseWriter, r *http.Request) {
	key, err := s.prefsIdentity(r)
	if err != nil {
		writeError(w, err)
		return
	}
	cur, err := s.store.GetPreferences(r.Context(), key)
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
	cur.Identity = key
	out, err := s.store.SetPreferences(r.Context(), *cur)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
