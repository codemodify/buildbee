package store

import (
	"context"
	"errors"
	"time"

	"github.com/codemodify/buildbee/internal/models"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
	ErrConflict  = errors.New("conflict")
)

// Store persists Project workspace records.
type Store interface {
	Ping(ctx context.Context) error

	CreateProject(ctx context.Context, name string, autoRun bool) (*models.ProjectBundle, error)
	GetProject(ctx context.Context, id string) (*models.ProjectBundle, error)
	ListProjects(ctx context.Context) ([]models.Project, error)
	UpdateProject(ctx context.Context, id string, autoRun *bool) (*models.Project, error)

	ListMembers(ctx context.Context, projectID string) ([]models.Member, error)
	AddMember(ctx context.Context, m models.Member) (*models.Member, error)
	GetMember(ctx context.Context, id string) (*models.Member, error)

	ListChannels(ctx context.Context, projectID string) ([]models.Channel, error)
	CreateChannel(ctx context.Context, projectID, name string) (*models.Channel, error)
	GetChannel(ctx context.Context, id string) (*models.Channel, error)

	ListMessages(ctx context.Context, channelID string) ([]models.Message, error)
	PostMessage(ctx context.Context, channelID, memberID, body string) (*models.Message, error)

	ListTasks(ctx context.Context, projectID string) ([]models.Task, error)
	CreateTask(ctx context.Context, projectID, title, assigneeMemberID string) (*models.Task, error)
	GetTask(ctx context.Context, id string) (*models.Task, error)
	UpdateTaskStatus(ctx context.Context, id, status string) (*models.Task, error)

	CreateHandoff(ctx context.Context, taskID, fromID, toID, note string) (*models.Handoff, error)
	GetHandoff(ctx context.Context, id string) (*models.Handoff, error)
	CompleteHandoff(ctx context.Context, id string) (*models.Handoff, error)

	ListDecisions(ctx context.Context, projectID string) ([]models.Decision, error)
	CreateDecision(ctx context.Context, projectID, prompt, recommendation string, options []string, assigneeMemberID string) (*models.Decision, error)
	CreateReusedDecision(ctx context.Context, projectID, prompt, recommendation string, options []string, answer, assigneeMemberID string) (*models.Decision, error)
	AnswerDecision(ctx context.Context, id, answer string) (*models.Decision, error)
	GetDecisionMemory(ctx context.Context, projectID, fingerprint string) (*models.DecisionMemory, error)
	ListDecisionMemories(ctx context.Context, projectID string) ([]models.DecisionMemory, error)

	ListActivity(ctx context.Context, projectID, typeFilter string) ([]models.Activity, error)

	CreateRun(ctx context.Context, taskID string) (*models.Run, error)
	GetRun(ctx context.Context, id string) (*models.Run, error)
	UpdateRun(ctx context.Context, id, status, detail string) (*models.Run, error)
	ListRuns(ctx context.Context, taskID string) ([]models.Run, error)
	AppendRunEvent(ctx context.Context, runID, kind string, payload map[string]any) (*models.RunEvent, error)
	ListRunEvents(ctx context.Context, runID string, afterSeq int) ([]models.RunEvent, error)

	CreateArtifact(ctx context.Context, in models.Artifact) (*models.Artifact, error)
	GetArtifact(ctx context.Context, id string) (*models.Artifact, error)
	ListArtifacts(ctx context.Context, taskID string) ([]models.Artifact, error)

	CreatePipeline(ctx context.Context, in models.Pipeline) (*models.Pipeline, error)
	ListPipelines(ctx context.Context, taskID string) ([]models.Pipeline, error)
	UpdatePipeline(ctx context.Context, id, status, externalURL string) (*models.Pipeline, error)
	GetPipeline(ctx context.Context, id string) (*models.Pipeline, error)

	UpsertIssueTask(ctx context.Context, projectID string, number int, title, issueURL string) (*models.Task, error)
	AppendActivity(ctx context.Context, projectID, typ string, payload map[string]any) error

	ListRoutines(ctx context.Context, projectID string) ([]models.Routine, error)
	ListEnabledRoutines(ctx context.Context) ([]models.Routine, error)
	CreateRoutine(ctx context.Context, in models.Routine) (*models.Routine, error)
	GetRoutine(ctx context.Context, id string) (*models.Routine, error)
	UpdateRoutine(ctx context.Context, id string, enabled *bool, lastRun *time.Time) (*models.Routine, error)

	CreateNotification(ctx context.Context, in models.Notification) (*models.Notification, error)
	ListNotifications(ctx context.Context, memberID string, unreadOnly bool) ([]models.Notification, error)
	GetNotification(ctx context.Context, id string) (*models.Notification, error)
	MarkNotificationRead(ctx context.Context, id string) (*models.Notification, error)
	MarkAllNotificationsRead(ctx context.Context, memberID string) (int, error)

	GetPreferences(ctx context.Context, memberID string) (*models.Preferences, error)
	SetPreferences(ctx context.Context, in models.Preferences) (*models.Preferences, error)
}
