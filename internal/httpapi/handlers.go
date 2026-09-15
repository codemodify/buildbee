package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/codemodify/buildbee/internal/core"
)

// --- inbox ---

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	p, err := pageParams(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	inbox, err := s.core.Notifications(r.Context(), actorFrom(r.Context()), flag(r, "unread"), p)
	respond(s, w, http.StatusOK, inbox, err)
}

func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	n, err := s.core.MarkRead(r.Context(), actorFrom(r.Context()), r.PathValue("id"))
	respond(s, w, http.StatusOK, n, err)
}

func (s *Server) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	n, err := s.core.MarkAllRead(r.Context(), actorFrom(r.Context()))
	respond(s, w, http.StatusOK, map[string]int{"read": n}, err)
}

func (s *Server) getPreferences(w http.ResponseWriter, r *http.Request) {
	p, err := s.core.Preferences(r.Context(), actorFrom(r.Context()))
	respond(s, w, http.StatusOK, p, err)
}

func (s *Server) patchPreferences(w http.ResponseWriter, r *http.Request) {
	var in core.PreferencesPatch
	if !s.decode(w, r, &in) {
		return
	}
	p, err := s.core.SetPreferences(r.Context(), actorFrom(r.Context()), in)
	respond(s, w, http.StatusOK, p, err)
}

// --- projects ---

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.Projects(r.Context(), flag(r, "archived"))
	respondItems(s, w, list, err)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string `json:"name"`
		AutoRun bool   `json:"auto_run"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	p, err := s.core.CreateProject(r.Context(), actorFrom(r.Context()), in.Name, in.AutoRun)
	respond(s, w, http.StatusCreated, p, err)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.core.Project(r.Context(), r.PathValue("id"))
	respond(s, w, http.StatusOK, p, err)
}

func (s *Server) patchProject(w http.ResponseWriter, r *http.Request) {
	var in core.ProjectPatch
	if !s.decode(w, r, &in) {
		return
	}
	p, err := s.core.UpdateProject(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusOK, p, err)
}

func (s *Server) joinProject(w http.ResponseWriter, r *http.Request) {
	m, err := s.core.Join(r.Context(), actorFrom(r.Context()), r.PathValue("id"))
	respond(s, w, http.StatusOK, m, err)
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.Members(r.Context(), r.PathValue("id"))
	respondItems(s, w, list, err)
}

func (s *Server) addMember(w http.ResponseWriter, r *http.Request) {
	var in core.NewMember
	if !s.decode(w, r, &in) {
		return
	}
	m, err := s.core.AddMember(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusCreated, m, err)
}

func (s *Server) patchMember(w http.ResponseWriter, r *http.Request) {
	var in core.MemberPatch
	if !s.decode(w, r, &in) {
		return
	}
	m, err := s.core.UpdateMember(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusOK, m, err)
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.Channels(r.Context(), r.PathValue("id"), flag(r, "archived"))
	respondItems(s, w, list, err)
}

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	ch, err := s.core.CreateChannel(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in.Name)
	respond(s, w, http.StatusCreated, ch, err)
}

func (s *Server) patchChannel(w http.ResponseWriter, r *http.Request) {
	var in core.ChannelPatch
	if !s.decode(w, r, &in) {
		return
	}
	ch, err := s.core.UpdateChannel(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusOK, ch, err)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	p, err := pageParams(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	list, more, err := s.core.Messages(r.Context(), r.PathValue("id"), p)
	respondPage(s, w, list, more, err)
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	m, err := s.core.PostMessage(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in.Body)
	respond(s, w, http.StatusCreated, m, err)
}

func (s *Server) listActivity(w http.ResponseWriter, r *http.Request) {
	p, err := pageParams(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	list, more, err := s.core.Activity(r.Context(), r.PathValue("id"), r.URL.Query().Get("type"), p)
	respondPage(s, w, list, more, err)
}

// --- tasks and handoffs ---

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.Tasks(r.Context(), r.PathValue("id"))
	respondItems(s, w, list, err)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var in core.NewTask
	if !s.decode(w, r, &in) {
		return
	}
	if q := r.URL.Query().Get("handoff"); q != "" {
		in.HandoffRole = q
	}
	t, err := s.core.CreateTask(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusCreated, t, err)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.core.Task(r.Context(), r.PathValue("id"))
	respond(s, w, http.StatusOK, t, err)
}

func (s *Server) patchTask(w http.ResponseWriter, r *http.Request) {
	var in core.TaskPatch
	if !s.decode(w, r, &in) {
		return
	}
	t, err := s.core.UpdateTask(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusOK, t, err)
}

func (s *Server) getTaskDetail(w http.ResponseWriter, r *http.Request) {
	d, err := s.core.TaskDetail(r.Context(), r.PathValue("id"))
	respond(s, w, http.StatusOK, d, err)
}

func (s *Server) listHandoffs(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.Handoffs(r.Context(), r.PathValue("id"))
	respondItems(s, w, list, err)
}

func (s *Server) createHandoff(w http.ResponseWriter, r *http.Request) {
	var in core.NewHandoff
	if !s.decode(w, r, &in) {
		return
	}
	if q := r.URL.Query().Get("to_role"); q != "" {
		in.ToRole = q
	}
	if flag(r, "autorun") {
		in.AutoRun = true
	}
	h, err := s.core.CreateHandoff(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusCreated, h, err)
}

func (s *Server) completeHandoff(w http.ResponseWriter, r *http.Request) {
	h, err := s.core.CompleteHandoff(r.Context(), actorFrom(r.Context()), r.PathValue("id"), flag(r, "decision"))
	respond(s, w, http.StatusOK, h, err)
}

// --- decisions ---

func (s *Server) listDecisions(w http.ResponseWriter, r *http.Request) {
	f := core.DecisionFilter{Open: flag(r, "open") || flag(r, "inbox"), Mine: flag(r, "mine")}
	list, err := s.core.Decisions(r.Context(), actorFrom(r.Context()), r.PathValue("id"), f)
	respondItems(s, w, list, err)
}

func (s *Server) createDecision(w http.ResponseWriter, r *http.Request) {
	var in core.NewDecision
	if !s.decode(w, r, &in) {
		return
	}
	d, err := s.core.CreateDecision(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusCreated, d, err)
}

func (s *Server) listDecisionMemories(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.DecisionMemories(r.Context(), r.PathValue("id"))
	respondItems(s, w, list, err)
}

func (s *Server) answerDecision(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Answer string `json:"answer"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	d, err := s.core.AnswerDecision(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in.Answer)
	respond(s, w, http.StatusOK, d, err)
}

