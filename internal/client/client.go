package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"strings"
	"time"
)

// Client calls the Server as a Person: As is sent in X-BuildBee-As.
type Client struct {
	Base   string
	As     string
	HTTP   *http.Client
	Output io.Writer
}

// New reads BUILDBEE_URL and BUILDBEE_AS (default: the login name).
func New() *Client {
	base := os.Getenv("BUILDBEE_URL")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	return &Client{
		Base:   strings.TrimRight(base, "/"),
		As:     actingAs(),
		HTTP:   &http.Client{Timeout: 3 * time.Minute},
		Output: os.Stdout,
	}
}

func actingAs() string {
	if v := strings.TrimSpace(os.Getenv("BUILDBEE_AS")); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
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
	if c.As != "" {
		req.Header.Set("X-BuildBee-As", c.As)
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

// CreateHandoff hands a Task from the acting Person to a Member or Role.
func (c *Client) CreateHandoff(taskID, toID, note, toRole string, autorun bool) (map[string]any, error) {
	var out map[string]any
	path := "/v1/tasks/" + taskID + "/handoffs"
	if autorun {
		path += "?autorun=1"
	}
	body := map[string]string{
		"to_member_id": toID,
		"to_role":      toRole,
		"note":         note,
	}
	err := c.do(http.MethodPost, path, body, &out)
	return out, err
}

// CreateRun queues a Run of a Task. agent and botMemberID may be empty.
func (c *Client) CreateRun(taskID, agent, botMemberID string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodPost, "/v1/tasks/"+taskID+"/runs", map[string]string{"agent": agent, "bot_member_id": botMemberID}, &out)
	return out, err
}

func (c *Client) GetRun(runID string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodGet, "/v1/runs/"+runID, nil, &out)
	return out, err
}

func (c *Client) UpdateRun(runID, status, detail string) (map[string]any, error) {
	var out map[string]any
	err := c.do(http.MethodPatch, "/v1/runs/"+runID, map[string]string{"status": status, "detail": detail}, &out)
	return out, err
}

// RunEvent is one event of a Run's stream.
type RunEvent struct {
	Seq     int            `json:"seq"`
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload"`
}

// RunEvents returns a Run's events after seq `after`.
func (c *Client) RunEvents(runID string, after int) ([]RunEvent, bool, error) {
	var out struct {
		Items   []RunEvent `json:"items"`
		HasMore bool       `json:"has_more"`
	}
	err := c.do(http.MethodGet, fmt.Sprintf("/v1/runs/%s/events?after=%d", runID, after), nil, &out)
	return out.Items, out.HasMore, err
}

// Follow calls each for every event of a Run until the Run finishes, and
// returns its final status.
func (c *Client) Follow(ctx context.Context, runID string, each func(RunEvent)) (string, error) {
	after := 0
	for {
		evs, more, err := c.RunEvents(runID, after)
		if err != nil {
			return "", err
		}
		for _, ev := range evs {
			after = ev.Seq
			each(ev)
		}
		if more {
			continue
		}
		run, err := c.GetRun(runID)
		if err != nil {
			return "", err
		}
		status, _ := run["status"].(string)
		if status == "succeeded" || status == "failed" || status == "canceled" {
			if evs, _, err := c.RunEvents(runID, after); err == nil && len(evs) > 0 {
				continue // drain what arrived with the final status
			}
			return status, nil
		}
		select {
		case <-ctx.Done():
			return status, ctx.Err()
		case <-time.After(FollowInterval):
		}
	}
}

// FollowInterval is how often Follow polls a Run for new events.
var FollowInterval = 500 * time.Millisecond

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
