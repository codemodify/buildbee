// Package httpapi is BuildBee's HTTP interface: thin handlers that resolve
// who is acting, decode input, call package core and write JSON.
package httpapi

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/codemodify/buildbee/internal/config"
	"github.com/codemodify/buildbee/internal/core"
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

// Server serves the REST API, the WebSocket streams and the web UI.
type Server struct {
	core *core.Service
	hub  *ws.Hub
	opts Options
	log  *slog.Logger
}

// NewServer serves svc; hub must be the Publisher svc was created with.
func NewServer(svc *core.Service, hub *ws.Hub, opts Options) *Server {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Server{core: svc, hub: hub, opts: opts, log: log}
}

// Handler returns the full HTTP handler with middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/{$}", s.index)
	mux.HandleFunc("GET /v1", s.index)

	// who is acting, and their inbox
	mux.HandleFunc("GET /v1/me", s.getMe)
	mux.HandleFunc("POST /v1/me", s.postMe)
	mux.HandleFunc("DELETE /v1/me", s.deleteMe)
	mux.HandleFunc("GET /v1/me/notifications", s.listNotifications)
	mux.HandleFunc("POST /v1/me/notifications/read-all", s.readAllNotifications)
	mux.HandleFunc("POST /v1/notifications/{id}/read", s.readNotification)
	mux.HandleFunc("GET /v1/me/preferences", s.getPreferences)
	mux.HandleFunc("PATCH /v1/me/preferences", s.patchPreferences)

	// live streams
	mux.HandleFunc("GET /v1/ws", func(w http.ResponseWriter, r *http.Request) {
		if a := actorFrom(r.Context()); a.IsPerson() {
			defer s.core.Online(a.PersonID)() // online while the app is open
		}
		s.hub.ServeMulti(w, r)
	})
	mux.HandleFunc("GET /v1/presence", s.presence)
	mux.HandleFunc("GET /v1/channels/{id}/ws", s.hub.ServeTopic(func(r *http.Request) string { return "channel:" + r.PathValue("id") }, false))
	mux.HandleFunc("GET /v1/runs/{id}/ws", s.hub.ServeTopic(func(r *http.Request) string { return "run:" + r.PathValue("id") }, true))

	// projects, members, channels, messages
	mux.HandleFunc("GET /v1/projects", s.listProjects)
	mux.HandleFunc("POST /v1/projects", s.createProject)
	mux.HandleFunc("GET /v1/projects/{id}", s.getProject)
	mux.HandleFunc("PATCH /v1/projects/{id}", s.patchProject)
	mux.HandleFunc("POST /v1/projects/{id}/join", s.joinProject)
	mux.HandleFunc("POST /v1/projects/{id}/leave", s.leaveProject)
	mux.HandleFunc("GET /v1/projects/{id}/members", s.listMembers)
	mux.HandleFunc("POST /v1/projects/{id}/members", s.addMember)
	mux.HandleFunc("PATCH /v1/members/{id}", s.patchMember)
	mux.HandleFunc("GET /v1/projects/{id}/channels", s.listChannels)
	mux.HandleFunc("POST /v1/projects/{id}/channels", s.createChannel)
	mux.HandleFunc("PATCH /v1/channels/{id}", s.patchChannel)
	mux.HandleFunc("GET /v1/channels/{id}/messages", s.listMessages)
	mux.HandleFunc("POST /v1/channels/{id}/messages", s.postMessage)
	mux.HandleFunc("POST /v1/channels/{id}/read", s.markChannelRead)
	mux.HandleFunc("GET /v1/messages/{id}/thread", s.getThread)
	mux.HandleFunc("POST /v1/messages/{id}/replies", s.postReply)
	mux.HandleFunc("GET /v1/projects/{id}/dms", s.listDMs)
	mux.HandleFunc("POST /v1/projects/{id}/dms", s.openDM)
	mux.HandleFunc("GET /v1/projects/{id}/unread", s.listUnread)
	mux.HandleFunc("GET /v1/projects/{id}/activity", s.listActivity)

	// tasks and handoffs
	mux.HandleFunc("GET /v1/projects/{id}/tasks", s.listTasks)
	mux.HandleFunc("POST /v1/projects/{id}/tasks", s.createTask)
	mux.HandleFunc("GET /v1/tasks/{id}", s.getTask)
	mux.HandleFunc("PATCH /v1/tasks/{id}", s.patchTask)
	mux.HandleFunc("GET /v1/tasks/{id}/detail", s.getTaskDetail)
	mux.HandleFunc("GET /v1/tasks/{id}/handoffs", s.listHandoffs)
	mux.HandleFunc("POST /v1/tasks/{id}/handoffs", s.createHandoff)
	mux.HandleFunc("POST /v1/handoffs/{id}/complete", s.completeHandoff)

	// decisions
	mux.HandleFunc("GET /v1/projects/{id}/decisions", s.listDecisions)
	mux.HandleFunc("POST /v1/projects/{id}/decisions", s.createDecision)
	mux.HandleFunc("GET /v1/projects/{id}/decisions/memories", s.listDecisionMemories)
	mux.HandleFunc("POST /v1/decisions/{id}/answer", s.answerDecision)

	// runs, events, artifacts, PRs, pipelines
	mux.HandleFunc("GET /v1/tasks/{id}/runs", s.listRuns)
	mux.HandleFunc("POST /v1/tasks/{id}/runs", s.createRun)
	mux.HandleFunc("GET /v1/runs/{id}", s.getRun)
	mux.HandleFunc("PATCH /v1/runs/{id}", s.patchRun)
	mux.HandleFunc("POST /v1/runs/{id}/heartbeat", s.heartbeatRun)
	mux.HandleFunc("POST /v1/runs/{id}/steer", s.steerRun)
	mux.HandleFunc("GET /v1/usage", s.usage)
	mux.HandleFunc("GET /v1/projects/{id}/usage", s.usage)
	mux.HandleFunc("POST /v1/worker/claim", s.claimRun)
	mux.HandleFunc("GET /v1/runs/{id}/events", s.listRunEvents)
	mux.HandleFunc("POST /v1/runs/{id}/events", s.createRunEvent)
	mux.HandleFunc("GET /v1/tasks/{id}/artifacts", s.listArtifacts)
	mux.HandleFunc("POST /v1/tasks/{id}/artifacts", s.createArtifact)
	mux.HandleFunc("GET /v1/artifacts/{id}", s.getArtifact)
	mux.HandleFunc("GET /v1/tasks/{id}/pipelines", s.listPipelines)
	mux.HandleFunc("POST /v1/tasks/{id}/pipelines", s.createPipeline)
	mux.HandleFunc("PATCH /v1/pipelines/{id}", s.patchPipeline)

	// integrations and routines
	mux.HandleFunc("POST /v1/pipelines/webhook", s.pipelinesWebhook)
	mux.HandleFunc("POST /v1/projects/{id}/issues/sync", s.syncIssues)
	mux.HandleFunc("POST /v1/issues/webhook", s.issuesWebhook)
	mux.HandleFunc("GET /v1/projects/{id}/routines", s.listRoutines)
	mux.HandleFunc("POST /v1/projects/{id}/routines", s.createRoutine)
	mux.HandleFunc("PATCH /v1/routines/{id}", s.patchRoutine)
	mux.HandleFunc("POST /v1/routines/{id}/run", s.fireRoutine)

	web := webui.Handler(s.opts.WebDir)
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/v1") {
			mux.ServeHTTP(w, r)
			return
		}
		web.ServeHTTP(w, r)
	})
	h = s.withActor(sameOrigin(h))
	if s.opts.MaxBodyBytes > 0 {
		h = http.MaxBytesHandler(h, s.opts.MaxBodyBytes)
	}
	return s.recoverPanics(s.logRequests(h))
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "buildbee",
		"api":     "v1",
		"identity": map[string]string{
			"cookie": personCookie,
			"header": asHeader,
			"worker": workerHeader,
		},
		"streams": []string{"/v1/ws", "/v1/runs/{id}/ws", "/v1/channels/{id}/ws"},
	})
}