// --- runs ---

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.Runs(r.Context(), r.PathValue("id"))
	respondItems(s, w, list, err)
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	var in core.NewRun
	if !s.decode(w, r, &in) {
		return
	}
	run, err := s.core.CreateRun(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusCreated, run, err)
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.core.Run(r.Context(), r.PathValue("id"))
	respond(s, w, http.StatusOK, run, err)
}

func (s *Server) patchRun(w http.ResponseWriter, r *http.Request) {
	var in core.RunReport
	if !s.decode(w, r, &in) {
		return
	}
	run, err := s.core.ReportRun(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusOK, run, err)
}

func (s *Server) listRunEvents(w http.ResponseWriter, r *http.Request) {
	after, limit := 0, 0
	for key, dst := range map[string]*int{"after": &after, "limit": &limit} {
		if v := strings.TrimSpace(r.URL.Query().Get(key)); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": key + " must be a non-negative integer"})
				return
			}
			*dst = n
		}
	}
	list, more, err := s.core.RunEvents(r.Context(), r.PathValue("id"), after, limit)
	respondPage(s, w, list, more, err)
}

func (s *Server) createRunEvent(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	ev, err := s.core.AppendRunEvent(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in.Kind, in.Payload)
	respond(s, w, http.StatusCreated, ev, err)
}

