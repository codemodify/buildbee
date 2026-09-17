//go:build unix

package acp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup starts the agent in its own process group so that it and
// every tool it spawned can be killed together.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
