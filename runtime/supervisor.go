// Package runtime is the Go supervisor for Bot Runs.
//
// A Run starts a Docker Sandbox (or a fake engine), optionally clones a Repo,
// executes a command, streams status to the Server, and stores logs as an Artifact.
package runtime

import (
	"context"
	"fmt"

	"github.com/codemodify/buildbee/runtime/notify"
	"github.com/codemodify/buildbee/runtime/sandbox"
)

// Status is the lifecycle state of a Run.
type Status string

const (
	StatusPending   Status = "pending"
	StatusStarting  Status = "starting"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

type Supervisor struct {
	Engine sandbox.Engine
	Server *notify.Client
}

func NewSupervisor(serverURL string, engine sandbox.Engine) *Supervisor {
	if engine == nil {
		if sandbox.Available() {
			engine = sandbox.DockerEngine{}
		} else {
			engine = sandbox.FakeEngine{Logs: "docker unavailable; fake Sandbox"}
		}
	}
	return &Supervisor{Engine: engine, Server: notify.New(serverURL)}
}

type Request struct {
	TaskID  string
	RunID   string
	RepoURL string
	Command string
	Fake    bool
	FakePR  bool
}

type Outcome struct {
	Run      *notify.Run
	Logs     string
	Artifact *notify.Artifact
	PR       *notify.Artifact
	UsedFake bool
}

// Execute creates or reuses a Run, drives Sandbox status, and posts Artifacts.
func (s *Supervisor) Execute(ctx context.Context, req Request) (*Outcome, error) {
	if req.TaskID == "" {
		return nil, fmt.Errorf("task id is required")
	}
	engine := s.Engine
	usedFake := req.Fake
	if req.Fake {
		engine = sandbox.FakeEngine{Logs: "fake sandbox (requested)\n"}
	}

	var run *notify.Run
	var err error
	if req.RunID != "" {
		run = &notify.Run{ID: req.RunID, TaskID: req.TaskID, Status: "pending"}
	} else {
		run, err = s.Server.CreateRun(req.TaskID)
		if err != nil {
			return nil, err
		}
	}

	if _, err := s.Server.UpdateRun(run.ID, string(StatusRunning), "sandbox starting"); err != nil {
		return nil, err
	}

	res, err := engine.Run(ctx, sandbox.Spec{
		RunID: run.ID, RepoURL: req.RepoURL, Command: req.Command,
	})
	if err != nil {
		_, _ = s.Server.UpdateRun(run.ID, string(StatusFailed), err.Error())
		return nil, err
	}

	status := StatusSucceeded
	detail := "sandbox exit 0"
	if res.ExitCode != 0 {
		status = StatusFailed
		detail = fmt.Sprintf("sandbox exit %d", res.ExitCode)
	}
	run, err = s.Server.UpdateRun(run.ID, string(status), detail)
	if err != nil {
		return nil, err
	}

	art, err := s.Server.CreateArtifact(req.TaskID, notify.Artifact{
		RunID: run.ID, Kind: "log", Name: "sandbox.log", Body: res.Logs,
	})
	if err != nil {
		return nil, err
	}

	out := &Outcome{Run: run, Logs: res.Logs, Artifact: art, UsedFake: usedFake}
	if req.FakePR || status == StatusSucceeded {
		pr, err := s.Server.OpenPR(req.TaskID, map[string]any{
			"fake":   true,
			"run_id": run.ID,
			"title":  "BuildBee Run Artifact",
			"body":   "Recorded by the runtime supervisor.",
		})
		if err == nil {
			out.PR = pr
		}
	}
	return out, nil
}

// StartRun is kept for older callers; it only returns pending.
func (s *Supervisor) StartRun(runID string) (Status, error) {
	_ = runID
	return StatusPending, nil
}
