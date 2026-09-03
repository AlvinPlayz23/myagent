package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/export"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// handleInput is the top of the key pipeline. Modal state captures keys
// first, then dropdowns, then the screen keymap.
func (a *app) handleInput(ev engine.Event) {
	switch {
	case ev.Paste != nil:
		a.onPaste(ev.Paste.Text)
		return
	case ev.Focus != nil:
		return
	case ev.Mouse != nil:
		a.onMouse(*ev.Mouse)
		return
	case ev.Key != nil:
		a.onKey(*ev.Key)
	}
}

// onMouse routes mouse events: wheel scrolls the transcript, drag selects.
func (a *app) onMouse(m engine.Mouse) {
	switch m.Action {
	case engine.MouseWheelUp:
		if !a.welcome {
			a.sbMode = true
			a.sb.scrollBy(-3, a.sbTotal(), a.sbHeight())
		}
	case engine.MouseWheelDown:
		if !a.welcome {
			a.sb.scrollBy(3, a.sbTotal(), a.sbHeight())
		}
	case engine.MousePress:
		if a.welcome {
			// Hit-test the welcome menu.
			a.welcomeClick(m)
			return
		}
		a.sbMode = false
	case engine.MouseDrag, engine.MouseRelease:
		// Text selection is handled by terminal-native selection; the app
		// only needs to stop claiming keys.
	}
}

// welcomeClick activates a welcome menu row by hit position.
func (a *app) welcomeClick(m engine.Mouse) {
	bodyY := 4
	menuH := len(welcomeMenuItems)
	menuY := a.h - menuH - 3
	_ = bodyY
	if m.Y >= menuY && m.Y < menuY+menuH {
		idx := m.Y - menuY
		if idx >= 0 && idx < menuH {
			a.welcomeSel = idx
			a.welcomeActivate(idx)
		}
	}
}

// onKey dispatches a decoded key press.
func (a *app) onKey(k engine.Key) {
	if a.modalKind != modalNone {
		a.modalKey(k)
		return
	}
	if a.welcome {
		a.welcomeKey(k)
		return
	}
	a.agentKey(k)
}

// welcomeKey handles keys on the welcome screen.
func (a *app) welcomeKey(k engine.Key) {
	switch k.Code {
	case "up":
		if a.welcomeSel > 0 {
			a.welcomeSel--
		}
	case "down":
		if a.welcomeSel < len(welcomeMenuItems)-1 {
			a.welcomeSel++
		}
	case "enter":
		a.welcomeActivate(a.welcomeSel)
	case "ctrl+c":
		a.quit = true
	case "tab":
		a.startAgent()
	default:
		// Printable keys jump straight into the session with the text
		// pre-filled, like the pager's welcome prompt.
		if k.Text != "" {
			a.startAgent()
			a.prompt.insertString(k.Text)
		}
	}
}

// welcomeActivate runs the selected welcome menu entry.
func (a *app) welcomeActivate(idx int) {
	if idx < 0 || idx >= len(welcomeMenuItems) {
		return
	}
	item := welcomeMenuItems[idx]
	switch item.cmd {
	case commandNone:
		a.startAgent()
	case commandResume:
		a.startAgent()
		a.runCommand("/resume")
	case commandModel:
		a.startAgent()
		a.runCommand("/model")
	case commandProviders:
		a.startAgent()
		a.runCommand("/providers")
	case commandCustomize:
		a.startAgent()
		a.runCommand("/customize")
	case commandHelp:
		a.startAgent()
		a.runCommand("/help")
	case commandQuit:
		a.quit = true
	}
}

// startAgent switches from the welcome screen into the session view.
func (a *app) startAgent() {
	if !a.welcome {
		return
	}
	a.welcome = false
	a.sbMode = false
	a.updateTerminalTitle()
}

