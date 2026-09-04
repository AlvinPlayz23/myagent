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
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"

	"github.com/AlvinPlayz23/myagent/internal/export"
	"github.com/AlvinPlayz23/myagent/internal/images"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/session"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// tickMsg drives the streaming-tail refresh + spinner animation.
type tickMsg struct{}

// clearStatusMsg clears a startup status after its configured lifetime.
type clearStatusMsg struct{ status string }

func firstSelectableRow() int {
	for i, row := range customizeRows {
		if !row.header {
			return i
		}
	}
	return 0
}

// modelsDiscoveredMsg carries the outcome of a background /v1/models lookup
// for the picker's active provider. models is informational: the picker
// refreshes itself from the catalog, which persists successful discoveries.
type modelsDiscoveredMsg struct {
	provider string
	models   []string
	err      error
}

// model is the bubbletea root model for the interactive TUI.
type model struct {
	ctx    context.Context
	cancel context.CancelFunc // aborts the in-flight run (esc)

	runner *runner
	queue  *msgQueue
	th     *theme
	md     *mdRenderer

	transcript      *transcript
	viewport        viewport.Model
	input           textarea.Model
	picker          commandPicker
	files           filePicker
	sessions        sessionPicker
	models          modelPicker
	effort          effortPicker
	providers       providerPicker
	customize       customizePicker
	exportPick      exportPicker
	exportName      textinput.Model
	exportFormat    export.Format
	exportOverwrite bool
	keyInput        textinput.Model
	keyFor          modelcatalog.Provider

	width, height int
	ready         bool

	working         bool // an agent Run is in progress
	abortRequested  bool // cancellation requested; runner is still unwinding
	queuedSteering  []types.Message
	queuedFollowUps []queuedMessage
	activePrompt    *types.Message
	spinnerFrame    int
	startedAt       time.Time
	statusMsg       string
	selection       *textSelection
	rows            []layoutRow
	unseenRows      int
	helpActive      bool
	clipboardWrite  func(string) error
	clipboardRead   func() (clipboardPayload, error)
	clipboardBusy   bool
	attachments     imageAttachments
	promptHistory   []string
	historyIndex    int // -1 means the composer is not browsing prompt history.
	welcomeStyle    welcomeStyle
	welcomeFrame    int
	promptStyle     promptStyle
	// defaultPrompt is the textarea's stock gutter and defaultMaxHeight its stock
	// row cap, both captured at construction so switching back from the ruled
	// style restores them without hardcoding bubbles' defaults.
	defaultPrompt    string
	defaultMaxHeight int

	modelID string
	cwd     string
	lastErr error

	sessionTitle     string
	hasSessionTitle  bool
	setTerminalTitle func(string)

	newSession         func() error
	listSessions       func() ([]session.Info, error)
	currentSessionID   func() string
	resumeSession      func(string) ([]types.Message, error)
	renameSession      func(string) error
	availableModels    func() []modelcatalog.Model
	selectModel        func(string, string) (llm.Provider, llm.Model, error)
	availableProviders func() []modelcatalog.Provider
	// discoverModels live-queries the active provider's own /v1/models
	// endpoint. It returns an error when the provider is unreachable; the
	// catalog-backed list stays usable either way. The context lets an
	// in-flight lookup die with the session.
	discoverModels     func(context.Context, string) ([]string, error)
	discovering        string // provider currently being live-discovered, "" = idle
	providerConfigured func(string) bool
	providerIsCustom   func(string) bool
	providerAPIKey     func(string) string
	configureProvider  func(modelcatalog.Provider, string) error
	saveWelcomeStyle   func(welcomeStyle) error
	savePromptStyle    func(promptStyle) error
	exportSession      func(export.Format, string, bool) (string, error)

	// usage accumulates across the session for the footer.
	usage types.Usage
}

// newModel constructs the root model.
func newModel(ctx context.Context, r *runner, q *msgQueue, th *theme, md *mdRenderer, modelID, cwd string, newSession ...func() error) *model {
	ta := textarea.New()
	ta.Placeholder = "Send a message (enter send, ctrl+v paste image, ctrl+enter newline)…"
	// Grok's prompt arrow, colored in the user accent via the textarea's
	// focused prompt style. defaultPrompt captures it so switching prompt
	// styles and back restores the arrow.
	ta.Prompt = "❯ "
	styles := ta.Styles()
	styles.Focused.Prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("#c8c8c8"))
	ta.SetStyles(styles)
	ta.ShowLineNumbers = false
	ta.SetHeight(defaultComposerHeight)
	ta.Focus()
	key := textinput.New()
	key.Placeholder = "Paste API key"
	key.CharLimit = 0
	key.EchoMode = textinput.EchoPassword
	exportName := textinput.New()
	exportName.Placeholder = "File name"

	var createSession func() error
	if len(newSession) > 0 {
		createSession = newSession[0]
	}
	return &model{
		ctx:              ctx,
		runner:           r,
		queue:            q,
		th:               th,
		md:               md,
		transcript:       newTranscript(th, md),
		input:            ta,
		keyInput:         key,
		exportName:       exportName,
		picker:           newCommandPicker(),
		clipboardWrite:   clipboard.WriteAll,
		clipboardRead:    readNativeClipboard,
		historyIndex:     -1,
		welcomeStyle:     welcomeDefault,
		promptStyle:      promptDefault,
		defaultPrompt:    ta.Prompt,
		defaultMaxHeight: ta.MaxHeight,
		modelID:          modelID,
		cwd:              cwd,
		newSession:       createSession,
	}
}

