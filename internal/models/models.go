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

// RunEvent kinds streamed during a Run: the agent's reply (token), its
// reasoning (thought), its plan, tool calls and their results, token usage,
// status changes and logs.
const (
	RunEventToken      = "token"
	RunEventThought    = "thought"
	RunEventPlan       = "plan"
	RunEventToolCall   = "tool_call"
	RunEventToolResult = "tool_result"
	RunEventUsage      = "usage"
	RunEventStatus     = "status"
	RunEventLog        = "log"
	RunEventSteer      = "steer" // a person's message to the working agent
)

// NormalizeRunEventKind returns the canonical kind of an event a worker may
// append, or "" if it is unknown or not a worker's to write.
func NormalizeRunEventKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case RunEventToken, RunEventThought, RunEventPlan, RunEventToolCall, RunEventToolResult, RunEventUsage, RunEventStatus, RunEventLog:
		return k
	default: // including steer, which only SteerRun records
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
	ID            string `json:"id"`
	Name          string `json:"name"`
	AutoRun       bool   `json:"auto_run"` // autopilot
	MergePolicy   string `json:"merge_policy"`
	MaxRuns       int    `json:"max_runs"` // Runs at once; 0 = no limit
	Instructions  string `json:"instructions,omitempty"`
	RepoURL       string `json:"repo_url,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
	AgentImage    string `json:"agent_image,omitempty"` // container image for its agents; "" = the worker's
	// AgentPermissions is "auto" (agents act freely in their Run) or "ask"
	// (each permission request is a Decision someone answers).
	AgentPermissions string     `json:"agent_permissions"`
	Kind             string     `json:"kind"` // ProjectKind or DirectSpace
	ArchivedAt       *time.Time `json:"archived_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// Project kinds. The direct space is one hidden Project that holds DMs
// between people, whichever Projects they share.
const (
	ProjectKind = "project"
	DirectSpace = "direct"
)

// DM is a direct message between people as its reader sees it: who else
// is in it and what they have not read.
type DM struct {
	Channel
	With    []DMPeer  `json:"with"`
	Unread  int       `json:"unread"`
	LastSeq int64     `json:"last_seq"`
	LastAt  time.Time `json:"last_at"`
}