// agentKey handles keys in the session view.
func (a *app) agentKey(k engine.Key) {
	// Scrollback mode (Tab focused): navigation keys scroll, Tab/Esc returns.
	if a.sbMode {
		switch k.Code {
		case "tab", "esc", "enter":
			a.sbMode = false
			a.sb.scrollEnd()
			return
		case "up":
			a.sb.scrollBy(-1, a.sbTotal(), a.sbHeight())
			return
		case "down":
			a.sb.scrollBy(1, a.sbTotal(), a.sbHeight())
			return
		case "pgup":
			a.sb.scrollBy(-a.sbHeight()/2, a.sbTotal(), a.sbHeight())
			return
		case "pgdown":
			a.sb.scrollBy(a.sbHeight()/2, a.sbTotal(), a.sbHeight())
			return
		case "home":
			a.sb.scrollHome()
			return
		case "end":
			a.sb.scrollEnd()
			return
		}
	}

	// Export overwrite confirm intercepts everything.
	if a.modalOverwrite {
		switch k.Code {
		case "enter":
			a.writeExport(true)
		case "esc":
			a.modalOverwrite = false
			a.exportFormat = ""
			a.statusMsg = "Export cancelled."
		}
		return
	}

	// Inline pickers capture navigation before the editor.
	if a.files.active {
		switch k.Code {
		case "up":
			a.files.move(-1)
			return
		case "down":
			a.files.move(1)
			return
		case "tab", "enter":
			a.acceptFile()
			return
		case "esc":
			a.files.dismiss(a.prompt.value())
			return
		}
	}
	if a.picker.active {
		switch k.Code {
		case "up":
			a.picker.move(-1)
			return
		case "down":
			a.picker.move(1)
			return
		case "tab":
			a.acceptCommand(false)
			return
		case "enter":
			a.acceptCommand(true)
			return
		case "esc":
			a.picker.dismiss(a.prompt.value())
			return
		}
	}

	switch k.Code {
	case "ctrl+c":
		// With text: clear the prompt even mid-run. Empty + running:
		// cancel. Idle: quit.
		if a.prompt.value() != "" {
			a.prompt.setValue("")
			a.statusMsg = "Prompt cleared."
			return
		}
		if a.abortActiveRun() {
			return
		}
		a.quit = true
		return

	case "ctrl+p":
		a.openPalette()
		return

	case "ctrl+o":
		a.sb.toggleExpand()
		return

	case "esc":
		now := time.Now()
		if a.working {
			a.abortActiveRun()
			return
		}
		if a.prompt.value() != "" {
			if now.Sub(a.escAt) <= escDoublePress {
				a.prompt.setValue("")
				a.escAt = time.Time{}
				a.statusMsg = "Prompt cleared."
			} else {
				a.escAt = now
				a.statusMsg = "Press esc again to clear the prompt."
			}
			return
		}
		return

	case "tab":
		a.sbMode = true
		return

	case "ctrl+v":
		a.readClipboard()
		return

	case "backspace":
		if a.prompt.value() == "" && a.att.removeLast() {
			a.statusMsg = "Removed the last image attachment."
			return
		}
		a.prompt.backspace()
		a.syncPickers()
		return

	case "delete":
		a.prompt.deleteKey()
		a.syncPickers()
		return

	case "enter":
		if a.multilineOn {
			a.prompt.insertString("\n")
			return
		}
		a.submit(submitFollowUp)
		return

	case "shift+enter", "ctrl+enter", "ctrl+j":
		a.prompt.insertString("\n")
		return

	case "alt+enter":
		a.submit(submitSteering)
		return

	case "ctrl+m":
		a.multilineOn = !a.multilineOn
		if a.multilineOn {
			a.statusMsg = "Multiline mode on — enter inserts a newline."
		} else {
			a.statusMsg = "Multiline mode off — enter sends."
		}
		return

	case "ctrl+left", "alt+left":
		a.prompt.move("ctrl+left")
		return
	case "ctrl+right", "alt+right":
		a.prompt.move("ctrl+right")
		return
	case "ctrl+u":
		a.prompt.killLine()
		return
	case "ctrl+w":
		a.prompt.killWordBack()
		return

	case "left", "right", "home", "end":
		a.prompt.move(k.Code)
		return

	case "up":
		if a.prompt.value() == "" || a.prompt.historyActive() {
			if a.prompt.navigateHistory(-1) {
				a.syncPickers()
			}
			return
		}
		a.prompt.move("up")
		return
	case "down":
		if a.prompt.historyActive() {
			if a.prompt.navigateHistory(1) {
				a.syncPickers()
			}
			return
		}
		a.prompt.move("down")
		return
	case "pgup":
		a.sb.scrollBy(-a.sbHeight()/2, a.sbTotal(), a.sbHeight())
		return
	case "pgdown":
		a.sb.scrollBy(a.sbHeight()/2, a.sbTotal(), a.sbHeight())
		return
	}

	// Editable text.
	if k.Text != "" {
		a.prompt.insertString(k.Text)
		a.historyBreak()
		a.syncPickers()
	}
}

// historyBreak exits history navigation when the user edits the draft.
func (a *app) historyBreak() {
	if a.prompt.historyActive() {
		a.prompt.historyIdx = -1
	}
}

