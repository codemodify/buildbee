package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/codemodify/buildbee/internal/githubconn"
	"github.com/codemodify/buildbee/internal/models"
)

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListRuns(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}

func (s *Server) getTaskDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("taskID")
	task, err := s.store.GetTask(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	runs, err := s.store.ListRuns(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	arts, err := s.store.ListArtifacts(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	pipes, err := s.store.ListPipelines(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.TaskDetail{
		Task: *task, Runs: runs, Artifacts: arts, Pipelines: pipes,
	})
}

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListArtifacts(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}

func (s *Server) createArtifact(w http.ResponseWriter, r *http.Request) {
	var in models.Artifact
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	in.TaskID = r.PathValue("taskID")
	a, err := s.store.CreateArtifact(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetArtifact(r.Context(), r.PathValue("artifactID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) listPipelines(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListPipelines(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeItems(w, items)
}

func (s *Server) createPipeline(w http.ResponseWriter, r *http.Request) {
	var in models.Pipeline
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	in.TaskID = r.PathValue("taskID")
	p, err := s.store.CreatePipeline(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	s.maybeNotifyPipeline(r.Context(), p)
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) updatePipeline(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status      string `json:"status"`
		ExternalURL string `json:"external_url"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Status) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status is required"})
		return
	}
	p, err := s.store.UpdatePipeline(r.Context(), r.PathValue("pipelineID"), normalizePipelineStatus(in.Status), in.ExternalURL)
	if err != nil {
		writeError(w, err)
		return
	}
	s.maybeNotifyPipeline(r.Context(), p)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) pipelinesWebhook(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if !verifyGitHubSignature(r, raw) {
		writeWebhookUnauthorized(w)
		return
	}
	taskID, name, status, ext, artifactID := parsePipelineWebhook(raw)
	if q := r.URL.Query().Get("task_id"); q != "" && taskID == "" {
		taskID = q
	}
	if taskID == "" || name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id and name are required"})
		return
	}
	p, err := s.store.CreatePipeline(r.Context(), models.Pipeline{
		TaskID: taskID, ArtifactID: artifactID, Name: name,
		Status: normalizePipelineStatus(status), ExternalURL: ext,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	s.maybeNotifyPipeline(r.Context(), p)
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) openPR(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		Path    string `json:"path"`
		Content string `json:"content"`
		Fake    bool   `json:"fake"`
		RunID   string `json:"run_id"`
		Repo    string `json:"repo"`
	}
	if err := decodeJSON(r, &in); err != nil {
		in.Fake = true
	}
	taskID := r.PathValue("taskID")
	if _, err := s.store.GetTask(r.Context(), taskID); err != nil {
		writeError(w, err)
		return
	}
	res, err := githubconn.OpenDraftPR(r.Context(), githubconn.Options{
		Title: in.Title, Body: in.Body, Path: in.Path, Content: in.Content,
		Fake: in.Fake, TaskID: taskID, RunID: in.RunID, Repo: in.Repo,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	a, err := s.store.CreateArtifact(r.Context(), models.Artifact{
		TaskID: taskID, RunID: in.RunID, Kind: "pr",
		Name: "Repo draft PR", URL: res.URL, Body: res.Error,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"artifact": a, "pr": res})
}

func parsePipelineWebhook(raw []byte) (taskID, name, status, ext, artifactID string) {
	var simple struct {
		TaskID      string `json:"task_id"`
		ArtifactID  string `json:"artifact_id"`
		Name        string `json:"name"`
		Status      string `json:"status"`
		ExternalURL string `json:"external_url"`
		CheckRun    *struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
		} `json:"check_run"`
		ClientPayload *struct {
			TaskID string `json:"task_id"`
		} `json:"client_payload"`
	}
	if err := json.Unmarshal(raw, &simple); err != nil {
		return "", "", "", "", ""
	}
	taskID = simple.TaskID
	artifactID = simple.ArtifactID
	name = simple.Name
	status = simple.Status
	ext = simple.ExternalURL
	if simple.ClientPayload != nil && taskID == "" {
		taskID = simple.ClientPayload.TaskID
	}
	if simple.CheckRun != nil {
		if name == "" {
			name = simple.CheckRun.Name
		}
		if status == "" {
			if simple.CheckRun.Conclusion != "" {
				status = simple.CheckRun.Conclusion
			} else {
				status = simple.CheckRun.Status
			}
		}
		if ext == "" {
			ext = simple.CheckRun.HTMLURL
		}
	}
	if name == "" {
		name = "pipeline"
	}
	return taskID, name, status, ext, artifactID
}

func normalizePipelineStatus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "success", "succeeded", "pass", "passed", "completed":
		return "success"
	case "failure", "failed", "error", "fail":
		return "failure"
	default:
		return "pending"
	}
}
