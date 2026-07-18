//go:build windows

package tui

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// enableVTIfRequested forces Windows VT output processing when VT_MYAGENT=true.
// This bypasses Bubble Tea's terminal detection for environments where stdout
// is not detected as a terminal but the console host still supports ANSI.
func enableVTIfRequested() error {
	v := os.Getenv("VT_MYAGENT")
	if strings.ToLower(v) != "true" {
		return nil
	}

	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return fmt.Errorf("VT_MYAGENT: GetConsoleMode: %w", err)
	}

	const vtOutput = windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
	if mode&vtOutput != 0 {
		fmt.Fprintln(os.Stderr, "myagent: VT output already enabled, VT_MYAGENT has no effect")
		return nil
	}

	if err := windows.SetConsoleMode(h, mode|vtOutput); err != nil {
		return fmt.Errorf("VT_MYAGENT: SetConsoleMode: %w", err)
	}

	fmt.Fprintln(os.Stderr, "myagent: enabled Windows VT output via VT_MYAGENT")
	return nil
}