// syncPickers refreshes the inline dropdowns from the prompt text.
func (a *app) syncPickers() {
	text := a.prompt.value()
	a.picker.sync(text)
	a.files.sync(text, a.cwd)
}

// filesStart computes the visible window start of the file dropdown.
func (a *app) filesStart(count int) int {
	count = min(count, a.files.height())
	start := a.files.sel - count + 1
	if start < 0 {
		start = 0
	}
	if maxStart := len(a.files.matched) - count; start > maxStart {
		start = maxStart
	}
	return start
}

// sbHeight is the scrollback viewport height (recomputed at render).
func (a *app) sbHeight() int {
	h := a.h - chromeHeight - a.composerHeight() - a.dropdownHeight()
	if h < 1 {
		return 1
	}
	return h
}

// sbTotal computes the total scrollback content rows.
func (a *app) sbTotal() int {
	total := 0
	width := a.w - chromeAccent - chromeLeft - chromeRight
	if width <= 0 {
		return 0
	}
	for _, e := range a.sb.entries {
		if e.kind == sbThinking && !a.sb.showThinking && !e.streaming {
			continue
		}
		total += e.entryHeight(width, a.sb.expanded, a.sb.showThinking, a.th)
	}
	return total
}

// openPalette opens the ctrl+p command palette.
func (a *app) openPalette() {
	a.modalKind = modalPalette
	a.modalInput = ""
	a.picker = newCommandPicker()
	a.picker.sync("")
	a.picker.active = true
	a.picker.dismissedText = ""
}

// modalKey routes keys into the active modal.
func (a *app) modalKey(k engine.Key) {
	switch a.modalKind {
	case modalCommands, modalPalette:
		switch k.Code {
		case "up":
			a.picker.move(-1)
		case "down":
			a.picker.move(1)
		case "enter":
			a.modalKind = modalNone
			a.startAgent()
			if item, ok := a.picker.selected(); ok {
				a.runCommand(item.name)
			}
		case "esc":
			a.modalKind = modalNone
		case "backspace":
			if a.modalInput != "" {
				r := []rune(a.modalInput)
				a.modalInput = string(r[:len(r)-1])
			}
			a.picker.sync("/" + a.modalInput)
		default:
			if k.Text != "" {
				a.modalInput += k.Text
				a.picker.sync("/" + a.modalInput)
			}
		}

	case modalModels:
		switch k.Code {
		case "up":
			a.models.move(-1)
		case "down":
			a.models.move(1)
		case "enter":
			a.selectPickedModel()
		case "esc":
			a.models.close()
			a.modalKind = modalNone
			a.statusMsg = "Model selection cancelled."
		case "backspace":
			if a.modalInput != "" {
				r := []rune(a.modalInput)
				a.modalInput = string(r[:len(r)-1])
			}
			a.models.setQuery(a.modalInput)
		default:
			if k.Text != "" {
				a.modalInput += k.Text
				a.models.setQuery(a.modalInput)
			}
		}

	case modalSessions:
		switch k.Code {
		case "up":
			a.sessions.move(-1)
		case "down":
			a.sessions.move(1)
		case "enter":
			a.resumeSelected()
		case "esc":
			a.sessions.close()
			a.modalKind = modalNone
			a.statusMsg = "Resume cancelled."
		}

	case modalEffort:
		switch k.Code {
		case "up":
			a.effort.move(-1)
		case "down":
			a.effort.move(1)
		case "enter":
			a.applyEffort(a.effort.items[a.effort.sel].effort)
		case "esc":
			a.effort.close()
			a.modalKind = modalNone
			a.statusMsg = "Effort selection cancelled."
		}

	case modalProviders:
		switch k.Code {
		case "up":
			a.providers.move(-1)
		case "down":
			a.providers.move(1)
		case "enter":
			a.openProviderKeyEntry()
		case "esc":
			a.providers.close()
			a.modalKind = modalNone
			a.statusMsg = "Provider selection cancelled."
		}

	case modalProviderKey:
		switch k.Code {
		case "esc":
			a.modalInput = ""
			a.keyFor = modelcatalog.Provider{}
			a.modalKind = modalProviders
			a.statusMsg = "Provider edit cancelled."
		case "enter":
			a.saveProviderKey()
		case "backspace":
			if a.modalInput != "" {
				r := []rune(a.modalInput)
				a.modalInput = string(r[:len(r)-1])
			}
		default:
			if k.Text != "" {
				a.modalInput += k.Text
			}
		}

	case modalCustomize:
		switch k.Code {
		case "up":
			a.customizeMove(-1)
		case "down":
			a.customizeMove(1)
		case "enter":
			a.applyCustomizeSelection()
		case "esc":
			a.modalKind = modalNone
			a.statusMsg = "Customization cancelled."
		}

	case modalExportFormat:
		switch k.Code {
		case "up":
			a.exportPick.move(-1)
		case "down":
			a.exportPick.move(1)
		case "enter":
			a.exportFormat = a.exportPick.items[a.exportPick.sel].format
			a.modalKind = modalExportName
			a.modalInput = export.DefaultFilename(a.sessionTitle)
			a.statusMsg = "Enter a file name, then press enter to export."
		case "esc":
			a.modalKind = modalNone
			a.statusMsg = "Export cancelled."
		}

	case modalExportName:
		switch k.Code {
		case "esc":
			a.modalKind = modalNone
			a.exportFormat = ""
			a.modalInput = ""
			a.statusMsg = "Export cancelled."
		case "enter":
			a.writeExport(false)
		case "backspace":
			if a.modalInput != "" {
				r := []rune(a.modalInput)
				a.modalInput = string(r[:len(r)-1])
			}
		default:
			if k.Text != "" {
				a.modalInput += k.Text
			}
		}
	}
}

