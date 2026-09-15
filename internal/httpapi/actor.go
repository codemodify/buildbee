package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/codemodify/buildbee/internal/core"
)

// There is no login on a LAN deployment. The acting Person comes from:
//   - the buildbee_person cookie, set by POST /v1/me (the web UI), or
//   - the X-BuildBee-As header carrying a name (CLI, scripts).
//
// Workers identify with X-BuildBee-Worker; their changes are attributed to
// the Bot running the Run. Requests with none of these are anonymous and
// may read, but actions that need a Person answer 401.
const (
	personCookie = "buildbee_person"
	asHeader     = "X-BuildBee-As"
	workerHeader = "X-BuildBee-Worker"
)

type actorKey struct{}

func actorFrom(ctx context.Context) core.Actor {
	a, _ := ctx.Value(actorKey{}).(core.Actor)
	return a
}

// withActor resolves who is acting once per request.
func (s *Server) withActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := core.Actor{Name: "anonymous"}
		switch {
		case strings.TrimSpace(r.Header.Get(asHeader)) != "":
			p, err := s.core.Hello(r.Context(), r.Header.Get(asHeader))
			if err != nil {
				s.fail(w, err)
				return
			}
			a = core.Actor{PersonID: p.ID, Name: p.Name}
		case strings.TrimSpace(r.Header.Get(workerHeader)) != "":
			a = core.WorkerActor(strings.TrimSpace(r.Header.Get(workerHeader)))
		default:
			if c, err := r.Cookie(personCookie); err == nil && c.Value != "" {
				if p, err := s.core.Person(r.Context(), c.Value); err == nil {
					a = core.Actor{PersonID: p.ID, Name: p.Name}
				} else {
					clearPersonCookie(w) // e.g. the database was recreated
				}
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
}

func setPersonCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{Name: personCookie, Value: id, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 400 * 24 * 3600})
}

func clearPersonCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: personCookie, Value: "", Path: "/", MaxAge: -1})
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	a := actorFrom(r.Context())
	if !a.IsPerson() {
		writeJSON(w, http.StatusOK, map[string]any{"person": nil})
		return
	}
	p, err := s.core.Person(r.Context(), a.PersonID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"person": p})
}

// postMe sets who this browser is: {"name": "Ada"}.
func (s *Server) postMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	p, err := s.core.Hello(r.Context(), in.Name)
	if err != nil {
		s.fail(w, err)
		return
	}
	setPersonCookie(w, p.ID)
	writeJSON(w, http.StatusOK, map[string]any{"person": p})
}

// patchMe renames the Person this browser is.
func (s *Server) patchMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	p, err := s.core.Rename(r.Context(), actorFrom(r.Context()), in.Name)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"person": p})
}

// deleteMe forgets who this browser is (switch to another name).
func (s *Server) deleteMe(w http.ResponseWriter, _ *http.Request) {
	clearPersonCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
