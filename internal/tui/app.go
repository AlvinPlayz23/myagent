package tui

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/export"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/session"
	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// chromeHeight counts the status line + footer rows under the scrollback.
const chromeHeight = 6

// tickRate drives the spinner/shimmer animations and elapsed timers.
const tickRate = 66 * time.Millisecond

// escDoublePress is the window in which a second Esc clears the prompt.
const escDoublePress = 800 * time.Millisecond

// loopEvent is the union of async inputs the loop reacts to.
type loopEvent struct {
	agent     *agentEventEnvelope
	title     *agentTitleEvent
	done      *agentDoneEvent
	discovery *discoveryResult
	clipboard *clipboardResult
}

type agentEventEnvelope struct {
	ev         types.AgentEvent
	generation uint64
}

type agentDoneEvent struct {
	err        error
	generation uint64
}

type agentTitleEvent struct {
	title      string
	generation uint64
}

type discoveryResult struct {
	provider string
	models   []string
	err      error
}

// app is the pager application: the engine terminal, the event loop, and all
// UI state (the pager's AppView + AgentView).
type app struct {
	ctx        context.Context
	cfg        agent.Config
	r          *runner
	q          *msgQueue
	th         *theme
	term       *engine.Terminal
	screen     *engine.Screen
	input      <-chan engine.Event
	resizeCh   chan os.Signal
	loopCh     chan loopEvent
	agentCh    chan agentEventEnvelope
	start      time.Time
	generation uint64

	w, h int
	quit bool

	// Screens.
	welcome    bool
	welcomeSel int
	sbMode     bool // tab-focus: scrollback keyboard mode

	// Agent view state.
	sb          *scrollback
	prompt      *promptWidget
	att         *imageAttachments
	promptSty   promptStyle
	welcomeSty  welcomeStyle
	multilineOn bool

	// Pickers / modals.
	modalKind      modalKind
	modalInput     string
	modalOverwrite bool
	picker         commandPicker
	files          filePicker
	models         modelPicker
	sessions       sessionPicker
	effort         effortPicker
	providers      providerPicker
	exportPick     exportPicker
	customizeSel   int

	// Run state.
	working        bool
	cancel         context.CancelFunc
	abortRequested bool
	activePrompt   *types.Message
	startedAt      time.Time
	spinnerFrame   int
	statusMsg      string
	lastErr        error
	usage          types.Usage

	// Session metadata.
	sessionTitle    string
	hasSessionTitle bool
	modelID         string
	cwd             string
	gitBranch       string
	discovering     string
	exportFormat    export.Format
	escAt           time.Time

	// Queued prompts echoed beside the composer.
	queuedFollowUps []queuedMessage
	queuedSteering  []types.Message

	// Wiring injected by Run, mirroring the previous model's seams.
	newSession         func() error
	saveWelcomeStyle   func(welcomeStyle) error
	savePromptStyle    func(promptStyle) error
	setTerminalTitle   func(string)
	availableModels    func() []modelcatalog.Model
	discoverModels     func(context.Context, string) ([]string, error)
	availableProviders func() []modelcatalog.Provider
	providerConfigured func(string) bool
	providerIsCustom   func(string) bool
	providerAPIKey     func(string) string
	configureProvider  func(modelcatalog.Provider, string) error
	selectModel        func(string, string) (llm.Provider, llm.Model, error)
	listSessions       func() ([]session.Info, error)
	currentSessionID   func() string
	resumeSession      func(string) ([]types.Message, error)
	exportSession      func(export.Format, string, bool) (string, error)
	renameSession      func(string) error
	clipboardRead      func() (clipboardPayload, error)

	keyFor modelcatalog.Provider
}

