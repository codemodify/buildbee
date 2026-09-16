package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/codemodify/buildbee/internal/models"
)

// api is the worker's view of the Server. Every request carries
// X-BuildBee-Worker so the Server knows which worker owns which Run.
type api struct {
	base string
	name string
	http *http.Client
}

// errConflict is the Server refusing a write (409), e.g. the Run was
// canceled or claimed by another worker.
var errConflict = errors.New("conflict")

// errRejected is the Server refusing a request as invalid (4xx other than
// 409): retrying the same request will not help.
var errRejected = errors.New("rejected")

func newAPI(base, name string) *api {
	// No client timeout: claims long-poll. Each call bounds itself by ctx.
	return &api{base: strings.TrimRight(base, "/"), name: name, http: &http.Client{}}
}

// claim asks for a queued Run, waiting up to wait. It returns nil, nil when
// there is none.
func (a *api) claim(ctx context.Context, agents []string, slots int, wait time.Duration) (*models.Claim, error) {
	var c models.Claim
	ok, err := a.do(ctx, http.MethodPost, "/v1/worker/claim",
		map[string]any{"agents": agents, "slots": slots, "wait_seconds": wait.Seconds()}, &c)
	if err != nil || !ok {
		return nil, err
	}
	return &c, nil
}

// file downloads a Task attachment the worker's Run may read.
func (a *api) file(ctx context.Context, runID, fileID, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base+"/v1/runs/"+runID+"/files/"+fileID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-BuildBee-Worker", a.name)
	res, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("%s: %s", res.Status, bytes.TrimSpace(msg))
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, res.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func (a *api) heartbeat(ctx context.Context, runID string) (*models.Run, error) {
	var r models.Run
	_, err := a.do(ctx, http.MethodPost, "/v1/runs/"+runID+"/heartbeat", nil, &r)
	return &r, err
}

func (a *api) report(ctx context.Context, runID string, status models.RunStatus, o outcome) error {
	// The Server counts characters; keep well inside its limits.
	_, err := a.do(ctx, http.MethodPatch, "/v1/runs/"+runID, map[string]string{"status": string(status),
		"detail": truncate(o.detail, 1900), "summary": truncate(o.summary, 15000), "branch": o.branch, "commit": o.commit, "pr_url": o.prURL}, nil)
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}

func (a *api) event(ctx context.Context, runID, kind string, payload map[string]any) error {
	_, err := a.do(ctx, http.MethodPost, "/v1/runs/"+runID+"/events", map[string]any{"kind": kind, "payload": payload}, nil)
	return err
}

func (a *api) artifact(ctx context.Context, taskID string, in newArtifact) error {
	_, err := a.do(ctx, http.MethodPost, "/v1/tasks/"+taskID+"/artifacts", in, nil)
	return err
}

type newArtifact struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Body  string `json:"body,omitempty"`
	URL   string `json:"url,omitempty"`
	RunID string `json:"run_id"`
}

// do sends one request. ok is false for 204 No Content.
func (a *api) do(ctx context.Context, method, path string, body, dest any) (ok bool, err error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return false, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, rdr)
	if err != nil {
		return false, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-BuildBee-Worker", a.name)
	res, err := a.http.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return false, err
	}
	switch {
	case res.StatusCode == http.StatusNoContent:
		return false, nil
	case res.StatusCode == http.StatusConflict:
		return false, fmt.Errorf("%s %s: %w: %s", method, path, errConflict, strings.TrimSpace(string(raw)))
	case res.StatusCode >= 400 && res.StatusCode < 500:
		return false, fmt.Errorf("%s %s: %w: %s: %s", method, path, errRejected, res.Status, strings.TrimSpace(string(raw)))
	case res.StatusCode >= 300:
		return false, fmt.Errorf("%s %s: %s: %s", method, path, res.Status, strings.TrimSpace(string(raw)))
	}
	if dest != nil && len(raw) > 0 {
		return true, json.Unmarshal(raw, dest)
	}
	return true, nil
}
