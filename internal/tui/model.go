package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/myagent/myagent/internal/types"
)

// tickMsg drives the streaming-tail refresh + spinner animation.
type tickMsg struct{}

// spinnerFrames is the working-state spinner (pi uses an animated Loader).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// model is the bubbletea root model for the interactive TUI.
type model struct {
	ctx    context.Context
	cancel context.CancelFunc // aborts the in-flight run (esc)

	runner *runner
	queue  *msgQueue
	th     *theme
	md     *mdRenderer

	transcript *transcript
	viewport   viewport.Model
	input      textarea.Model

	width, height int
	ready         bool

	working      bool // an agent Run is in progress
	spinnerFrame int
	startedAt    time.Time
	statusMsg    string

	modelID string
	cwd     string
	lastErr error

	// usage accumulates across the session for the footer.
	usage types.Usage
}

// newModel constructs the root model.
func newModel(ctx context.Context, r *runner, q *msgQueue, th *theme, md *mdRenderer, modelID, cwd string) *model {
	ta := textarea.New()
	ta.Placeholder = "Send a message (enter to send, ctrl+c to quit)…"
	ta.ShowLineNumbers = false
	ta.Focus()

	return &model{
		ctx:        ctx,
		runner:     r,
		queue:      q,
		th:         th,
		md:         md,
		transcript: newTranscript(th, md),
		input:      ta,
		modelID:    modelID,
		cwd:        cwd,
	}
}

