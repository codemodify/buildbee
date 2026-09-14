// Package models holds Project workspace records used by the Server.
package models

import (
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Activity Type values recorded on a Project feed.
const (
	TypeProject      = "project"
	TypeMember       = "member"
	TypeChannel      = "channel"
	TypeMessage      = "message"
	TypeTask         = "task"
	TypeHandoff      = "handoff"
	TypeDecision     = "decision"
	TypeRun          = "run"
	TypeArtifact     = "artifact"
	TypePipeline     = "pipeline"
	TypeRepo         = "repo"
	TypeRoutine      = "routine"
	TypeIssue        = "issue"
	TypeMention      = "mention"
	TypeMemory       = "memory"
	TypeNotification = "notification"
	TypeInvite       = "invite"
)

// Bot Role values seeded on every new Project.
const (
	RoleOwner   = "owner"
	RoleAdmin   = "admin"
	RoleMember  = "member"
	RoleScout   = "scout"
	RoleBuilder = "builder"
	RoleSentry  = "sentry"
	RolePulse   = "pulse"
)

// CanManageInvites is true for Project owner or admin.
func CanManageInvites(role string) bool {
	r := strings.ToLower(strings.TrimSpace(role))
	return r == RoleOwner || r == RoleAdmin
}

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	AutoRun   bool      `json:"auto_run"`
	CreatedAt time.Time `json:"created_at"`
}

// ProjectBundle is a Project plus seed/list Members and Channels.
type ProjectBundle struct {
	Project
	Members  []Member  `json:"members"`
	Channels []Channel `json:"channels"`
}

type Member struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Kind         string    `json:"kind"` // human | bot
	DisplayName  string    `json:"display_name"`
	Role         string    `json:"role"`
	Instructions string    `json:"instructions,omitempty"`
	Identity     string    `json:"identity"`
	GitHubLogin  string    `json:"github_login,omitempty"`
	GitHubID     string    `json:"github_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// BotSeed is the Role + instructions blurb for a seeded Bot Member.
type BotSeed struct {
	Name         string
	Role         string
	Instructions string
}

// DefaultBots are the four Bots created with every Project (Scout, Builder, Sentry, Pulse).
func DefaultBots() []BotSeed {
	return []BotSeed{
		{Name: "Scout", Role: RoleScout, Instructions: "Triage incoming Tasks. Clarify scope, flag ambiguity, and Handoff to Builder when ready."},
		{Name: "Builder", Role: RoleBuilder, Instructions: "Implement Tasks in a Sandbox. Produce Artifacts and Handoff to Sentry for review."},
		{Name: "Sentry", Role: RoleSentry, Instructions: "Review Runs, watch CI Pipelines, and flag regressions before merge."},
		{Name: "Pulse", Role: RolePulse, Instructions: "Run Routines: morning digests, Activity summaries, and Channel nudges."},
	}
}

// MemberByRole returns the first Member with the given Role (case-insensitive).
func MemberByRole(members []Member, role string) *Member {
	want := strings.ToLower(strings.TrimSpace(role))
	for i := range members {
		if strings.ToLower(members[i].Role) == want {
			return &members[i]
		}
	}
	return nil
}

var mentionToken = regexp.MustCompile(`(?i)@([a-z0-9_-]+)`)

// MentionedBots returns Bot Members referenced as @Role or @Name in Channel text.
func MentionedBots(body string, members []Member) []Member {
	found := mentionToken.FindAllStringSubmatch(body, -1)
	if len(found) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, m := range found {
		want[strings.ToLower(m[1])] = true
	}
	var out []Member
	seen := map[string]bool{}
	for _, mem := range members {
		if mem.Kind != "bot" || seen[mem.ID] {
			continue
		}
		if want[strings.ToLower(mem.Role)] || want[strings.ToLower(mem.DisplayName)] {
			out = append(out, mem)
			seen[mem.ID] = true
		}
	}
	return out
}

// TaskLooksAmbiguous is the thin Scout heuristic for a Decision stub.
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

// Identity is who a Member is (GitHub OAuth for humans; server-issued for Bots).
type Identity struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	DisplayName string    `json:"display_name"`
	GitHubLogin string    `json:"github_login,omitempty"`
	GitHubID    string    `json:"github_id,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

// HumanIdentityKey is the stable login key for a GitHub-linked human.
func HumanIdentityKey(login string) string {
	return "github:" + strings.ToLower(strings.TrimSpace(login))
}

