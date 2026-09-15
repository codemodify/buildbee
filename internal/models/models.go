// Package models holds the BuildBee domain records and the rules that need
// no I/O: status vocabularies and transitions, mention parsing, Decision
// fingerprints.
package models

import (
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Activity types recorded on a Project feed.
const (
	TypeProject  = "project"
	TypeMember   = "member"
	TypeChannel  = "channel"
	TypeMessage  = "message"
	TypeTask     = "task"
	TypeHandoff  = "handoff"
	TypeDecision = "decision"
	TypeRun      = "run"
	TypeArtifact = "artifact"
	TypePipeline = "pipeline"
	TypeRoutine  = "routine"
	TypeIssue    = "issue"
	TypeMention  = "mention"
	TypeMemory   = "memory"
)

// RunEvent kinds streamed during a Run (ACP tokens, tools, status, logs).
const (
	RunEventToken      = "token"
	RunEventToolCall   = "tool_call"
	RunEventToolResult = "tool_result"
	RunEventStatus     = "status"
	RunEventLog        = "log"
)

// NormalizeRunEventKind returns the canonical kind, or "" if unknown.
func NormalizeRunEventKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case RunEventToken, RunEventToolCall, RunEventToolResult, RunEventStatus, RunEventLog:
		return k
	default:
		return ""
	}
}

// Roles. Humans are owner, admin or member; Bots carry their function.
const (
	RoleOwner   = "owner"
	RoleAdmin   = "admin"
	RoleMember  = "member"
	RoleScout   = "scout"
	RoleBuilder = "builder"
	RoleSentry  = "sentry"
	RolePulse   = "pulse"
)

// HumanRole reports whether r is a role a person can hold.
func HumanRole(r string) bool { return r == RoleOwner || r == RoleAdmin || r == RoleMember }

// Member kinds.
const (
	KindHuman = "human"
	KindBot   = "bot"
)

// --- status vocabularies ---------------------------------------------------

// TaskStatus is where a Task stands on the board.
type TaskStatus string

const (
	TaskOpen       TaskStatus = "open"
	TaskInProgress TaskStatus = "in_progress"
	TaskDone       TaskStatus = "done"
	TaskCanceled   TaskStatus = "canceled"
)

// ParseTaskStatus accepts the canonical values only.
func ParseTaskStatus(s string) (TaskStatus, bool) {
	switch v := TaskStatus(strings.TrimSpace(s)); v {
	case TaskOpen, TaskInProgress, TaskDone, TaskCanceled:
		return v, true
	}
	return "", false
}

// RunStatus is the lifecycle of one Run.
type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCanceled  RunStatus = "canceled"
)

// ParseRunStatus accepts the canonical values only.
func ParseRunStatus(s string) (RunStatus, bool) {
	switch v := RunStatus(strings.TrimSpace(s)); v {
	case RunPending, RunRunning, RunSucceeded, RunFailed, RunCanceled:
		return v, true
	}
	return "", false
}

// Terminal reports whether no further transition is allowed.
func (s RunStatus) Terminal() bool {
	return s == RunSucceeded || s == RunFailed || s == RunCanceled
}

// CanBecome reports whether a Run may move from s to next.
// pending → running | failed | canceled; running → succeeded | failed | canceled.
func (s RunStatus) CanBecome(next RunStatus) bool {
	switch s {
	case RunPending:
		return next == RunRunning || next == RunFailed || next == RunCanceled
	case RunRunning:
		return next == RunSucceeded || next == RunFailed || next == RunCanceled
	}
	return false
}

// HandoffStatus is open until the receiver completes it.
type HandoffStatus string

const (
	HandoffOpen     HandoffStatus = "open"
	HandoffComplete HandoffStatus = "complete"
)

// PipelineStatus is the state of one CI check.
type PipelineStatus string

const (
	PipelinePending PipelineStatus = "pending"
	PipelineSuccess PipelineStatus = "success"
	PipelineFailure PipelineStatus = "failure"
)

// ParsePipelineStatus maps CI vocabularies (GitHub check_run conclusions and
// statuses included) to a PipelineStatus.
func ParsePipelineStatus(s string) (PipelineStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success", "succeeded", "pass", "passed", "completed", "neutral", "skipped":
		return PipelineSuccess, true
	case "failure", "failed", "error", "fail", "cancelled", "canceled", "timed_out", "action_required", "startup_failure":
		return PipelineFailure, true
	case "pending", "queued", "in_progress", "requested", "waiting", "":
		return PipelinePending, true
	}
	return "", false
}

// --- records -----------------------------------------------------------------

