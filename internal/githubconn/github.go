// Package githubconn reads a GitHub repository's open Issues for Issue sync.
// Pull requests are opened and merged by workers, with their own gh login.
package githubconn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotConfigured means no token or repo was supplied for a real call.
var ErrNotConfigured = errors.New("GitHub is not configured: set GITHUB_TOKEN and GITHUB_REPO")

type Issue struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HTMLURL string `json:"html_url"`
}

func ListOpenIssues(ctx context.Context, token, repo string) ([]Issue, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return nil, fmt.Errorf("GITHUB_REPO must be owner/name")
	}
	var issues []Issue
	err := ghJSON(ctx, &http.Client{Timeout: 20 * time.Second}, token, http.MethodGet,
		"/repos/"+owner+"/"+name+"/issues?state=open&per_page=50", nil, &issues)
	return issues, err
}

func ghJSON(ctx context.Context, client *http.Client, token, method, path string, body any, dest any) error {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("github %s %s: %s", method, path, strings.TrimSpace(string(raw)))
	}
	if dest == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dest)
}