// customizeMove skips header rows when walking the customize list.
func (a *app) customizeMove(delta int) {
	for i := 0; i < len(customizeRows); i++ {
		a.customizeSel = (a.customizeSel + delta + len(customizeRows)) % len(customizeRows)
		if !customizeRows[a.customizeSel].header {
			return
		}
	}
}

// customizeIsCurrent reports whether a row matches the active settings.
func (a *app) customizeIsCurrent(r customizeRow) bool {
	if r.section == sectionStartup {
		return r.welcome == a.welcomeSty
	}
	return r.prompt == a.promptSty
}

// onPaste inserts bracketed-paste text, or treats it as an image when the
// clipboard carried one (handled by readClipboard).
func (a *app) onPaste(text string) {
	a.prompt.insertString(text)
	a.historyBreak()
	a.syncPickers()
}

// readClipboard fetches the clipboard payload off the UI path.
func (a *app) readClipboard() {
	if a.clipboardRead == nil {
		a.statusMsg = "Clipboard is unavailable."
		return
	}
	a.statusMsg = "Reading clipboard."
	read := a.clipboardRead
	go func() {
		payload, err := read()
		a.loopCh <- loopEvent{clipboard: &clipboardResult{payload: payload, err: err}}
	}()
}

// dispatchAgent routes async loop events into state updates.
func (a *app) dispatchAgent(env agentEventEnvelope) {
	if env.generation != a.generation && env.generation != 0 {
		return
	}
	ev := env.ev
	switch ev.Type {
	case types.EventMessageStart:
		if ev.Message != nil {
			switch ev.Message.Role {
			case types.RoleUser:
				if a.activePrompt != nil && sameUserMessage(*a.activePrompt, *ev.Message) {
					a.activePrompt = nil
				} else if i := messageIndex(a.queuedSteering, *ev.Message); i >= 0 {
					a.queuedSteering = append(a.queuedSteering[:i], a.queuedSteering[i+1:]...)
				} else if i := queuedMessageIndex(a.queuedFollowUps, *ev.Message); i >= 0 {
					queued := a.queuedFollowUps[i]
					a.queuedFollowUps = append(a.queuedFollowUps[:i], a.queuedFollowUps[i+1:]...)
					a.sb.addUser(queued.display)
					a.statusMsg = ""
				}
			case types.RoleAssistant:
				a.sb.beginAssistant()
			}
		}
	case types.EventMessageUpdate:
		ame := ev.AssistantMessageEvent
		if ame == nil || ame.Delta == "" {
			return
		}
		switch ame.Type {
		case "text_delta":
			a.sb.appendAssistantDelta(ame.Delta)
		case "thinking_start":
			a.sb.beginThinking()
		case "thinking_delta":
			a.sb.appendThinkingDelta(ame.Delta)
		case "thinking_end":
			a.sb.endThinking()
		}
	case types.EventMessageEnd:
		if ev.Message != nil {
			switch ev.Message.Role {
			case types.RoleAssistant:
				if ev.Message.Usage != nil {
					a.addUsage(*ev.Message.Usage)
				}
				if ev.Message.StopReason == types.StopAborted {
					a.sb.addErrorText("Operation aborted")
				} else if ev.Message.StopReason == types.StopError && ev.Message.ErrorMessage != "" {
					a.sb.addErrorText("Error: " + ev.Message.ErrorMessage)
				}
				a.sb.endAssistant()
			}
		}
	case types.EventToolExecutionStart:
		a.sb.startTool(ev.ToolCallID, ev.ToolName, ev.Args)
	case types.EventToolExecutionEnd:
		a.sb.endTool(ev.ToolCallID, ev.Result, ev.IsError)
	case types.EventCompactionEnd:
		if ev.Compaction != nil {
			a.sb.addNotice(fmt.Sprintf(
				"∼ Context compacted: %d → %d tokens (kept recent history).",
				ev.Compaction.TokensBefore, ev.Compaction.TokensAfter,
			))
		}
	case types.EventRetry:
		a.sb.addNotice(fmt.Sprintf(
			"∼ Provider error, retrying… (attempt %d/%d)",
			ev.Attempt, ev.MaxAttempts,
		))
	}
}

