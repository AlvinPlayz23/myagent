package tui

import (
	"context"
	"errors"
	"fmt"
	"math"
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

type submissionMode int

const (
	submitFollowUp submissionMode = iota
	submitSteering
)

type queuedMessage struct {
	display string
	message types.Message
}

// spinnerFrames is the working-state spinner (pi uses an animated Loader).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const (
	sessionPickerMaxVisible = 5
	promptHistoryLimit      = 100
)

type sessionPicker struct {
	items     []session.Info
	sel       int
	active    bool
	currentID string
}

// open pins the active session at the top of the picker. It remains selectable
// so Enter can explicitly mean "keep working in this session."
func (p *sessionPicker) open(items []session.Info, currentID string) {
	p.items = items
	p.currentID = currentID
	if currentID != "" {
		for i, info := range p.items {
			if info.ID == currentID {
				p.items[0], p.items[i] = p.items[i], p.items[0]
				break
			}
		}
	}
	p.sel = 0
	p.active = len(items) > 0
}

func (p *sessionPicker) close() {
	p.items = nil
	p.sel = 0
	p.active = false
	p.currentID = ""
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
	// Header, the pinned current row, and its divider precede up to five
	// historical sessions. Without a current row this is the ordinary list.
	if len(p.items) > 0 && p.currentID != "" && p.items[0].ID == p.currentID {
		return 3 + min(sessionPickerMaxVisible, len(p.items)-1)
	}
	return 1 + min(sessionPickerMaxVisible, len(p.items))
}

type welcomeStyle string

const (
	welcomeDefault welcomeStyle = "default"
	welcomeOrb     welcomeStyle = "orb"
	welcomeBanner  welcomeStyle = "banner"
	welcomeWave    welcomeStyle = "wave"
	welcomeRain    welcomeStyle = "rain"
	welcomeFill    welcomeStyle = "fill"
)

type welcomeChoice struct {
	style       welcomeStyle
	label       string
	description string
}

var welcomeChoices = []welcomeChoice{
	{style: welcomeDefault, label: "Default", description: "myagent text"},
	{style: welcomeOrb, label: "Orb", description: "animated dotted orb"},
	{style: welcomeBanner, label: "Banner", description: "block letters with a shimmer sweep"},
	{style: welcomeWave, label: "Wave", description: "flowing ripple under the title"},
	{style: welcomeRain, label: "Rain", description: "drifting dots behind the title"},
	{style: welcomeFill, label: "Fill", description: "block letters filling with liquid"},
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
	{style: promptDefault, label: "Default", description: "(default) tall box with a bar gutter"},
	{style: promptRuled, label: "Ruled", description: "one line framed by a rule above and below"},
}

// defaultComposerHeight keeps the default composer at its established outer
// height after the top border and bottom info divider are added.
const defaultComposerHeight = 6

const (
	// composerChromeRows are the top border and bottom model/info divider.
	composerChromeRows = 2
	// These widths mirror the reference prompt chrome: an accent rail, two
	// cells of left padding, and one cell reserved by the right border.
	composerAccentWidth = 1
	composerPadLeft     = 2
	composerPadRight    = 1

	// The ruled composer opens one line tall and grows with the text, up to
	// ruledComposerMaxRows, after which it scrolls internally.
	ruledComposerMinRows = 1
	ruledComposerMaxRows = 8
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

// customizeSection identifies which setting a /customize row belongs to.
type customizeSection int

const (
	sectionStartup customizeSection = iota
	sectionComposer
)

// customizeRow is one line of the /customize panel. Header rows title a group
// and carry no value, so navigation skips over them.
type customizeRow struct {
	section     customizeSection
	header      bool
	label       string
	description string
	welcome     welcomeStyle
	prompt      promptStyle
}

// customizeRows flattens the grouped settings into the display order used by
// both the renderer and the picker's cursor.
var customizeRows = buildCustomizeRows()

func buildCustomizeRows() []customizeRow {
	rows := []customizeRow{{section: sectionStartup, header: true, label: "1. Startup Style", description: "empty-session welcome"}}
	for _, choice := range welcomeChoices {
		rows = append(rows, customizeRow{
			section:     sectionStartup,
			label:       choice.label,
			description: choice.description,
			welcome:     choice.style,
		})
	}
	rows = append(rows, customizeRow{section: sectionComposer, header: true, label: "2. Composer (Prompt Box)", description: "where you type"})
	for _, choice := range promptChoices {
		rows = append(rows, customizeRow{
			section:     sectionComposer,
			label:       choice.label,
			description: choice.description,
			prompt:      choice.style,
		})
	}
	return rows
}

type customizePicker struct {
	active bool
	sel    int
}

// open positions the cursor on the row matching the active startup style so the
// panel opens showing what is currently in effect.
func (p *customizePicker) open(current welcomeStyle) {
	p.active = true
	p.sel = firstSelectableRow()
	for i, row := range customizeRows {
		if !row.header && row.section == sectionStartup && row.welcome == current {
			p.sel = i
			break
		}
	}
}

func firstSelectableRow() int {
	for i, row := range customizeRows {
		if !row.header {
			return i
		}
	}
	return 0
}

func (p *customizePicker) close() { p.active = false }

// move advances the cursor by delta selectable rows, stepping over the group
// headers in either direction.
func (p *customizePicker) move(delta int) {
	n := len(customizeRows)
	if delta == 0 || n == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	for ; delta > 0; delta-- {
		for i := 0; i < n; i++ {
			p.sel = (p.sel + step + n) % n
			if !customizeRows[p.sel].header {
				break
			}
		}
	}
}

func (p *customizePicker) selected() customizeRow {
	if p.sel < 0 || p.sel >= len(customizeRows) {
		return customizeRow{}
	}
	return customizeRows[p.sel]
}

func normalizeWelcomeStyle(style string) welcomeStyle {
	for _, choice := range welcomeChoices {
		if choice.style == welcomeStyle(style) {
			return choice.style
		}
	}
	return welcomeDefault
}

// animatedWelcome reports whether the style needs per-tick frame advancement.
func (s welcomeStyle) animated() bool { return s != welcomeDefault }

type exportPicker struct {
	active bool
	sel    int
}

type effortChoice struct {
	effort      llm.Effort
	label       string
	description string
}

var effortChoices = []effortChoice{
	{label: "Default", description: "use the provider default"},
	{effort: llm.EffortMinimal, label: "Minimal", description: "shortest reasoning pass"},
	{effort: llm.EffortLow, label: "Low", description: "fast, lightweight reasoning"},
	{effort: llm.EffortMedium, label: "Medium", description: "balanced reasoning"},
	{effort: llm.EffortHigh, label: "High", description: "deeper reasoning"},
	{effort: llm.EffortXHigh, label: "XHigh", description: "extended reasoning"},
	{effort: llm.EffortMax, label: "Max", description: "maximum reasoning"},
}

type effortPicker struct {
	active bool
	sel    int
}

func (p *effortPicker) open(current llm.Effort) {
	p.active = true
	p.sel = 0
	for i, choice := range effortChoices {
		if choice.effort == current {
			p.sel = i
			break
		}
	}
}

func (p *effortPicker) close() { p.active = false }

func (p *effortPicker) move(delta int) {
	p.sel = (p.sel + delta + len(effortChoices)) % len(effortChoices)
}

func (p *effortPicker) selected() effortChoice { return effortChoices[p.sel] }

func (p *exportPicker) open()          { p.active, p.sel = true, 0 }
func (p *exportPicker) close()         { p.active = false }
func (p *exportPicker) move(delta int) { p.sel = (p.sel + delta + 2) % 2 }
func (p *exportPicker) format() export.Format {
	if p.sel == 1 {
		return export.HTML
	}
	return export.Markdown
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
	layout          tuiLayout
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
	ta.Prompt = promptGlyph() + " "
	ta.SetPromptFunc(lipgloss.Width(ta.Prompt), func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return promptGlyph() + " "
		}
		return "  "
	})
	ta.Placeholder = "Send a message (enter send, ctrl+v paste image, ctrl+enter newline)…"
	ta.ShowLineNumbers = false
	configureTextareaTheme(&ta, th)
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
	case tea.WindowSizeMsg:
		m.cancelSelection()
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
		if m.exportFormat != "" {
			var cmd tea.Cmd
			m.exportName, cmd = m.exportName.Update(msg)
			return m, cmd
		}
		if m.modalActive() {
			return m, nil
		}
		previous := m.input.Value()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != previous {
			m.historyIndex = -1
		}
		m.syncPickers()
		m.updateLayout()
		return m, cmd

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

	case tea.MouseWheelMsg:
		return m.onMouseWheel(msg)

	case tea.MouseClickMsg:
		return m.onMouseClick(msg)

	case tea.MouseMotionMsg:
		return m.onMouseMotion(msg)

	case tea.MouseReleaseMsg:
		return m.onMouseRelease(msg)

	case modelsDiscoveredMsg:
		if m.discovering == msg.provider {
			m.discovering = ""
		}
		if msg.err != nil {
			// The catalog list stays usable, but if the picker is sitting on an
			// empty list the spinner just vanished — explain why instead.
			if m.models.active && len(m.models.matched) == 0 {
				m.statusMsg = fmt.Sprintf("Model discovery failed: %v", msg.err)
				return m, clearStatusCmd(m.statusMsg)
			}
			return m, nil
		}
		if len(msg.models) == 0 || m.availableModels == nil {
			return m, nil
		}
		// Diff the picker's stale items against a freshly built candidate
		// list, so the count describes models actually entering view rather
		// than a guess measured against an already-outdated snapshot.
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
		// Re-arm the pump to keep consuming events.
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

	// Delegate other messages to the focused input.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// onResize recomputes layout on a window-size change.
