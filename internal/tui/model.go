package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/myagent/myagent/internal/llm"
	modelcatalog "github.com/myagent/myagent/internal/models"
	"github.com/myagent/myagent/internal/session"
	"github.com/myagent/myagent/internal/types"
)

// tickMsg drives the streaming-tail refresh + spinner animation.
type tickMsg struct{}

// spinnerFrames is the working-state spinner (pi uses an animated Loader).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const sessionPickerMaxVisible = 5

type sessionPicker struct {
	items  []session.Info
	sel    int
	active bool
}

func (p *sessionPicker) open(items []session.Info) {
	p.items = items
	p.sel = 0
	p.active = len(items) > 0
}

func (p *sessionPicker) close() {
	p.items = nil
	p.sel = 0
	p.active = false
}

func (p *sessionPicker) move(delta int) {
	if !p.active || len(p.items) == 0 {
		return
	}
	p.sel = (p.sel + delta + len(p.items)) % len(p.items)
}

func (p *sessionPicker) selected() (session.Info, bool) {
	if !p.active || p.sel < 0 || p.sel >= len(p.items) {
		return session.Info{}, false
	}
	return p.items[p.sel], true
}

func (p *sessionPicker) height() int {
	if !p.active {
		return 0
	}
	return 1 + min(sessionPickerMaxVisible, len(p.items))
}

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
	picker     commandPicker
	sessions   sessionPicker
	models     modelPicker
	providers  providerPicker
	keyInput   textinput.Model
	keyFor     modelcatalog.Provider

	width, height int
	ready         bool

	working      bool // an agent Run is in progress
	spinnerFrame int
	startedAt    time.Time
	statusMsg    string

	modelID string
	cwd     string
	lastErr error

	newSession         func() error
	listSessions       func() ([]session.Info, error)
	resumeSession      func(string) ([]types.Message, error)
	availableModels    func() []modelcatalog.Model
	selectModel        func(string, string) (llm.Provider, llm.Model, error)
	availableProviders func() []modelcatalog.Provider
	providerConfigured func(string) bool
	providerIsCustom   func(string) bool
	providerAPIKey     func(string) string
	configureProvider  func(modelcatalog.Provider, string) error

	// usage accumulates across the session for the footer.
	usage types.Usage
}

