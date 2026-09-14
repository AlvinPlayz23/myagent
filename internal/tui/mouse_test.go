package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
)

// openSlashPicker types "/" and records the panel layout via View.
func openSlashPicker(t *testing.T, m *model) {
	t.Helper()
	m.input.SetValue("/")
	m.syncPickers()
	m.updateLayout()
	m.View()
	if !m.picker.active {
		t.Fatal("slash picker did not open")
	}
}

func TestInlineClickPreviewsThenConfirms(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	openSlashPicker(t, m)

	// Find a row for an arg-taking command so confirm only fills input.
	target := -1
	for i, idx := range m.picker.matched {
		if m.picker.items[idx].name == "/models" {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("no /models row in picker")
	}
	y := m.panelStartY
	// Walk inline rows to the target's view row.
	row := -1
	for line, item := range m.inlineRowItems {
		if item == target {
			row = y + line
			break
		}
	}
	if row < 0 {
		t.Fatal("no recorded row for /models")
	}
	m.onMouseClick(tea.MouseClickMsg{X: 2, Y: row, Button: tea.MouseLeft})
	if m.picker.sel != target {
		t.Fatalf("preview sel = %d, want %d", m.picker.sel, target)
	}
	m.onMouseClick(tea.MouseClickMsg{X: 2, Y: row, Button: tea.MouseLeft})
	if got := m.input.Value(); got != "/models " {
		t.Fatalf("confirmed input = %q, want /models", got)
	}
}

func TestWheelOverPanelMovesPicker(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	openSlashPicker(t, m)

	m.onMouseWheel(tea.MouseWheelMsg{Y: m.panelStartY, Button: tea.MouseWheelDown})
	if m.picker.sel != 1 {
		t.Fatalf("wheel-down sel = %d, want 1", m.picker.sel)
	}
	m.onMouseWheel(tea.MouseWheelMsg{Y: m.panelStartY, Button: tea.MouseWheelUp})
	if m.picker.sel != 0 {
		t.Fatalf("wheel-up sel = %d, want 0", m.picker.sel)
	}
}

func TestModelPickerClickPreviewsThenConfirms(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 24)
	m.selectModel = func(provider, id string) (llm.Provider, llm.Model, error) {
		return nil, llm.Model{}, fmt.Errorf("nope")
	}
	items := []modelcatalog.Model{
		{Provider: "p", ProviderName: "P", ID: "a"},
		{Provider: "p", ProviderName: "P", ID: "b"},
	}
	m.models.open(items, "")
	m.updateLayout()
	m.View()
	if len(m.inlineRowItems) == 0 {
		t.Fatal("inline rows not recorded")
	}
	// Second recorded row carrying item 1.
	line := -1
	for i, item := range m.inlineRowItems {
		if item == 1 {
			line = i
			break
		}
	}
	if line < 0 {
		t.Fatal("no recorded row for item 1")
	}
	y := m.panelStartY + line
	m.onMouseClick(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
	if m.models.sel != 1 {
		t.Fatalf("preview sel = %d, want 1", m.models.sel)
	}
	m.onMouseClick(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
	if !strings.Contains(m.statusMsg, "Could not select model") {
		t.Fatalf("confirm status = %q, want select error", m.statusMsg)
	}
}

func TestWelcomeClickRunsCommand(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 30)
	m.View() // records the welcome menu range
	if !m.showWelcome() {
		t.Fatal("welcome not showing")
	}
	y := m.welcomeMenu[0] + 1 // "/help" row
	m.onMouseClick(tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	if got := m.transcript.render(80); !strings.Contains(got, "Commands:") {
		t.Fatalf("help click missing notice:\n%s", got)
	}
}

func TestWelcomeQuitClickArmsPending(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 30)
	m.View()
	y := m.welcomeMenu[0] + len(welcomeMenuActions) - 1 // Quit row
	m.onMouseClick(tea.MouseClickMsg{X: 4, Y: y, Button: tea.MouseLeft})
	if m.pendingKey != "ctrl+c" {
		t.Fatalf("pending = %q, want ctrl+c", m.pendingKey)
	}
}

func TestHoverTracksWelcome(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 30)
	m.View()
	m.updateHover(4, m.welcomeMenu[0])
	if m.hoverKind != hoverWelcome || m.hoverIdx != 0 {
		t.Fatalf("hover = %d/%d, want welcome/0", m.hoverKind, m.hoverIdx)
	}
	if !strings.Contains(m.renderWelcome(), "↵") {
		t.Fatal("welcome lost menu after hover")
	}
}

func TestDoubleClickSelectsWordAndKeeps(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.hasSessionTitle = true
	m.transcript.addUser("hello world")
	m.onResize(40, 20)
	var copied string
	m.clipboardWrite = func(text string) error {
		copied = text
		return nil
	}
	for range 2 {
		m.onMouseClick(tea.MouseClickMsg{X: 2, Y: 0, Button: tea.MouseLeft})
	}
	m.onMouseRelease(tea.MouseReleaseMsg{X: 2, Y: 0, Button: tea.MouseLeft})
	if copied != "hello" {
		t.Fatalf("clipboard = %q, want hello", copied)
	}
	if m.selection == nil || !m.keepSelection {
		t.Fatal("word selection was not kept")
	}
	// Next press clears the sticky highlight.
	m.onMouseClick(tea.MouseClickMsg{X: 2, Y: 0, Button: tea.MouseLeft})
	if m.keepSelection {
		t.Fatal("press did not clear sticky selection")
	}
}

