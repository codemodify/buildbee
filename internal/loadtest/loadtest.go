// Package loadtest drives a Server with many Runs at once, executed by
// real workers whose agent is simulated, and reports how it kept up.
package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/codemodify/buildbee/internal/models"
	"github.com/codemodify/buildbee/internal/worker"
	"github.com/codemodify/buildbee/internal/worker/acp"
)

// Options is one load test.
type Options struct {
	Server   string        // Server base URL
	As       string        // the Person queueing the work
	Runs     int           // Runs to queue
	Workers  int           // workers to start
	Slots    int           // Runs each worker executes at once
	Work     time.Duration // how long one simulated agent turn takes
	Events   int           // events one Run streams
	Deadline time.Duration // give up after this
	Log      *slog.Logger
}

// Report is what the run showed.
type Report struct {
	Runs          int           `json:"runs"`
	Succeeded     int           `json:"succeeded"`
	Failed        int           `json:"failed"`
	Wall          time.Duration `json:"wall"`
	RunsPerMinute float64       `json:"runs_per_minute"`
	PeakRunning   int           `json:"peak_running"`
	WaitP50       time.Duration `json:"wait_p50"`  // queued until a worker took it
	WaitP95       time.Duration `json:"wait_p95"`
	RunP50        time.Duration `json:"run_p50"`   // taken until finished
	RunP95        time.Duration `json:"run_p95"`
	EventsPerSec  float64       `json:"events_per_second"`
	Errors        []string      `json:"errors,omitempty"`
}

func (r Report) String() string {
	return fmt.Sprintf(`runs:        %d (%d succeeded, %d failed)
wall:        %s  (%.0f runs/minute)
concurrency: %d at once (peak)
queue wait:  p50 %s  p95 %s
run time:    p50 %s  p95 %s
events:      %.0f/second`, r.Runs, r.Succeeded, r.Failed, r.Wall.Round(time.Millisecond), r.RunsPerMinute, r.PeakRunning,
		r.WaitP50.Round(time.Millisecond), r.WaitP95.Round(time.Millisecond),
		r.RunP50.Round(time.Millisecond), r.RunP95.Round(time.Millisecond), r.EventsPerSec)
}