// Init starts the event pump and the animation ticker.
func (m *model) Init() tea.Cmd {
	return tea.Batch(m.runner.waitForEvent(), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

// Update handles messages: key input, window resize, agent events, and ticks.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.onResize(msg.Width, msg.Height)

	case tea.KeyPressMsg:
		return m.onKey(msg)

	case agentEventMsg:
		cmd := m.onAgentEvent(msg.ev)
		// Re-arm the pump to keep consuming events.
		return m, tea.Batch(cmd, m.runner.waitForEvent())

	case eventChannelClosedMsg:
		return m, nil

	case agentDoneMsg:
		m.working = false
		m.statusMsg = ""
		if msg.err != nil && m.ctx.Err() == nil {
			m.lastErr = msg.err
			m.transcript.addErrorText("Error: " + msg.err.Error())
			m.refreshViewport()
		}
		if m.cancel != nil {
			m.cancel = nil
		}
		return m, nil

	case tickMsg:
		if m.working {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			m.refreshViewport()
		}
		return m, tickCmd()
	}

	// Delegate other messages to the focused input.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// onResize recomputes layout on a window-size change.
func (m *model) onResize(w, h int) (tea.Model, tea.Cmd) {
	m.width, m.height = w, h
	inputHeight := 3
	footerHeight := 2
	statusHeight := 1
	vpHeight := h - inputHeight - footerHeight - statusHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	if !m.ready {
		m.viewport = viewport.New(viewport.WithWidth(w), viewport.WithHeight(vpHeight))
		m.ready = true
	} else {
		m.viewport.SetWidth(w)
		m.viewport.SetHeight(vpHeight)
	}
	m.input.SetWidth(w)
	m.transcript.invalidate()
	m.refreshViewport()
	return m, nil
}

// onKey routes key presses. Uses Keystroke() strings for robust v2 matching.
func (m *model) onKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ks := k.Keystroke()
	switch ks {
	case "ctrl+c":
		// Abort a running turn if any; otherwise quit.
		if m.working && m.cancel != nil {
			m.cancel()
			return m, nil
		}
		return m, tea.Quit

	case "ctrl+o":
		m.transcript.toggleExpand()
		m.refreshViewport()
		return m, nil

	case "esc":
		if m.working && m.cancel != nil {
			m.cancel()
			m.statusMsg = "Aborting…"
			return m, nil
		}
		return m, nil

	case "enter":
		return m.submit(false)

	case "alt+enter":
		return m.submit(true)

	case "pgup":
		m.viewport.ScrollUp(m.viewport.Height() / 2)
		return m, nil
	case "pgdown":
		m.viewport.ScrollDown(m.viewport.Height() / 2)
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

// submit handles Enter (send/steer) and Alt+Enter (follow-up). When idle, both
// start a new run; when working, Enter enqueues steering and Alt+Enter
// enqueues a follow-up, matching pi's message-queue semantics.
func (m *model) submit(followUp bool) (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	um := userMessage(text)

	if m.working {
		if followUp {
			m.queue.EnqueueFollowUp(um)
			m.statusMsg = fmt.Sprintf("Queued follow-up (%d pending)", m.queue.PendingCount())
		} else {
			m.queue.EnqueueSteering(um)
			m.statusMsg = fmt.Sprintf("Queued steering (%d pending)", m.queue.PendingCount())
		}
		// Show the queued user message immediately.
		m.transcript.addUser(text)
		m.refreshViewport()
		return m, nil
	}

	// Idle: show the user's prompt, then start a fresh run. (The loop also
	// emits message_start for this user message, but onAgentEvent skips
	// RoleUser to avoid a duplicate.)
	m.transcript.addUser(text)
	m.refreshViewport()
	runCtx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	m.working = true
	m.startedAt = time.Now()
	m.statusMsg = ""
	m.lastErr = nil
	return m, m.runner.start(runCtx, um)
}

// onAgentEvent updates the transcript from a single AgentEvent, mirroring pi's
// component reactions.
func (m *model) onAgentEvent(ev types.AgentEvent) tea.Cmd {
	switch ev.Type {
	case types.EventMessageStart:
		if ev.Message != nil {
			switch ev.Message.Role {
			case types.RoleUser:
				// User prompts submitted from the input are already shown; only
				// add ones we didn't originate (steering injected by the loop is
				// also shown at submit time). Skip to avoid duplicates.
			case types.RoleAssistant:
				m.transcript.beginAssistant()
			}
		}
	case types.EventMessageUpdate:
		ame := ev.AssistantMessageEvent
		if ame != nil && ame.Type == "text_delta" && ame.Delta != "" {
			m.transcript.appendAssistantDelta(ame.Delta)
		}
	case types.EventMessageEnd:
		if ev.Message != nil {
			switch ev.Message.Role {
			case types.RoleAssistant:
				if ev.Message.Usage != nil {
					m.addUsage(*ev.Message.Usage)
				}
				if ev.Message.StopReason == types.StopAborted {
					m.transcript.addErrorText("Operation aborted")
				} else if ev.Message.StopReason == types.StopError && ev.Message.ErrorMessage != "" {
					m.transcript.addErrorText("Error: " + ev.Message.ErrorMessage)
				}
				m.transcript.endAssistant()
			}
		}
	case types.EventToolExecutionStart:
		m.transcript.startTool(ev.ToolCallID, ev.ToolName, ev.Args)
	case types.EventToolExecutionEnd:
		m.transcript.endTool(ev.ToolCallID, ev.Result, ev.IsError)
	case types.EventCompactionEnd:
		if ev.Compaction != nil {
			m.transcript.addNotice(fmt.Sprintf(
				"∼ Context compacted: %d → %d tokens (kept recent history).",
				ev.Compaction.TokensBefore, ev.Compaction.TokensAfter,
			))
		}
	}
	m.refreshViewport()
	return nil
}

func (m *model) addUsage(u types.Usage) {
	m.usage.Input += u.Input
	m.usage.Output += u.Output
	m.usage.CacheRead += u.CacheRead
	m.usage.CacheWrite += u.CacheWrite
	m.usage.Cost.Total += u.Cost.Total
}

// refreshViewport re-renders the transcript into the viewport and sticks to the
// bottom while working (so streaming text stays visible).
func (m *model) refreshViewport() {
	if !m.ready {
		return
	}
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.transcript.render(m.width))
	if atBottom || m.working {
		m.viewport.GotoBottom()
	}
}

// View composes the transcript viewport, status line, input, and footer.
func (m *model) View() tea.View {
	if !m.ready {
		return tea.NewView("")
	}
	var sb strings.Builder
	sb.WriteString(m.viewport.View())
	sb.WriteByte('\n')
	sb.WriteString(m.statusLine())
	sb.WriteByte('\n')
	sb.WriteString(m.input.View())
	sb.WriteByte('\n')
	sb.WriteString(m.footer())

	v := tea.NewView(sb.String())
	v.AltScreen = false
	return v
}

// statusLine shows the working spinner + elapsed time, or a transient status.
func (m *model) statusLine() string {
	if m.working {
		frame := m.th.spinner.Render(spinnerFrames[m.spinnerFrame])
		elapsed := time.Since(m.startedAt).Seconds()
		msg := "Working…"
		if m.statusMsg != "" {
			msg = m.statusMsg
		}
		return fmt.Sprintf("%s %s", frame, m.th.muted.Render(fmt.Sprintf("%s (%.1fs, esc to cancel)", msg, elapsed)))
	}
	if m.statusMsg != "" {
		return m.th.muted.Render(m.statusMsg)
	}
	return ""
}

// footer renders the cwd/model line and the token/cost stats line.
func (m *model) footer() string {
	left := m.th.footer.Render(collapseHome(m.cwd))
	right := m.th.footerRight.Render(m.modelID)
	line1 := padBetween(left, right, m.width)

	stats := fmt.Sprintf("↑%s ↓%s R%s W%s $%.4f",
		compact(m.usage.Input), compact(m.usage.Output),
		compact(m.usage.CacheRead), compact(m.usage.CacheWrite),
		m.usage.Cost.Total)
	line2 := m.th.footer.Render(stats)
	return line1 + "\n" + line2
}

func userMessage(text string) types.Message {
	return types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{types.TextBlock(text)},
	}
}
