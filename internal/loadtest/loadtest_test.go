package loadtest

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codemodify/buildbee/internal/core"
	"github.com/codemodify/buildbee/internal/httpapi"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/codemodify/buildbee/internal/testdb"
	"github.com/codemodify/buildbee/internal/ws"
)

func TestMain(m *testing.M) { testdb.Main(m) }

func TestLoadTestDrivesRunsThroughWorkers(t *testing.T) {
	var svc *core.Service
	hub := ws.NewHub(func(ctx context.Context, topic string, after int64) ([]core.Event, error) {
		return svc.Replay(ctx, topic, after)
	}, nil)
	t.Cleanup(hub.Close)
	svc = core.New(store.New(testdb.New(t)), hub, core.Options{})
	srv := httptest.NewServer(httpapi.NewServer(svc, hub, httpapi.Options{}).Handler())
	t.Cleanup(srv.Close)

	rep, err := Run(context.Background(), Options{Server: srv.URL, Runs: 6, Bots: 2, Slots: 2,
		Work: 150 * time.Millisecond, Events: 4, Deadline: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Succeeded != 6 || rep.Failed != 0 || len(rep.Errors) != 0 {
		t.Fatalf("report: %+v", rep)
	}
	if rep.PeakRunning < 2 {
		t.Errorf("runs went one at a time: %+v", rep)
	}
	if rep.RunP50 < 100*time.Millisecond || rep.Wall == 0 || rep.RunsPerMinute == 0 {
		t.Errorf("timings: %s", rep)
	}
}
