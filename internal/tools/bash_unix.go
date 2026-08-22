//go:build unix

package tools

import (
	"fmt"
	"os/exec"
	"syscall"
)

// processGroup contains the shell's whole descendant tree via a POSIX process
// group: killing the negative PID signals every process in the group,
// including orphans that outlived the shell.
type processGroup struct {
	kill    func()
	release func()
}

// prepareProcessGroup puts the child in its own process group so the whole
// tree can be signalled at once.
func prepareProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// attachProcessGroup captures the child's group id for later tree kills.
func attachProcessGroup(cmd *exec.Cmd) (*processGroup, error) {
	if cmd.Process == nil {
		return nil, fmt.Errorf("process not started")
	}
	pgid := -cmd.Process.Pid
	return &processGroup{
		kill: func() {
			_ = syscall.Kill(pgid, syscall.SIGKILL)
			_ = cmd.Process.Kill()
		},
		release: func() {},
	}, nil
}

// exitMessage renders a human-readable termination status, distinguishing
// signal deaths (ExitCode is -1 there) from normal exit codes.
func exitMessage(err *exec.ExitError) string {
	if ws, ok := err.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return fmt.Sprintf("Command killed by signal %d", ws.Signal())
	}
	return fmt.Sprintf("Command exited with code %d", err.ExitCode())
}
