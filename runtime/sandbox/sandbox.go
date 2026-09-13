// Package sandbox describes the Docker isolation boundary for a Bot Run.
package sandbox

// Spec is what the supervisor would pass to Docker to start a Sandbox.
type Spec struct {
	RunID   string
	Image   string
	WorkDir string
}

// Sandbox is a stub handle for one isolated container.
type Sandbox struct {
	Spec Spec
}

// Create does not start a container. It returns a handle for later wiring.
func Create(spec Spec) (*Sandbox, error) {
	return &Sandbox{Spec: spec}, nil
}
