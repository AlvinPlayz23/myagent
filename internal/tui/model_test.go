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
