//go:build unix

package ptytest

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Timeouts are generous but bounded: startup includes Go build cache warmup,
// the child's terminal capability queries, and glamour's first render, none
// of which the tests control. Mid-stream determinism comes from server gates,
// not from sleeps.
const (
	startupWait = 45 * time.Second
	stepWait    = 30 * time.Second
)

func mustServer(t *testing.T) *Server {
	t.Helper()
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("start fake server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// name: startup-exit
func TestStartupAndCleanExit(t *testing.T) {
	srv := mustServer(t)
	app := Launch(t, srv)

	// The welcome screen renders: title, subtitle, and the /help hint.
	app.RequireContains("Your terminal coding agent", startupWait)
	app.RequireContains("/help for commands", stepWait)

	// Ctrl+C on an idle app quits; the TUI restores the terminal and main
	// prints the resume instructions before exiting 0.
	app.Send("\x03")
	app.RequireContains("Resume this session", stepWait)
	if err := app.WaitExit(stepWait); err != nil {
		t.Fatalf("clean exit after ctrl+c: %v", err)
	}
	app.RequireNoPanic()
}

// name: prompt-response
func TestPromptReceivesMockedResponse(t *testing.T) {
	srv := mustServer(t)
	srv.EnqueueScript(Script{
		Thinking: []string{"Considering the question. "},
		Text: []string{
			"Mocked ", "assistant ", "reply ", "from ", "the ", "fake ", "server.",
		},
	})
	app := Launch(t, srv)
	app.RequireContains("Your terminal coding agent", startupWait)

	app.Send("What is a PTY?\r")
	// The working status appears while the turn streams, then the scripted
	// assistant text lands in the transcript.
	app.RequireContains("esc to cancel", stepWait)
	app.RequireContains("Mocked assistant reply from the fake server.", stepWait)
	app.QuitClean()
}

// name: esc-abort
func TestAbortWithEscWhileStreaming(t *testing.T) {
	srv := mustServer(t)
	// Gate after the first delta: the turn is provably mid-stream when the
	// test sends Esc, and provably unfinished until Release.
	srv.EnqueueScript(Script{
		Text: []string{
			"First visible delta.",
			" never", " reaches", " the", " screen.",
		},
		GateAfter: 1,
	})
	app := Launch(t, srv)
	app.RequireContains("Your terminal coding agent", startupWait)

	app.Send("Answer slowly please.\r")
	app.RequireContains("First visible delta.", stepWait)
	app.Send("\x1b") // Esc aborts the running turn

	// The turn ends: the working status clears and the composer returns,
	// and the gated tail of the scripted reply never renders.
	app.RequireGone("esc to cancel", stepWait)
	app.RequireContains("Send a message", stepWait)
	app.RequireAlive()
	if strings.Contains(app.RawOutput(), "reaches the screen") {
		t.Fatal("aborted tail of the scripted stream rendered on screen")
	}
	// The abort is durable: the session file records the assistant message
	// with the aborted stop reason. (The on-screen "Operation aborted"
	// notice cannot be asserted because the app's event sink races the
	// cancel and drops that event about half the time; see README.)
	if sess := app.SessionText(); !strings.Contains(sess, `"stopReason":"aborted"`) {
		t.Fatalf("session does not record an aborted turn:\n%s", sess)
	}

	srv.Release()
	app.QuitClean()
}

// name: queue-followup
func TestQueueFollowUpWhileStreaming(t *testing.T) {
	srv := mustServer(t)
	srv.EnqueueScript(Script{
		Text:      []string{"First reply half.", " Second half follows."},
		GateAfter: 1,
	})
	srv.EnqueueScript(Script{
		Text: []string{"Second queued ", "reply ", "arrived."},
	})
	app := Launch(t, srv)
	app.RequireContains("Your terminal coding agent", startupWait)

	app.Send("First prompt please.\r")
	app.RequireContains("First reply half.", stepWait)

	// A second prompt while the turn runs is queued, not submitted.
	app.Send("Second prompt queued.\r")
	app.RequireContains("↳ next", stepWait)
	app.RequireContains("Second prompt queued.", stepWait)

	// Releasing the gate finishes turn one, and the loop drains the queue:
	// the follow-up gets its own request and its own reply.
	srv.Release()
	app.RequireContains("First reply half. Second half follows.", stepWait)
	app.RequireContains("Second queued reply arrived.", stepWait)
	app.QuitClean()
}

// name: resize-stream
func TestResizeWhileStreaming(t *testing.T) {
	srv := mustServer(t)
	srv.EnqueueScript(Script{
		Text:      []string{"Resize live reply start.", " The stream continues."},
		GateAfter: 1,
	})
	app := Launch(t, srv)
	app.RequireContains("Your terminal coding agent", startupWait)

	app.Send("Reply while I resize.\r")
	app.RequireContains("Resize live reply start.", stepWait)

	// Resizing mid-stream must not crash the app, and the screen keeps
	// rendering: the streamed text and the footer survive the repaint.
	app.Resize(120, 36)
	app.RequireAlive()
	app.RequireContains("Resize live reply start.", stepWait)
	app.RequireContains(ModelRef, stepWait)

	srv.Release()
	app.RequireContains("Resize live reply start. The stream continues.", stepWait)
	app.QuitClean()
}

// name: help-overlay
func TestHelpOverlayOpenAndDismiss(t *testing.T) {
	srv := mustServer(t)
	app := Launch(t, srv)
	app.RequireContains("Your terminal coding agent", startupWait)

	// Typing "/" opens the command-picker overlay with the /help suggestion.
	app.Send("/")
	app.RequireContains("› /help", stepWait)
	// Esc dismisses the overlay; the composer text is untouched, so the
	// welcome hint (not the empty-input placeholder) proves the app is
	// still rendering.
	app.Send("\x1b")
	app.RequireGone("› /help", stepWait)
	app.RequireContains("/help for commands", stepWait)
	app.RequireAlive()

	// Typing the rest of the command and pressing enter runs /help, which
	// appends the command reference to the transcript.
	app.Send("help\r")
	app.RequireContains("Commands:", stepWait)
	// The Keys line is longer than the terminal is wide, so only its prefix
	// is on screen; assert the part that fits.
	app.RequireContains("Keys: enter send/queue follow-up", stepWait)
	app.QuitClean()
}

// name: pageup-scroll
func TestPageUpScrollKeepsAppResponsive(t *testing.T) {
	srv := mustServer(t)
	// Thirty paragraphs overflow the ~20-row viewport, so the transcript is
	// scrollable once the turn completes. Deltas carry their own blank-line
	// separators so glamour renders them as distinct paragraphs.
	var paras []string
	for i := 0; i < 30; i++ {
		paras = append(paras, fmt.Sprintf("PARA %02d The quick brown fox jumps over the lazy dog number %d.\n\n", i, i))
	}
	srv.EnqueueScript(Script{Text: paras})
	app := Launch(t, srv)
	app.RequireContains("Your terminal coding agent", startupWait)

	app.Send("Write thirty paragraphs.\r")
	// The tail is on screen when the viewport sits at the bottom.
	app.RequireContains("PARA 29", startupWait)
	app.WaitIdle(stepWait)

	// PageUp scrolls the transcript back to the top without wedging the app.
	for range 6 {
		app.Send("\x1b[5~")
	}
	app.RequireContains("PARA 00", stepWait)
	app.RequireContains("Send a message", stepWait)
	app.RequireContains(ModelRef, stepWait)
	app.QuitClean()
}

// name: screen-projection
func TestScreenProjectionStripsANSI(t *testing.T) {
	// A focused unit check of the projector: styled text written with cursor
	// addressing, colors, and erases must surface as plain visible rows.
	s := NewScreen(20, 5)
	s.Feed([]byte("\x1b[2J\x1b[H\x1b[1;31mHello\x1b[0m \x1b[1;32mworld\x1b[0m\x1b[3;1Hsecond line\r\nthird"))
	if text := s.Text(); !strings.Contains(text, "Hello world") ||
		!strings.Contains(text, "second line") ||
		!strings.Contains(text, "third") {
		t.Fatalf("unexpected projection:\n%s", text)
	}
}
