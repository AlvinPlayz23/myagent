//go:build windows

package tools

import (
	"fmt"
	"os/exec"
	"syscall"
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

// prepareProcessGroup hides the console window Windows would otherwise
// allocate for the shell. Windows creates a console for a console-subsystem
// child when its parent has none, so a shell spawned by a console-less parent
// (the desktop app's `myagent serve`) gets a *new visible* conhost window for
// the lifetime of every command. Stdio is piped and stdin is NUL, so the
// window can never show anything useful. HideWindow adds STARTF_USESHOWWINDOW
// + SW_HIDE and CREATE_NO_WINDOW suppresses console creation outright; setting
// both is belt-and-braces. Tree containment is unaffected: it is attached as a
// Job Object after Start.
func prepareProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}

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