// newApp assembles the app over the given agent config and history.
func newApp(ctx context.Context, cfg agent.Config, history []types.Message, modelID, cwd string) *app {
	queue := newMsgQueue()
	r := newRunner(cfg, queue, history)
	a := &app{
		ctx:          ctx,
		cfg:          cfg,
		r:            r,
		q:            queue,
		th:           currentTheme(),
		loopCh:       make(chan loopEvent, 256),
		agentCh:      make(chan agentEventEnvelope, 1024),
		start:        time.Now(),
		sb:           newScrollback(),
		prompt:       newPromptWidget(),
		att:          newImageAttachments(),
		modelID:      modelID,
		cwd:          cwd,
		welcome:      true,
		promptSty:    promptDefault,
		welcomeSty:   welcomeDefault,
		spinnerFrame: 0,
	}
	a.picker = newCommandPicker()
	a.effort = newEffortPicker("")
	return a
}

// loop is the main event loop: render, wait for one event, dispatch.
func (a *app) loop() error {
	tick := time.NewTicker(tickRate)
	defer tick.Stop()
	a.onResize()
	for !a.quit {
		a.render()
		select {
		case ev := <-a.input:
			a.handleInput(ev)
		case env := <-a.agentCh:
			a.dispatchAgent(env)
		case le := <-a.loopCh:
			a.dispatchLoop(le)
		case <-a.resizeCh:
			a.onResize()
		case <-tick.C:
			a.spinnerFrame = (a.spinnerFrame + 1) % len(spinnerFrames)
		}
	}
	return nil
}

// dispatchLoop routes runner completions, titles, and background results.
func (a *app) dispatchLoop(le loopEvent) {
	switch {
	case le.done != nil:
		a.finishRun(le.done.err)
	case le.title != nil:
		if le.title.generation == a.generation || le.title.generation == a.r.generation {
			a.setSessionTitle(le.title.title)
			a.statusMsg = "Session title: " + le.title.title
		}
	case le.discovery != nil:
		a.onDiscovered(le.discovery.provider, le.discovery.models, le.discovery.err)
	case le.clipboard != nil:
		a.onClipboard(le.clipboard.payload, le.clipboard.err)
	}
}

// onResize re-reads the terminal size and invalidates layout caches.
func (a *app) onResize() {
	w, h := termSize(a.term)
	if w <= 0 || h <= 0 {
		return
	}
	if a.screen == nil || a.screen.W != w || a.screen.H != h {
		a.screen = engine.NewScreen(w, h)
		if a.term != nil {
			a.term.Resize(w, h)
		}
	}
	a.w, a.h = w, h
	a.sb.invalidate()
}

// render paints one full frame; the engine diffs it onto the tty.
func (a *app) render() {
	if a.screen == nil {
		return
	}
	scr := a.screen
	scr.CursorVisible = false
	th := a.th

	if a.welcome {
		scr.Fill(engine.Rect{X: 0, Y: 0, W: a.w, H: a.h}, engine.Style{}.WithBg(th.BGTerminal))
		a.welcomeRender(scr, engine.Rect{X: 0, Y: 0, W: a.w, H: a.h}, a.welcomeSel, time.Now())
		if a.modalKind != modalNone {
			a.renderModal(scr, a.w, a.h)
		}
		if a.term != nil {
			a.term.Flush(scr, -1, -1, false)
		}
		return
	}

	scr.Fill(engine.Rect{X: 0, Y: 0, W: a.w, H: a.h}, engine.Style{}.WithBg(th.BGBase))

	// Layout: scrollback, then status, composer stack, footer.
	footerH := 2
	statusH := 1
	composerH := a.composerHeight()
	dropdownH := a.dropdownHeight()
	sbH := a.h - footerH - statusH - composerH - dropdownH
	if sbH < 1 {
		sbH = 1
	}

	sbArea := engine.Rect{X: 0, Y: 0, W: a.w, H: sbH}
	a.sb.render(scr, sbArea)

	y := sbH
	// Dropdown overlays sit directly above the composer.
	if dropdownH > 0 {
		a.renderDropdown(engine.Rect{X: 0, Y: y, W: a.w, H: dropdownH})
		y += dropdownH
	}
	a.renderStatusLine(engine.Rect{X: 0, Y: y, W: a.w, H: 1})
	y++
	a.renderComposer(engine.Rect{X: 0, Y: y, W: a.w, H: composerH})
	y += composerH
	a.renderFooter(engine.Rect{X: 0, Y: y, W: a.w, H: footerH})

	if a.modalKind != modalNone {
		a.renderModal(scr, a.w, a.h)
	}

	if a.term != nil {
		if scr.CursorVisible {
			a.term.Flush(scr, scr.CursorX, scr.CursorY, true)
		} else {
			a.term.Flush(scr, -1, -1, false)
		}
	}
}

