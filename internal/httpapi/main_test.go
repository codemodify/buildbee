package httpapi

import (
	"net/http"
	"testing"

	"github.com/codemodify/buildbee/internal/auth"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/codemodify/buildbee/internal/testdb"
	"github.com/codemodify/buildbee/internal/ws"
)

func TestMain(m *testing.M) { testdb.Main(m) }

// newTestMux serves the API over a fresh Postgres database with dev auth.
func newTestMux(t *testing.T) http.Handler {
	return newSrv(store.NewPostgres(testdb.New(t)), ws.NewHub(), auth.NewDev()).Handler()
}

func newTestMuxSecure(t *testing.T) http.Handler {
	return newSrv(store.NewPostgres(testdb.New(t)), ws.NewHub(), auth.New(auth.Config{ClientID: "test"})).Handler()
}