func (m *model) onResize(w, h int) (tea.Model, tea.Cmd) {
	m.width, m.height = w, h
	// The ruled composer's growth budget derives from the terminal height, and
	// its wrap width from the terminal width, so refit it before sizing the
	// viewport around it.
	m.syncComposerStyle()
	m.computeLayout()
	m.syncComposerFocus()
	vpHeight := m.layout.scrollback.Height
	vpWidth := max(1, m.layout.scrollbackContent.Width)
	if !m.ready {
		m.viewport = viewport.New(viewport.WithWidth(vpWidth), viewport.WithHeight(vpHeight))
		m.ready = true
	} else {
		m.viewport.SetWidth(vpWidth)
		m.viewport.SetHeight(vpHeight)
	}
	m.transcript.invalidate()
	m.refreshViewport()
	return m, nil
}

// chromeHeight is the top bar, status, shortcut, and two footer rows. The
// composer's own height varies with the prompt style, so it is added separately.
const chromeHeight = 5

// composerHeight reports the rows the composer occupies, including its prompt
// chrome. The default style keeps its established outer height; the ruled style
// grows with the textarea and adds the same two chrome rows.
func (m *model) composerHeight() int {
	if m.promptStyle == promptDefault {
		return defaultComposerHeight
	}
	height := m.input.Height()
	return max(1, height) + composerChromeRows
}

// fixedHeight is the part of the layout the transcript can never use. The
// command picker borrows rows from the transcript while always leaving it at
// least one row.
func (m *model) fixedHeight() int { return chromeHeight + m.composerHeight() }

func (m *model) viewportHeight() int {
	if m.layout.scrollback.Height > 0 {
		return m.layout.scrollback.Height
	}
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

func (m *model) panelHeight() int {
	if m.ready {
		if m.modalActive() {
			return m.desiredPanelRows()
		}
		if desired := m.desiredPanelRows(); desired != m.layout.panel.Height {
			m.computeLayout()
		}
		return m.layout.panel.Height
	}
	return m.desiredPanelRows()
}

func (m *model) desiredPanelRows() int {
	desired := m.picker.height()
	if m.files.active {
		desired = m.files.height()
	}
	if m.sessions.active {
		desired = m.sessions.height()
	}
	if m.models.active {
		desired = m.models.height()
		if m.discovering != "" {
			desired++ // room for the live-discovery indicator line
		}
	}
	if m.effort.active {
		desired = len(effortChoices) + 1
	}
	if m.providers.active || m.keyFor.ID != "" {
		desired = min(10, max(2, len(m.providers.items)+1))
	}
	if m.exportPick.active || m.exportFormat != "" || m.exportOverwrite {
		desired = 3
	} else if m.customize.active {
		desired = len(customizeRows) + 1
	}
	return max(0, desired)
}

func (m *model) updateLayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.computeLayout()
	m.syncComposerFocus()
	if !m.ready {
		return
	}
	m.viewport.SetWidth(max(1, m.layout.scrollbackContent.Width))
	m.viewport.SetHeight(m.layout.scrollback.Height)
	m.refreshViewport()
}

func (m *model) computeLayout() {
	timelineWidth := timelineLaneWidth(m.width, m.timelineTurnCount())
	params := tuiLayoutParams{
		width:          m.width,
		height:         m.height,
		topBarRows:     1,
		panelRows:      m.layoutPanelRows(),
		queueRows:      m.queuedFollowUpHeight(),
		attachmentRows: m.attachmentHeight(),
		composerRows:   m.composerHeight(),
		statusRows:     1,
		shortcutRows:   1,
		footerRows:     2,
		timelineWidth:  timelineWidth,
	}
	m.layout = computeTUILayout(params)
	// A two-cell rail needs one chevron row, one tick row, and one chevron
	// row. Do not reserve it when the terminal cannot render that geometry.
	if timelineWidth > 0 && m.layout.scrollback.Height < 3 {
		params.timelineWidth = 0
		m.layout = computeTUILayout(params)
	}
	if params.timelineWidth == 0 && m.transcriptNeedsScrollbar(m.layout) {
		params.scrollbarWidth = fallbackScrollbarLaneWidth
		m.layout = computeTUILayout(params)
	}
}

func (m *model) transcriptNeedsScrollbar(layout tuiLayout) bool {
	if m.transcript == nil || len(m.transcript.blocks) == 0 || layout.scrollback.empty() {
		return false
	}
	// Keep the full-width candidate until overflow is known. Reserving the lane
	// first would make a fitting transcript look narrower for no visual reason.
	content := m.transcript.render(max(1, layout.scrollback.Width))
	if content == "" {
		return false
	}
	return strings.Count(content, "\n")+1 > layout.scrollback.Height
}

func (m *model) layoutPanelRows() int {
	if m.modalActive() {
		return 0
	}
	return m.desiredPanelRows()
}

// modalActive identifies the existing picker states that should float above
// the base agent view. The slash command picker intentionally remains inline:
// its completion list is part of the prompt's established viewport contract.
func (m *model) modalActive() bool {
	return m.exportPick.active || m.exportOverwrite || m.exportFormat != "" ||
		m.sessions.active || m.models.active || m.effort.active ||
		m.customize.active || m.keyFor.ID != "" || m.providers.active || m.files.active
}

