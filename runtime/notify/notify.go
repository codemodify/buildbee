// Package notify posts Run status, Artifacts, and fake PRs to the Server.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	Base string
	HTTP *http.Client
}

func New(base string) *Client {
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	return &Client{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type Run struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type Task struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (c *Client) GetTask(taskID string) (*Task, error) {
	var t Task
	err := c.do(http.MethodGet, "/v1/tasks/"+taskID, nil, &t)
	return &t, err
}

type Artifact struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Body  string `json:"body,omitempty"`
	URL   string `json:"url,omitempty"`
	RunID string `json:"run_id,omitempty"`
}

func (c *Client) CreateRun(taskID string) (*Run, error) {
	var run Run
	err := c.do(http.MethodPost, "/v1/tasks/"+taskID+"/runs", nil, &run)
	return &run, err
}

func (c *Client) UpdateRun(runID, status, detail string) (*Run, error) {
	var run Run
	err := c.do(http.MethodPatch, "/v1/runs/"+runID, map[string]string{"status": status, "detail": detail}, &run)
	return &run, err
}

type RunEvent struct {
	ID      string         `json:"id"`
	RunID   string         `json:"run_id"`
	Seq     int            `json:"seq"`
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload"`
}

func (c *Client) PostRunEvent(runID, kind string, payload map[string]any) (*RunEvent, error) {
	var ev RunEvent
	err := c.do(http.MethodPost, "/v1/runs/"+runID+"/events", map[string]any{"kind": kind, "payload": payload}, &ev)
	return &ev, err
}

func (c *Client) CreateArtifact(taskID string, a Artifact) (*Artifact, error) {
	var out Artifact
	err := c.do(http.MethodPost, "/v1/tasks/"+taskID+"/artifacts", a, &out)
	return &out, err
}

func (c *Client) OpenPR(taskID string, body map[string]any) (*Artifact, error) {
	var out struct {
		Artifact Artifact `json:"artifact"`
	}
	if err := c.do(http.MethodPost, "/v1/tasks/"+taskID+"/pr", body, &out); err != nil {
		return nil, err
	}
	return &out.Artifact, nil
}

// FakeSuccess creates a Run and marks it succeeded (no Sandbox).
func (c *Client) FakeSuccess(taskID string) (*Run, error) {
	run, err := c.CreateRun(taskID)
	if err != nil {
		return nil, err
	}
	return c.UpdateRun(run.ID, "succeeded", "fake success (runtime stub)")
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
	if dest == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dest)
}
