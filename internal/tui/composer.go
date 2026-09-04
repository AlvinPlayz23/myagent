package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

type submissionMode int

const (
	submitFollowUp submissionMode = iota
	submitSteering
)

type queuedMessage struct {
	display string
	message types.Message
}

// promptStyle selects the composer chrome drawn around the textarea.
type promptStyle string

const (
	promptDefault promptStyle = "default"
	promptRuled   promptStyle = "ruled"
)

type promptChoice struct {
	style       promptStyle
	label       string
	description string
}

var promptChoices = []promptChoice{
	{style: promptDefault, label: "Default", description: "(default) framed panel with a ❯ arrow"},
	{style: promptRuled, label: "Ruled", description: "one line framed by a rule above and below"},
}

// defaultComposerHeight is the framed composer's body height. The panel adds
// a top and bottom border, so the chrome totals the same six rows the
// bubbles textarea default occupied — keeping short-terminal layouts intact.
const defaultComposerHeight = 4

// ruledPrompt is the marker drawn at the start of the ruled composer's line.
const ruledPrompt = "› "

const (
	// ruledComposerRules counts the rules drawn above and below the textarea.
	ruledComposerRules = 2
	// composerFrameRows counts the rounded panel's top and bottom border.
	composerFrameRows = 2
	// The ruled composer opens one line tall and grows with the text, up to
	// ruledComposerMaxRows, after which it scrolls internally.
	ruledComposerMinRows = 1
	ruledComposerMaxRows = 10
	// ruledComposerReserve is the transcript rows kept free when the terminal is
	// too short to give the composer its full growth range.
	ruledComposerReserve = 4
	// composerContentRows bounds the text the composer will accept. Setting it at
	// all is what makes MaxHeight cap only the visible rows instead of blocking
	// input, so it just has to exceed any realistic prompt.
	composerContentRows = 1000
)

func normalizePromptStyle(style string) promptStyle {
	for _, choice := range promptChoices {
		if choice.style == promptStyle(style) {
			return choice.style
		}
	}
	return promptDefault
}

// composerHeight reports the rows the composer occupies, including the frame
// the active prompt style draws around the textarea.
func (m *model) composerHeight() int {
	height := m.input.Height()
	if m.promptStyle == promptRuled {
		height += ruledComposerRules
	} else {
		height += composerFrameRows
	}
	return height
}

// composerTextWidth is the textarea width that fits inside the active
// style's frame: the ruled rules span the terminal, while the default
// panel's border and padding take two columns on each side.
func (m *model) composerTextWidth() int {
	if m.promptStyle == promptRuled {
		return max(1, m.width)
	}
	return max(1, m.width-4)
}

// submit starts a run while idle. During a run, Enter queues a follow-up and
// Alt+Enter injects steering into the current work.
func (m *model) submit(mode submissionMode) (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" && m.attachments.len() == 0 {
		return m, nil
	}
	if m.clipboardBusy {
		m.statusMsg = "Wait for the clipboard image to finish loading."
		return m, nil
	}
	if m.abortRequested {
		m.statusMsg = "Wait for the current run to finish aborting."
		return m, nil
	}
	m.picker.close()
	m.files.close()
	m.updateLayout()
	if strings.HasPrefix(text, "/") {
		m.input.Reset()
		m.historyIndex = -1

		m.updateLayout()
		return m.runCommand(text)
	}
	content, err := expandPromptContent(text, m.cwd)
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}
	attachmentCount := m.attachments.len()
	content, err = m.attachments.appendTo(content)
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}
	display := text
	if attachmentCount > 0 {
		display = attachmentDisplay(text, attachmentCount)
	}
	m.addPromptHistory(text)
	m.input.Reset()
	m.attachments.clear()
	m.historyIndex = -1

	m.updateLayout()
	um := userMessageContent(content)

	if m.working {
		if mode == submitFollowUp {
			m.queue.EnqueueFollowUp(um)
			m.queuedFollowUps = append(m.queuedFollowUps, queuedMessage{display: display, message: um})

		} else {
			m.queue.EnqueueSteering(um)
			m.queuedSteering = append(m.queuedSteering, um)
			m.statusMsg = fmt.Sprintf("Queued steering (%d pending)", m.queue.PendingCount())
		}

		if mode == submitSteering {
			m.transcript.addUser(display)
		}
		m.updateLayout()
		return m, nil
	}

	return m.startRun(display, um)
}

// startRun begins a fresh agent run while idle. display is echoed into the
// transcript, which lets a command send the model a longer prompt than the
// text the user actually typed. (The loop also emits message_start for this
// user message, but onAgentEvent skips RoleUser to avoid a duplicate.)
func (m *model) startRun(display string, um types.Message) (tea.Model, tea.Cmd) {
	m.transcript.addUser(display)
	m.refreshViewport()
	runCtx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.working = true
	m.abortRequested = false
	m.activePrompt = &um
	m.startedAt = time.Now()
	m.statusMsg = ""
	m.lastErr = nil
	return m, m.runner.start(runCtx, um)
}

// addPromptHistory stores recent submitted prompts for the current TUI session.
func (m *model) addPromptHistory(text string) {
	text = strings.TrimSpace(text)
	if text == "" || (len(m.promptHistory) > 0 && m.promptHistory[0] == text) {
		return
	}
	m.promptHistory = append([]string{text}, m.promptHistory...)
	if len(m.promptHistory) > promptHistoryLimit {
		m.promptHistory = m.promptHistory[:promptHistoryLimit]
	}
}

// navigatePromptHistory recalls older (-1) or newer (1) submitted prompts.
// It returns false when the requested direction has no history entry.
func (m *model) navigatePromptHistory(direction int) bool {
	if len(m.promptHistory) == 0 {
		return false
	}

	index := m.historyIndex - direction
	if index < -1 || index >= len(m.promptHistory) {
		return false
	}
	m.historyIndex = index
	if index == -1 {
		m.input.Reset()
	} else {
		m.input.SetValue(m.promptHistory[index])
	}
	m.syncPickers()
	m.updateLayout()
	return true
}

// syncComposerStyle reconfigures the textarea for the active prompt style. The
// ruled style grows with its content between one row and the terminal's budget;
// MaxContentHeight is what keeps MaxHeight a display cap rather than an input
// limit. SetWidth must run last: it re-measures the gutter and, in doing so,
// refits the height to the new bounds.
func (m *model) syncComposerStyle() {
	if m.promptStyle == promptRuled {
		m.input.Prompt = ruledPrompt
		m.input.DynamicHeight = true
		m.input.MinHeight = ruledComposerMinRows
		m.input.MaxHeight = m.ruledGrowthLimit()
		m.input.MaxContentHeight = composerContentRows
	} else {
		m.input.Prompt = m.defaultPrompt
		m.input.DynamicHeight = false
		m.input.MinHeight = 0
		m.input.MaxHeight = m.defaultMaxHeight
		m.input.MaxContentHeight = 0
		m.input.SetHeight(defaultComposerHeight)
	}
	m.input.SetWidth(m.composerTextWidth())
}

// ruledGrowthLimit is the tallest the ruled textarea may render on this
// terminal, leaving room for its rules, the surrounding chrome, and a few
// transcript rows.
func (m *model) ruledGrowthLimit() int {
	if m.height <= 0 {
		return ruledComposerMaxRows
	}
	budget := m.height - chromeHeight - ruledComposerRules - ruledComposerReserve
	return max(ruledComposerMinRows, min(ruledComposerMaxRows, budget))
}