// Run queues the work, runs the workers and reports. It leaves the Project
// it made behind, archived.
func Run(ctx context.Context, o Options) (Report, error) {
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if o.As == "" {
		o.As = "loadtest"
	}
	if o.Work <= 0 {
		o.Work = 2 * time.Second
	}
	if o.Events <= 0 {
		o.Events = 40
	}
	if o.Deadline <= 0 {
		o.Deadline = 10 * time.Minute
	}
	c := &client{base: o.Server, as: o.As, http: &http.Client{Timeout: 60 * time.Second}}
	var rep Report
	rep.Runs = o.Runs

	var project struct {
		ID string `json:"id"`
	}
	name := fmt.Sprintf("loadtest %d", time.Now().UnixNano())
	if err := c.do(ctx, http.MethodPost, "/v1/projects", map[string]any{"name": name}, &project); err != nil {
		return rep, err
	}
	runIDs := make([]string, 0, o.Runs)
	for i := range o.Runs {
		var task, run struct {
			ID string `json:"id"`
		}
		if err := c.do(ctx, http.MethodPost, "/v1/projects/"+project.ID+"/tasks",
			map[string]any{"title": fmt.Sprintf("load %d", i+1), "handoff_role": "none"}, &task); err != nil {
			return rep, err
		}
		if err := c.do(ctx, http.MethodPost, "/v1/tasks/"+task.ID+"/runs", map[string]any{"agent": "fake"}, &run); err != nil {
			return rep, err
		}
		runIDs = append(runIDs, run.ID)
	}

	ctx, cancel := context.WithTimeout(ctx, o.Deadline)
	defer cancel()
	start := time.Now()
	var wg sync.WaitGroup
	for i := range o.Workers {
		w, err := worker.New(worker.Config{Server: o.Server, Name: fmt.Sprintf("loadtest-%d", i+1), Agents: []string{"fake"},
			Slots: o.Slots, Exec: simulate(o.Work, o.Events), Log: o.Log})
		if err != nil {
			return rep, err
		}
		wg.Go(func() { w.Run(ctx) })
	}

	peak := 0
	watch := time.NewTicker(250 * time.Millisecond)
	defer watch.Stop()
	for {
		var presence struct {
			Running int `json:"running"`
		}
		if err := c.do(ctx, http.MethodGet, "/v1/presence", nil, &presence); err == nil {
			peak = max(peak, presence.Running)
		}
		done, err := c.finished(ctx, runIDs)
		if err == nil && done {
			break
		}
		select {
		case <-ctx.Done():
			rep.Errors = append(rep.Errors, "gave up waiting: "+ctx.Err().Error())
			cancel()
			wg.Wait()
			return rep, nil
		case <-watch.C:
		}
	}
	rep.Wall = time.Since(start)
	rep.PeakRunning = peak
	cancel()
	wg.Wait()

	var waits, runs []time.Duration
	for _, id := range runIDs {
		var r models.Run
		if err := c.do(context.Background(), http.MethodGet, "/v1/runs/"+id, nil, &r); err != nil {
			rep.Errors = append(rep.Errors, err.Error())
			continue
		}
		switch r.Status {
		case models.RunSucceeded:
			rep.Succeeded++
		default:
			rep.Failed++
			if r.Detail != "" && len(rep.Errors) < 5 {
				rep.Errors = append(rep.Errors, string(r.Status)+": "+r.Detail)
			}
		}
		if r.StartedAt != nil {
			waits = append(waits, r.StartedAt.Sub(r.CreatedAt))
			if r.FinishedAt != nil {
				runs = append(runs, r.FinishedAt.Sub(*r.StartedAt))
			}
		}
	}
	rep.WaitP50, rep.WaitP95 = percentile(waits, 0.5), percentile(waits, 0.95)
	rep.RunP50, rep.RunP95 = percentile(runs, 0.5), percentile(runs, 0.95)
	if rep.Wall > 0 {
		rep.RunsPerMinute = float64(rep.Succeeded) / rep.Wall.Minutes()
		rep.EventsPerSec = float64(rep.Succeeded*(o.Events+3)) / rep.Wall.Seconds()
	}
	archived := true
	_ = c.do(context.Background(), http.MethodPatch, "/v1/projects/"+project.ID, map[string]any{"archived": archived}, nil)
	return rep, nil
}

// simulate is an agent that streams Events pieces over Work and stops.
func simulate(work time.Duration, events int) worker.Exec {
	return func(ctx context.Context, job worker.Job, emit acp.Handler) (string, error) {
		gap := work / time.Duration(max(events, 1))
		if err := emit(acp.Event{Kind: "plan", Payload: map[string]any{"entries": []map[string]string{{"content": "work", "status": "in_progress"}}}}); err != nil {
			return "", err
		}
		var out bytes.Buffer
		for i := range events {
			select {
			case <-ctx.Done():
				return out.String(), ctx.Err()
			case <-time.After(gap):
			}
			text := fmt.Sprintf("step %d of %d\n", i+1, events)
			out.WriteString(text)
			if err := emit(acp.Event{Kind: "token", Payload: map[string]any{"text": text}}); err != nil {
				return out.String(), err
			}
		}
		if err := emit(acp.Event{Kind: "usage", Payload: map[string]any{"used": 1000, "size": 200000}}); err != nil {
			return out.String(), err
		}
		return out.String(), nil
	}
}

type client struct {
	base, as string
	http     *http.Client
}

func (c *client) do(ctx context.Context, method, path string, body, dest any) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BuildBee-As", c.as)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s: %s", method, path, res.Status, bytes.TrimSpace(raw))
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(raw, dest)
}

// finished reports whether every Run has ended.
func (c *client) finished(ctx context.Context, ids []string) (bool, error) {
	var page struct {
		Items []models.RunRow `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/runs", nil, &page); err != nil {
		return false, err
	}
	for _, r := range page.Items {
		if slices.Contains(ids, r.ID) && !r.Status.Terminal() {
			return false, nil
		}
	}
	return true, nil
}

func percentile(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	i := int(math.Ceil(p*float64(len(ds)))) - 1
	return ds[min(max(i, 0), len(ds)-1)]
}