// PreferencesKey scopes mute prefs: shared Identity, else one Member.
func PreferencesKey(m Member) string {
	if m.GitHubLogin != "" {
		return HumanIdentityKey(m.GitHubLogin)
	}
	if id := strings.TrimSpace(m.Identity); id != "" && id != "human:stub" && id != "dev" {
		return id
	}
	return "member:" + m.ID
}

// Preferences are per-Identity (or per-Member when no GitHub login).
type Preferences struct {
	Identity     string    `json:"identity"`
	MuteMentions bool      `json:"mute_mentions"`
	MuteRoutines bool      `json:"mute_routines"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

type Channel struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Message is Channel chat; its Activity Type is "message".
type Message struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	MemberID  string    `json:"member_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type Task struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"project_id"`
	Title            string    `json:"title"`
	Status           string    `json:"status"`
	AssigneeMemberID string    `json:"assignee_member_id,omitempty"`
	IssueNumber      int       `json:"issue_number,omitempty"`
	IssueURL         string    `json:"issue_url,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Handoff struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"task_id"`
	FromMemberID string     `json:"from_member_id"`
	ToMemberID   string     `json:"to_member_id"`
	Note         string     `json:"note"`
	Status       string     `json:"status"` // open | complete
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

type Decision struct {
	ID               string     `json:"id"`
	ProjectID        string     `json:"project_id"`
	Prompt           string     `json:"prompt"`
	Options          []string   `json:"options"`
	Recommendation   string     `json:"recommendation"`
	Answer           string     `json:"answer,omitempty"`
	Reused           bool       `json:"reused,omitempty"`
	Fingerprint      string     `json:"fingerprint,omitempty"`
	AssigneeMemberID string     `json:"assignee_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	AnsweredAt       *time.Time `json:"answered_at,omitempty"`
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

type Activity struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

type Run struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	ProjectID string    `json:"project_id"`
	Status    string    `json:"status"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Artifact is a file, log, or Repo link produced by a Task or Run.
type Artifact struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	TaskID    string    `json:"task_id"`
	RunID     string    `json:"run_id,omitempty"`
	Kind      string    `json:"kind"` // log | pr | file | repo
	Name      string    `json:"name"`
	Body      string    `json:"body,omitempty"`
	URL       string    `json:"url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Pipeline is a check recorded on a Task (or Artifact), e.g. GitHub Actions.
type Pipeline struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	TaskID      string    `json:"task_id"`
	ArtifactID  string    `json:"artifact_id,omitempty"`
	Name        string    `json:"name"`
	Status      string    `json:"status"` // pending | success | failure
	ExternalURL string    `json:"external_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TaskDetail is a Task plus related Runs, Artifacts, and Pipelines.
type TaskDetail struct {
	Task
	Runs      []Run      `json:"runs"`
	Artifacts []Artifact `json:"artifacts"`
	Pipelines []Pipeline `json:"pipelines"`
}

// Routine is a repeatable Project workflow (interval schedule).
type Routine struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"project_id"`
	BotMemberID string     `json:"bot_member_id,omitempty"`
	Name        string     `json:"name"`
	Schedule    string     `json:"schedule"` // duration like 24h, or "daily"
	Enabled     bool       `json:"enabled"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Invite lets a human join a Project via token link.
type Invite struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	Email             string     `json:"email,omitempty"`
	GitHubLogin       string     `json:"github_login,omitempty"`
	Role              string     `json:"role"`
	Token             string     `json:"token,omitempty"`
	InvitedByMemberID string     `json:"invited_by_member_id,omitempty"`
	AcceptedMemberID  string     `json:"accepted_member_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	AcceptedAt        *time.Time `json:"accepted_at,omitempty"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
}

// Status is pending, accepted, or revoked.
func (i Invite) Status() string {
	if i.RevokedAt != nil {
		return "revoked"
	}
	if i.AcceptedAt != nil {
		return "accepted"
	}
	return "pending"
}

// InvitePath is the hash-route accept link.
func InvitePath(token string) string {
	return "#/invite/" + token
}

// Notification is an unread/read inbox item for a Member.
type Notification struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"project_id"`
	MemberID  string     `json:"member_id"`
	Kind      string     `json:"kind"` // decision | handoff | mention | pipeline
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	Href      string     `json:"href,omitempty"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}
