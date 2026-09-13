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
		HTTP:   &http.Client{Timeout: 15 * time.Second},
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
