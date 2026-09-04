package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestHelpOverlayRoutesThroughOverlayStack(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 24)
	m.input.SetValue("/help")
	view, cmd := m.submit(submitFollowUp)
	if !m.helpActive {
		t.Fatal("/help did not open the help overlay")
	}
	if view == nil || cmd != nil {
		t.Fatalf("submit returned (%v, %v), want a model and no command", view, cmd)
	}
	if got := m.renderPanel(); !strings.Contains(got, "Help") || !strings.Contains(got, "/model") {
		t.Fatalf("help overlay render = %q", got)
	}

	// Keys are consumed by the overlay: typing must not reach the composer.
	before := m.input.Value()
	m.onKey(tea.KeyPressMsg(tea.Key{Code: 'x'}))
	if m.input.Value() != before {
		t.Fatalf("overlay leaked key to composer: %q", m.input.Value())
	}

	// Esc dismisses and returns the rows to the transcript.
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.helpActive {
		t.Fatal("esc did not dismiss the help overlay")
	}
	if m.renderPanel() != "" {
		t.Fatalf("panel remained after dismiss: %q", m.renderPanel())
	}

	// Ctrl+C still quits from under the overlay rather than being swallowed.
	m.helpActive = true
	next, cmd := m.onKey(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	_ = next
	if cmd == nil {
		t.Fatal("ctrl+c was swallowed by the help overlay instead of quitting")
	}
}

func TestResizeNarrowWideNarrowPreservesContentAndSelectionMath(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.hasSessionTitle = true
	long := strings.Repeat("word ", 40)
	m.transcript.addUser(long)
	m.transcript.beginAssistant()
	m.transcript.appendAssistantDelta("short answer")
	m.transcript.endAssistant()

	for _, w := range []int{80, 20, 80, 12, 80} {
		m.onResize(w, 24)
		rows := m.transcript.layout(w)
		for i, r := range rows {
			if got := r.width(); got > w {
				t.Fatalf("width %d: row %d is %d cells wide", w, i, got)
			}
		}
		if len(rows) == 0 {
			t.Fatalf("width %d: layout lost all rows", w)
		}
	}

	// After the resize round-trip, a selection still copies real text.
	m.onResize(80, 24)
	m.viewport.GotoBottom()
	m.refreshViewport()
	last := len(m.rows) - 1
	if last < 0 {
		t.Fatal("no rows after resize")
	}
	y := min(m.viewport.Height()-1, last)
	m.onMouseClick(tea.MouseClickMsg{X: 0, Y: y, Button: tea.MouseLeft})
	m.onMouseMotion(tea.MouseMotionMsg{X: 5, Y: y, Button: tea.MouseLeft})
	var copied string
	m.clipboardWrite = func(text string) error { copied = text; return nil }
	m.onMouseRelease(tea.MouseReleaseMsg{X: 5, Y: y, Button: tea.MouseLeft})
	if copied == "" {
		t.Fatal("selection after resize copied nothing")
	}
	if strings.Contains(copied, "\x1b") {
		t.Fatalf("copied text contains ANSI: %q", copied)
	}
}

func TestComposerStatesRouting(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(60, 20)

	// Typing "/" opens the slash menu and typing stays in the composer.
	m.input.SetValue("/")
	m.syncPickers()
	if !m.picker.active {
		t.Fatal("slash menu did not open for /")
	}
	m.onKey(tea.KeyPressMsg(tea.Key{Code: 'm', Text: "m"}))
	if !strings.HasPrefix(m.input.Value(), "/m") {
		t.Fatalf("composer lost typed text: %q", m.input.Value())
	}

	// Esc dismisses the menu without aborting anything (no run is active).
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.picker.active {
		t.Fatal("esc did not dismiss the slash menu")
	}
	if m.statusMsg != "" {
		t.Fatalf("esc set a status with no run active: %q", m.statusMsg)
	}

	// Tab completes the top match into the composer (/help takes no argument,
	// so no trailing space is added).
	m.input.SetValue("/he")
	m.syncPickers()
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if got := m.input.Value(); got != "/help" {
		t.Fatalf("tab completion = %q, want /help", got)
	}
}
