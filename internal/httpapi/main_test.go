package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/codemodify/buildbee/internal/core"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/codemodify/buildbee/internal/testdb"
	"github.com/codemodify/buildbee/internal/ws"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMain(m *testing.M) { testdb.Main(m) }

type stack struct {
	t    *testing.T
	h    http.Handler
	pool *pgxpool.Pool
}

// newStack wires store, core, hub and HTTP over a fresh database.
func newStack(t *testing.T, opts Options) *stack {
	pool := testdb.New(t)
	var svc *core.Service
	hub := ws.NewHub(func(ctx context.Context, topic string, after int64) ([]core.Event, error) {
		return svc.Replay(ctx, topic, after)
	}, nil)
	svc = core.New(store.New(pool), hub, core.Options{GitHub: opts.GitHub})
	t.Cleanup(hub.Close)
	return &stack{t: t, h: NewServer(svc, hub, opts).Handler(), pool: pool}
}

// call makes a request as the named Person ("" = anonymous).
func (s *stack) call(method, path, as string, body any) *httptest.ResponseRecorder {
	s.t.Helper()
	var rdr io.Reader
	if raw, ok := body.(string); ok {
		rdr = bytes.NewBufferString(raw)
	} else if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			s.t.Fatal(err)
		}
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	if as != "" {
		req.Header.Set(asHeader, as)
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

// ok decodes a successful response into T.
func ok[T any](t *testing.T, rec *httptest.ResponseRecorder, want int) T {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status %d want %d: %s", rec.Code, want, rec.Body.String())
	}
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return v
}

type obj = map[string]any

type projectJSON struct {
	ID      string `json:"id"`
	Members []struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		Role     string `json:"role"`
		PersonID string `json:"person_id"`
	} `json:"members"`
	Channels []struct {
		ID string `json:"id"`
	} `json:"channels"`
}

func (p projectJSON) role(role string) string {
	for _, m := range p.Members {
		if m.Role == role {
			return m.ID
		}
	}
	return ""
}

func (s *stack) project(as, name string) projectJSON {
	s.t.Helper()
	return ok[projectJSON](s.t, s.call(http.MethodPost, "/v1/projects", as, obj{"name": name, "default_bots": true}), http.StatusCreated)
}
