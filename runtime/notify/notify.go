// Package notify posts Run status updates to the Server.
// The supervisor will call this after a Sandbox finishes; today it can
// also fake a successful Run for local demos.
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
	return &Client{Base: strings.TrimRight(base, "/"), HTTP: &http.Client{Timeout: 15 * time.Second}}
}

type Run struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func (c *Client) CreateRun(taskID string) (*Run, error) {
	return c.do(http.MethodPost, "/v1/tasks/"+taskID+"/runs", nil)
}

func (c *Client) UpdateRun(runID, status, detail string) (*Run, error) {
	return c.do(http.MethodPatch, "/v1/runs/"+runID, map[string]string{"status": status, "detail": detail})
}

// FakeSuccess creates a Run and marks it succeeded (no Sandbox, no ACP).
func (c *Client) FakeSuccess(taskID string) (*Run, error) {
	run, err := c.CreateRun(taskID)
	if err != nil {
		return nil, err
	}
	return c.UpdateRun(run.ID, "succeeded", "fake success (runtime stub)")
}

func (c *Client) do(method, path string, body any) (*Run, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.Base+path, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s", method, path, strings.TrimSpace(string(raw)))
	}
	var run Run
	if err := json.Unmarshal(raw, &run); err != nil {
		return nil, err
	}
	return &run, nil
}
