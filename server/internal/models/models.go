// Package models holds Project workspace records used by the Server.
package models

import "time"

// Activity Type values recorded on a Project feed.
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
	TypeRepo     = "repo"
)

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// ProjectBundle is a Project plus seed/list Members and Channels.
type ProjectBundle struct {
	Project
	Members  []Member  `json:"members"`
	Channels []Channel `json:"channels"`
}

type Member struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Kind        string    `json:"kind"` // human | bot
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	Identity    string    `json:"identity"`
	CreatedAt   time.Time `json:"created_at"`
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
	ID             string     `json:"id"`
	ProjectID      string     `json:"project_id"`
	Prompt         string     `json:"prompt"`
	Options        []string   `json:"options"`
	Recommendation string     `json:"recommendation"`
	Answer         string     `json:"answer,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	AnsweredAt     *time.Time `json:"answered_at,omitempty"`
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
