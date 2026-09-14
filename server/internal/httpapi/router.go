package httpapi

import (
	"net/http"
	"strings"

	"github.com/codemodify/buildbee/server/internal/auth"
	"github.com/codemodify/buildbee/server/internal/store"
	"github.com/codemodify/buildbee/server/internal/webui"
	"github.com/codemodify/buildbee/server/internal/ws"
)

// Server hosts REST + Channel WebSocket for a Project workspace.
type Server struct {
	store store.Store
	hub   *ws.Hub
	auth  *auth.Service
}

func newSrv(st store.Store, hub *ws.Hub, a *auth.Service) *Server {
	if hub == nil {
		hub = ws.NewHub()
	}
	if a == nil {
		a = auth.New(auth.FromEnv())
	}
	return &Server{store: st, hub: hub, auth: a}
}

func NewServer(st store.Store, hub *ws.Hub) *Server {
	return newSrv(st, hub, auth.New(auth.FromEnv()))
}

// NewMux returns a Server with an in-memory Store and dev auth (tests / no OAuth).
func NewMux() http.Handler {
	return newSrv(store.NewMemory(), ws.NewHub(), auth.NewDev()).Handler()
}

func NewMuxSecure() http.Handler {
	return newSrv(store.NewMemory(), ws.NewHub(), auth.New(auth.Config{ClientID: "test"})).Handler()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", Health)
	mux.HandleFunc("GET /v1/{$}", s.v1Index)
	mux.HandleFunc("GET /v1", s.v1Index)

	mux.HandleFunc("GET /v1/auth/me", s.authMe)
	mux.HandleFunc("GET /v1/auth/github", s.authGitHub)
	mux.HandleFunc("GET /v1/auth/callback", s.authCallback)
	mux.HandleFunc("POST /v1/auth/logout", s.authLogout)

	mux.HandleFunc("GET /v1/projects", s.listProjects)
	mux.HandleFunc("POST /v1/projects", s.createProject)
	mux.HandleFunc("GET /v1/projects/{projectID}", s.getProject)
	mux.HandleFunc("PATCH /v1/projects/{projectID}", s.updateProject)

	mux.HandleFunc("GET /v1/projects/{projectID}/members", s.listMembers)
	mux.HandleFunc("POST /v1/projects/{projectID}/members", s.addMember)

	mux.HandleFunc("GET /v1/projects/{projectID}/channels", s.listChannels)
	mux.HandleFunc("POST /v1/projects/{projectID}/channels", s.createChannel)

	mux.HandleFunc("GET /v1/channels/{channelID}/messages", s.listMessages)
	mux.HandleFunc("POST /v1/channels/{channelID}/messages", s.postMessage)
	mux.HandleFunc("GET /v1/channels/{channelID}/ws", s.hub.ServeChannel)

	mux.HandleFunc("GET /v1/projects/{projectID}/tasks", s.listTasks)
	mux.HandleFunc("POST /v1/projects/{projectID}/tasks", s.createTask)
	mux.HandleFunc("GET /v1/tasks/{taskID}", s.getTask)
	mux.HandleFunc("GET /v1/tasks/{taskID}/detail", s.getTaskDetail)
	mux.HandleFunc("PATCH /v1/tasks/{taskID}", s.updateTask)

	mux.HandleFunc("POST /v1/tasks/{taskID}/handoffs", s.createHandoff)
	mux.HandleFunc("POST /v1/handoffs/{handoffID}/complete", s.completeHandoff)

	mux.HandleFunc("GET /v1/projects/{projectID}/decisions/memories", s.listDecisionMemories)
	mux.HandleFunc("GET /v1/projects/{projectID}/decisions", s.listDecisions)
	mux.HandleFunc("POST /v1/projects/{projectID}/decisions", s.createDecision)
	mux.HandleFunc("POST /v1/decisions/{decisionID}/answer", s.answerDecision)

	mux.HandleFunc("GET /v1/projects/{projectID}/activity", s.listActivity)

	mux.HandleFunc("POST /v1/tasks/{taskID}/runs", s.createRun)
	mux.HandleFunc("GET /v1/tasks/{taskID}/runs", s.listRuns)
	mux.HandleFunc("GET /v1/runs/{runID}", s.getRun)
	mux.HandleFunc("PATCH /v1/runs/{runID}", s.updateRun)

	mux.HandleFunc("GET /v1/tasks/{taskID}/artifacts", s.listArtifacts)
	mux.HandleFunc("POST /v1/tasks/{taskID}/artifacts", s.createArtifact)
	mux.HandleFunc("GET /v1/artifacts/{artifactID}", s.getArtifact)
	mux.HandleFunc("POST /v1/tasks/{taskID}/pr", s.openPR)

	mux.HandleFunc("GET /v1/tasks/{taskID}/pipelines", s.listPipelines)
	mux.HandleFunc("POST /v1/tasks/{taskID}/pipelines", s.createPipeline)
	mux.HandleFunc("PATCH /v1/pipelines/{pipelineID}", s.updatePipeline)
	mux.HandleFunc("POST /v1/pipelines/webhook", s.pipelinesWebhook)

	mux.HandleFunc("POST /v1/projects/{projectID}/issues/sync", s.syncIssues)
	mux.HandleFunc("POST /v1/issues/webhook", s.issuesWebhook)

	mux.HandleFunc("GET /v1/projects/{projectID}/routines", s.listRoutines)
	mux.HandleFunc("POST /v1/projects/{projectID}/routines", s.createRoutine)
	mux.HandleFunc("POST /v1/routines/{routineID}/run", s.fireRoutine)

	mux.HandleFunc("GET /v1/me/preferences", s.getPreferences)
	mux.HandleFunc("PATCH /v1/me/preferences", s.patchPreferences)

	mux.HandleFunc("GET /v1/notifications", s.listNotifications)
	mux.HandleFunc("POST /v1/notifications/read-all", s.readAllNotifications)
	mux.HandleFunc("POST /v1/notifications/{notificationID}/read", s.readNotification)

	mux.HandleFunc("GET /v1/projects/{projectID}/invites", s.listInvites)
	mux.HandleFunc("POST /v1/projects/{projectID}/invites", s.createInvite)
	mux.HandleFunc("GET /v1/invites/{token}", s.getInvitePreview)
	mux.HandleFunc("POST /v1/invites/{token}/accept", s.acceptInvite)
	mux.HandleFunc("DELETE /v1/invites/{inviteID}", s.revokeInvite)

	web := webui.Handler()
	return s.auth.RequireMutating(withCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/v1") {
			mux.ServeHTTP(w, r)
			return
		}
		web.ServeHTTP(w, r)
	})))
}

func (s *Server) v1Index(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "buildbee",
		"api":     "v1",
		"auth":    map[string]any{"dev": s.auth.Dev()},
		"resources": []string{
			"projects", "members", "channels", "messages",
			"tasks", "handoffs", "decisions", "activity", "runs",
			"artifacts", "pipelines", "issues", "routines", "roles", "memories", "notifications", "invites", "preferences",
		},
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Member-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