// addUsage accumulates token/cost usage.
func (a *app) addUsage(u types.Usage) {
	a.usage.Input += u.Input
	a.usage.Output += u.Output
	a.usage.CacheRead += u.CacheRead
	a.usage.CacheWrite += u.CacheWrite
	a.usage.Cost.Total += u.Cost.Total
}

// abortActiveRun is the single cancellation path used by Esc and Ctrl+C.
// Cancellation is deliberately cooperative: working remains true until the
// runner goroutine has actually stopped.
func (a *app) abortActiveRun() bool {
	if !a.working || a.cancel == nil {
		return false
	}
	if !a.abortRequested {
		a.abortRequested = true
		a.q.DrainAll()
		a.queuedSteering = nil
		a.queuedFollowUps = nil
		a.activePrompt = nil
		a.cancel()
	}
	a.statusMsg = "Aborting…"
	return true
}

// submit sends the prompt: follow-up (queued while working), steering, or a
// fresh run when idle.
func (a *app) submit(mode submissionMode) {
	text := strings.TrimSpace(a.prompt.value())
	if text == "" && a.att.len() == 0 {
		return
	}
	if a.abortRequested {
		a.statusMsg = "Wait for the current run to finish aborting."
		return
	}
	a.picker.close()
	a.files.close()
	if strings.HasPrefix(text, "/") {
		a.prompt.setValue("")
		a.runCommand(text)
		return
	}
	content, err := expandPromptContent(text, a.cwd)
	if err != nil {
		a.statusMsg = err.Error()
		return
	}
	attachmentCount := a.att.len()
	content, err = a.att.appendTo(content)
	if err != nil {
		a.statusMsg = err.Error()
		return
	}
	display := text
	if attachmentCount > 0 {
		display = attachmentDisplay(text, attachmentCount)
	}
	a.prompt.addHistory(text)
	a.prompt.setValue("")
	a.att.clear()

	um := userMessageContent(content)
	if a.working {
		if mode == submitFollowUp {
			a.q.EnqueueFollowUp(um)
			a.queuedFollowUps = append(a.queuedFollowUps, queuedMessage{display: display, message: um})
		} else {
			a.q.EnqueueSteering(um)
			a.queuedSteering = append(a.queuedSteering, um)
			a.statusMsg = fmt.Sprintf("Queued steering (%d pending)", a.q.PendingCount())
			a.sb.addUser(display)
		}
		return
	}
	a.startRun(display, um)
}

// startRun begins a fresh agent run while idle.
func (a *app) startRun(display string, um types.Message) {
	a.sb.addUser(display)
	a.startAgent()
	runCtx, cancel := context.WithCancel(a.ctx)
	a.cancel = cancel
	a.working = true
	a.abortRequested = false
	a.activePrompt = &um
	a.startedAt = time.Now()
	a.statusMsg = ""
	a.lastErr = nil
	a.r.start(runCtx, um)
}

// finishRun settles the run state after the runner reports completion.
func (a *app) finishRun(err error) {
	a.working = false
	a.cancel = nil
	a.abortRequested = false
	a.activePrompt = nil
	if err != nil {
		if errors.Is(err, context.Canceled) {
			a.statusMsg = "Cancelled."
		} else {
			a.lastErr = err
			a.sb.addErrorText("Error: " + err.Error())
			a.statusMsg = ""
		}
		return
	}
	a.statusMsg = ""
}

// updateTerminalTitle refreshes the terminal + session title.
func (a *app) updateTerminalTitle() {
	if a.setTerminalTitle == nil {
		return
	}
	title := "myagent"
	if a.sessionTitle != "" && a.sessionTitle != "new" {
		title += " — " + a.sessionTitle
	}
	title += " · " + a.modelID
	a.setTerminalTitle(title)
}

var _ = llm.EffortLevels
var _ = modelcatalog.Model{}