func (m *model) updateTerminalTitle() {
	if m.setTerminalTitle == nil {
		return
	}
	title := "new"
	if m.hasSessionTitle {
		title = m.sessionTitle
	}
	m.setTerminalTitle("myagent - " + title)
}

// setSessionTitle records the first prompt as the session title. Future
// generated or user-editable titles should be applied here instead.
func (m *model) setSessionTitle(title string) {
	if m.hasSessionTitle {
		return
	}
	m.sessionTitle = title
	m.hasSessionTitle = true
	m.updateTerminalTitle()
}

// Init starts the event pump and the animation ticker.
func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.runner.waitForEvent(), tickCmd()}
	if m.statusMsg != "" {
		cmds = append(cmds, clearStatusCmd(m.statusMsg))
	}
	return tea.Batch(cmds...)
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func clearStatusCmd(status string) tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{status: status} })
}

// Update handles messages: key input, window resize, agent events, and ticks.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case clipboardResultMsg:
		m.clipboardBusy = false
		if msg.err != nil {
			m.statusMsg = "Could not read clipboard: " + msg.err.Error()
			return m, nil
		}
		if len(msg.payload.image) > 0 {
			if err := m.attachments.add(msg.payload.image); err != nil {
				m.statusMsg = "Could not attach clipboard image: " + err.Error()
				return m, nil
			}
			m.statusMsg = fmt.Sprintf("Attached clipboard image (%d/%d).", m.attachments.len(), images.MaxImages)
			m.updateLayout()
			return m, nil
		}
		if msg.payload.text != "" {
			m.input.InsertString(msg.payload.text)
			m.historyIndex = -1
			m.syncPickers()
			m.updateLayout()
			return m, nil
		}
		m.statusMsg = "Clipboard does not contain text or an image."
		return m, nil

	case modelsDiscoveredMsg:
		if m.discovering == msg.provider {
			m.discovering = ""
		}
		if msg.err != nil {

			if m.models.active && len(m.models.matched) == 0 {
				m.statusMsg = fmt.Sprintf("Model discovery failed: %v", msg.err)
				return m, clearStatusCmd(m.statusMsg)
			}
			return m, nil
		}
		if len(msg.models) == 0 || m.availableModels == nil {
			return m, nil
		}

		before := make(map[string]struct{}, len(m.models.items))
		for _, item := range m.models.items {
			before[item.Ref()] = struct{}{}
		}
		items := m.availableModels()
		added := 0
		for _, item := range items {
			if _, ok := before[item.Ref()]; !ok {
				added++
			}
		}
		if m.models.active {
			m.models.replace(items)
			m.updateLayout()
		}
		if added > 0 {
			plural := "s"
			if added == 1 {
				plural = ""
			}
			m.statusMsg = fmt.Sprintf("Discovered %d new model%s from %s.", added, plural, msg.provider)
		}
		return m, nil

	case agentEventMsg:
		if msg.generation != m.runner.generation {
			return m, m.runner.waitForEvent()
		}
		cmd := m.onAgentEvent(msg.ev)

		return m, tea.Batch(cmd, m.runner.waitForEvent())

	case agentTitleMsg:
		if msg.generation != m.runner.generation || msg.title == "" {
			return m, m.runner.waitForEvent()
		}
		m.sessionTitle = msg.title
		m.hasSessionTitle = true
		m.updateTerminalTitle()
		return m, m.runner.waitForEvent()

	case eventChannelClosedMsg:
		return m, nil

	case agentDoneMsg:
		if msg.generation != m.runner.generation {
			return m, m.runner.waitForEvent()
		}
		wasAborted := m.abortRequested
		m.working = false
		m.abortRequested = false
		m.activePrompt = nil
		m.statusMsg = ""
		if errors.Is(msg.err, errNothingToCompact) {
			m.statusMsg = msg.err.Error()
		} else if msg.err != nil && m.ctx.Err() == nil && !wasAborted {
			m.lastErr = msg.err
			m.transcript.addErrorText("Error: " + msg.err.Error())
			m.refreshViewport()
		}
		if m.cancel != nil {
			m.cancel = nil
		}
		return m, m.runner.waitForEvent()

	case tickMsg:
		refresh := false
		if m.working || m.discovering != "" {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			refresh = true
		}
		if m.showWelcome() && m.welcomeStyle.animated() {
			m.welcomeFrame = (m.welcomeFrame + 1) % welcomeFrameCount
			refresh = true
		}
		if refresh {
			m.refreshViewport()
		}
		return m, tickCmd()

	case clearStatusMsg:
		if m.statusMsg == msg.status {
			m.statusMsg = ""
		}
		return m, nil
	}

	if act := normalizeMessage(msg); act.kind != actionNone {
		return m.handleAction(act)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// onResize recomputes layout on a window-size change.
func (m *model) onResize(w, h int) (tea.Model, tea.Cmd) {
	m.width, m.height = w, h

	m.syncComposerStyle()
	vpHeight := m.viewportHeight()
	if !m.ready {
		m.viewport = viewport.New(viewport.WithWidth(w), viewport.WithHeight(vpHeight))
		m.ready = true
	} else {
		m.viewport.SetWidth(w)
		m.viewport.SetHeight(vpHeight)
	}
	m.transcript.invalidate()
	m.refreshViewport()
	return m, nil
}

// chromeHeight is the status row plus the two footer rows. The composer's own
// height varies with the prompt style, so it is added separately. The blocks in
// View are joined by newlines rather than separated by blank rows, so the
// separators cost no extra rows.
const chromeHeight = 3

// fixedHeight is the part of the layout the transcript can never use. The
// command picker borrows rows from the transcript while always leaving it at
// least one row.
func (m *model) fixedHeight() int { return chromeHeight + m.composerHeight() }

func (m *model) viewportHeight() int {
	height := m.height - m.fixedHeight() - m.panelHeight() - m.queuedFollowUpHeight() - m.attachmentHeight()
	return max(1, height)
}

func (m *model) attachmentHeight() int {
	if m.attachments.len() == 0 {
		return 0
	}
	return 1
}

func (m *model) queuedFollowUpHeight() int {
	if len(m.queuedFollowUps) == 0 || m.width <= 0 {
		return 0
	}
	return strings.Count(m.renderQueuedFollowUps(), "\n") + 1
}

// panelHeight reports the rows the topmost active overlay borrows from the
// transcript, always leaving the transcript at least one row.
func (m *model) panelHeight() int {
	available := m.height - m.fixedHeight() - 1
	for _, o := range m.overlayRoute() {
		if o.overlayActive() {
			return min(o.overlayHeight(), max(0, available))
		}
	}
	return 0
}

func (m *model) updateLayout() {
	if !m.ready {
		return
	}
	m.viewport.SetHeight(m.viewportHeight())
	m.refreshViewport()
}

func (m *model) syncPickers() {
	m.picker.sync(m.input.Value())
	if !m.picker.active {
		m.files.sync(m.input.Value(), m.cwd)
	} else {
		m.files.close()
	}
}

func (m *model) acceptFilePicker() (tea.Model, tea.Cmd) {
	path, ok := m.files.selected()
	if !ok {
		return m, nil
	}
	value := m.input.Value()
	value = value[:m.files.start] + "@" + path + value[m.files.end:]
	m.input.SetValue(value)
	m.files.dismiss(value)
	m.updateLayout()
	return m, nil
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
		return m.submit(submitFollowUp)
	}
	return m, nil
}

