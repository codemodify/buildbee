package store

import (
	"context"
	"errors"

	"github.com/codemodify/buildbee/server/internal/models"
)

var ErrNotFound = errors.New("not found")

// Store persists Project workspace records.
type Store interface {
	CreateProject(ctx context.Context, name string) (*models.ProjectBundle, error)
	GetProject(ctx context.Context, id string) (*models.ProjectBundle, error)
	ListProjects(ctx context.Context) ([]models.Project, error)

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
	CreateDecision(ctx context.Context, projectID, prompt, recommendation string, options []string) (*models.Decision, error)
	AnswerDecision(ctx context.Context, id, answer string) (*models.Decision, error)

	ListActivity(ctx context.Context, projectID, typeFilter string) ([]models.Activity, error)

	CreateRun(ctx context.Context, taskID string) (*models.Run, error)
	GetRun(ctx context.Context, id string) (*models.Run, error)
	UpdateRun(ctx context.Context, id, status, detail string) (*models.Run, error)
	ListRuns(ctx context.Context, taskID string) ([]models.Run, error)

	CreateArtifact(ctx context.Context, in models.Artifact) (*models.Artifact, error)
	GetArtifact(ctx context.Context, id string) (*models.Artifact, error)
	ListArtifacts(ctx context.Context, taskID string) ([]models.Artifact, error)

	CreatePipeline(ctx context.Context, in models.Pipeline) (*models.Pipeline, error)
	ListPipelines(ctx context.Context, taskID string) ([]models.Pipeline, error)
	UpdatePipeline(ctx context.Context, id, status, externalURL string) (*models.Pipeline, error)
	GetPipeline(ctx context.Context, id string) (*models.Pipeline, error)
}
