package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
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

func (c *Client) CreateHandoff(taskID, fromID, toID, note string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodPost, "/v1/tasks/"+taskID+"/handoffs", map[string]string{
		"from_member_id": fromID,
		"to_member_id":   toID,
		"note":           note,
	}, &out)
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

func (c *Client) RuntimeStart(runtimeURL, taskID, runID, repoURL, cmd string, fake bool) (map[string]any, error) {
	rt := &Client{Base: strings.TrimRight(runtimeURL, "/"), HTTP: c.HTTP, Output: c.Output}
	var out map[string]any
	err := rt.do(http.MethodPost, "/runs", map[string]any{
		"task_id": taskID, "run_id": runID, "repo_url": repoURL, "command": cmd, "fake": fake, "fake_pr": true,
	}, &out)
	return out, err
}