// Person is someone using BuildBee, identified by the name they chose.
// A Person is a Member of each Project they work in.
type Person struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Project struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	AutoRun       bool       `json:"auto_run"`
	RepoURL       string     `json:"repo_url,omitempty"`
	DefaultBranch string     `json:"default_branch,omitempty"`
	ArchivedAt    *time.Time `json:"archived_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// ProjectBundle is a Project plus its Members and Channels.
type ProjectBundle struct {
	Project
	Members  []Member  `json:"members"`
	Channels []Channel `json:"channels"`
}

// Member is a Person or a Bot in one Project.
type Member struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	PersonID     string    `json:"person_id,omitempty"`
	Kind         string    `json:"kind"`
	DisplayName  string    `json:"display_name"`
	Role         string    `json:"role"`
	Instructions string    `json:"instructions,omitempty"`
	Agent        string    `json:"agent,omitempty"` // Bots: the agent CLI they run as
	CreatedAt    time.Time `json:"created_at"`
}

// BotSeed is the Role + instructions blurb for a seeded Bot Member.
type BotSeed struct {
	Name         string
	Role         string
	Instructions string
}

// DefaultBots are the four Bots created with every Project.
func DefaultBots() []BotSeed {
	return []BotSeed{
		{Name: "Scout", Role: RoleScout, Instructions: "Triage incoming Tasks. Clarify scope, flag ambiguity, and Handoff to Builder when ready."},
		{Name: "Builder", Role: RoleBuilder, Instructions: "Implement Tasks in a Sandbox. Produce Artifacts and Handoff to Sentry for review."},
		{Name: "Sentry", Role: RoleSentry, Instructions: "Review Runs, watch CI Pipelines, and flag regressions before merge."},
		{Name: "Pulse", Role: RolePulse, Instructions: "Run Routines: morning digests, Activity summaries, and Channel nudges."},
	}
}

// MemberByRole returns the earliest Member with the given Role.
func MemberByRole(members []Member, role string) *Member {
	want := strings.ToLower(strings.TrimSpace(role))
	for i := range members {
		if strings.ToLower(members[i].Role) == want {
			return &members[i]
		}
	}
	return nil
}

var mentionToken = regexp.MustCompile(`@([\p{L}\p{N}_.-]+)`)

// MentionedBots returns Bot Members referenced as @Role or @Name in text.
func MentionedBots(body string, members []Member) []Member {
	return mentioned(body, members, KindBot)
}

// MentionedPeople returns human Members referenced as @Name in text
// (a name's spaces may be written as underscores or dashes: @Ada_Lovelace).
func MentionedPeople(body string, members []Member) []Member {
	return mentioned(body, members, KindHuman)
}

func mentioned(body string, members []Member, kind string) []Member {
	found := mentionToken.FindAllStringSubmatch(body, -1)
	if len(found) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, m := range found {
		// "@Scout." ends a sentence; "@Ada-Lovelace" and "@Ada_Lovelace" match.
		key := strings.ToLower(strings.TrimRight(m[1], ".-_"))
		want[strings.ReplaceAll(key, "-", "_")] = true
	}
	var out []Member
	seen := map[string]bool{}
	for _, mem := range members {
		if mem.Kind != kind || seen[mem.ID] {
			continue
		}
		hit := want[handle(mem.DisplayName)]
		if kind == KindBot {
			hit = hit || want[strings.ToLower(mem.Role)]
		}
		if hit {
			out = append(out, mem)
			seen[mem.ID] = true
		}
	}
	return out
}

// handle is how a display name is written after @: lowercase, spaces as _.
func handle(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), "_"))
}

// TaskLooksAmbiguous is the thin Scout heuristic for opening a Decision.
func TaskLooksAmbiguous(title, note string) bool {
	if strings.Contains(title, "?") {
		return true
	}
	s := strings.ToLower(title + " " + note)
	for _, k := range []string{"ambiguous", "unclear", "tbd", "not sure"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// DecisionFingerprint normalizes a Decision prompt for reuse ("don't ask twice").
func DecisionFingerprint(prompt string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(prompt)) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Preferences are one Person's notification settings.
type Preferences struct {
	PersonID     string    `json:"person_id"`
	MuteMentions bool      `json:"mute_mentions"`
	MuteRoutines bool      `json:"mute_routines"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

type Channel struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"project_id"`
	Name       string     `json:"name"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Message is Channel chat. Seq orders messages and pages through them.
type Message struct {
	ID        string    `json:"id"`
	Seq       int64     `json:"seq"`
	ChannelID string    `json:"channel_id"`
	ProjectID string    `json:"project_id"`
	MemberID  string    `json:"member_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type Task struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	Title             string     `json:"title"`
	Body              string     `json:"body,omitempty"`
	Status            TaskStatus `json:"status"`
	AssigneeMemberID  string     `json:"assignee_member_id,omitempty"`
	CreatedByMemberID string     `json:"created_by_member_id,omitempty"`
	IssueNumber       int        `json:"issue_number,omitempty"`
	IssueURL          string     `json:"issue_url,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Handoff struct {
	ID           string        `json:"id"`
	TaskID       string        `json:"task_id"`
	ProjectID    string        `json:"project_id"`
	FromMemberID string        `json:"from_member_id"`
	ToMemberID   string        `json:"to_member_id"`
	Note         string        `json:"note"`
	Status       HandoffStatus `json:"status"`
	CreatedAt    time.Time     `json:"created_at"`
	CompletedAt  *time.Time    `json:"completed_at,omitempty"`
}