// maxClaimWait bounds a worker's long-poll so proxies do not cut it off.
const maxClaimWait = 30 * time.Second

// claimRun hands the calling worker a queued Run (200) or nothing (204)
// after waiting up to wait_seconds for one.
func (s *Server) claimRun(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Agents      []string `json:"agents"`
		WaitSeconds float64  `json:"wait_seconds"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	wait := min(max(time.Duration(in.WaitSeconds*float64(time.Second)), 0), maxClaimWait)
	c, err := s.core.ClaimRun(r.Context(), actorFrom(r.Context()), in.Agents, wait)
	if err == nil && c == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	respond(s, w, http.StatusOK, c, err)
}

func (s *Server) heartbeatRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.core.Heartbeat(r.Context(), actorFrom(r.Context()), r.PathValue("id"))
	respond(s, w, http.StatusOK, run, err)
}

// --- artifacts, PRs, pipelines ---

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.Artifacts(r.Context(), r.PathValue("id"))
	respondItems(s, w, list, err)
}

func (s *Server) createArtifact(w http.ResponseWriter, r *http.Request) {
	var in core.NewArtifact
	if !s.decode(w, r, &in) {
		return
	}
	a, err := s.core.CreateArtifact(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	if a != nil {
		a.Body = "" // the caller already has it
	}
	respond(s, w, http.StatusCreated, a, err)
}

func (s *Server) getArtifact(w http.ResponseWriter, r *http.Request) {
	a, err := s.core.Artifact(r.Context(), r.PathValue("id"))
	respond(s, w, http.StatusOK, a, err)
}

func (s *Server) openPR(w http.ResponseWriter, r *http.Request) {
	var in core.NewPR
	if !s.decode(w, r, &in) {
		return
	}
	pr, err := s.core.OpenPR(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusCreated, pr, err)
}

func (s *Server) listPipelines(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.Pipelines(r.Context(), r.PathValue("id"))
	respondItems(s, w, list, err)
}

func (s *Server) createPipeline(w http.ResponseWriter, r *http.Request) {
	var in core.NewPipeline
	if !s.decode(w, r, &in) {
		return
	}
	p, err := s.core.RecordPipeline(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusCreated, p, err)
}

func (s *Server) patchPipeline(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status      string `json:"status"`
		ExternalURL string `json:"external_url"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	p, err := s.core.UpdatePipeline(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in.Status, in.ExternalURL)
	respond(s, w, http.StatusOK, p, err)
}

// --- integrations ---

