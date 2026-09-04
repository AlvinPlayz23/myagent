package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// actionKind discriminates normalized UI actions.
type actionKind int

const (
	actionNone actionKind = iota
	actionKey
	actionMouseClick
	actionMouseDrag
	actionMouseHover
	actionMouseRelease
	actionWheel
	actionResize
	actionPaste
)

// uiAction is a semantic input or lifecycle action. Raw Bubble Tea key,
// mouse, paste, and resize messages are normalized into actions once, at the
// Update boundary; overlays, the composer, and global handlers then consume
// stable semantics instead of platform-specific message shapes.
//
// Agent and provider work (agentEventMsg, agentDoneMsg, agentTitleMsg,
// clipboardResultMsg, modelsDiscoveredMsg, ticks) is a separate stream that
// never passes through this layer, so UI routing cannot shadow agent state.
type uiAction struct {
	kind actionKind

	key           tea.KeyPressMsg   // actionKey
	wheel         tea.MouseWheelMsg // actionWheel, kept for viewport delegation
	x, y          int               // mouse actions
	button        tea.MouseButton
	width, height int    // actionResize
	text          string // actionPaste
}

// normalizeMessage maps one raw Bubble Tea message onto a semantic action.
// It returns actionNone for everything else (bubbles-internal messages).
func normalizeMessage(msg tea.Msg) uiAction {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return uiAction{kind: actionKey, key: msg}
	case tea.WindowSizeMsg:
		return uiAction{kind: actionResize, width: msg.Width, height: msg.Height}
	case tea.PasteMsg:
		return uiAction{kind: actionPaste, text: msg.Content}
	case tea.MouseClickMsg:
		return uiAction{kind: actionMouseClick, x: msg.X, y: msg.Y, button: msg.Button}
	case tea.MouseMotionMsg:
		if msg.Button == tea.MouseLeft {
			return uiAction{kind: actionMouseDrag, x: msg.X, y: msg.Y, button: msg.Button}
		}
		return uiAction{kind: actionMouseHover, x: msg.X, y: msg.Y, button: msg.Button}
	case tea.MouseReleaseMsg:
		return uiAction{kind: actionMouseRelease, x: msg.X, y: msg.Y, button: msg.Button}
	case tea.MouseWheelMsg:
		return uiAction{kind: actionWheel, wheel: msg, x: msg.X, y: msg.Y, button: msg.Button}
	}
	return uiAction{}
}

// handleAction routes one normalized action. Key actions are offered to the
// topmost active overlay first, then fall through to the composer; mouse
// actions are region-routed so pointer input over the composer never scrolls
// the transcript and vice versa.
func (m *model) handleAction(act uiAction) (tea.Model, tea.Cmd) {
	switch act.kind {
	case actionKey:
		for _, o := range m.overlayRoute() {
			if !o.overlayActive() {
				continue
			}
			if view, cmd, consumed := o.overlayKey(act.key); consumed {
				return view, cmd
			}
		}
		return m.composerKey(act.key)

	case actionMouseClick:
		return m.onMouseClick(tea.MouseClickMsg(tea.Mouse{X: act.x, Y: act.y, Button: act.button}))
	case actionMouseDrag:
		return m.onMouseMotion(tea.MouseMotionMsg(tea.Mouse{X: act.x, Y: act.y, Button: act.button}))
	case actionMouseHover:
		// Hover has no transcript behavior yet; keeping it explicit here
		// reserves the semantic for future hit-testing.
		return m, nil
	case actionMouseRelease:
		return m.onMouseRelease(tea.MouseReleaseMsg(tea.Mouse{X: act.x, Y: act.y, Button: act.button}))
	case actionWheel:
		return m.onMouseWheel(act.wheel)
	case actionResize:
		m.cancelSelection()
		return m.onResize(act.width, act.height)
	case actionPaste:
		return m.onPaste(act.text)
	}
	return m, nil
}

// onKey keeps the historical key entry point: a key action routed through the
// standard overlay → composer order.
func (m *model) onKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return m.handleAction(uiAction{kind: actionKey, key: k})
}

