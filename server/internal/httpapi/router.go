package httpapi

import (
	"net/http"

	"github.com/codemodify/buildbee/server/internal/store"
	"github.com/codemodify/buildbee/server/internal/ws"
)

// Server hosts REST + Channel WebSocket for a Project workspace.
type Server struct {
	store store.Store
	hub   *ws.Hub
}

func NewServer(st store.Store, hub *ws.Hub) *Server {
	if hub == nil {
		hub = ws.NewHub()
	}
	return &Server{store: st, hub: hub}
}

// NewMux returns a Server with an in-memory Store (tests and no-DB `go run`).
func NewMux() http.Handler {
	return NewServer(store.NewMemory(), ws.NewHub()).Handler()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", Health)
	mux.HandleFunc("GET /v1/{$}", s.v1Index)
	mux.HandleFunc("GET /v1", s.v1Index)

	mux.HandleFunc("GET /v1/projects", s.listProjects)
	mux.HandleFunc("POST /v1/projects", s.createProject)
	mux.HandleFunc("GET /v1/projects/{projectID}", s.getProject)

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

	return withCORS(mux)
}

func (s *Server) v1Index(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "buildbee",
		"api":     "v1",
		"resources": []string{
			"projects", "members", "channels", "messages",
			"tasks", "handoffs", "decisions", "activity", "runs",
			"artifacts", "pipelines",
		},
	})
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Member-ID")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