func (m *model) resumeSelectedSession() (tea.Model, tea.Cmd) {
	info, ok := m.sessions.selected()
	if !ok || m.resumeSession == nil {
		return m, nil
	}
	if info.ID == m.sessions.currentID {
		m.sessions.close()
		m.statusMsg = "Continuing current session."
		m.updateLayout()
		return m, nil
	}
	history, err := m.resumeSession(info.ID)
	if err != nil {
		m.statusMsg = "Could not resume session: " + err.Error()
		return m, nil
	}
	m.runner.resume(history)
	m.sessionTitle = info.Title
	if m.sessionTitle == "" {
		m.sessionTitle = session.Title(history)
	}
	m.hasSessionTitle = m.sessionTitle != "new"
	m.updateTerminalTitle()
	m.sessions.close()
	m.transcript.clear()
	seedTranscript(m.transcript, history)
	m.usage = types.Usage{}
	m.statusMsg = "Resumed session " + info.ID + "."
	m.updateLayout()
	return m, nil
}

func (m *model) transcriptPoint(x, y int) (textPoint, bool) {
	if !m.ready || y < 0 || y >= m.viewport.Height() || x < 0 || x >= m.viewport.Width() {
		return textPoint{}, false
	}
	return textPoint{row: m.viewport.YOffset() + y, col: m.viewport.XOffset() + x}, true
}

