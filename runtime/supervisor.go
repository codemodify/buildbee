// Package runtime is the Go supervisor for Bot Runs.
//
// A Run starts a Docker Sandbox, speaks ACP to the agent inside it, and
// reports Activity back to the Server. This package is a stub: no containers
// are started and ACP is not implemented yet.
package runtime

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

// Supervisor coordinates Sandbox containers for ACP agent Runs.
type Supervisor struct{}

func NewSupervisor() *Supervisor {
	return &Supervisor{}
}

// StartRun is a stub. A later revision will create a Sandbox and begin ACP.
func (s *Supervisor) StartRun(runID string) (Status, error) {
	_ = runID
	return StatusPending, nil
}
