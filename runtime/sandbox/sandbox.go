package sandbox

// Create returns a handle describing a future Docker Sandbox.
// Execution goes through Engine.Run (DockerEngine or FakeEngine).
func Create(spec Spec) (*Sandbox, error) {
	return &Sandbox{Spec: spec}, nil
}

// Sandbox is a handle for one isolated container.
type Sandbox struct {
	Spec Spec
}
