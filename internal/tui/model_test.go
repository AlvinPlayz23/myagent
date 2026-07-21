package tui

import (
	"fmt"
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
	if !strings.Contains(view.Content, "/help") || !strings.Contains(view.Content, "/model [provider/model-id]") {
		t.Fatal("picker view does not contain command choices")
	}

	m.picker.dismiss(m.input.Value())
	m.updateLayout()
	if m.viewport.Height() != baseHeight {
		t.Fatalf("viewport height after dismiss = %d, want %d", m.viewport.Height(), baseHeight)
	}
}

func TestStartupStatusClearsAfterFiveSeconds(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.statusMsg = "Loaded AGENTS.md"
	m.Update(clearStatusMsg{status: "Loaded AGENTS.md"})
	if m.statusMsg != "" {
		t.Fatalf("status = %q, want empty", m.statusMsg)
	}
}

func TestStartupStatusDoesNotClearNewerStatus(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.statusMsg = "Model set to test/model."
	m.Update(clearStatusMsg{status: "Loaded AGENTS.md"})
	if m.statusMsg != "Model set to test/model." {
		t.Fatalf("status = %q, want newer status", m.statusMsg)
	}
}

func TestPromptHistoryNavigatesFromNewestToOldest(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.addPromptHistory("first prompt")
	m.addPromptHistory("second prompt")

	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	if got := m.input.Value(); got != "second prompt" {
		t.Fatalf("first up = %q, want newest prompt", got)
	}
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	if got := m.input.Value(); got != "first prompt" {
		t.Fatalf("second up = %q, want oldest prompt", got)
	}
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	if got := m.input.Value(); got != "first prompt" {
		t.Fatalf("up at oldest = %q, want oldest prompt", got)
	}
}

func TestPromptHistoryDownReturnsToEmptyComposer(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.addPromptHistory("first prompt")
	m.addPromptHistory("second prompt")
	m.navigatePromptHistory(-1)
	m.navigatePromptHistory(-1)

	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if got := m.input.Value(); got != "second prompt" {
		t.Fatalf("first down = %q, want newer prompt", got)
	}
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if got := m.input.Value(); got != "" {
		t.Fatalf("down after newest = %q, want empty composer", got)
	}
	if m.historyIndex != -1 {
		t.Fatalf("history index = %d, want -1", m.historyIndex)
	}
}

func TestPromptHistoryExcludesSlashCommandsAndConsecutiveDuplicates(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.addPromptHistory("prompt")
	m.addPromptHistory("prompt")
	if len(m.promptHistory) != 1 {
		t.Fatalf("history length = %d, want 1", len(m.promptHistory))
	}

	m.input.SetValue("/help")
	m.submit(false)
	if len(m.promptHistory) != 1 {
		t.Fatalf("slash command was added to history: %#v", m.promptHistory)
	}
}

func TestEditingRecalledPromptExitsHistoryNavigation(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.addPromptHistory("prompt")
	m.navigatePromptHistory(-1)
	m.onKey(tea.KeyPressMsg(tea.Key{Text: "x", Code: 'x'}))
	if m.historyIndex != -1 {
		t.Fatalf("history index = %d, want -1 after editing", m.historyIndex)
	}
}

func TestPromptHistoryKeepsMostRecentHundredPrompts(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	for i := 0; i <= promptHistoryLimit; i++ {
		m.addPromptHistory(fmt.Sprintf("prompt %d", i))
	}
	if len(m.promptHistory) != promptHistoryLimit {
		t.Fatalf("history length = %d, want %d", len(m.promptHistory), promptHistoryLimit)
	}
	if m.promptHistory[0] != "prompt 100" || m.promptHistory[len(m.promptHistory)-1] != "prompt 1" {
		t.Fatalf("unexpected retained history: newest=%q oldest=%q", m.promptHistory[0], m.promptHistory[len(m.promptHistory)-1])
	}
}
