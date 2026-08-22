//go:build windows

package tools

import (
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processGroup contains the shell's whole descendant tree via a Windows Job
// Object: terminating the job kills every process ever assigned to it,
// including orphans that outlived the shell.
type processGroup struct {
	kill    func()
	release func()
}

// prepareProcessGroup is a no-op on Windows; containment is set up after
// Start via the Job Object.
func prepareProcessGroup(cmd *exec.Cmd) {}

// attachProcessGroup assigns the started process to a fresh Job Object. The
// job deliberately does NOT use JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE: background
// processes the command intentionally left running must survive normal
// completion, so the tree is terminated explicitly (kill) and the handle is
// merely detached afterwards (release).
func attachProcessGroup(cmd *exec.Cmd) (*processGroup, error) {
	if cmd.Process == nil {
		return nil, fmt.Errorf("process not started")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	if _, err := windows.SetInformationJobObject(job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	var assignErr error
	if err := cmd.Process.WithHandle(func(h uintptr) {
		assignErr = windows.AssignProcessToJobObject(job, windows.Handle(h))
	}); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	if assignErr != nil {
		_ = windows.CloseHandle(job)
		return nil, assignErr
	}
	return &processGroup{
		kill: func() {
			_ = windows.TerminateJobObject(job, 1)
		},
		release: func() {
			_ = windows.CloseHandle(job)
		},
	}, nil
}

// exitMessage renders a human-readable termination status.
func exitMessage(err *exec.ExitError) string {
	return fmt.Sprintf("Command exited with code %d", err.ExitCode())
}
