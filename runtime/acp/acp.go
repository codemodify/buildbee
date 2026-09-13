// Package acp is a stub for the agent communication protocol used during a Run.
//
// ACP is how the supervisor talks to the Bot process inside a Sandbox
// (start, stream progress, collect Artifacts, finish). This scaffold does
// not implement the protocol.
package acp

// Session is a placeholder connection to an agent inside a Sandbox.
type Session struct {
	RunID string
}

// Dial is a stub. A later revision opens the ACP stream for the Run.
func Dial(runID string) (*Session, error) {
	return &Session{RunID: runID}, nil
}