// composerKey handles keys that reach the prompt: the global bindings, then
// plain editing delegated to the textarea.
func (m *model) composerKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ks := k.Keystroke()
	switch ks {
	case "ctrl+c":
		// Abort a running turn if any; otherwise quit.
		if m.abortActiveRun() {
			return m, nil
		}
		return m, tea.Quit

	case "ctrl+o":
		m.cancelSelection()
		m.transcript.toggleExpand()
		m.refreshViewport()
		return m, nil

	case "esc":
		m.abortActiveRun()
		return m, nil

	case "ctrl+v":
		if m.clipboardBusy {
			return m, nil
		}
		m.clipboardBusy = true
		m.statusMsg = "Reading clipboard."
		return m, readClipboardCmd(m.clipboardRead)

	case "backspace":
		if m.input.Value() == "" && m.attachments.removeLast() {
			m.statusMsg = "Removed the last image attachment."
			m.updateLayout()
			return m, nil
		}

	case "enter":
		return m.submit(submitFollowUp)

	case "ctrl+enter", "ctrl+j":
		// Ctrl+Enter is distinct in terminals with keyboard enhancements. Ctrl+J
		// is retained as the unambiguous fallback for terminals that do not
		// encode the modifier on Enter.
		m.input.InsertString("\n")
		m.historyIndex = -1
		m.syncPickers()
		m.updateLayout()
		return m, nil

	case "alt+enter":
		return m.submit(submitSteering)

	case "pgup":
		m.viewport.ScrollUp(m.viewport.Height() / 2)
		return m, nil
	case "pgdown":
		m.viewport.ScrollDown(m.viewport.Height() / 2)
		return m, nil
	case "up":
		if m.input.Value() == "" || m.historyIndex >= 0 {
			if m.navigatePromptHistory(-1) {
				return m, nil
			}
		}
	case "down":
		if m.historyIndex >= 0 && m.navigatePromptHistory(1) {
			return m, nil
		}
	}

	previous := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	if m.input.Value() != previous {
		m.historyIndex = -1
	}
	m.syncPickers()
	m.updateLayout()
	return m, cmd
}

// onPaste routes pasted text. Paste never passes through composerKey, so an
// open provider-key entry never leaks a key into the conversation composer.
func (m *model) onPaste(text string) (tea.Model, tea.Cmd) {
	paste := tea.PasteMsg{Content: text}
	if m.keyFor.ID != "" {
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(paste)
		return m, cmd
	}
	previous := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(paste)
	if m.input.Value() != previous {
		m.historyIndex = -1
	}
	m.syncPickers()
	m.updateLayout()
	return m, cmd
}

// onMouseWheel scrolls the transcript only when the pointer is over the
// transcript region, so wheel input above the composer never moves scrollback.
func (m *model) onMouseWheel(wheel tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if wheel.Y < 0 || wheel.Y >= m.viewport.Height() {
		return m, nil
	}
	m.cancelSelection()
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(wheel)
	return m, cmd
}

// onMouseClick starts a transcript selection (or clears one) on left clicks
// inside the transcript region. Clicking a tool or thinking header row
// toggles that block's fold instead of selecting.
func (m *model) onMouseClick(mouse tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if mouse.Button != tea.MouseLeft {
		return m, nil
	}
	point, ok := m.transcriptPoint(mouse.X, mouse.Y)
	if !ok || m.showWelcome() {
		m.cancelSelection()
		return m, nil
	}
	if point.row < len(m.rows) {
		row := m.rows[point.row]
		if row.toggle {
			m.cancelSelection()
			m.transcript.toggleBlockFold(row.blockID)
			m.refreshViewport()
			return m, nil
		}
		if !row.selectable() {
			m.cancelSelection()
			return m, nil
		}
	}
	m.selection = &textSelection{anchor: point, current: point}
	return m, nil
}

// onMouseMotion extends an in-progress selection while the left button is held.
func (m *model) onMouseMotion(mouse tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if m.selection == nil || mouse.Button != tea.MouseLeft {
		return m, nil
	}
	point, ok := m.transcriptPoint(mouse.X, mouse.Y)
	if !ok {
		return m, nil
	}
	m.selection.current = point
	m.selection.dragged = point != m.selection.anchor
	m.refreshViewport()
	return m, nil
}

// onMouseRelease finishes a selection and copies it to the clipboard.
func (m *model) onMouseRelease(mouse tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if m.selection == nil || (mouse.Button != tea.MouseLeft && mouse.Button != tea.MouseNone) {
		return m, nil
	}
	if point, ok := m.transcriptPoint(mouse.X, mouse.Y); ok {
		m.selection.current = point
		m.selection.dragged = m.selection.dragged || point != m.selection.anchor
	}
	selection := *m.selection
	text := copyRowsText(m.rows, selection)
	m.cancelSelection()
	if text == "" {
		return m, nil
	}
	if err := m.clipboardWrite(text); err != nil {
		m.statusMsg = "Could not copy selection: " + err.Error()
		return m, nil
	}
	m.statusMsg = fmt.Sprintf("Copied %d characters.", len([]rune(text)))
	return m, clearStatusCmd(m.statusMsg)
}
