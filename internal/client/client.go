package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/codemodify/buildbee/internal/worker/acp"
)

type Client struct {
	Base   string
	HTTP   *http.Client
	Output io.Writer
}

func New() *Client {
	base := os.Getenv("BUILDBEE_URL")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	return &Client{
		Base:   strings.TrimRight(base, "/"),
		HTTP:   &http.Client{Timeout: 3 * time.Minute},
		Output: os.Stdout,
	}
}

func (c *Client) do(method, path string, body any, dest any) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.Base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %s", method, path, strings.TrimSpace(string(raw)))
	}
	if dest != nil {
		return json.Unmarshal(raw, dest)
	}
	return nil
}

func (c *Client) PrintJSON(v any) error {
	enc := json.NewEncoder(c.Output)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func (c *Client) CreateProject(name string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodPost, "/v1/projects", map[string]string{"name": name}, &out)
	return out, err
}

func (c *Client) ListTasks(projectID string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodGet, "/v1/projects/"+projectID+"/tasks", nil, &out)
	return out, err
}

func (c *Client) GetTask(taskID string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodGet, "/v1/tasks/"+taskID, nil, &out)
	return out, err
}

func (c *Client) CreateHandoff(taskID, fromID, toID, note, toRole string, autorun bool) (map[string]any, error) {
	var out map[string]any
	path := "/v1/tasks/" + taskID + "/handoffs"
	if autorun {
		path += "?autorun=1"
	}
	body := map[string]string{
		"from_member_id": fromID,
		"to_member_id":   toID,
		"to_role":        toRole,
		"note":           note,
	}
	err := c.do(http.MethodPost, path, body, &out)
	return out, err
}

func (c *Client) CreateRun(taskID string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodPost, "/v1/tasks/"+taskID+"/runs", map[string]string{}, &out)
	return out, err
}

func (c *Client) UpdateRun(runID, status, detail string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodPatch, "/v1/runs/"+runID, map[string]string{"status": status, "detail": detail}, &out)
	return out, err
}

func (c *Client) PostRunEvent(runID, kind string, payload map[string]any) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodPost, "/v1/runs/"+runID+"/events", map[string]any{"kind": kind, "payload": payload}, &out)
	return out, err
}

func (c *Client) ListRunEvents(runID string, after int) (map[string]any, error) {
	var out map[string]any
	path := "/v1/runs/" + runID + "/events"
	if after > 0 {
		path += "?after=" + fmt.Sprintf("%d", after)
	}
	err := c.do(http.MethodGet, path, nil, &out)
	return out, err
}

func (c *Client) CreateArtifact(taskID string, body any) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodPost, "/v1/tasks/"+taskID+"/artifacts", body, &out)
	return out, err
}

func (c *Client) OpenPR(taskID string, fake bool, runID string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodPost, "/v1/tasks/"+taskID+"/pr", map[string]any{"fake": fake, "run_id": runID}, &out)
	return out, err
}

func (c *Client) FakeStartRun(taskID, cmd, repoURL string) (map[string]any, error) {
	run, err := c.CreateRun(taskID)
	if err != nil {
		return nil, err
	}
	runID, _ := run["id"].(string)
	if _, err := c.UpdateRun(runID, "running", "fake sandbox"); err != nil {
		return nil, err
	}
	if _, err := c.UpdateRun(runID, "succeeded", "fake success"); err != nil {
		return nil, err
	}
	logs := "fake sandbox cmd=" + cmd + " repo=" + repoURL + "\n"
	art, err := c.CreateArtifact(taskID, map[string]string{"kind": "log", "name": "sandbox.log", "body": logs, "run_id": runID})
	if err != nil {
		return nil, err
	}
	pr, err := c.OpenPR(taskID, true, runID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"run": run, "artifact": art, "pr": pr, "fake": true}, nil
}

func (c *Client) ListRoutines(projectID string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodGet, "/v1/projects/"+projectID+"/routines", nil, &out)
	return out, err
}

func (c *Client) RunRoutine(id string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodPost, "/v1/routines/"+id+"/run", map[string]string{}, &out)
	return out, err
}

// WorkerStart asks the worker at workerURL to execute an existing Run.
func (c *Client) WorkerStart(workerURL, taskID, runID, repoURL, cmd string, fake, acpMode bool, agent, title, notes string) (map[string]any, error) {
	wk := &Client{Base: strings.TrimRight(workerURL, "/"), HTTP: c.HTTP, Output: c.Output}
	var out map[string]any
	err := wk.do(http.MethodPost, "/runs", map[string]any{
		"task_id": taskID, "run_id": runID, "repo_url": repoURL, "command": cmd,
		"fake": fake, "fake_pr": fake, "acp": acpMode, "agent": agent, "title": title, "notes": notes,
	}, &out)
	return out, err
}

// FakeACPRun records a FakeACP Run end to end without a worker (--acp --agent fake).
func (c *Client) FakeACPRun(taskID, agent string) (map[string]any, error) {
	run, err := c.CreateRun(taskID)
	if err != nil {
		return nil, err
	}
	runID, _ := run["id"].(string)
	return c.completeFakeACP(taskID, runID, agent)
}

func (c *Client) completeFakeACP(taskID, runID, agent string) (map[string]any, error) {
	agent = "fake"
	title := ""
	if t, err := c.GetTask(taskID); err == nil {
		title, _ = t["title"].(string)
	}
	if _, err := c.UpdateRun(runID, "running", "acp "+agent); err != nil {
		return nil, err
	}
	prompt := acp.Prompt(title, "", "")
	n := 0
	_, logs, err := acp.Stream(context.Background(), acp.Config{Agent: agent}, prompt, func(ev acp.Event) error {
		posted, perr := c.PostRunEvent(runID, ev.Kind, ev.Payload)
		if perr != nil {
			return perr
		}
		n++
		seq, _ := posted["seq"].(float64)
		fmt.Fprintf(os.Stderr, "run event seq=%v kind=%s\n", seq, ev.Kind)
		return nil
	})
	if err != nil {
		_, _ = c.UpdateRun(runID, "failed", err.Error())
		return nil, err
	}
	if _, err := c.UpdateRun(runID, "succeeded", "acp "+agent+" ok"); err != nil {
		return nil, err
	}
	art, err := c.CreateArtifact(taskID, map[string]string{"kind": "log", "name": "acp.log", "body": logs, "run_id": runID})
	if err != nil {
		return nil, err
	}
	pr, err := c.OpenPR(taskID, true, runID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"run":      map[string]any{"id": runID, "task_id": taskID, "status": "succeeded"},
		"artifact": art, "pr": pr, "fake": true, "acp_agent": agent, "streamed": n,
	}, nil
}
