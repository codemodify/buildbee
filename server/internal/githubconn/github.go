// Package githubconn talks to GitHub as a Repo / Issues / Pipelines source.
// v0 can open a draft PR (or record a fake PR URL Artifact when no token).
package githubconn

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Options struct {
	Token   string
	Repo    string // owner/name
	Title   string
	Body    string
	Branch  string
	Path    string
	Content string
	Fake    bool
	TaskID  string
	RunID   string
}

type Result struct {
	URL   string `json:"url"`
	Fake  bool   `json:"fake"`
	Repo  string `json:"repo,omitempty"`
	Error string `json:"error,omitempty"`
}

func FromEnv() (token, repo string) {
	return os.Getenv("GITHUB_TOKEN"), os.Getenv("GITHUB_REPO")
}

// OpenDraftPR creates a branch + file commit + draft PR, or a fake URL.
func OpenDraftPR(ctx context.Context, opt Options) (*Result, error) {
	if opt.Token == "" {
		opt.Token, _ = FromEnv()
	}
	if opt.Repo == "" {
		_, opt.Repo = FromEnv()
	}
	if opt.Title == "" {
		opt.Title = "BuildBee Artifact"
	}
	if opt.Path == "" {
		opt.Path = "buildbee/artifact.md"
	}
	if opt.Content == "" {
		opt.Content = "# BuildBee\n\nRecorded from a Run.\n"
	}
	if opt.Branch == "" {
		opt.Branch = "buildbee/" + shortID(opt.TaskID, opt.RunID)
	}

	if opt.Fake || opt.Token == "" || opt.Repo == "" {
		if !opt.Fake && opt.Token == "" {
			return &Result{
				URL:   fakeURL(opt.Repo, opt.Branch),
				Fake:  true,
				Repo:  opt.Repo,
				Error: "GITHUB_TOKEN missing; recorded fake PR URL",
			}, nil
		}
		if !opt.Fake && opt.Repo == "" {
			return &Result{
				URL:   fakeURL("example/repo", opt.Branch),
				Fake:  true,
				Error: "GITHUB_REPO missing; recorded fake PR URL",
			}, nil
		}
		return &Result{URL: fakeURL(opt.Repo, opt.Branch), Fake: true, Repo: opt.Repo}, nil
	}

	url, err := createDraftPR(ctx, opt)
	if err != nil {
		return nil, err
	}
	return &Result{URL: url, Repo: opt.Repo}, nil
}

func fakeURL(repo, branch string) string {
	if repo == "" {
		repo = "example/buildbee"
	}
	return fmt.Sprintf("https://github.com/%s/pull/fake-%s", strings.Trim(repo, "/"), strings.ReplaceAll(branch, "/", "-"))
}

func shortID(taskID, runID string) string {
	id := runID
	if id == "" {
		id = taskID
	}
	if id == "" {
		return fmt.Sprintf("%d", time.Now().Unix())
	}
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func createDraftPR(ctx context.Context, opt Options) (string, error) {
	owner, name, ok := strings.Cut(opt.Repo, "/")
	if !ok {
		return "", fmt.Errorf("GITHUB_REPO must be owner/name")
	}
	client := &http.Client{Timeout: 20 * time.Second}

	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := ghJSON(ctx, client, opt.Token, http.MethodGet, "/repos/"+owner+"/"+name, nil, &repo); err != nil {
		return "", err
	}
	refPath := "/repos/" + owner + "/" + name + "/git/ref/heads/" + repo.DefaultBranch
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := ghJSON(ctx, client, opt.Token, http.MethodGet, refPath, nil, &ref); err != nil {
		return "", err
	}
	createRef := map[string]string{
		"ref": "refs/heads/" + opt.Branch,
		"sha": ref.Object.SHA,
	}
	if err := ghJSON(ctx, client, opt.Token, http.MethodPost, "/repos/"+owner+"/"+name+"/git/refs", createRef, nil); err != nil {
		return "", err
	}
	putFile := map[string]any{
		"message": opt.Title,
		"content": base64.StdEncoding.EncodeToString([]byte(opt.Content)),
		"branch":  opt.Branch,
	}
	if err := ghJSON(ctx, client, opt.Token, http.MethodPut, "/repos/"+owner+"/"+name+"/contents/"+opt.Path, putFile, nil); err != nil {
		return "", err
	}
	prBody := map[string]any{
		"title": opt.Title,
		"body":  opt.Body,
		"head":  opt.Branch,
		"base":  repo.DefaultBranch,
		"draft": true,
	}
	var pr struct {
		HTMLURL string `json:"html_url"`
	}
	if err := ghJSON(ctx, client, opt.Token, http.MethodPost, "/repos/"+owner+"/"+name+"/pulls", prBody, &pr); err != nil {
		return "", err
	}
	return pr.HTMLURL, nil
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