// Keys: enter sends or queues a follow-up; ctrl+enter inserts a newline.
// ctrl+j is retained as a reliable newline alternative for terminals that do
// not encode Ctrl+Enter distinctly; alt+enter sends a steering message.
func (m *model) onKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	ks := k.Keystroke()
	if m.exportPick.active {
		switch ks {
		case "up":
			m.exportPick.move(-1)
		case "down":
			m.exportPick.move(1)
		case "enter":
			m.exportFormat = m.exportPick.format()
			m.exportPick.close()
			m.exportName.SetValue(export.DefaultFilename(m.sessionTitle))
			m.exportName.Focus()
			m.statusMsg = "Enter a file name, then press enter to export."
			m.updateLayout()
		case "esc":
			m.exportPick.close()
			m.statusMsg = "Export cancelled."
			m.updateLayout()
		}
		return m, nil
	}
	if m.exportOverwrite {
		switch ks {
		case "up", "down":
			m.exportOverwrite = false
			m.updateLayout()
		case "enter":
			return m.writeExport(true)
		case "esc":
			m.exportOverwrite = false
			m.exportFormat = ""
			m.statusMsg = "Export cancelled."
			m.updateLayout()
		}
		return m, nil
	}
	if m.exportFormat != "" {
		switch ks {
		case "esc":
			m.exportFormat = ""
			m.exportName.Reset()
			m.statusMsg = "Export cancelled."
			m.updateLayout()
			return m, nil
		case "enter":
			return m.writeExport(false)
		}
		var cmd tea.Cmd
		m.exportName, cmd = m.exportName.Update(k)
		return m, cmd
	}
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
	if m.effort.active {
		switch ks {
		case "up":
			m.effort.move(-1)
		case "down":
			m.effort.move(1)
		case "enter":
			return m.applyEffort(m.effort.selected().effort)
		case "esc":
			m.effort.close()
			m.statusMsg = "Effort selection cancelled."
			m.updateLayout()
		}
		return m, nil
	}
	if m.customize.active {
		switch ks {
		case "up":
			m.customize.move(-1)
		case "down":
			m.customize.move(1)
		case "enter":
			return m.applyCustomizeSelection()
		case "esc":
			m.customize.close()
			m.statusMsg = "Customization cancelled."
			m.updateLayout()
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
	if m.files.active {
		switch ks {
		case "up":
			m.files.move(-1)
			return m, nil
		case "down":
			m.files.move(1)
			return m, nil
		case "tab", "enter":
			return m.acceptFilePicker()
		case "esc":
			m.files.dismiss(m.input.Value())
			m.updateLayout()
			return m, nil
		}
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

// onMouseWheel forwards wheel events over the transcript to its viewport.
func (m *model) onMouseWheel(mouse tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.modalActive() || !m.layout.scrollbackContent.contains(mouse.X, mouse.Y) {
		m.cancelSelection()
		return m, nil
	}
	m.cancelSelection()
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(mouse)
	return m, cmd
}

func (m *model) transcriptPoint(x, y int) (textPoint, bool) {
	if !m.ready || m.modalActive() || !m.layout.scrollbackContent.contains(x, y) {
		return textPoint{}, false
	}
	localX := x - m.layout.scrollbackContent.X
	localY := y - m.layout.scrollbackContent.Y
	return textPoint{
		row: m.viewport.YOffset() + localY,
		col: m.viewport.XOffset() + localX,
	}, true
}

func (m *model) onMouseClick(mouse tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.modalActive() {
		m.cancelSelection()
		return m, nil
	}
	if mouse.Button != tea.MouseLeft {
		return m, nil
	}
	if target, handled := m.timelineTargetAt(mouse.X, mouse.Y); handled {
		if target >= 0 {
			m.jumpToTimelineTurn(target)
		}
		return m, nil
	}
	if m.scrollbarTargetAt(mouse.X, mouse.Y) {
		return m, nil
	}
	point, ok := m.transcriptPoint(mouse.X, mouse.Y)
	if !ok || m.showWelcome() {
		m.cancelSelection()
		return m, nil
	}
	m.selection = &textSelection{anchor: point, current: point}
	return m, nil
}

func (m *model) onMouseMotion(mouse tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if m.modalActive() {
		m.cancelSelection()
		return m, nil
	}
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

func (m *model) onMouseRelease(mouse tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if m.modalActive() {
		m.cancelSelection()
		return m, nil
	}
	if m.selection == nil || (mouse.Button != tea.MouseLeft && mouse.Button != tea.MouseNone) {
		return m, nil
	}
	point, ok := m.transcriptPoint(mouse.X, mouse.Y)
	if !ok {
		m.cancelSelection()
		return m, nil
	}
	m.selection.current = point
	m.selection.dragged = m.selection.dragged || point != m.selection.anchor
	selection := *m.selection
	text := selectedRenderedText(m.transcript.render(m.scrollbackContentWidth()), selection)
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

func (m *model) cancelSelection() {
	if m.selection == nil {
		return
	}
	m.selection = nil
	m.refreshViewport()
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
		// Resetting shrinks a grown composer, so the transcript reclaims its rows.
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
	// Resetting shrinks a grown composer, so the transcript reclaims its rows.
	m.updateLayout()
	um := userMessageContent(content)

	if m.working {
		if mode == submitFollowUp {
			m.queue.EnqueueFollowUp(um)
			m.queuedFollowUps = append(m.queuedFollowUps, queuedMessage{display: display, message: um})
			// The "↳ next" line beside the composer reports queue state; no
			// status message needed.
		} else {
			m.queue.EnqueueSteering(um)
			m.queuedSteering = append(m.queuedSteering, um)
			m.statusMsg = fmt.Sprintf("Queued steering (%d pending)", m.queue.PendingCount())
		}
		// Steering is already active conversation input. Follow-ups remain beside
		// the composer until the loop begins processing them.
		if mode == submitSteering {
			m.transcript.addUser(display)
		}
		m.updateLayout()
		return m, nil
	}

	// Idle: show the user's prompt, then start a fresh run.
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

func (m *model) runCommand(text string) (tea.Model, tea.Cmd) {
	cmd, err := parseSlashCommand(text)
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}
	if m.working && cmd.kind != commandThinking {
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
		m.sessionTitle = ""
		m.hasSessionTitle = false
		m.updateTerminalTitle()
		m.statusMsg = "Started a new conversation."
		m.refreshViewport()
	case commandInit:
		return m.startRun("/init", userMessage(initPrompt))
	case commandThinking:
		return m.applyShowThinking(cmd.arg), nil
	case commandModel:
		return m.openModelPicker(cmd.arg)
	case commandEffort:
		return m.openEffortPicker(cmd.arg)
	case commandProviders:
		return m.openProviderPicker()
	case commandCustomize:
		m.customize.open(m.welcomeStyle)
		m.statusMsg = "Choose the empty-session startup style."
		m.updateLayout()
	case commandCompact:
		runCtx, cancel := context.WithCancel(m.ctx)
		m.cancel = cancel
		m.working = true
		m.abortRequested = false
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
		currentID := ""
		if m.currentSessionID != nil {
			currentID = m.currentSessionID()
		}
		m.sessions.open(infos, currentID)
		m.statusMsg = "Select a session to resume."
		m.updateLayout()
	case commandExport:
		if m.exportSession == nil {
			m.statusMsg = "Export is unavailable."
			return m, nil
		}
		m.exportPick.open()
		m.statusMsg = "Choose an export format."
		m.updateLayout()
	case commandRename:
		if m.renameSession == nil {
			m.statusMsg = "Unable to rename the current session."
			return m, nil
		}
		if err := m.renameSession(cmd.arg); err != nil {
			m.statusMsg = "Could not rename session: " + err.Error()
			return m, nil
		}
		m.sessionTitle = cmd.arg
		m.hasSessionTitle = true
		m.updateTerminalTitle()
		m.statusMsg = "Session renamed."
	}
	return m, nil
}

func (m *model) writeExport(overwrite bool) (tea.Model, tea.Cmd) {
	if m.exportSession == nil {
		return m, nil
	}
	path, err := m.exportSession(m.exportFormat, m.exportName.Value(), overwrite)
	if errors.Is(err, export.ErrFileExists) {
		m.exportOverwrite = true
		m.statusMsg = "File exists — enter overwrite, up/down return to name."
		m.updateLayout()
		return m, nil
	}
	if err != nil {
		m.statusMsg = "Could not export session: " + err.Error()
		return m, nil
	}
	m.exportFormat = ""
	m.exportOverwrite = false
	m.exportName.Reset()
	m.statusMsg = "Exported session to " + path
	m.updateLayout()
	return m, nil
}

// applyCustomizeSelection saves whichever grouped setting the cursor sits on.
func (m *model) applyCustomizeSelection() (tea.Model, tea.Cmd) {
	row := m.customize.selected()
	if row.header {
		return m, nil
	}
	if row.section == sectionComposer {
		return m.applyPromptStyle(row)
	}
	return m.applyWelcomeStyle(row)
}

func (m *model) applyWelcomeStyle(row customizeRow) (tea.Model, tea.Cmd) {
	if m.saveWelcomeStyle != nil {
		if err := m.saveWelcomeStyle(row.welcome); err != nil {
			m.statusMsg = "Could not save customization: " + err.Error()
			return m, nil
		}
	}
	m.welcomeStyle = row.welcome
	m.welcomeFrame = 0
	m.customize.close()
	m.statusMsg = "Startup style set to " + row.label + "."
	m.updateLayout()
	return m, nil
}

func (m *model) applyPromptStyle(row customizeRow) (tea.Model, tea.Cmd) {
	if m.savePromptStyle != nil {
		if err := m.savePromptStyle(row.prompt); err != nil {
			m.statusMsg = "Could not save customization: " + err.Error()
			return m, nil
		}
	}
	m.promptStyle = row.prompt
	m.syncComposerStyle()
	m.customize.close()
	m.statusMsg = "Composer style set to " + row.label + "."
	m.updateLayout()
	return m, nil
}

// syncComposerStyle reconfigures the textarea for the active prompt style. The
// ruled style grows with its content between one row and the terminal's budget;
// MaxContentHeight is what keeps MaxHeight a display cap rather than an input
// limit. SetWidth must run last: it re-measures the gutter and, in doing so,
// refits the height to the new bounds.
func (m *model) syncComposerStyle() {
	if m.promptStyle == promptRuled {
		m.input.Prompt = promptGlyph() + " "
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
		m.input.SetHeight(max(1, defaultComposerHeight-composerChromeRows))
	}
	// Leave room for the Grok-style accent rail, horizontal padding, and the
	// right border. Textarea.SetWidth accounts for its own prompt gutter.
	innerWidth := m.width - composerAccentWidth - composerPadLeft - composerPadRight
	m.input.SetWidth(max(1, innerWidth))
}

// syncComposerFocus keeps the textarea's visual focus state aligned with the
// existing modal ownership rules. It does not change which keys are routed to
// an overlay; it only lets the composer render as inactive behind it.
func (m *model) syncComposerFocus() {
	if m.modalActive() {
		m.input.Blur()
		return
	}
	m.input.Focus()
}

// ruledGrowthLimit is the tallest the ruled textarea may render on this
// terminal, leaving room for its rules, the surrounding chrome, and a few
// transcript rows.
func (m *model) ruledGrowthLimit() int {
	if m.height <= 0 {
		return ruledComposerMaxRows
	}
	budget := m.height - chromeHeight - composerChromeRows - ruledComposerReserve
	return max(ruledComposerMinRows, min(ruledComposerMaxRows, budget))
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
	if len(items) == 0 && m.discoverModels != nil {
		// Nothing in the catalog for the configured providers (offline first
		// run, or an endpoint models.dev does not know). Open the picker
		// anyway and populate it from the provider's live /v1/models listing.
		if cmd := m.discoverActiveProviderModels(); cmd != nil {
			m.models.open(items, query)
			m.statusMsg = "Catalog unavailable; discovering models from the provider…"
			m.updateLayout()
			return m, cmd
		}
	}
	if len(items) == 0 {
		m.statusMsg = "No catalog models are available for configured providers."
		return m, nil
	}
	for _, item := range items {
		if strings.EqualFold(item.Ref(), strings.TrimSpace(query)) {
			return m.applyModel(item)
		}
	}
	cmd := m.discoverActiveProviderModels()
	m.models.open(items, query)
	m.statusMsg = "Search models, use up/down, enter selects, esc cancels."
	m.updateLayout()
	return m, cmd
}

// discoverActiveProviderModels live-refreshes the picker with the active
// provider's own /v1/models listing in the background. The catalog-backed
// list opens the picker immediately; discovered IDs merge in when the
// lookup returns. Failures are silent because the catalog remains usable.
func (m *model) discoverActiveProviderModels() tea.Cmd {
	if m.discoverModels == nil {
		return nil
	}
	provider, _, ok := strings.Cut(strings.TrimSpace(m.modelID), "/")
	provider = strings.TrimSpace(provider)
	if !ok || provider == "" || m.discovering == provider {
		return nil
	}
	m.discovering = provider
	target := provider
	return func() tea.Msg {
		ids, err := m.discoverModels(m.ctx, target)
		return modelsDiscoveredMsg{provider: target, models: ids, err: err}
	}
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
	if effort, err := llm.NormalizeEffort(selected, m.runner.cfg.Effort); err == nil {
		m.runner.setEffort(effort)
	} else {
		m.runner.setEffort("")
	}
	m.modelID = item.Ref()
	m.models.close()
	m.statusMsg = "Model set to " + item.Ref() + "."
	m.updateLayout()
	return m, nil
}

func (m *model) openEffortPicker(value string) (tea.Model, tea.Cmd) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "default") {
		return m.applyEffort("")
	}
	if value != "" {
		effort, err := llm.ParseEffort(value)
		if err != nil {
			m.statusMsg = err.Error()
			return m, nil
		}
		return m.applyEffort(effort)
	}
	m.effort.open(m.runner.cfg.Effort)
	m.statusMsg = "Choose reasoning effort."
	m.updateLayout()
	return m, nil
}

// applyShowThinking flips (no arg) or sets ("on"/"off") the transcript's
// thinking visibility. It is a pure display toggle, so it is also allowed
// while a run is streaming.
func (m *model) applyShowThinking(arg string) tea.Model {
	var show bool
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "":
		show = !m.transcript.showThinking
	case "on", "true", "yes", "1":
		show = true
	case "off", "false", "no", "0":
		show = false
	default:
		m.statusMsg = "usage: /thinking [on|off]"
		return m
	}
	m.transcript.setShowThinking(show)
	if show {
		m.statusMsg = "Thinking shown."
	} else {
		m.statusMsg = "Thinking hidden."
	}
	m.refreshViewport()
	return m
}

func (m *model) applyEffort(effort llm.Effort) (tea.Model, tea.Cmd) {
	effort, err := llm.NormalizeEffort(m.runner.cfg.Model, effort)
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}
	m.runner.setEffort(effort)
	m.effort.close()
	if effort == "" {
		m.statusMsg = "Reasoning effort reset to provider default."
	} else {
		m.statusMsg = "Reasoning effort set to " + string(effort) + "."
	}
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
func (m *model) refreshViewport() {
	if !m.ready {
		return
	}
	atBottom := m.viewport.AtBottom()
	m.computeLayout()
	m.viewport.SetWidth(max(1, m.layout.scrollbackContent.Width))
	m.viewport.SetHeight(m.layout.scrollback.Height)
	contentWidth := m.scrollbackContentWidth()
	content := m.transcript.render(contentWidth)
	if m.showWelcome() {
		content = m.renderWelcome()
	} else {
		content = renderTextSelection(content, m.selection, m.th.selection)
	}
	m.viewport.SetContent(content)
	if m.selection == nil && (atBottom || m.working) {
		m.viewport.GotoBottom()
	}
}

func (m *model) scrollbackContentWidth() int {
	if m.layout.scrollbackContent.Width > 0 {
		return m.layout.scrollbackContent.Width
	}
	return max(1, m.width)
}

func (m *model) showWelcome() bool {
	return !m.hasSessionTitle && len(m.transcript.blocks) == 0
}

// renderWelcome returns the transient empty-session content. It deliberately
// lives outside the transcript so it is never persisted as conversation
// history and disappears as soon as the first prompt gives the session a title.
func (m *model) renderWelcome() string {
	title := centerLine(m.th.cmdPickerSel.Render("myagent"), m.width)
	if m.welcomeStyle == welcomeBanner && m.width >= bannerMinWidth {
		title = m.renderBanner()
	}
	if m.width < 24 {
		if m.welcomeStyle == welcomeOrb {
			return m.renderOrb(true) + "\n\n" + title
		}
		return title
	}
	if m.welcomeStyle == welcomeDefault && m.width >= welcomeHeroMinWidth && m.welcomeViewportRows() >= welcomeHeroMinHeight {
		return m.renderWelcomeHero()
	}

	subtitle := centerLine(m.th.muted.Render("Your terminal coding agent"), m.width)
	hint := "Type a prompt to begin · /help for commands"
	if m.width < 44 {
		hint = "Type a prompt · /help for commands"
	}
	if m.width < 34 {
		hint = "Type a prompt to begin"
	}
	hint = centerLine(m.th.muted.Render(hint), m.width)
	if rows := m.welcomeViewportRows(); rows > 0 && rows < 4 {
		return title + "\n" + hint
	}
	compact := m.viewport.Height() < 14
	logo := m.renderGrokLogo(compact)

	switch m.welcomeStyle {
	case welcomeOrb:
		return m.renderOrb(compact) + "\n\n" + title + "\n" + subtitle + "\n\n" + hint
	case welcomeBanner:
		// The block letters already carry the wordmark; the subtitle would only
		// crowd them.
		return title + "\n\n" + hint
	case welcomeWave:
		return title + "\n" + subtitle + "\n\n" + m.renderWave() + "\n\n" + hint
	case welcomeRain:
		return m.renderRain(compact, title, subtitle) + "\n\n" + hint
	case welcomeFill:
		if m.width < wordmarkWidth+4 {
			break
		}
		// The filling letters are the wordmark; a text title would duplicate it.
		return m.renderFill() + "\n\n" + hint
	}
	if logo != "" {
		return logo + "\n\n" + title + "\n" + subtitle + "\n\n" + hint
	}
	return title + "\n" + subtitle + "\n\n" + hint
}

const (
	welcomeHeroMinWidth  = 90
	welcomeHeroMaxWidth  = 120
	welcomeHeroMinHeight = 11
)

func (m *model) welcomeViewportRows() int {
	if m.layout.scrollback.Height > 0 {
		return m.layout.scrollback.Height
	}
	return m.viewport.Height()
}

// renderWelcomeHero mirrors the reference's wide welcome composition: a
// bounded bordered card with the mark on the left and actionable menu content
// on the right. Custom animated styles keep their existing stacked renderers.
func (m *model) renderWelcomeHero() string {
	cardWidth := min(welcomeHeroMaxWidth, max(1, m.width-6))
	if cardWidth < 3 {
		return ""
	}
	innerWidth := cardWidth - 2
	leftWidth := min(34, max(28, (innerWidth-3)/2))
	rightWidth := max(1, innerWidth-leftWidth-3)

	logoLines := strings.Split(grokLogo, "\n")
	for i, line := range logoLines {
		logoLines[i] = m.th.assistantTxt.Render(strings.TrimRight(line, " "))
	}
	menuLines := []string{
		m.th.cmdPickerSel.Render("myagent"),
		m.th.muted.Render("Your terminal coding agent"),
		"",
		m.th.promptPrompt.Render("new session") + m.th.footerRight.Render("  enter"),
		m.th.footer.Render("/help") + m.th.footerRight.Render("  commands"),
		m.th.footer.Render("/customize") + m.th.footerRight.Render("  appearance"),
		m.th.footer.Render("model") + m.th.footerRight.Render("  "+m.modelID),
	}
	innerHeight := max(len(logoLines), len(menuLines))

	rows := make([]string, 0, innerHeight+2)
	rows = append(rows, m.th.modalBorder.Render("╭"+strings.Repeat("─", cardWidth-2)+"╮"))
	for i := 0; i < innerHeight; i++ {
		left, right := "", ""
		if i < len(logoLines) {
			left = logoLines[i]
		}
		if i < len(menuLines) {
			right = menuLines[i]
		}
		left = centerWelcomeCell(left, leftWidth)
		right = padWelcomeCell(right, rightWidth)
		body := left + strings.Repeat(" ", 3) + right
		rows = append(rows, m.th.modalBorder.Render("│")+body+m.th.modalBorder.Render("│"))
	}
	rows = append(rows, m.th.modalBorder.Render("╰"+strings.Repeat("─", cardWidth-2)+"╯"))

	card := centerLine(strings.Join(rows, "\n"), m.width)
	available := max(0, m.welcomeViewportRows()-len(rows))
	return strings.Repeat("\n", available/3) + card
}

func centerWelcomeCell(content string, width int) string {
	content = truncateColumns(content, width)
	padding := max(0, width-lipgloss.Width(content))
	return strings.Repeat(" ", padding/2) + content + strings.Repeat(" ", padding-padding/2)
}

func padWelcomeCell(content string, width int) string {
	content = truncateColumns(content, width)
	return content + strings.Repeat(" ", max(0, width-lipgloss.Width(content)))
}

// grokLogo is the compact braille mark used by the reference welcome hero.
// It stays text-only so terminals without image or truecolor support still get
// the same silhouette.
const grokLogo = "⠀⠀⠀⠀⠀⠀⣀⣀⡀⠀⠀⠀⢀⠄\n" +
	"⠀⠀⠀⣠⣾⠿⠛⠛⠛⠛⢀⡴⠁⠀\n" +
	"⠀⠀⣼⡟⠁⠀⠀⠀⢀⡴⠻⣿⡀⠀\n" +
	"⠀⠀⣿⡇⠀⠀⠀⠔⠁⠀⠀⣿⡇⠀\n" +
	"⠀⠀⢹⣷⠀⠀⠀⠀⠀⢀⣴⡿⠀⠀\n" +
	"⠀⢀⠞⠁⠠⢶⣶⣶⣶⠿⠋⠀⠀⠀\n" +
	"⠐⠁⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀⠀"

func (m *model) renderGrokLogo(compact bool) string {
	if compact || m.width < 32 {
		return ""
	}
	lines := strings.Split(grokLogo, "\n")
	for i, line := range lines {
		style := m.th.muted
		if (i+m.welcomeFrame/8)%5 == 0 {
			style = m.th.assistantTxt
		}
		lines[i] = centerLine(style.Render(line), m.width)
	}
	return strings.Join(lines, "\n")
}

// welcomeFrameCount is the shared animation cycle length for every animated
// welcome style. 96 divides evenly by the orb's 32-frame period, so the orb
// keeps its original cadence while wave/rain/banner get a longer loop.
const welcomeFrameCount = 96

// bannerRows is a two-row half-block rendering of "myagent". Cells animate by
// color only; the silhouette never changes, so the layout cannot jitter.
var bannerRows = [2]string{
	"█▀▄▀█ █ █ ▄▀█ █▀▀ █▀▀ █▄ █ ▀█▀",
	"█ ▀ █ ▀▄█ █▀█ █▄█ ██▄ █ ▀█  █ ",
}

// bannerMinWidth is the terminal width below which the banner falls back to the
// plain centered title.
const bannerMinWidth = 34

// renderBanner draws the block-letter wordmark with a bright highlight band
// sweeping horizontally through it.
func (m *model) renderBanner() string {
	bannerWidth := len([]rune(bannerRows[0]))
	// Sweep travels the full width plus a lead-in/out so the band enters and
	// exits cleanly rather than popping at the edges.
	span := float64(bannerWidth) + 8
	head := span*float64(m.welcomeFrame)/welcomeFrameCount - 4

	rows := make([]string, 0, len(bannerRows))
	for _, row := range bannerRows {
		var sb strings.Builder
		for x, cell := range []rune(row) {
			if cell == ' ' {
				sb.WriteRune(' ')
				continue
			}
			switch distance := math.Abs(float64(x) - head); {
			case distance < 2:
				sb.WriteString(m.th.orbBright.Render(string(cell)))
			case distance < 5:
				sb.WriteString(m.th.orbMedium.Render(string(cell)))
			default:
				sb.WriteString(m.th.orbDim.Render(string(cell)))
			}
		}
		rows = append(rows, centerLine(sb.String(), m.width))
	}
	return strings.Join(rows, "\n")
}

// waveGlyphs ramps from an empty cell to a full one; the ripple picks a glyph
// per column from a scrolling sine so the crest appears to travel sideways.
var waveGlyphs = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▅', '▄', '▃', '▂'}

// renderWave draws a single scrolling sine ripple, brightest at its crests.
func (m *model) renderWave() string {
	width := min(max(m.width-8, 12), 48)
	phase := 2 * math.Pi * float64(m.welcomeFrame) / welcomeFrameCount

	var sb strings.Builder
	for x := range width {
		// Two summed sines keep the ripple from looking mechanically periodic.
		height := 0.6*math.Sin(float64(x)*0.35-phase*2) + 0.4*math.Sin(float64(x)*0.13-phase)
		glyph := waveGlyphs[int((height+1)/2*float64(len(waveGlyphs)-1)+0.5)]
		switch {
		case height > 0.55:
			sb.WriteString(m.th.orbBright.Render(string(glyph)))
		case height > -0.2:
			sb.WriteString(m.th.orbMedium.Render(string(glyph)))
		default:
			sb.WriteString(m.th.orbDim.Render(string(glyph)))
		}
	}
	return centerLine(sb.String(), m.width)
}

// renderRain lays sparse drifting dots behind the title and subtitle. Drop
// positions come from a hash of the column, so the field is deterministic and
// stable across renders at the same frame.
func (m *model) renderRain(compact bool, centeredRows ...string) string {
	height := 7
	if compact {
		height = 5
	}
	fieldWidth := min(max(m.width-4, 16), 64)
	left := max(0, (m.width-fieldWidth)/2)

	// Rows the title/subtitle occupy stay clear of drops so text is readable.
	titleRow := height / 2
	occupied := map[int]string{}
	for i, row := range centeredRows {
		occupied[titleRow+i] = row
	}

	rows := make([]string, 0, height+len(centeredRows))
	for y := 0; y < height; y++ {
		if row, ok := occupied[y]; ok {
			rows = append(rows, row)
			continue
		}
		var sb strings.Builder
		sb.WriteString(strings.Repeat(" ", left))
		for x := range fieldWidth {
			cell := ' '
			var style lipgloss.Style
			// Each column runs its own drop on a column-specific period and
			// offset, so the field never falls in lockstep.
			period := 11 + rainHash(x)%9
			offset := rainHash(x*7+1) % period
			if (m.welcomeFrame+offset)%period == y%period {
				cell, style = '·', m.th.orbDim
			} else if (m.welcomeFrame+offset)%period == (y+1)%period {
				cell, style = '·', m.th.orbMedium
			}
			if cell == ' ' {
				sb.WriteRune(' ')
				continue
			}
			sb.WriteString(style.Render(string(cell)))
		}
		rows = append(rows, strings.TrimRight(sb.String(), " "))
	}
	return strings.Join(rows, "\n")
}

// rainHash is a small deterministic integer hash used to scatter rain columns.
func rainHash(n int) int {
	n = (n ^ 61) ^ (n >> 16)
	n *= 9
	n ^= n >> 4
	n *= 0x27d4eb2d
	n ^= n >> 15
	if n < 0 {
		n = -n
	}
	return n
}

// wordmarkFont is a 5-row bitmap of the letters in "myagent". Letters are
// deliberately hollow rather than solid: dense blocks die immediately under
// Conway's rules, which would blow the wordmark apart before it is readable.
var wordmarkFont = [][]string{
	{"#   #", "## ##", "# # #", "#   #", "#   #"}, // m
	{"#   #", " # # ", "  #  ", "  #  ", "  #  "}, // y
	{" ## ", "#  #", "####", "#  #", "#  #"},      // a
	{" ###", "#   ", "# ##", "#  #", " ###"},      // g
	{"####", "#   ", "### ", "#   ", "####"},      // e
	{"#  #", "## #", "# ##", "#  #", "#  #"},      // n
	{"###", " # ", " # ", " # ", " # "},           // t
}

// wordmarkRows renders wordmarkFont into 5 equal-length rows, one column of
// blank space between letters. '#' marks a set pixel.
var wordmarkRows = func() []string {
	rows := make([]string, len(wordmarkFont[0]))
	for y := range rows {
		parts := make([]string, 0, len(wordmarkFont))
		for _, letter := range wordmarkFont {
			parts = append(parts, letter[y])
		}
		rows[y] = strings.Join(parts, " ")
	}
	return rows
}()

// wordmarkWidth is the rendered pixel width of wordmarkRows.
var wordmarkWidth = len(wordmarkRows[0])

// Terminal cells are roughly twice as tall as they are wide, so drawing one
// font pixel per column makes the letters look stretched and thin. Doubling the
// horizontal scale restores squarer, chunkier proportions; we fall back to
// single scale when the terminal is too narrow to fit the wide form.
const wordmarkScale = 2

// wordmarkScaleFor picks the largest horizontal scale that fits the width.
func wordmarkScaleFor(width int) int {
	if width >= wordmarkWidth*wordmarkScale+4 {
		return wordmarkScale
	}
	return 1
}

// Fill shades a letter pixel by how far it sits below the waterline: an unfilled
// pixel is a faint ghost, the waterline itself is a mid tone, and submerged
// pixels are solid. Only the shade changes, so the letterform always reads.
const (
	fillEmpty     = '░'
	fillWaterline = '▒'
	fillSubmerged = '█'
)

// renderFill draws the wordmark filling with a rising liquid whose surface
// ripples, then draining back down. Every letter pixel keeps its cell, so the
// silhouette — and therefore the layout — never changes.
func (m *model) renderFill() string {
	rowCount := len(wordmarkRows)
	// The level sweeps 0→1→0 across the cycle so it fills, then drains.
	progress := float64(m.welcomeFrame) / welcomeFrameCount
	level := 2 * progress
	if level > 1 {
		level = 2 - level
	}
	// Overshoot both ends so the letters rest fully empty and fully full for a
	// beat rather than only touching those states for a single frame.
	surface := float64(rowCount)*(1.25-1.5*level) - 0.5
	phase := 2 * math.Pi * float64(m.welcomeFrame) / 16
	scale := wordmarkScaleFor(m.width)

	rows := make([]string, 0, rowCount)
	for y, row := range wordmarkRows {
		var sb strings.Builder
		for x, cell := range row {
			if cell != '#' {
				sb.WriteString(strings.Repeat(" ", scale))
				continue
			}
			// A travelling sine ripples the waterline across the columns.
			waterline := surface + 0.45*math.Sin(float64(x)*0.45+phase)
			var glyph string
			var style lipgloss.Style
			switch depth := float64(y) - waterline; {
			case depth > 0.5:
				glyph, style = string(fillSubmerged), m.th.orbBright
			case depth > -0.5:
				glyph, style = string(fillWaterline), m.th.orbMedium
			default:
				glyph, style = string(fillEmpty), m.th.orbDim
			}
			sb.WriteString(style.Render(strings.Repeat(glyph, scale)))
		}
		rows = append(rows, centerLine(sb.String(), m.width))
	}
	return strings.Join(rows, "\n")
}

// renderOrb draws a fixed dotted sphere while a bright, slightly curved band
// moves across it. Keeping the silhouette stable avoids layout jitter.
func (m *model) renderOrb(compact bool) string {
	halfWidths := []int{2, 4, 6, 7, 7, 6, 4, 2}
	if compact {
		halfWidths = []int{1, 3, 4, 3, 1}
	}
	phase := 2 * math.Pi * float64(m.welcomeFrame%32) / 32
	travel := float64(halfWidths[len(halfWidths)/2] - 1)
	center := travel * math.Sin(phase)

	rows := make([]string, 0, len(halfWidths))
	for y, halfWidth := range halfWidths {
		wave := center + 0.7*math.Sin(float64(y)*0.8+phase)
		var row strings.Builder
		for x := -halfWidth; x <= halfWidth; x++ {
			distance := math.Abs(float64(x) - wave)
			switch {
			case distance < 1.25:
				row.WriteString(m.th.orbBright.Render("●"))
			case distance < 3.25:
				row.WriteString(m.th.orbMedium.Render("•"))
			default:
				row.WriteString(m.th.orbDim.Render("·"))
			}
		}
		rows = append(rows, centerLine(row.String(), m.width))
	}
	return strings.Join(rows, "\n")
}

// View composes the named agent-view regions. Runtime and input handling stay
// independent of this composition so resizing cannot change event semantics.
func (m *model) View() tea.View {
	if !m.ready {
		return tea.NewView("")
	}
	regions := make([]string, 0, 9)
	if !m.layout.topBar.empty() {
		regions = append(regions, renderRegion(m.renderTopBar(), m.layout.topBar))
	}
	if !m.layout.panel.empty() {
		regions = append(regions, renderRegion(m.renderPanel(), m.layout.panel))
	}
	regions = append(regions, renderRegion(m.renderScrollback(), m.layout.scrollback))
	if !m.layout.queue.empty() {
		regions = append(regions, renderRegion(m.renderQueuedFollowUps(), m.layout.queue))
	}
	if !m.layout.attachments.empty() {
		regions = append(regions, renderRegion(m.attachments.render(m.th, m.width), m.layout.attachments))
	}
	if !m.layout.statusLine.empty() {
		regions = append(regions, renderRegion(m.statusLine(), m.layout.statusLine))
	}
	if !m.layout.composer.empty() {
		regions = append(regions, renderRegion(m.renderComposer(), m.layout.composer))
	}
	if !m.layout.shortcuts.empty() {
		regions = append(regions, renderRegion(m.renderShortcutBar(), m.layout.shortcuts))
	}
	if !m.layout.footer.empty() {
		regions = append(regions, renderRegion(m.footer(), m.layout.footer))
	}

	content := strings.Join(regions, "\n")
	if m.modalActive() {
		content = m.renderModalOverlay(content)
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.BackgroundColor = lipgloss.Color(m.th.palette.bg)
	v.ForegroundColor = lipgloss.Color(m.th.palette.fg)
	v.MouseMode = tea.MouseModeCellMotion
	// Ask compatible terminals to encode modifiers on every key. Without this,
	// terminals that collapse Shift+Enter to Enter make the two actions
	// indistinguishable.
	v.KeyboardEnhancements.ReportAllKeysAsEscapeCodes = true
	v.KeyboardEnhancements.ReportAssociatedText = true
	return v
}

func (m *model) renderModalOverlay(base string) string {
	title := ""
	footer := "↑/↓ navigate · enter select · esc cancel"
	switch {
	case m.exportPick.active:
		title = "Export session"
	case m.exportOverwrite:
		title = "Confirm export"
		footer = "enter overwrite · ↑/↓ edit name · esc cancel"
	case m.exportFormat != "":
		title = "Export session"
	case m.sessions.active:
		title = "Sessions"
	case m.models.active:
		title = "Choose model"
	case m.effort.active:
		title = "Reasoning effort"
	case m.customize.active:
		title = "Customize"
	case m.keyFor.ID != "":
		title = "Provider API key"
		footer = "enter save · esc cancel"
	case m.providers.active:
		title = "Providers"
	case m.files.active:
		title = "Files"
	}
	body := m.renderPanel()
	bounds, frame := renderModalFrame(title, body, footer, m.width, m.height, m.th)
	return overlayModal(base, m.width, m.height, bounds, frame)
}

func (m *model) renderTopBar() string {
	left := m.th.topBar.Render(formatLocationLine(m.cwd))
	center := ""
	if m.hasSessionTitle {
		center = m.th.topBar.Render(m.sessionTitle)
	}
	right := m.th.topBar.Render(m.modelID)
	return renderStatusTriplet(left, center, right, max(1, m.width))
}

func (m *model) renderScrollback() string {
	height := max(1, m.layout.scrollback.Height)
	width := m.scrollbackContentWidth()
	rows := strings.Split(strings.TrimSuffix(m.viewport.View(), "\n"), "\n")
	if len(rows) == 1 && rows[0] == "" {
		rows = nil
	}
	trail, hasTrail := m.timelineRailFor(width)
	lines := make([]string, height)
	for i := range lines {
		line := ""
		if i < len(rows) {
			line = truncateColumns(rows[i], width)
		}
		line += strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
		if !m.layout.timeline.empty() {
			line += m.renderTimelineRow(i, trail, hasTrail)
		} else if !m.layout.scrollbar.empty() {
			line += " " // keep the track visually separated from the transcript
			line += m.renderScrollbarRow(i)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderTimelineRow(row int, rail timelineRail, hasRail bool) string {
	width := m.layout.timeline.Width
	if width <= 0 {
		return ""
	}
	if !hasRail {
		return strings.Repeat(" ", width)
	}
	// The rail geometry uses screen coordinates for hit testing; translate the
	// viewport-local render row back into that same coordinate system.
	screenRow := row + m.layout.scrollback.Y
	if screenRow == rail.upY {
		style := m.th.timeline
		if rail.upTarget < 0 {
			style = m.th.muted
		}
		return fitTimelineGlyph(style.Render(" "+timelineChevronUpGlyph()), width)
	}
	if screenRow == rail.downY {
		style := m.th.timeline
		if rail.downTarget < 0 {
			style = m.th.muted
		}
		return fitTimelineGlyph(style.Render(" "+timelineChevronDownGlyph()), width)
	}
	for turn := rail.windowFrom; turn < rail.windowTo; turn++ {
		if screenRow == timelineTickRow(rail, turn) {
			style := m.th.timeline
			if turn == rail.active {
				style = m.th.assistantTxt
			}
			return fitTimelineGlyph(style.Render(timelineTickGlyph(turn == rail.active)), width)
		}
	}
	return strings.Repeat(" ", width)
}

func (m *model) renderScrollbarRow(row int) string {
	width := m.layout.scrollbar.Width
	if width <= 0 {
		return ""
	}
	track := strings.Repeat(" ", width)
	total := m.viewport.TotalLineCount()
	viewportHeight := m.layout.scrollback.Height
	if total <= viewportHeight || viewportHeight <= 0 {
		return track
	}

	thumbHeight := max(1, viewportHeight*viewportHeight/total)
	maxStart := max(0, viewportHeight-thumbHeight)
	maxOffset := max(1, total-viewportHeight)
	thumbStart := m.viewport.YOffset() * maxStart / maxOffset
	if row < thumbStart || row >= thumbStart+thumbHeight {
		return m.th.timeline.Render(strings.Repeat("│", width))
	}
	style := m.th.accent
	if m.viewport.AtBottom() {
		style = m.th.muted
	}
	return style.Render(strings.Repeat("┃", width))
}

func fitTimelineGlyph(glyph string, width int) string {
	if width <= 0 {
		return ""
	}
	if got := lipgloss.Width(glyph); got < width {
		return glyph + strings.Repeat(" ", width-got)
	}
	return truncateColumns(glyph, width)
}

func (m *model) renderShortcutBar() string {
	hints := []shortcutHint{
		{key: "enter", label: "send", pinned: true},
		{key: "ctrl+enter", label: "newline"},
		{key: "alt+enter", label: "steer"},
		{key: "↑/↓", label: "history"},
		{key: "ctrl+o", label: "expand"},
	}
	return renderShortcuts(hints, max(1, m.width), m.th, m.layout.compact)
}

// renderComposer draws the textarea with the reference prompt chrome while
// leaving the textarea responsible for cursor, wrapping, and editing state.
func (m *model) renderComposer() string {
	view := m.input.View()
	lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	rows := max(1, m.layout.composer.Height)
	if rows == 1 {
		return m.renderPromptContentLine(lines[0], m.layout.composer.Width)
	}

	contentRows := max(1, rows-composerChromeRows)
	if len(lines) > contentRows {
		lines = lines[:contentRows]
	}
	for len(lines) < contentRows {
		lines = append(lines, "")
	}

	focused := m.input.Focused()
	result := make([]string, 0, rows)
	result = append(result, m.renderPromptDivider(true, focused))
	for _, line := range lines {
		result = append(result, m.renderPromptContentLine(line, m.layout.composer.Width))
	}
	result = append(result, m.renderPromptDivider(false, focused))
	return strings.Join(result, "\n")
}

func (m *model) promptBorderStyle(focused bool) lipgloss.Style {
	if focused {
		return m.th.promptBorderActive
	}
	return m.th.promptBorder
}

func (m *model) promptRailStyle(focused bool) lipgloss.Style {
	if focused {
		return m.th.promptRailActive
	}
	return m.th.promptRail
}

// promptDividerCells builds a width-safe border one cell at a time so info
// labels can replace dashes without ever painting over a corner.
func promptDividerCells(width int, border lipgloss.Style, left, right rune) []string {
	if width <= 0 {
		return nil
	}
	cells := make([]string, width)
	for i := range cells {
		cells[i] = border.Render("─")
	}
	cells[0] = border.Render(string(left))
	if width > 1 {
		cells[width-1] = border.Render(string(right))
	}
	return cells
}

func placePromptLabel(cells []string, start, limit int, label string, style lipgloss.Style) {
	if start < 0 {
		start = 0
	}
	if limit > len(cells) {
		limit = len(cells)
	}
	if start >= limit || label == "" {
		return
	}
	label = truncateColumns(label, limit-start)
	column := start
	for _, r := range label {
		glyph := string(r)
		glyphWidth := lipgloss.Width(glyph)
		if glyphWidth <= 0 || column+glyphWidth > limit {
			break
		}
		cells[column] = style.Render(glyph)
		for offset := 1; offset < glyphWidth && column+offset < limit; offset++ {
			cells[column+offset] = style.Render(" ")
		}
		column += glyphWidth
	}
}

func (m *model) renderPromptDivider(top, focused bool) string {
	width := max(1, m.layout.composer.Width)
	border := m.promptBorderStyle(focused)
	if width == 1 {
		return border.Render("│")
	}
	if top {
		cells := promptDividerCells(width, border, '╭', '╮')
		if m.hasSessionTitle && strings.TrimSpace(m.sessionTitle) != "" {
			end := width - composerPadRight - 1
			label := " " + strings.TrimSpace(m.sessionTitle) + " "
			label = truncateColumns(label, max(1, end-composerAccentWidth))
			placePromptLabel(cells, max(composerAccentWidth, end-lipgloss.Width(label)), end, label, m.th.promptInfo)
		}
		return strings.Join(cells, "")
	}

	cells := promptDividerCells(width, border, '╰', '╯')
	modelName := strings.TrimSpace(m.modelID)
	if modelName == "" {
		modelName = "unknown"
	}
	leftStart := min(width-1, composerAccentWidth+composerPadLeft)
	rightLimit := max(leftStart, width-composerPadRight-1)
	leftLabel := " " + modelName + " "
	rightLabel := ""
	if strings.Contains(m.input.Value(), "\n") {
		rightLabel = " multiline "
	}
	rightWidth := lipgloss.Width(rightLabel)
	leftLimit := max(leftStart, rightLimit-rightWidth-1)
	leftLabel = truncateColumns(leftLabel, max(1, leftLimit-leftStart))
	placePromptLabel(cells, leftStart, leftLimit, leftLabel, m.th.promptInfo)
	if rightLabel != "" && rightWidth > 0 {
		placePromptLabel(cells, max(leftLimit+1, rightLimit-rightWidth), rightLimit, rightLabel, m.th.promptInfoMuted)
	}
	return strings.Join(cells, "")
}

func (m *model) renderPromptContentLine(line string, width int) string {
	width = max(1, width)
	if width == 1 {
		return m.promptRailStyle(m.input.Focused()).Render(accentBarGlyph)
	}
	focused := m.input.Focused()
	border := m.promptBorderStyle(focused)
	rightBorderWidth := 1
	leftPad := min(composerPadLeft, max(0, width-composerAccentWidth-rightBorderWidth))
	contentWidth := width - composerAccentWidth - leftPad - rightBorderWidth
	if contentWidth > 0 {
		line = truncateColumns(line, contentWidth)
		if padding := contentWidth - lipgloss.Width(line); padding > 0 {
			line += m.th.promptBase.Render(strings.Repeat(" ", padding))
		}
	} else {
		line = ""
	}
	left := m.promptRailStyle(focused).Render(accentBarGlyph)
	left += m.th.promptBase.Render(strings.Repeat(" ", leftPad))
	return left + line + border.Render("│")
}

func (m *model) renderQueuedFollowUps() string {
	if len(m.queuedFollowUps) == 0 {
		return ""
	}
	width := max(1, m.width)
	lines := make([]string, 0, len(m.queuedFollowUps))
	for i, queued := range m.queuedFollowUps {
		label := "↳ next"
		if len(m.queuedFollowUps) > 1 {
			label = fmt.Sprintf("↳ next %d/%d", i+1, len(m.queuedFollowUps))
		}
		// One dim line per queued prompt: collapse newlines, truncate to fit.
		body := strings.Join(strings.Fields(queued.display), " ")
		bodyWidth := max(1, width-len([]rune(label))-4)
		if r := []rune(body); len(r) > bodyWidth {
			body = string(r[:bodyWidth-1]) + "…"
		}
		lines = append(lines, " "+m.th.queuedLabel.Render(label)+"  "+m.th.muted.Render(body))
	}
	return strings.Join(lines, "\n")
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

func (m *model) renderPanel() string {
	if m.exportPick.active {
		return m.renderExportPicker()
	}
	if m.exportOverwrite {
		return m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("File exists — enter overwrite, ↑/↓ return to name, esc cancel")
	}
	if m.exportFormat != "" {
		return m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Export as " + export.Label(m.exportFormat) + " — " + m.exportName.View() + " · enter export, esc cancel")
	}
	if m.sessions.active {
		return m.renderSessionPicker()
	}
	if m.models.active {
		return m.renderModelPicker()
	}
	if m.effort.active {
		return m.renderEffortPicker()
	}
	if m.customize.active {
		return m.renderCustomizePicker()
	}
	if m.keyFor.ID != "" {
		return m.renderProviderKeyEntry()
	}
	if m.providers.active {
		return m.renderProviderPicker()
	}
	if m.files.active {
		return m.renderFilePicker()
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

func (m *model) renderExportPicker() string {
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Export session as — ↑/↓ select, enter continue, esc cancel")}
	for i, format := range []export.Format{export.Markdown, export.HTML} {
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.exportPick.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		lines = append(lines, style.Render(marker+export.Label(format)))
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderFilePicker() string {
	count := m.panelHeight()
	if count == 0 {
		return ""
	}
	start, end := m.files.visibleRange(count)
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Files — ↑/↓ select, enter or tab insert, esc cancel")}
	count = min(count-1, end-start)
	if count <= 0 {
		return strings.Join(lines, "\n")
	}
	start, end = m.files.visibleRange(count)
	for i := start; i < end; i++ {
		path := m.files.items[m.files.matched[i]]
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.files.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		line := marker + path
		if len(m.files.matched) > count && i == end-1 {
			line = padBetween(line, fmt.Sprintf("%d/%d", m.files.sel+1, len(m.files.matched)), m.width)
		}
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(line))
	}
	return strings.Join(lines, "\n")
}

// renderCustomizePicker draws the settings grouped under numbered headers. The
// window scrolls so the cursor stays visible on short terminals.
func (m *model) renderCustomizePicker() string {
	height := m.panelHeight()
	if height == 0 {
		return ""
	}
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Customize — ↑/↓ select, enter save, esc cancel")}
	count := min(height-1, len(customizeRows))
	if count <= 0 {
		return strings.Join(lines, "\n")
	}
	start := max(0, m.customize.sel-count+1)
	if maxStart := len(customizeRows) - count; start > maxStart {
		start = maxStart
	}
	for i := start; i < start+count; i++ {
		row := customizeRows[i]
		if row.header {
			line := fmt.Sprintf("%s  %s", row.label, m.th.muted.Render(row.description))
			lines = append(lines, m.th.pickerGroup.MaxWidth(max(1, m.width)).Render(line))
			continue
		}
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.customize.sel {
			marker, style = "> ", m.th.cmdPickerSel
		}
		current := ""
		if m.rowIsCurrent(row) {
			current = "  (current)"
		}
		line := fmt.Sprintf("  %s%-10s %s%s", marker, row.label, row.description, current)
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(line))
	}
	return strings.Join(lines, "\n")
}

// rowIsCurrent reports whether a row holds the value its group is set to.
func (m *model) rowIsCurrent(row customizeRow) bool {
	if row.header {
		return false
	}
	if row.section == sectionComposer {
		return row.prompt == m.promptStyle
	}
	return row.welcome == m.welcomeStyle
}

func (m *model) renderEffortPicker() string {
	height := m.panelHeight()
	if height == 0 {
		return ""
	}
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Reasoning effort — ↑/↓ select, enter apply, esc cancel")}
	count := min(height-1, len(effortChoices))
	start := max(0, m.effort.sel-count+1)
	if maxStart := len(effortChoices) - count; start > maxStart {
		start = maxStart
	}
	current := m.runner.cfg.Effort
	for i := start; i < start+count; i++ {
		choice := effortChoices[i]
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.effort.sel {
			marker, style = "> ", m.th.cmdPickerSel
		}
		selected := ""
		if choice.effort == current {
			selected = "  (current)"
		}
		line := fmt.Sprintf("%s%-9s %s%s", marker, choice.label, choice.description, selected)
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
	current := len(m.sessions.items) > 0 && m.sessions.currentID != "" && m.sessions.items[0].ID == m.sessions.currentID
	first, fixedRows := 0, 1
	if current {
		info := m.sessions.items[0]
		marker, style := "  ", m.th.cmdPickerItem
		if m.sessions.sel == 0 {
			marker, style = "› ", m.th.cmdPickerSel
		}
		id := info.ID
		if len(id) > 8 {
			id = id[:8]
		}
		title := info.Title
		if title == "" {
			title = info.Preview
		}
		if title == "" {
			title = "(no messages)"
		}
		line := fmt.Sprintf("%s● CURRENT  %s  %s  %s", marker, info.Modified.Local().Format("Jan 02 15:04"), id, title)
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(line))
		lines = append(lines, m.th.muted.MaxWidth(max(1, m.width)).Render(strings.Repeat("─", max(1, m.width))))
		first, fixedRows = 1, 3
	}
	count := min(height-fixedRows, len(m.sessions.items)-first)
	if count <= 0 {
		return strings.Join(lines, "\n")
	}
	start := m.sessions.sel - count + 1
	if start < first {
		start = first
	}
	if maxStart := len(m.sessions.items) - count; start > maxStart {
		start = maxStart
	}
	for i := start; i < start+count; i++ {
		info := m.sessions.items[i]
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.sessions.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		id := info.ID
		if len(id) > 8 {
			id = id[:8]
		}
		title := info.Title
		if title == "" {
			title = info.Preview
		}
		if title == "" {
			title = "(no messages)"
		}
		line := fmt.Sprintf("%s%s  %s  %s", marker, info.Modified.Local().Format("Jan 02 15:04"), id, title)
		if len(m.sessions.items)-first > count && i == start+count-1 {
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
	// While a background /v1/models lookup runs, reserve a line for its
	// animated indicator so the picker explains itself instead of looking
	// silently incomplete.
	extra := 0
	if m.discovering != "" {
		extra = 1
	}
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Model: " + m.models.query)}
	count := max(0, min(height-1-extra, len(m.models.matched)))
	if count == 0 {
		if extra > 0 {
			return strings.Join(append(lines, m.discoveryLine()), "\n")
		}
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
	if extra > 0 {
		lines = append(lines, m.discoveryLine())
	}
	return strings.Join(lines, "\n")
}

// discoveryLine renders the animated in-picker indicator for a running
// /v1/models lookup.
func (m *model) discoveryLine() string {
	frame := m.th.spinner.Render(spinnerFrames[m.spinnerFrame])
	return m.th.muted.Render(fmt.Sprintf("%s Checking %s for models…", frame, m.discovering))
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

// statusLine is the compact turn-status strip between scrollback and the
// composer. Its left and right segments mirror the reference activity/timer
// split while retaining myagent's existing status messages and usage counters.
func (m *model) statusLine() string {
	if m.working {
		frame := m.th.spinner.Render(spinnerFrames[m.spinnerFrame])
		elapsed := time.Since(m.startedAt).Seconds()
		msg := "Working…"
		if m.statusMsg != "" {
			msg = m.statusMsg
		}
		left := frame + " " + m.th.muted.Render(msg)
		right := m.th.muted.Render(fmt.Sprintf("%.1fs", elapsed))
		if m.usage.Output > 0 {
			right += m.th.muted.Render(" · ↓" + compact(m.usage.Output))
		}
		right += "  " + m.th.errorText.Render("[esc stop]")
		if m.width <= 0 {
			return left + " " + right
		}
		return renderStatusTriplet(left, "", right, max(1, m.width))
	}
	if m.statusMsg != "" {
		return m.th.muted.Render(truncateColumns(m.statusMsg, max(1, m.width)))
	}
	return ""
}

// footer renders the session label and token/cost stats line. Repository
// location belongs in the top status bar, so it is not duplicated here.
func (m *model) footer() string {
	leftText := "new session"
	if m.hasSessionTitle {
		leftText = m.sessionTitle
	}
	left := m.th.footer.Render(leftText)
	right := m.th.footerRight.Render("alt+enter steer")
	line1 := padBetween(left, right, m.width)

	stats := fmt.Sprintf("↑%s ↓%s R%s W%s $%.4f",
		compact(m.usage.Input), compact(m.usage.Output),
		compact(m.usage.CacheRead), compact(m.usage.CacheWrite),
		m.usage.Cost.Total)
	line2 := m.th.footer.Render(stats)
	return line1 + "\n" + line2
}

func renderStatusTriplet(left, center, right string, width int) string {
	if width <= 0 {
		return ""
	}
	if center == "" {
		return truncateColumns(padBetween(left, right, width), width)
	}
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	centerWidth := lipgloss.Width(center)
	centerStart := max(leftWidth+1, (width-centerWidth)/2)
	if centerStart+centerWidth+rightWidth+1 > width {
		return truncateColumns(padBetween(left, right, width), width)
	}
	line := left + strings.Repeat(" ", max(1, centerStart-leftWidth)) + center
	line += strings.Repeat(" ", max(1, width-lipgloss.Width(line)-rightWidth)) + right
	return truncateColumns(line, width)
}

func renderRegion(content string, rect tuiRect) string {
	if rect.empty() {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	if content == "" {
		lines = []string{""}
	}
	if len(lines) > rect.Height {
		lines = lines[:rect.Height]
	}
	for len(lines) < rect.Height {
		lines = append(lines, "")
	}
	for i, line := range lines {
		line = truncateColumns(line, rect.Width)
		padding := rect.Width - lipgloss.Width(line)
		if padding > 0 {
			line += strings.Repeat(" ", padding)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
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