// composerHeight computes the composer stack height: border, editor rows,
// attachment row, and queued follow-up rows.
func (a *app) composerHeight() int {
	rows := len(a.prompt.lines)
	if a.prompt.value() == "" {
		rows = 1
	}
	maxRows := 8
	if a.promptSty == promptRuled {
		maxRows = 6
	}
	if rows > maxRows {
		rows = maxRows
	}
	h := rows + 2 // border
	if a.promptSty == promptRuled {
		h = rows + 2 // rules above/below
	}
	if a.att.len() > 0 {
		h++
	}
	h += len(a.queuedFollowUps)
	return h
}

// dropdownHeight reports the completion dropdown height (0 = hidden).
func (a *app) dropdownHeight() int {
	if a.files.active {
		return a.files.height() + 2
	}
	if a.picker.active {
		return a.picker.height() + 1
	}
	return 0
}

// renderDropdown paints the @ file / / command dropdown above the composer.
func (a *app) renderDropdown(area engine.Rect) {
	th := a.th
	border := engine.Style{}.WithFg(th.PromptBorder).WithBg(th.BGBase)
	selSt := engine.Style{}.WithFg(th.FG).WithBg(th.BGHighlight).Bold()
	itemSt := engine.Style{}.WithFg(th.FGDark).WithBg(th.BGBase)

	y := area.Y
	w := area.W
	if a.files.active {
		for c := 0; c < w; c++ {
			a.screen.SetCell(c, y, engine.Cell{Ch: '─', Width: 1, Style: border})
		}
		y++
		count := a.files.height()
		start := a.filesStart(count)
		for i := start; i < start+count && y < area.Y+area.H; i, y = i+1, y+1 {
			path := a.files.items[a.files.matched[i]]
			st := itemSt
			if i == a.files.sel {
				st = selSt
				path = "› " + path
			} else {
				path = "  " + path
			}
			a.screen.SetString(0, y, path, st)
		}
		return
	}
	if a.picker.active {
		count := a.picker.height()
		start, end := a.picker.visibleRange(count)
		for i := start; i < end && y < area.Y+area.H; i, y = i+1, y+1 {
			item := a.picker.items[a.picker.matched[i]]
			st := itemSt
			text := "  " + item.name + "  " + item.description
			if i == a.picker.sel {
				st = selSt
				text = "› " + item.name + "  " + item.description
			}
			a.screen.SetString(0, y, text, st)
		}
	}
}

// renderStatusLine paints the spinner / transient status row.
func (a *app) renderStatusLine(area engine.Rect) {
	line := a.statusSpans()
	if len(line) == 0 {
		return
	}
	a.screen.Line(area.X+1, area.Y, line)
}

// statusSpans builds the status row: working spinner or transient message.
func (a *app) statusSpans() []engine.Span {
	th := a.th
	if a.working {
		frame := spinnerFrames[a.spinnerFrame]
		st := engine.Style{}.WithFg(th.AccentRunning).WithBg(th.BGBase)
		dim := engine.Style{}.WithFg(th.Comment).WithBg(th.BGBase)
		elapsed := time.Since(a.startedAt).Seconds()
		msg := "Working…"
		if a.statusMsg != "" {
			msg = a.statusMsg
		}
		return []engine.Span{
			{Text: frame + " ", Style: st},
			{Text: fmt.Sprintf("%s (%.1fs, esc to cancel)", msg, elapsed), Style: dim},
		}
	}
	if a.statusMsg != "" {
		return []engine.Span{{Text: a.statusMsg, Style: th.muted()}}
	}
	return nil
}

