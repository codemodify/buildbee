// Package runtime is the Go supervisor for Bot Runs.
//
// A Run starts a Docker Sandbox (or a fake engine), optionally clones a Repo,
// executes a command, streams status to the Server, and stores logs as an Artifact.
package runtime

import (
	"context"
	"fmt"

	"github.com/codemodify/buildbee/runtime/acp"
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
	TaskID  string `json:"task_id"`
	RunID   string `json:"run_id"`
	RepoURL string `json:"repo_url"`
	Command string `json:"command"`
	Fake    bool   `json:"fake"`
	FakePR  bool   `json:"fake_pr"`
	ACP     bool   `json:"acp"`
	Agent   string `json:"agent"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Notes   string `json:"notes"`
}

type Outcome struct {
	Run      *notify.Run
	Logs     string
	Artifact *notify.Artifact
	PR       *notify.Artifact
	UsedFake bool
	ACPAgent string `json:"acp_agent,omitempty"`
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

	if req.ACP {
		return s.executeACP(ctx, req, run)
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

func (s *Supervisor) executeACP(ctx context.Context, req Request, run *notify.Run) (*Outcome, error) {
	if _, err := s.Server.UpdateRun(run.ID, string(StatusRunning), "acp starting"); err != nil {
		return nil, err
	}
	title, body := req.Title, req.Body
	if title == "" {
		if t, err := s.Server.GetTask(req.TaskID); err == nil {
			title = t.Title
			if body == "" {
				body = t.Body
			}
		}
	}
	prompt := acp.Prompt(title, body, req.Notes)
	agent := req.Agent
	if req.Fake && agent == "" {
		agent = "fake"
	}
	name, logs, err := acp.Stream(ctx, acp.Config{Agent: agent}, prompt, func(ev acp.Event) error {
		_, perr := s.Server.PostRunEvent(run.ID, ev.Kind, ev.Payload)
		return perr
	})
	status := StatusSucceeded
	detail := "acp " + name + " ok"
	if err != nil {
		status = StatusFailed
		detail = err.Error()
		if logs == "" {
			logs = detail
		}
	}
	run, uerr := s.Server.UpdateRun(run.ID, string(status), detail)
	if uerr != nil {
		return nil, uerr
	}
	art, aerr := s.Server.CreateArtifact(req.TaskID, notify.Artifact{
		RunID: run.ID, Kind: "log", Name: "acp.log", Body: logs,
	})
	if aerr != nil {
		return nil, aerr
	}
	out := &Outcome{Run: run, Logs: logs, Artifact: art, UsedFake: name == "fake", ACPAgent: name}
	if req.FakePR || status == StatusSucceeded {
		pr, err := s.Server.OpenPR(req.TaskID, map[string]any{
			"fake":   true,
			"run_id": run.ID,
			"title":  "BuildBee ACP Run Artifact",
			"body":   "Recorded by the ACP supervisor (" + name + ").",
		})
		if err == nil {
			out.PR = pr
		}
	}
	if err != nil {
		return out, err
	}
	return out, nil
}

// StartRun is kept for older callers; it only returns pending.
func (s *Supervisor) StartRun(runID string) (Status, error) {
	_ = runID
	return StatusPending, nil
}
