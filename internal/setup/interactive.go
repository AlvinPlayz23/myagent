package setup

import (
	"os"

	"github.com/charmbracelet/x/term"
)

// isInteractive reports whether stdin AND stdout are attached to a terminal
// (a controlling tty). The wizard needs stdin to read keystrokes and stdout
// to render; piping either side fails this check and RunWizard returns
// ErrNoTty so non-interactive callers get a clear message instead of a
// broken full-screen UI.
func isInteractive() bool {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return false
	}
	if !term.IsTerminal(os.Stdout.Fd()) {
		return false
	}
	return true
}
