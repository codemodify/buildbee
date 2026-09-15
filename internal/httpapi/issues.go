package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/codemodify/buildbee/internal/githubconn"
	"github.com/codemodify/buildbee/internal/models"
)

func (s *Server) syncIssues(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Repo string `json:"repo"`
		Fake bool   `json:"fake"`
	}
	if err := decodeJSON(r, &in); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	projectID := r.PathValue("projectID")
	if _, err := s.store.GetProject(r.Context(), projectID); err != nil {
		writeError(w, err)
		return
	}
	token := s.opts.GitHub.Token
	repo := in.Repo
	if repo == "" {
		repo = s.opts.GitHub.Repo
	}
	var items []models.Task
	if !in.Fake && (token == "" || repo == "") {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": githubconn.ErrNotConfigured.Error()})
		return
	}
	if in.Fake {
		samples := []struct {
			n int
			t string
		}{{1, "Sample Issue: welcome"}, {2, "Sample Issue: follow-up"}}
		for _, sm := range samples {
			url := "https://github.com/example/buildbee/issues/" + strconv.Itoa(sm.n)
			if repo != "" {
				url = "https://github.com/" + repo + "/issues/" + strconv.Itoa(sm.n)
			}
			tk, err := s.store.UpsertIssueTask(r.Context(), projectID, sm.n, sm.t, url)
			if err != nil {
				writeError(w, err)
				return
			}
			items = append(items, *tk)
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "fake": true})
		return
	}
	issues, err := githubconn.ListOpenIssues(r.Context(), token, repo)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	for _, is := range issues {
		tk, err := s.store.UpsertIssueTask(r.Context(), projectID, is.Number, is.Title, is.HTMLURL)
		if err != nil {
			writeError(w, err)
			return
		}
		items = append(items, *tk)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "fake": false, "repo": repo})
}

func (s *Server) issuesWebhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if !s.verifyGitHubSignature(r, raw) {
		writeWebhookUnauthorized(w)
		return
	}
	var body struct {
		Action string `json:"action"`
		Issue  *struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
		} `json:"issue"`
		Repository *struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		ClientPayload *struct {
			ProjectID string `json:"project_id"`
		} `json:"client_payload"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Issue == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "issue payload required"})
		return
	}
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" && body.ClientPayload != nil {
		projectID = body.ClientPayload.ProjectID
	}
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id query or client_payload required"})
		return
	}
	switch strings.ToLower(body.Action) {
	case "opened", "edited", "reopened", "":
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "action": body.Action})
		return
	}
	tk, err := s.store.UpsertIssueTask(r.Context(), projectID, body.Issue.Number, body.Issue.Title, body.Issue.HTMLURL)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tk)
}
