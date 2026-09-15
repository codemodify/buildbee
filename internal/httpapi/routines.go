package httpapi

import (
	"net/http"
	"strings"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/routines"
)

func (s *Server) listRoutines(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListRoutines(r.Context(), r.PathValue("projectID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}

func (s *Server) createRoutine(w http.ResponseWriter, r *http.Request) {
	var in models.Routine
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	in.ProjectID = r.PathValue("projectID")
	out, err := s.store.CreateRoutine(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) fireRoutine(w http.ResponseWriter, r *http.Request) {
	out, err := routines.Fire(r.Context(), s.store, r.PathValue("routineID"))
	if err != nil {
		writeError(w, err)
		return
	}
	s.notifyHumans(r.Context(), out.ProjectID, "routine",
		"Routine: "+out.Name, out.Name, "#/projects/"+out.ProjectID)
	writeJSON(w, http.StatusOK, out)
}