// newModel constructs the root model.
func newModel(ctx context.Context, r *runner, q *msgQueue, th *theme, md *mdRenderer, modelID, cwd string, newSession ...func() error) *model {
	ta := textarea.New()
	ta.Placeholder = "Send a message (enter to send, ctrl+c to quit)…"
	ta.ShowLineNumbers = false
	ta.Focus()
	key := textinput.New()
	key.Placeholder = "Paste API key"
	key.CharLimit = 0
	key.EchoMode = textinput.EchoPassword
	key.EchoCharacter = '*'

	var createSession func() error
	if len(newSession) > 0 {
		createSession = newSession[0]
	}
	return &model{
		ctx:        ctx,
		runner:     r,
		queue:      q,
		th:         th,
		md:         md,
		transcript: newTranscript(th, md),
		input:      ta,
		keyInput:   key,
		picker:     newCommandPicker(),
		modelID:    modelID,
		cwd:        cwd,
		newSession: createSession,
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

	case tea.PasteMsg:
		// Paste messages do not pass through onKey. Route them explicitly so a
		// provider key never falls through to the main conversation composer.
		if m.keyFor.ID != "" {
			var cmd tea.Cmd
			m.keyInput, cmd = m.keyInput.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.picker.sync(m.input.Value())
		m.updateLayout()
		return m, cmd

	case tea.MouseWheelMsg:
		return m.onMouseWheel(msg)

	case agentEventMsg:
		if msg.generation != m.runner.generation {
			return m, m.runner.waitForEvent()
		}
		cmd := m.onAgentEvent(msg.ev)
		// Re-arm the pump to keep consuming events.
		return m, tea.Batch(cmd, m.runner.waitForEvent())

	case eventChannelClosedMsg:
		return m, nil

	case agentDoneMsg:
		if msg.generation != m.runner.generation {
			return m, nil
		}
		m.working = false
		m.statusMsg = ""
		if errors.Is(msg.err, errNothingToCompact) {
			m.statusMsg = msg.err.Error()
		} else if msg.err != nil && m.ctx.Err() == nil {
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
	vpHeight := m.viewportHeight()
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

// The fixed UI occupies nine rows: three for the textarea, two for the
// footer, one status row, and three separating newlines. The command picker
// borrows rows from the transcript while always leaving it at least one row.
func (m *model) viewportHeight() int {
	const fixedHeight = 9
	height := m.height - fixedHeight - m.panelHeight()
	return max(1, height)
}

func (m *model) panelHeight() int {
	const fixedHeight = 9
	available := m.height - fixedHeight - 1
	desired := m.picker.height()
	if m.sessions.active {
		desired = m.sessions.height()
	}
	if m.models.active {
		desired = m.models.height()
	}
	if m.providers.active || m.keyFor.ID != "" {
		desired = min(10, max(2, len(m.providers.items)+1))
	}
	return min(desired, max(0, available))
}

func (m *model) updateLayout() {
	if !m.ready {
		return
	}
	m.viewport.SetHeight(m.viewportHeight())
	m.refreshViewport()
}

// onKey routes key presses. Uses Keystroke() strings for robust v2 matching.
func (m *model) onKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ks := k.Keystroke()
	if m.sessions.active {
		switch ks {
		case "up":
			m.sessions.move(-1)
			return m, nil
		case "down":
			m.sessions.move(1)
			return m, nil
		case "enter":
			return m.resumeSelectedSession()
		case "esc":
			m.sessions.close()
			m.statusMsg = "Resume cancelled."
			m.updateLayout()
			return m, nil
		case "ctrl+c":
			// Preserve the global quit behavior below.
		default:
			return m, nil
		}
	}
	if m.models.active {
		switch ks {
		case "up":
			m.models.move(-1)
		case "down":
			m.models.move(1)
		case "enter":
			return m.selectPickedModel()
		case "esc":
			m.models.close()
			m.statusMsg = "Model selection cancelled."
			m.updateLayout()
		case "backspace":
			if len(m.models.query) > 0 {
				m.models.query = m.models.query[:len(m.models.query)-1]
				m.models.filter()
				m.updateLayout()
			}
		default:
			if k.Text != "" {
				m.models.query += k.Text
				m.models.filter()
				m.updateLayout()
			}
		}
		return m, nil
	}
	if m.keyFor.ID != "" {
		switch ks {
		case "esc":
			m.keyInput.Reset()
			m.keyFor = modelcatalog.Provider{}
			m.providers.active = true
			m.statusMsg = "Provider edit cancelled."
			m.updateLayout()
			return m, nil
		case "enter":
			return m.saveProviderKey()
		}
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(k)
		return m, cmd
	}
	if m.providers.active {
		switch ks {
		case "up":
			m.providers.move(-1)
		case "down":
			m.providers.move(1)
		case "enter":
			return m.openProviderKeyEntry()
		case "esc":
			m.providers.close()
			m.statusMsg = "Provider selection cancelled."
			m.updateLayout()
		}
		return m, nil
	}
	if m.picker.active {
		switch ks {
		case "up":
			m.picker.move(-1)
			return m, nil
		case "down":
			m.picker.move(1)
			return m, nil
		case "tab":
			return m.acceptCommandPicker(false)
		case "enter":
			return m.acceptCommandPicker(true)
		case "esc":
			m.picker.dismiss(m.input.Value())
			m.updateLayout()
			return m, nil
		}
	}
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
	m.picker.sync(m.input.Value())
	m.updateLayout()
	return m, cmd
}

func (m *model) acceptCommandPicker(submit bool) (tea.Model, tea.Cmd) {
	item, ok := m.picker.selected()
	if !ok {
		return m, nil
	}
	value := item.name
	if item.requiresArg {
		value += " "
	}
	m.input.SetValue(value)
	m.picker.dismiss(value)
	m.updateLayout()
	if submit && !item.requiresArg {
		return m.submit(false)
	}
	return m, nil
}

func (m *model) resumeSelectedSession() (tea.Model, tea.Cmd) {
	info, ok := m.sessions.selected()
	if !ok || m.resumeSession == nil {
		return m, nil
	}
	history, err := m.resumeSession(info.ID)
	if err != nil {
		m.statusMsg = "Could not resume session: " + err.Error()
		return m, nil
	}
	m.runner.resume(history)
	m.sessions.close()
	m.transcript.clear()
	seedTranscript(m.transcript, history)
	m.usage = types.Usage{}
	m.statusMsg = "Resumed session " + info.ID + "."
	m.updateLayout()
	return m, nil
}

// onMouseWheel forwards wheel events over the transcript to its viewport.
func (m *model) onMouseWheel(mouse tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if mouse.Y < 0 || mouse.Y >= m.viewport.Height() {
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(mouse)
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
	m.picker.close()
	m.updateLayout()
	if strings.HasPrefix(text, "/") {
		m.input.Reset()
		return m.runCommand(text)
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

func (m *model) runCommand(text string) (tea.Model, tea.Cmd) {
	cmd, err := parseSlashCommand(text)
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}
	if m.working {
		m.statusMsg = "Cancel the current run before using slash commands."
		return m, nil
	}

	switch cmd.kind {
	case commandHelp:
		m.transcript.addNotice(helpText)
		m.refreshViewport()
	case commandClear:
		m.runner.discardEvents()
		m.transcript.clear()
		m.statusMsg = "Transcript cleared; conversation context is retained."
		m.refreshViewport()
	case commandNew:
		if m.newSession == nil {
			m.statusMsg = "Unable to create a new session."
			return m, nil
		}
		if err := m.newSession(); err != nil {
			m.statusMsg = "Could not create a new session: " + err.Error()
			return m, nil
		}
		m.transcript.clear()
		m.usage = types.Usage{}
		m.statusMsg = "Started a new conversation."
		m.refreshViewport()
	case commandModel:
		return m.openModelPicker(cmd.arg)
	case commandProviders:
		return m.openProviderPicker()
	case commandCompact:
		runCtx, cancel := context.WithCancel(m.ctx)
		m.cancel = cancel
		m.working = true
		m.startedAt = time.Now()
		m.statusMsg = "Compacting context…"
		m.lastErr = nil
		return m, m.runner.compact(runCtx)
	case commandResume:
		if m.listSessions == nil || m.resumeSession == nil {
			m.statusMsg = "Unable to resume sessions."
			return m, nil
		}
		infos, err := m.listSessions()
		if err != nil {
			m.statusMsg = "Could not list sessions: " + err.Error()
			return m, nil
		}
		if len(infos) == 0 {
			m.statusMsg = "No sessions found."
			return m, nil
		}
		m.sessions.open(infos)
		m.statusMsg = "Select a session to resume."
		m.updateLayout()
	}
	return m, nil
}

func (m *model) openProviderPicker() (tea.Model, tea.Cmd) {
	if m.availableProviders == nil || m.providerConfigured == nil || m.configureProvider == nil {
		m.statusMsg = "Provider configuration is unavailable."
		return m, nil
	}
	items := m.availableProviders()
	if len(items) == 0 {
		m.statusMsg = "No compatible providers are available in the catalog yet."
		return m, nil
	}
	m.providers.open(items)
	m.statusMsg = "Select a provider to add or replace its API key."
	m.updateLayout()
	return m, nil
}

func (m *model) openProviderKeyEntry() (tea.Model, tea.Cmd) {
	provider, ok := m.providers.selected()
	if !ok {
		return m, nil
	}
	if m.providerIsCustom != nil && m.providerIsCustom(provider.ID) {
		m.statusMsg = provider.Name + " is managed as a custom provider. Delete or rename it in `myagent auth` to use the built-in configuration."
		return m, nil
	}
	m.providers.active = false
	m.keyFor = provider
	m.keyInput.SetValue(m.providerAPIKey(provider.ID))
	m.keyInput.Placeholder = "API key for " + provider.Name
	cmd := m.keyInput.Focus()
	if m.providerConfigured(provider.ID) {
		m.statusMsg = "Replace the masked API key, then press enter to save."
	} else {
		m.statusMsg = "Enter API key, then press enter to save."
	}
	m.updateLayout()
	return m, cmd
}

func (m *model) saveProviderKey() (tea.Model, tea.Cmd) {
	key := strings.TrimSpace(m.keyInput.Value())
	if key == "" {
		m.statusMsg = "An API key is required."
		return m, nil
	}
	if err := m.configureProvider(m.keyFor, key); err != nil {
		m.statusMsg = "Could not save provider: " + err.Error()
		return m, nil
	}
	name := m.keyFor.Name
	m.keyInput.Reset()
	m.keyFor = modelcatalog.Provider{}
	m.providers.active = true
	m.statusMsg = name + " configured."
	m.updateLayout()
	return m, nil
}

func (m *model) openModelPicker(query string) (tea.Model, tea.Cmd) {
	if m.availableModels == nil || m.selectModel == nil {
		m.statusMsg = "Model selection is unavailable."
		return m, nil
	}
	items := m.availableModels()
	if len(items) == 0 {
		m.statusMsg = "No catalog models are available for configured providers."
		return m, nil
	}
	for _, item := range items {
		if strings.EqualFold(item.Ref(), strings.TrimSpace(query)) {
			return m.applyModel(item)
		}
	}
	m.models.open(items, query)
	m.statusMsg = "Search models, use up/down, enter selects, esc cancels."
	m.updateLayout()
	return m, nil
}

func (m *model) selectPickedModel() (tea.Model, tea.Cmd) {
	item, ok := m.models.selected()
	if !ok {
		return m, nil
	}
	return m.applyModel(item)
}

func (m *model) applyModel(item modelcatalog.Model) (tea.Model, tea.Cmd) {
	provider, selected, err := m.selectModel(item.Provider, item.ID)
	if err != nil {
		m.statusMsg = "Could not select model: " + err.Error()
		return m, nil
	}
	m.runner.setModel(provider, selected)
	m.modelID = item.Ref()
	m.models.close()
	m.statusMsg = "Model set to " + item.Ref() + "."
	m.updateLayout()
	return m, nil
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
	if picker := m.renderPanel(); picker != "" {
		sb.WriteString(picker)
		sb.WriteByte('\n')
	}
	sb.WriteString(m.input.View())
	sb.WriteByte('\n')
	sb.WriteString(m.footer())

	v := tea.NewView(sb.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *model) renderPanel() string {
	if m.sessions.active {
		return m.renderSessionPicker()
	}
	if m.models.active {
		return m.renderModelPicker()
	}
	if m.keyFor.ID != "" {
		return m.renderProviderKeyEntry()
	}
	if m.providers.active {
		return m.renderProviderPicker()
	}
	count := m.panelHeight()
	if count == 0 {
		return ""
	}
	start, end := m.picker.visibleRange(count)
	var lines []string
	for i := start; i < end; i++ {
		item := m.picker.items[m.picker.matched[i]]
		marker := "  "
		style := m.th.cmdPickerItem
		if i == m.picker.sel {
			marker = "› "
			style = m.th.cmdPickerSel
		}
		line := fmt.Sprintf("%s%-18s %s", marker, item.usage, item.description)
		if len(m.picker.matched) > count && i == end-1 {
			line = padBetween(line, fmt.Sprintf("%d/%d", m.picker.sel+1, len(m.picker.matched)), m.width)
		}
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(line))
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderSessionPicker() string {
	height := m.panelHeight()
	if height == 0 {
		return ""
	}
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Resume session — ↑/↓ select, enter resume, esc cancel")}
	count := min(height-1, len(m.sessions.items))
	if count <= 0 {
		return strings.Join(lines, "\n")
	}
	start := m.sessions.sel - count + 1
	if start < 0 {
		start = 0
	}
	if maxStart := len(m.sessions.items) - count; start > maxStart {
		start = maxStart
	}
	for i := start; i < start+count; i++ {
		info := m.sessions.items[i]
		marker := "  "
		style := m.th.cmdPickerItem
		if i == m.sessions.sel {
			marker = "› "
			style = m.th.cmdPickerSel
		}
		id := info.ID
		if len(id) > 8 {
			id = id[:8]
		}
		preview := info.Preview
		if preview == "" {
			preview = "(no messages)"
		}
		line := fmt.Sprintf("%s%s  %s  %s", marker, info.Modified.Local().Format("Jan 02 15:04"), id, preview)
		if len(m.sessions.items) > count && i == start+count-1 {
			line = padBetween(line, fmt.Sprintf("%d/%d", m.sessions.sel+1, len(m.sessions.items)), m.width)
		}
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(line))
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderModelPicker() string {
	height := m.panelHeight()
	if height == 0 {
		return ""
	}
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Model: " + m.models.query)}
	count := min(height-1, len(m.models.matched))
	if count == 0 {
		return strings.Join(append(lines, m.th.muted.Render("  No matching configured-provider models.")), "\n")
	}
	start := max(0, m.models.sel-count+1)
	if maxStart := len(m.models.matched) - count; start > maxStart {
		start = maxStart
	}
	for i := start; i < start+count; i++ {
		item := m.models.items[m.models.matched[i]]
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.models.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		limit := ""
		if item.ContextWindow > 0 {
			limit = fmt.Sprintf("  %dk", item.ContextWindow/1000)
		}
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(marker+item.Ref()+limit))
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderProviderPicker() string {
	height := m.panelHeight()
	if height == 0 {
		return ""
	}
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Providers: [x] configured, enter edits key")}
	count := min(height-1, len(m.providers.items))
	start := max(0, m.providers.sel-count+1)
	if maxStart := len(m.providers.items) - count; start > maxStart {
		start = maxStart
	}
	for i := start; i < start+count; i++ {
		item := m.providers.items[i]
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.providers.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		locked := ""
		if m.providerIsCustom != nil && m.providerIsCustom(item.ID) {
			locked = "  managed as custom"
		} else if m.providerConfigured(item.ID) {
			locked = "  [x]"
		}
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(marker+item.Name+locked))
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderProviderKeyEntry() string {
	action := "Configure "
	if m.providerConfigured(m.keyFor.ID) {
		action = "Edit "
	}
	return m.th.cmdPickerSel.Render(action+m.keyFor.Name+"\n") + m.keyInput.View()
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