// renderComposer paints the composer stack: attachments, border + editor,
// and queued follow-up rows.
func (a *app) renderComposer(area engine.Rect) {
	th := a.th
	y := area.Y

	// Attachment row.
	if a.att.len() > 0 {
		a.screen.SetString(area.X+2, y, a.att.summary(a.w-4), th.muted())
		y++
	}

	// Queued follow-ups echo under the composer, like the old "↳ next" rows.
	for _, q := range a.queuedFollowUps {
		a.screen.SetString(area.X+2, y, "↳ next: "+firstLineOf(q.display), th.muted())
		y++
	}

	editorH := area.Y + area.H - y

	if a.promptSty == promptRuled {
		rule := engine.Style{}.WithFg(th.PromptBorder).WithBg(th.BGBase)
		for c := 0; c < area.W; c++ {
			a.screen.SetCell(c, y, engine.Cell{Ch: '─', Width: 1, Style: rule})
			a.screen.SetCell(c, y+editorH-1, engine.Cell{Ch: '─', Width: 1, Style: rule})
		}
		marker := engine.Style{}.WithFg(th.GrayBright).WithBg(th.BGBase)
		a.screen.SetString(area.X+2, y+1, "› ", marker)
		a.prompt.render(a.screen, engine.Rect{X: area.X + 4, Y: y + 1, W: area.W - 6, H: editorH - 2}, th)
		return
	}

	// Grok-style bordered composer: dim border, brighter while focused.
	borderColor := th.PromptBorder
	if a.sbMode {
		borderColor = th.PromptBorder
	} else {
		borderColor = th.PromptActive
	}
	bSt := engine.Style{}.WithFg(borderColor).WithBg(th.BGBase)
	for c := 0; c < area.W; c++ {
		a.screen.SetCell(c, y, engine.Cell{Ch: '─', Width: 1, Style: bSt})
		a.screen.SetCell(c, y+editorH-1, engine.Cell{Ch: '─', Width: 1, Style: bSt})
	}
	for r := y + 1; r < y+editorH-1; r++ {
		a.screen.SetCell(0, r, engine.Cell{Ch: '│', Width: 1, Style: bSt})
		a.screen.SetCell(area.W-1, r, engine.Cell{Ch: '│', Width: 1, Style: bSt})
	}
	a.screen.SetCell(0, y, engine.Cell{Ch: '╭', Width: 1, Style: bSt})
	a.screen.SetCell(area.W-1, y, engine.Cell{Ch: '╮', Width: 1, Style: bSt})
	a.screen.SetCell(0, y+editorH-1, engine.Cell{Ch: '╰', Width: 1, Style: bSt})
	a.screen.SetCell(area.W-1, y+editorH-1, engine.Cell{Ch: '╯', Width: 1, Style: bSt})

	a.prompt.render(a.screen, engine.Rect{X: area.X + 2, Y: y + 1, W: area.W - 4, H: editorH - 2}, th)
}

// renderFooter paints the cwd/model and usage/hints rows.
func (a *app) renderFooter(area engine.Rect) {
	th := a.th
	dim := engine.Style{}.WithFg(th.Comment).WithBg(th.BGBase)

	left := branchIcon() + " " + collapseHome(a.cwd)
	if a.gitBranch != "" {
		left = branchIcon() + " " + a.gitBranch + "  " + collapseHome(a.cwd)
	}
	right := a.modelID
	if a.effort.value != "" {
		right += " · " + string(a.effort.value)
	}
	a.padBetween(area.X, area.Y, area.W, left, right, dim)

	stats := fmt.Sprintf("↑%s ↓%s R%s W%s $%.4f",
		compact(a.usage.Input), compact(a.usage.Output),
		compact(a.usage.CacheRead), compact(a.usage.CacheWrite),
		a.usage.Cost.Total)
	hints := "ctrl+p palette · ctrl+o expand · tab scrollback · ctrl+v image"
	a.padBetween(area.X, area.Y+1, area.W, stats, hints, dim)
}

// padBetween paints left/right text on one footer row.
func (a *app) padBetween(x, y, w int, left, right string, st engine.Style) {
	lw := len(left)
	rw := len(right)
	gap := w - lw - rw
	if gap < 1 {
		gap = 1
	}
	a.screen.SetString(x, y, left, st)
	a.screen.SetString(x+lw+gap, y, right, st)
}