type Decision struct {
	ID                 string     `json:"id"`
	ProjectID          string     `json:"project_id"`
	TaskID             string     `json:"task_id,omitempty"`
	Prompt             string     `json:"prompt"`
	Options            []string   `json:"options"`
	Recommendation     string     `json:"recommendation"`
	Answer             string     `json:"answer,omitempty"`
	AnsweredByMemberID string     `json:"answered_by_member_id,omitempty"`
	Reused             bool       `json:"reused,omitempty"`
	Fingerprint        string     `json:"fingerprint,omitempty"`
	AssigneeMemberID   string     `json:"assignee_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	AnsweredAt         *time.Time `json:"answered_at,omitempty"`
}

// DecisionMemory is a remembered answer so the same question is not asked twice.
type DecisionMemory struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Fingerprint string    `json:"fingerprint"`
	Prompt      string    `json:"prompt"`
	Answer      string    `json:"answer"`
	DecisionID  string    `json:"decision_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Activity is one entry in a Project's event log. It is written in the same
// transaction as the change it describes and names who acted.
type Activity struct {
	Seq           int64          `json:"seq"`
	ProjectID     string         `json:"project_id"`
	ActorMemberID string         `json:"actor_member_id,omitempty"`
	Actor         string         `json:"actor"`
	Type          string         `json:"type"`
	Action        string         `json:"action"`
	SubjectID     string         `json:"subject_id,omitempty"`
	Payload       map[string]any `json:"payload"`
	CreatedAt     time.Time      `json:"created_at"`
}

type Run struct {
	ID          string     `json:"id"`
	TaskID      string     `json:"task_id"`
	ProjectID   string     `json:"project_id"`
	BotMemberID string     `json:"bot_member_id,omitempty"`
	Status      RunStatus  `json:"status"`
	Detail      string     `json:"detail"`
	Agent       string     `json:"agent,omitempty"`
	Prompt      string     `json:"prompt,omitempty"`
	Worker      string     `json:"worker,omitempty"`
	LeaseUntil  *time.Time `json:"lease_until,omitempty"`
	Attempts    int        `json:"attempts"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

// RunEvent is one incremental ACP / Sandbox event on a Run.
type RunEvent struct {
	ID        string         `json:"id"`
	RunID     string         `json:"run_id"`
	Seq       int            `json:"seq"`
	Kind      string         `json:"kind"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

// Artifact is a file, log, or Repo link produced by a Task or Run. Listings
// carry Size but not Body; fetch one Artifact to read its Body.
type Artifact struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	TaskID    string    `json:"task_id"`
	RunID     string    `json:"run_id,omitempty"`
	Kind      string    `json:"kind"` // log | pr | file | repo
	Name      string    `json:"name"`
	Body      string    `json:"body,omitempty"`
	Size      int       `json:"size"`
	URL       string    `json:"url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Pipeline is a CI check recorded on a Task.
type Pipeline struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id"`
	TaskID      string         `json:"task_id"`
	ArtifactID  string         `json:"artifact_id,omitempty"`
	Name        string         `json:"name"`
	Status      PipelineStatus `json:"status"`
	ExternalURL string         `json:"external_url,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// TaskDetail is a Task plus everything recorded against it.
type TaskDetail struct {
	Task
	Handoffs  []Handoff  `json:"handoffs"`
	Runs      []Run      `json:"runs"`
	Artifacts []Artifact `json:"artifacts"`
	Pipelines []Pipeline `json:"pipelines"`
}

// Routine is a repeatable Project workflow on an interval schedule.
type Routine struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	BotMemberID string     `json:"bot_member_id,omitempty"`
	Name        string     `json:"name"`
	Schedule    string     `json:"schedule"` // Go duration like 24h, or "daily"
	Enabled     bool       `json:"enabled"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Notification is an inbox item for one Person.
type Notification struct {
	ID        string     `json:"id"`
	Seq       int64      `json:"seq"`
	PersonID  string     `json:"person_id"`
	ProjectID string     `json:"project_id"`
	Kind      string     `json:"kind"` // decision | handoff | mention | pipeline | routine
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	Href      string     `json:"href,omitempty"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Agents BuildBee can drive, and "fake" for tests and demos. A Bot's Agent
// and a Run's Agent must be one of these (or empty: the worker's default).
var Agents = []string{"claude", "grok", "codex", "opencode", "goose", "fake"}

// ValidAgent reports whether a is empty or a known agent.
func ValidAgent(a string) bool {
	if a == "" {
		return true
	}
	for _, k := range Agents {
		if a == k {
			return true
		}
	}
	return false
}

// Claim is a Run handed to a worker, with what it needs to execute it.
type Claim struct {
	Run     Run       `json:"run"`
	Task    Task      `json:"task"`
	Project Project   `json:"project"`
	Bot     *Member   `json:"bot,omitempty"`
	Notes   []Handoff `json:"handoffs"`
}
