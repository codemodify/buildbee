package httpapi

import (
	"net/http"
	"testing"

	"github.com/codemodify/buildbee/internal/store"
	"github.com/codemodify/buildbee/internal/testdb"
	"github.com/codemodify/buildbee/internal/ws"
)

func TestMain(m *testing.M) { testdb.Main(m) }

// newTestMux serves the API over a fresh Postgres database.
func newTestMux(t *testing.T) http.Handler { return newTestMuxWith(t, Options{}) }

func newTestMuxWith(t *testing.T, opts Options) http.Handler {
	return NewServer(store.NewPostgres(testdb.New(t)), ws.NewHub(), opts).Handler()
}
