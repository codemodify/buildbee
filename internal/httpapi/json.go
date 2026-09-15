package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/codemodify/buildbee/internal/core"
	"github.com/codemodify/buildbee/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// fail maps an error to a status. Unexpected errors are logged with their
// cause and reported as a generic 500, so driver errors never reach clients.
func (s *Server) fail(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, core.ErrNoActor):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
	case errors.Is(err, core.ErrUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	case errors.As(err, &tooBig):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body is too large"})
	case errors.Is(err, context.Canceled):
		// The client went away (a worker stopping a Run, a closed tab);
		// nobody reads this answer and nothing is wrong with the Server.
		w.WriteHeader(499)
	default:
		s.log.Error("request failed", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
	}
}

// decode reads a JSON body into dst after replacing NUL escapes Postgres
// cannot store. An empty body leaves dst unchanged. On error it answers
// the request and returns false.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.fail(w, err)
		return false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	if err := json.Unmarshal(sanitizeJSON(raw), dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body: " + err.Error()})
		return false
	}
	return true
}

// items writes a list as {"items": [...]}.
func items[T any](w http.ResponseWriter, list []T) {
	if list == nil {
		list = []T{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

// page writes a cursor page as {"items": [...], "has_more": bool}.
func page[T any](w http.ResponseWriter, list []T, more bool) {
	if list == nil {
		list = []T{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "has_more": more})
}

// pageParams reads ?before=, ?after= and ?limit=.
func pageParams(r *http.Request) (store.Page, error) {
	q := r.URL.Query()
	var p store.Page
	for key, dst := range map[string]*int64{"before": &p.Before, "after": &p.After} {
		if v := q.Get(key); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				return p, errors.Join(store.ErrInvalid, errors.New(key+" must be a non-negative integer"))
			}
			*dst = n
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return p, errors.Join(store.ErrInvalid, errors.New("limit must be a positive integer"))
		}
		p.Limit = n
	}
	return p, nil
}

func flag(r *http.Request, key string) bool {
	switch r.URL.Query().Get(key) {
	case "1", "true", "yes":
		return true
	}
	return false
}