func TestQuitNeedsSecondPress(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	_, cmd := m.onKey(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if cmd == nil || m.pendingKey != "ctrl+c" {
		t.Fatal("first ctrl+c did not arm quit confirm")
	}
	if m.statusMsg != "Press ctrl+c again to quit." {
		t.Fatalf("status = %q", m.statusMsg)
	}
	mod, cmd := m.onKey(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	_ = mod
	if cmd == nil || reflect.ValueOf(cmd).Pointer() != reflect.ValueOf(tea.Quit).Pointer() {
		t.Fatal("second ctrl+c did not quit")
	}
}

func TestPendingExpiresAndEscCancels(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	m.onKey(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	if m.pendingKey == "" {
		t.Fatal("pending not armed")
	}
	m.Update(pendingTimeoutMsg{key: "ctrl+c"})
	if m.pendingKey != "" || m.statusMsg != "" {
		t.Fatalf("pending survived timeout: %q/%q", m.pendingKey, m.statusMsg)
	}
	m.onKey(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.pendingKey != "" {
		t.Fatal("esc did not cancel pending")
	}
}

func TestMouseToggleCommand(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	if _, _, handled := m.dispatchPluginCommand(mustParse(t, "/mouse off")); handled {
		t.Fatal("/mouse swallowed by plugin dispatch")
	}
	m.runCommand("/mouse off")
	if m.mouseCapture {
		t.Fatal("mouse still captured after /mouse off")
	}
	if mode := m.View().MouseMode; mode != tea.MouseModeNone {
		t.Fatalf("view mouse mode = %v, want None", mode)
	}
	m.runCommand("/mouse on")
	if !m.mouseCapture || m.View().MouseMode != tea.MouseModeAllMotion {
		t.Fatal("/mouse on did not restore AllMotion")
	}
}

func mustParse(t *testing.T, text string) slashCommand {
	t.Helper()
	cmd, err := parseSlashCommand(text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	return cmd
}

func TestToastQueueDrains(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	m.statusMsg = "busy"
	m.pushToast("next")
	m.Update(clearStatusMsg{status: "busy"})
	if m.statusMsg != "next" {
		t.Fatalf("status = %q, want next toast", m.statusMsg)
	}
}

func TestInlineModelPickerFitsTerminal(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	items := []modelcatalog.Model{
		{Provider: "p", ProviderName: "P", ID: "a"},
		{Provider: "p", ProviderName: "P", ID: "b"},
		{Provider: "p", ProviderName: "P", ID: "c"},
		{Provider: "p", ProviderName: "P", ID: "d"},
		{Provider: "p", ProviderName: "P", ID: "e"},
		{Provider: "p", ProviderName: "P", ID: "f"},
	}
	m.models.open(items, "")
	m.updateLayout()
	view := m.View()
	if got := strings.Count(view.Content, "\n") + 1; got > m.height {
		t.Fatalf("view height with picker = %d, terminal height = %d", got, m.height)
	}
}

func TestInlineRowsFitWidth(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 24)
	items := []modelcatalog.Model{
		{Provider: "anthropic", ProviderName: "Anthropic", ID: "claude-opus-4-6-with-a-very-long-model-name", Name: "Claude Opus 4.6 Extended"},
		{Provider: "openai", ProviderName: "OpenAI", ID: "gpt-5-pro-max-ultra-long-model-identifier", Name: "GPT-5 Pro Max Ultra Long"},
	}
	m.models.open(items, "")
	m.updateLayout()
	m.View()
	// Hover the second row, then verify every panel line fits the terminal
	// with no chopped escape fragments leaking across rows.
	line := -1
	for i, item := range m.inlineRowItems {
		if item == 1 {
			line = i
			break
		}
	}
	if line < 0 {
		t.Fatal("no recorded row for item 1")
	}
	m.updateHover(5, m.panelStartY+line)
	panel := m.renderPanel()
	for _, pline := range strings.Split(panel, "\n") {
		if w := lipgloss.Width(pline); w > m.width {
			t.Fatalf("panel line %d cells wide, terminal is %d: %q", w, m.width, pline)
		}
	}
	stripped := ansi.Strip(panel)
	if strings.Contains(stripped, "\x1b") || strings.Contains(panel, "m\x1b[") {
		t.Fatalf("panel leaks escape fragments across rows:\n%s", panel)
	}
	if !strings.Contains(stripped, "claude-opus") || !strings.Contains(stripped, "gpt-5") {
		t.Fatalf("panel rows truncated too aggressively:\n%s", stripped)
	}
}

func TestStatusLineShowsHoverHint(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 20)
	if got := m.statusLine(); got != "" {
		t.Fatalf("idle status = %q, want empty", got)
	}
	m.hoverHint = "ctrl+click to open https://x.test"
	if got := m.statusLine(); !strings.Contains(got, "https://x.test") {
		t.Fatalf("hover status = %q", got)
	}
	m.statusMsg = "busy"
	if got := m.statusLine(); !strings.Contains(got, "busy") {
		t.Fatalf("status with message = %q, want busy", got)
	}
}