// pipelinesWebhook accepts {task_id, name, status, external_url} or a GitHub
// check_run payload carrying task_id (body, client_payload or ?task_id=).
func (s *Server) pipelinesWebhook(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.webhookBody(w, r)
	if !ok {
		return
	}
	var in struct {
		TaskID      string `json:"task_id"`
		ArtifactID  string `json:"artifact_id"`
		Name        string `json:"name"`
		Status      string `json:"status"`
		ExternalURL string `json:"external_url"`
		Branch      string `json:"branch"`
		CheckRun    *struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
			CheckSuite struct {
				HeadBranch string `json:"head_branch"`
			} `json:"check_suite"`
		} `json:"check_run"`
		ClientPayload *struct {
			TaskID string `json:"task_id"`
		} `json:"client_payload"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if in.TaskID == "" && in.ClientPayload != nil {
		in.TaskID = in.ClientPayload.TaskID
	}
	if in.TaskID == "" {
		in.TaskID = r.URL.Query().Get("task_id")
	}
	if cr := in.CheckRun; cr != nil {
		if in.Name == "" {
			in.Name = cr.Name
		}
		if in.Status == "" {
			in.Status = cr.Conclusion
			if in.Status == "" {
				in.Status = cr.Status
			}
		}
		if in.ExternalURL == "" {
			in.ExternalURL = cr.HTMLURL
		}
		if in.Branch == "" {
			in.Branch = cr.CheckSuite.HeadBranch
		}
	}
	if in.TaskID == "" && in.Branch != "" { // CI on a branch a Builder pushed
		t, err := s.core.TaskByBranch(r.Context(), in.Branch)
		if err != nil {
			s.fail(w, err)
			return
		}
		in.TaskID = t.ID
	}
	if in.Name == "" {
		in.Name = "pipeline"
	}
	if in.TaskID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id or branch is required"})
		return
	}
	p, err := s.core.RecordPipeline(r.Context(), core.System("github"), in.TaskID, core.NewPipeline{
		Name: in.Name, Status: in.Status, ExternalURL: in.ExternalURL, ArtifactID: in.ArtifactID,
	})
	respond(s, w, http.StatusCreated, p, err)
}

func (s *Server) syncIssues(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Repo string `json:"repo"`
		Fake bool   `json:"fake"`
	}
	if !s.decode(w, r, &in) {
		return
	}
	list, err := s.core.SyncIssues(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in.Repo, in.Fake)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "fake": in.Fake})
}

// issuesWebhook applies GitHub "issues" events to ?project_id= (or
// client_payload.project_id).
func (s *Server) issuesWebhook(w http.ResponseWriter, r *http.Request) {
	raw, ok := s.webhookBody(w, r)
	if !ok {
		return
	}
	var in struct {
		Action string `json:"action"`
		Issue  *struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
		} `json:"issue"`
		ClientPayload *struct {
			ProjectID string `json:"project_id"`
		} `json:"client_payload"`
	}
	if err := json.Unmarshal(raw, &in); err != nil || in.Issue == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "an issue payload is required"})
		return
	}
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" && in.ClientPayload != nil {
		projectID = in.ClientPayload.ProjectID
	}
	if projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id is required (query or client_payload)"})
		return
	}
	switch strings.ToLower(in.Action) {
	case "opened", "edited", "reopened", "":
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "action": in.Action})
		return
	}
	t, err := s.core.IssueWebhook(r.Context(), projectID, in.Issue.Number, in.Issue.Title, in.Issue.HTMLURL)
	respond(s, w, http.StatusOK, t, err)
}

// webhookBody reads and verifies a webhook body; it answers on failure.
func (s *Server) webhookBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	defer r.Body.Close()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.fail(w, err)
		return nil, false
	}
	if !s.verifyGitHubSignature(r, raw) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid GitHub webhook signature"})
		return nil, false
	}
	return sanitizeJSON(raw), true
}

// --- routines ---

func (s *Server) listRoutines(w http.ResponseWriter, r *http.Request) {
	list, err := s.core.Routines(r.Context(), r.PathValue("id"))
	respondItems(s, w, list, err)
}

func (s *Server) createRoutine(w http.ResponseWriter, r *http.Request) {
	var in core.NewRoutine
	if !s.decode(w, r, &in) {
		return
	}
	rt, err := s.core.CreateRoutine(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusCreated, rt, err)
}

func (s *Server) patchRoutine(w http.ResponseWriter, r *http.Request) {
	var in core.RoutinePatch
	if !s.decode(w, r, &in) {
		return
	}
	rt, err := s.core.UpdateRoutine(r.Context(), actorFrom(r.Context()), r.PathValue("id"), in)
	respond(s, w, http.StatusOK, rt, err)
}

func (s *Server) fireRoutine(w http.ResponseWriter, r *http.Request) {
	rt, err := s.core.FireRoutine(r.Context(), actorFrom(r.Context()), r.PathValue("id"))
	respond(s, w, http.StatusOK, rt, err)
}

// --- helpers ---

func respond[T any](s *Server, w http.ResponseWriter, status int, v T, err error) {
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, status, v)
}

func respondItems[T any](s *Server, w http.ResponseWriter, list []T, err error) {
	if err != nil {
		s.fail(w, err)
		return
	}
	items(w, list)
}

func respondPage[T any](s *Server, w http.ResponseWriter, list []T, more bool, err error) {
	if err != nil {
		s.fail(w, err)
		return
	}
	page(w, list, more)
}
