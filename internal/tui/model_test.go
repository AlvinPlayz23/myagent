package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestTranscriptScrollsWithMouseWheel(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 12)
	m.viewport.SetContent(strings.Repeat("line\n", m.viewport.Height()*2))
	m.viewport.GotoBottom()

	initialOffset := m.viewport.YOffset()
	m.onMouseWheel(tea.MouseWheelMsg{Y: 0, Button: tea.MouseWheelUp})
	if m.viewport.YOffset() >= initialOffset {
		t.Fatalf("wheel up offset = %d, want less than %d", m.viewport.YOffset(), initialOffset)
	}

	scrolledOffset := m.viewport.YOffset()
	m.onMouseWheel(tea.MouseWheelMsg{Y: m.viewport.Height(), Button: tea.MouseWheelDown})
	if m.viewport.YOffset() != scrolledOffset {
		t.Fatalf("wheel outside transcript offset = %d, want %d", m.viewport.YOffset(), scrolledOffset)
	}
}

func TestViewFitsTerminalHeight(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 12)

	view := m.View()
	if !view.AltScreen {
		t.Fatal("view should use the alternate screen")
	}
	if got := strings.Count(view.Content, "\n") + 1; got > m.height {
		t.Fatalf("view height = %d, terminal height = %d", got, m.height)
	}
}

func TestTypingSlashOpensCommandPicker(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	m.onKey(tea.KeyPressMsg(tea.Key{Text: "/", Code: '/'}))

	if got := m.input.Value(); got != "/" {
		t.Fatalf("input = %q, want slash", got)
	}
	if !m.picker.active || len(m.picker.matched) != len(commandItems) {
		t.Fatalf("picker = active %v, matches %d; want all commands", m.picker.active, len(m.picker.matched))
	}
}

func TestCommandPickerFitsTerminalAndBorrowsViewportRows(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 12)
	baseHeight := m.viewport.Height()
	m.input.SetValue("/")
	m.picker.sync(m.input.Value())
	m.updateLayout()

	if !m.picker.active {
		t.Fatal("picker did not open")
	}
	if m.viewport.Height() >= baseHeight {
		t.Fatalf("viewport height = %d, want less than %d", m.viewport.Height(), baseHeight)
	}
	view := m.View()
	if got := strings.Count(view.Content, "\n") + 1; got > m.height {
		t.Fatalf("view height with picker = %d, terminal height = %d", got, m.height)
	}
	if !strings.Contains(view.Content, "/help") || !strings.Contains(view.Content, "/model-id <id>") {
		t.Fatal("picker view does not contain command choices")
	}

	m.picker.dismiss(m.input.Value())
	m.updateLayout()
	if m.viewport.Height() != baseHeight {
		t.Fatalf("viewport height after dismiss = %d, want %d", m.viewport.Height(), baseHeight)
	}
}
