package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// latencyBuckets are the upper bounds, in seconds, of the request
// duration histogram.
var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// requestMetrics counts requests by method and status and times them.
// Long-polls and WebSockets are counted but not timed: their duration is
// how long a client waited or stayed, not how fast the Server answered.
type requestMetrics struct {
	mu     sync.Mutex
	counts map[[2]string]uint64 // method, code
	hist   []uint64             // per bucket, then +Inf
	sum    float64
	timed  uint64
}

func newRequestMetrics() *requestMetrics {
	return &requestMetrics{counts: map[[2]string]uint64{}, hist: make([]uint64, len(latencyBuckets)+1)}
}

func (m *requestMetrics) observe(method string, code int, d time.Duration, timed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counts[[2]string{method, strconv.Itoa(code)}]++
	if !timed {
		return
	}
	sec := d.Seconds()
	i, _ := slices.BinarySearch(latencyBuckets, sec)
	m.hist[i]++
	m.sum += sec
	m.timed++
}

// untimed are paths whose requests wait on purpose.
func untimed(path string) bool {
	return path == "/v1/worker/claim" || path == "/v1/ws" || strings.HasSuffix(path, "/ws")
}

// serveMetrics writes Prometheus text: requests, and what the Server's
// agents and people are doing.
func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	snap, err := s.core.Metrics(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	p := func(format string, args ...any) { fmt.Fprintf(w, format, args...) }
	help := func(name, kind, text string) { p("# HELP %s %s\n# TYPE %s %s\n", name, text, name, kind) }

	help("buildbee_runs", "gauge", "Runs by status.")
	for _, st := range sortedKeys(snap.RunsByStatus) {
		p("buildbee_runs{status=%q} %d\n", st, snap.RunsByStatus[st])
	}
	help("buildbee_bot_runs", "gauge", "Runs running and queued, by Bot.")
	for _, b := range snap.Presence.Bots {
		w := snap.Presence.Work[b.BotID]
		p("buildbee_bot_runs{bot=%s,ai=%s,state=\"running\"} %d\n", label(b.Name), label(b.AI), w.Running)
		p("buildbee_bot_runs{bot=%s,ai=%s,state=\"queued\"} %d\n", label(b.Name), label(b.AI), w.Queued)
	}
	help("buildbee_bots_connected", "gauge", "Bots whose agent has been seen in the last 90 seconds.")
	p("buildbee_bots_connected %d\n", len(snap.Presence.Bots))
	help("buildbee_bot_slots", "gauge", "Runs the connected Bots take at once.")
	p("buildbee_bot_slots %d\n", snap.Presence.Slots)
	help("buildbee_people_online", "gauge", "People with the app open.")
	p("buildbee_people_online %d\n", len(snap.Presence.People))

	m := s.metrics
	m.mu.Lock()
	defer m.mu.Unlock()
	help("buildbee_http_requests_total", "counter", "HTTP requests by method and status.")
	keys := make([][2]string, 0, len(m.counts))
	for k := range m.counts {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b [2]string) int { return strings.Compare(a[0]+a[1], b[0]+b[1]) })
	for _, k := range keys {
		p("buildbee_http_requests_total{method=%s,code=%q} %d\n", label(k[0]), k[1], m.counts[k])
	}
	help("buildbee_http_request_duration_seconds", "histogram", "HTTP request durations, long-polls and WebSockets excluded.")
	var cum uint64
	for i, le := range latencyBuckets {
		cum += m.hist[i]
		p("buildbee_http_request_duration_seconds_bucket{le=%q} %d\n", strconv.FormatFloat(le, 'g', -1, 64), cum)
	}
	cum += m.hist[len(latencyBuckets)]
	p("buildbee_http_request_duration_seconds_bucket{le=\"+Inf\"} %d\n", cum)
	p("buildbee_http_request_duration_seconds_sum %g\n", m.sum)
	p("buildbee_http_request_duration_seconds_count %d\n", m.timed)
	_, _ = io.WriteString(w, "")
}

// label quotes a label value the Prometheus way.
func label(v string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(v) + `"`
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