func (m *model) cancelSelection() {
	if m.selection == nil {
		return
	}
	m.selection = nil
	m.refreshViewport()
}

// onAgentEvent updates the transcript from a single AgentEvent, mirroring pi's
// component reactions.
func (m *model) onAgentEvent(ev types.AgentEvent) tea.Cmd {
	switch ev.Type {
	case types.EventMessageStart:
		if ev.Message != nil {
			switch ev.Message.Role {
			case types.RoleUser:
				if m.activePrompt != nil && sameUserMessage(*m.activePrompt, *ev.Message) {
					m.activePrompt = nil
				} else if i := messageIndex(m.queuedSteering, *ev.Message); i >= 0 {
					m.queuedSteering = append(m.queuedSteering[:i], m.queuedSteering[i+1:]...)
				} else if i := queuedMessageIndex(m.queuedFollowUps, *ev.Message); i >= 0 {
					queued := m.queuedFollowUps[i]
					m.queuedFollowUps = append(m.queuedFollowUps[:i], m.queuedFollowUps[i+1:]...)
					m.transcript.addUser(queued.display)
					m.statusMsg = ""
					m.updateLayout()
				}
			case types.RoleAssistant:
				m.transcript.beginAssistant()
			}
		}
	case types.EventMessageUpdate:
		ame := ev.AssistantMessageEvent
		if ame == nil || ame.Delta == "" {
			break
		}
		switch ame.Type {
		case "text_delta":
			m.transcript.appendAssistantDelta(ame.Delta)
		case "thinking_start":
			m.transcript.beginThinking()
		case "thinking_delta":
			m.transcript.appendThinkingDelta(ame.Delta)
		case "thinking_end":
			m.transcript.endThinking()
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
	case types.EventRetry:
		m.transcript.addNotice(fmt.Sprintf(
			"∼ Provider error, retrying… (attempt %d/%d)",
			ev.Attempt, ev.MaxAttempts,
		))
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
// refreshViewport re-renders the transcript into the viewport. Output is
// followed only while the viewport sits at the bottom, so scrolling up during
// a streaming turn holds position instead of yanking back to the tail; the
// status line then reports how many rows wait below.
func (m *model) refreshViewport() {
	if !m.ready {
		return
	}
	atBottom := m.viewport.AtBottom()
	m.rows = m.transcript.layout(m.width)
	var content string
	if m.showWelcome() {
		content = m.renderWelcome()
	} else {
		content = strings.Join(renderRowsSelection(m.rows, m.selection, m.th.selection), "\n") + "\n"
	}
	m.viewport.SetContent(content)
	if m.selection == nil && atBottom {
		m.viewport.GotoBottom()
	}
	if m.viewport.AtBottom() || m.selection != nil {
		m.unseenRows = 0
	} else {
		m.unseenRows = max(0, len(m.rows)-(m.viewport.YOffset()+m.viewport.Height()))
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
	if queued := m.renderQueuedFollowUps(); queued != "" {
		sb.WriteString(queued)
		sb.WriteByte('\n')
	}
	if attachments := m.attachments.render(m.th, m.width); attachments != "" {
		sb.WriteString(attachments)
		sb.WriteByte('\n')
	}
	sb.WriteString(m.renderComposer())
	sb.WriteByte('\n')
	sb.WriteString(m.footer())

	v := tea.NewView(sb.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	v.KeyboardEnhancements.ReportAllKeysAsEscapeCodes = true
	v.KeyboardEnhancements.ReportAssociatedText = true
	return v
}

func messageIndex(messages []types.Message, want types.Message) int {
	for i, message := range messages {
		if sameUserMessage(message, want) {
			return i
		}
	}
	return -1
}

func queuedMessageIndex(messages []queuedMessage, want types.Message) int {
	for i, message := range messages {
		if sameUserMessage(message.message, want) {
			return i
		}
	}
	return -1
}

func sameUserMessage(a, b types.Message) bool {
	return a.Role == b.Role && a.Timestamp == b.Timestamp && textOf(a) == textOf(b)
}

func userMessage(text string) types.Message {
	return userMessageContent([]types.ContentBlock{types.TextBlock(text)})
}

func userMessageContent(content []types.ContentBlock) types.Message {
	return types.Message{
		Role:      types.RoleUser,
		Content:   append([]types.ContentBlock(nil), content...),
		Timestamp: time.Now().UnixMilli(),
	}
}
