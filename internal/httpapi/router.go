package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/codemodify/buildbee/internal/config"
	"github.com/codemodify/buildbee/internal/store"
	"github.com/codemodify/buildbee/internal/webui"
	"github.com/codemodify/buildbee/internal/ws"
)

// Options configures a Server. The zero value is valid for tests.
type Options struct {
	GitHub       config.GitHub
	WebDir       string // serve the UI from disk instead of the embed
	MaxBodyBytes int64  // 0 = no limit
	Logger       *slog.Logger
}

// Server hosts REST + Channel WebSocket for a Project workspace.
type Server struct {
	store store.Store
	hub   *ws.Hub
	opts  Options
	log   *slog.Logger
}

// NewServer wires a Server over st; a nil hub gets a fresh one.
func NewServer(st store.Store, hub *ws.Hub, opts Options) *Server {
	if hub == nil {
		hub = ws.NewHub()
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Server{store: st, hub: hub, opts: opts, log: log}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/{$}", s.v1Index)
	mux.HandleFunc("GET /v1", s.v1Index)

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
	mux.HandleFunc("POST /v1/runs/{runID}/events", s.createRunEvent)
	mux.HandleFunc("GET /v1/runs/{runID}/events", s.listRunEvents)
	mux.HandleFunc("GET /v1/runs/{runID}/ws", s.hub.ServeRun)

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

	web := webui.Handler(s.opts.WebDir)
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/v1") {
			mux.ServeHTTP(w, r)
			return
		}
		web.ServeHTTP(w, r)
	})
	h = sameOrigin(h)
	if s.opts.MaxBodyBytes > 0 {
		h = http.MaxBytesHandler(h, s.opts.MaxBodyBytes)
	}
	return s.recoverPanics(s.logRequests(h))
}

func (s *Server) v1Index(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "buildbee",
		"api":     "v1",
		"resources": []string{
			"projects", "members", "channels", "messages",
			"tasks", "handoffs", "decisions", "activity", "runs",
			"artifacts", "pipelines", "issues", "routines", "memories", "notifications", "preferences", "run_events",
		},
	})
}