// DMPeer is someone else in a DM.
type DMPeer struct {
	MemberID string `json:"member_id"`
	PersonID string `json:"person_id,omitempty"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Role     string `json:"role"`
}

// ProjectBundle is a Project plus its Members and Channels.
type ProjectBundle struct {
	Project
	Members  []Member  `json:"members"`
	Channels []Channel `json:"channels"`
}

// Member is a Person or a Bot in one Project.
type Member struct {
	ID           string     `json:"id"`
	ProjectID    string     `json:"project_id"`
	PersonID     string     `json:"person_id,omitempty"`
	Kind         string     `json:"kind"`
	DisplayName  string     `json:"display_name"`
	Role         string     `json:"role"`
	Instructions string     `json:"instructions,omitempty"`
	Agent        string     `json:"agent,omitempty"` // Bots: the agent CLI they run as
	LeftAt       *time.Time `json:"left_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

// BotSeed is the Role + instructions blurb for a seeded Bot Member.
type BotSeed struct {
	Name         string `json:"name"`
	Role         string `json:"role"`
	Instructions string `json:"instructions"`
}

// DefaultBots are the autopilot's Bots: offered when adding a Bot, and
// created with a Project only when asked (default_bots).
func DefaultBots() []BotSeed {
	return []BotSeed{
		{Name: "Scout", Role: RoleScout, Instructions: "You are Scout. You turn Tasks into clear, small plans and say when a Task is unclear."},
		{Name: "Builder", Role: RoleBuilder, Instructions: "You are Builder. You make focused, tested changes."},
		{Name: "Sentry", Role: RoleSentry, Instructions: "You are Sentry. You review changes for correctness, tests and security, and say exactly what must change."},
		{Name: "Pulse", Role: RolePulse, Instructions: "You are Pulse. You open the Tasks a Project's Routines schedule."},
	}
}

// MemberByRole returns the earliest current Member with the given Role.
func MemberByRole(members []Member, role string) *Member {
	want := strings.ToLower(strings.TrimSpace(role))
	for i := range members {
		if strings.ToLower(members[i].Role) == want && members[i].LeftAt == nil {
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
		if mem.Kind != kind || seen[mem.ID] || (kind == KindBot && mem.LeftAt != nil) { // removed Bots do nothing
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

// TasksChannel is every Project's first Channel, where each Task's thread
// lives. It cannot be renamed or archived.
const TasksChannel = "tasks"

// Channel kinds: open channels, and DMs between chosen Members.
const (
	ChannelOpen = "channel"
	ChannelDM   = "dm"
)

type Channel struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"project_id"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Locked     bool       `json:"locked,omitempty"`     // #tasks
	Members    []string   `json:"member_ids,omitempty"` // DMs only
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Allows reports whether Member memberID may read and write the Channel:
// every Member for open Channels, only its Members for a DM.
func (c *Channel) Allows(memberID string) bool {
	if c.Kind != ChannelDM {
		return true
	}
	for _, m := range c.Members {
		if m == memberID {
			return true
		}
	}
	return false
}

// Message is Channel chat. Seq orders messages and pages through them. A
// root message may start a thread of replies; a Task's thread is where its
// Bots report progress.
type Message struct {
	ID          string     `json:"id"`
	Seq         int64      `json:"seq"`
	ChannelID   string     `json:"channel_id"`
	ProjectID   string     `json:"project_id"`
	MemberID    string     `json:"member_id"`
	Body        string     `json:"body"`
	ThreadID    string     `json:"thread_id,omitempty"`
	ReplyCount  int        `json:"reply_count,omitempty"`
	LastReplyAt *time.Time `json:"last_reply_at,omitempty"`
	TaskID      string     `json:"task_id,omitempty"`
	EditedAt    *time.Time `json:"edited_at,omitempty"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"` // the body is gone
	CreatedAt   time.Time  `json:"created_at"`
	Files       []File     `json:"files,omitempty"`
}

// File is an attachment: uploaded to a Channel, then attached to one
// message. Its bytes are in the blob store under BlobKey.
type File struct {
	ID               string    `json:"id"`
	ChannelID        string    `json:"channel_id"`
	MessageID        string    `json:"message_id,omitempty"`
	UploaderPersonID string    `json:"-"`
	Name             string    `json:"name"`
	ContentType      string    `json:"content_type"`
	Size             int64     `json:"size"`
	SHA256           string    `json:"sha256"`
	BlobKey          string    `json:"-"`
	CreatedAt        time.Time `json:"created_at"`
	URL              string    `json:"url"` // where to download it
}

// InlineImage reports whether a browser may show the file as an image:
// raster types only, never SVG (it can carry script).
func (f File) InlineImage() bool {
	switch f.ContentType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	}
	return false
}

// SearchHit is a message found by search, with where it is.
type SearchHit struct {
	Message
	ChannelName string `json:"channel_name"`
	ChannelKind string `json:"channel_kind"`
	ProjectName string `json:"project_name,omitempty"` // empty for DMs between people
	AuthorName  string `json:"author_name"`
	AuthorKind  string `json:"author_kind"`
}

// Unread is how much of a Channel a Person has not read.
type Unread struct {
	ChannelID string `json:"channel_id"`
	Unread    int    `json:"unread"`
	LastSeq   int64  `json:"last_seq"`
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
	Branch            string     `json:"branch,omitempty"`
	HeadCommit        string     `json:"head_commit,omitempty"`
	PRURL             string     `json:"pr_url,omitempty"`
	MergedAt          *time.Time `json:"merged_at,omitempty"`
	ThreadID          string     `json:"thread_id,omitempty"`
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
	Action             string     `json:"action,omitempty"` // "merge": answering "merge" merges the Task's PR
	Commit             string     `json:"commit,omitempty"` // the commit a merge Decision is about
	RunID              string     `json:"run_id,omitempty"` // for permission: the Run whose agent asks
	CreatedAt          time.Time  `json:"created_at"`
	AnsweredAt         *time.Time `json:"answered_at,omitempty"`
}

// Agent permission modes.
const (
	PermissionsAuto = "auto"
	PermissionsAsk  = "ask"
)

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
	ID          string    `json:"id"`
	TaskID      string    `json:"task_id"`
	ProjectID   string    `json:"project_id"`
	BotMemberID string    `json:"bot_member_id,omitempty"`
	Status      RunStatus `json:"status"`
	Kind        RunKind   `json:"kind"`
	Detail      string    `json:"detail"`
	Summary     string    `json:"summary,omitempty"`
	Branch      string    `json:"branch,omitempty"`
	Commit      string    `json:"commit,omitempty"`
	PRURL       string    `json:"pr_url,omitempty"`
	Verdict     string    `json:"verdict,omitempty"`
	// Usage, as the agent reported it: peak context and session cost.
	ContextTokens int64      `json:"context_tokens,omitempty"`
	ContextSize   int64      `json:"context_size,omitempty"`
	Cost          float64    `json:"cost,omitempty"`
	CostCurrency  string     `json:"cost_currency,omitempty"`
	Agent         string     `json:"agent,omitempty"`
	Prompt        string     `json:"prompt,omitempty"`
	Worker        string     `json:"worker,omitempty"`
	LeaseUntil    *time.Time `json:"lease_until,omitempty"`
	Attempts      int        `json:"attempts"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

// RunRow is a Run on the Server's dashboard, with what it is about.
type RunRow struct {
	Run
	TaskTitle   string `json:"task_title"`
	ProjectName string `json:"project_name"`
	BotName     string `json:"bot_name,omitempty"`
}

// RunKind is what a Run is for.
type RunKind string

const (
	RunPlan   RunKind = "plan"   // Scout: read the Task and the repo, propose a plan; no changes
	RunBuild  RunKind = "build"  // Builder: change the repo and push the Task's branch
	RunReview RunKind = "review" // Sentry: review the branch and give a verdict; no changes
	RunMerge  RunKind = "merge"  // a worker merges the Task's branch; no agent
)

// RunKindForRole is the kind of Run a Bot with role does.
func RunKindForRole(role string) RunKind {
	switch strings.ToLower(role) {
	case RoleScout:
		return RunPlan
	case RoleSentry:
		return RunReview
	default:
		return RunBuild
	}
}

// ParseVerdict reads a review's verdict from the reviewer's last
// "VERDICT: APPROVE" or "VERDICT: REQUEST_CHANGES" line; "" when there is none.
func ParseVerdict(summary string) string {
	verdict := ""
	for _, line := range strings.Split(summary, "\n") {
		line = strings.ToUpper(strings.Trim(strings.TrimSpace(line), "*_`#> "))
		rest, ok := strings.CutPrefix(line, "VERDICT:")
		if !ok {
			continue
		}
		switch strings.Trim(strings.TrimSpace(rest), "*_`. ") {
		case "APPROVE", "APPROVED":
			verdict = "approve"
		case "REQUEST_CHANGES", "REQUEST CHANGES", "CHANGES_REQUESTED", "CHANGES REQUESTED":
			verdict = "changes"
		}
	}
	return verdict
}

// Merge policies.
const (
	MergeAuto     = "auto"     // merge when Sentry approves and CI passes
	MergeApproval = "approval" // then ask a person
)

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
	// BlobKey is set when the body is in the blob store; the API then
	// returns its first MiB (Truncated if there is more) and RawURL.
	BlobKey   string `json:"-"`
	Truncated bool   `json:"truncated,omitempty"`
	RawURL    string `json:"raw_url,omitempty"`
}

// Pipeline is a CI check recorded on a Task.
type Pipeline struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id"`
	TaskID      string         `json:"task_id"`
	ArtifactID  string         `json:"artifact_id,omitempty"`
	Name        string         `json:"name"`
	Status      PipelineStatus `json:"status"`
	Commit      string         `json:"commit,omitempty"`
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

// Routine opens a Task on a schedule, such as "update dependencies" every
// week, and hands it to a Bot, which starts working on it.
type Routine struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	BotMemberID string     `json:"bot_member_id,omitempty"` // who gets the Task; default Scout
	Name        string     `json:"name"`
	Prompt      string     `json:"prompt"`
	Schedule    string     `json:"schedule"` // Go duration like 24h, or "daily"
	Enabled     bool       `json:"enabled"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	LastTaskID  string     `json:"last_task_id,omitempty"`
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
	// Seq is the Run's last event when it was claimed; steering messages
	// after it are for the worker to pass on.
	Seq int `json:"seq"`
}

// Usage sums what Runs used over a period.
type Usage struct {
	Since     time.Time          `json:"since"`
	Runs      int                `json:"runs"`
	Cost      map[string]float64 `json:"cost"` // by currency
	ByAgent   []UsageRow         `json:"by_agent"`
	ByProject []UsageRow         `json:"by_project,omitempty"`
}

// UsageRow is one group of a Usage summary.
type UsageRow struct {
	Key           string             `json:"key"` // agent name or Project ID
	Name          string             `json:"name,omitempty"`
	Runs          int                `json:"runs"`
	Cost          map[string]float64 `json:"cost"`
	ContextTokens int64              `json:"context_tokens"` // sum of each Run's peak
}

// RunLoad is how many Runs of one status, kind, requested agent ("" =
// any) and worker ("" while queued) there are.
type RunLoad struct {
	Status RunStatus
	Kind   RunKind
	Agent  string
	Worker string
	Runs   int
}

// Roster is everyone who is part of this Server: people with the Projects
// they are in, the Bots of every Project, and who joined or left lately.
type Roster struct {
	People []RosterPerson `json:"people"`
	Bots   []RosterBot    `json:"bots"`
	Events []RosterEvent  `json:"events"`
}

// RosterPerson is a Person and their Projects.
type RosterPerson struct {
	Person
	Projects []Membership `json:"projects"`
}

// Membership is a Person's place in one Project.
type Membership struct {
	ProjectID   string     `json:"project_id"`
	ProjectName string     `json:"project_name"`
	MemberID    string     `json:"member_id"`
	Role        string     `json:"role"`
	JoinedAt    time.Time  `json:"joined_at"`
	LeftAt      *time.Time `json:"left_at,omitempty"`
}

// RosterBot is a Bot and the Project it works in.
type RosterBot struct {
	Member
	ProjectName string `json:"project_name"`
}

// RosterEvent is someone creating, joining, leaving or being added to a Project.
type RosterEvent struct {
	Name        string    `json:"name"`
	Action      string    `json:"action"` // joined | left | added
	ProjectID   string    `json:"project_id"`
	ProjectName string    `json:"project_name"`
	At          time.Time `json:"at"`
}
