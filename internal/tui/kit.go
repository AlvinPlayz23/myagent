package tui

import (
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// uiEnvVar selects the shell for the interactive TUI. Since the S5 cutover the
// retained kit shell is the default: unset and every value except the exact
// escape hatch "legacy" select it. MYAGENT_UI=legacy keeps the pre-cutover
// frame renderer byte for byte for one release.
const uiEnvVar = "MYAGENT_UI"

// UIKitEnabled reports whether the retained kit shell should run. It is true by
// default; only the exact value "legacy" opts out. "kit" remains accepted as a
// no-op alias so existing launch scripts keep working.
func UIKitEnabled() bool { return os.Getenv(uiEnvVar) != "legacy" }

// KitBlock is one visible transcript run: either a real block (cached through
// transcript.renderBlock) or a folded read summary. The kit never copies the
// block cache or its invalidation rules.
type KitBlock interface {
	// Render returns the run's authoritative cached paint at width.
	Render(width int) string
	// Rev is the transcript revision that produced the paint.
	Rev() int
}

// KitBlockKind is the optional run metadata extension. The shell uses it to
// attach hover treatment to tool cards without making Kind a required method
// on KitBlock (fake/test blocks keep the two-method contract).
type KitBlockKind interface {
	KitKind() string
}

// KitTab is one chip in the session rail. The app shell hides the rail until
// two tabs exist (§4), so an empty slice keeps the S3 geometry.
type KitTab struct {
	ID     string
	Title  string
	Busy   bool
	Unread bool
}

// KitStat is one sidebar metadata row. Alert paints the value in the error
// feedback role.
type KitStat struct {
	Label string
	Value string
	Alert bool
}

// KitTodoStatus mirrors the four-state todo grammar without importing kit.
type KitTodoStatus uint8

const (
	KitTodoPending KitTodoStatus = iota
	KitTodoActive
	KitTodoDone
	KitTodoCancelled
)

// KitTodo is one todo row supplied by the host.
type KitTodo struct {
	Text   string
	Status KitTodoStatus
}

// KitFooter is the composer status row state (§3.0.D: left spinner/cwd, right
// file-context chip).
type KitFooter struct {
	Busy        bool
	Cwd         string
	File        string
	FilePending bool
	Hint        string
}

// KitNoticeSeverity selects the toast tint for a status line.
type KitNoticeSeverity uint8

const (
	KitNoticeInfo KitNoticeSeverity = iota
	KitNoticeSuccess
	KitNoticeWarning
	KitNoticeError
)

// KitNotice is the one status line promoted to a toast. An empty Text clears
// the emitter so an identical message can show again later.
type KitNotice struct {
	Text     string
	Severity KitNoticeSeverity
}

// KitPickerMode selects the dialog body the shell should build. Select keeps
// the S4 fuzzy DialogSelect; the other modes render host-authoritative list,
// input, or confirm bodies whose keys all round-trip through KitKey.
type KitPickerMode uint8

const (
	// KitPickerSelect is the zero value so the S4 models/commands projection
	// keeps its existing shell path without an explicit assignment.
	KitPickerSelect KitPickerMode = iota
	KitPickerList
	KitPickerInput
	KitPickerConfirm
)

// KitPickerItem is one dialog-select/list row bridged from an existing picker.
type KitPickerItem struct {
	ID     string
	Title  string
	Desc   string
	Group  string
	Footer string
	// Header marks a non-selectable grouping row (History, etc.).
	Header bool
}

// KitPickerState is the neutral projection of a legacy panel. The shell owns
// no panel logic; accept/cancel run the legacy paths.
type KitPickerState struct {
	Kind        string
	Mode        KitPickerMode
	Title       string
	Placeholder string
	Query       string
	SelectedID  string
	Items       []KitPickerItem
	// Body is the confirm-mode message.
	Body string
	// Input is the already-rendered input line (masked by the legacy widget
	// when Masked is true).
	Input  string
	Masked bool
	// Hint is the muted secondary line/instruction.
	Hint string
	// Footer labels. Empty values fall back to cancel/confirm/enter.
	ConfirmLabel string
	ConfirmKey   string
	CancelLabel  string
}

// KitDockRow is one composer-adjacent dock row (image attachments, queued
// follow-ups). The dock is passive: rows are painted, not clicked.
type KitDockRow struct {
	Label string
	Text  string
	Hint  string
}

// KitDock is the bottom-stack projection above the composer (§3.0.D).
type KitDock struct {
	Rows []KitDockRow
}

// KitDockHost is an optional host extension so existing KitHost implementations
// and their goldens stay source-compatible.
type KitDockHost interface {
	KitDock() KitDock
}

// KitHost is the narrow model-side surface the kit shell consumes. It keeps
// internal/ui/app free of unexported tui state while reusing the existing
// transcript, composer, and selection implementations.
type KitHost interface {
	KitShowWelcome() bool
	KitWelcome(width, height int) string
	KitBlocks() []KitBlock
	KitTranscriptRev() int

	KitComposer(width int) string
	KitComposerHeight() int
	KitComposerEmpty() bool
	KitComposerRev() int
	KitKey(ev tea.KeyPressMsg) tea.Cmd

	KitMouseClick(row, col int, mod tea.KeyMod) tea.Cmd
	KitMouseMotion(row, col int, button tea.MouseButton) tea.Cmd
	KitMouseRelease(row, col int) tea.Cmd
	KitOverlay(content string, startRow int) string

	// KitSetSize publishes the transcript content width and viewport height.
	// A width change invalidates the transcript through its existing path.
	KitSetSize(contentWidth, viewportHeight int)

	// S4 chrome surface.
	KitTabs() ([]KitTab, string)
	KitSelectTab(id string) tea.Cmd
	KitSidebar() (string, []KitStat)
	KitTodos() []KitTodo
	KitFooter() KitFooter
	KitNotice() KitNotice

	// S4 picker bridge. KitPicker reports the active legacy picker (models or
	// commands); accept/cancel reuse the same close/apply paths as the keys.
	KitPicker() (KitPickerState, bool)
	KitPickerAccept(id string) tea.Cmd
	KitPickerCancel() tea.Cmd
}

// KitUI is the app-shell surface the model drives. The implementation lives in
// internal/ui/app because kit imports tui for its theme aliases; tui cannot
// import kit without a cycle.
type KitUI interface {
	Render(width, height int) string
	Key(ev tea.KeyPressMsg) bool
	Mouse(ev tea.MouseMsg) bool
	Resize(width, height int)
	Sync()
	ComposeCount() int
	TakeCmd() tea.Cmd
}

// KitUI factory registration. The app package registers New in init; main
// blank-imports it so the production binary always has the default kit shell.
// SetKitUIFactory(nil) is the test-only way to force the legacy fallback.
type kitUIFactoryFunc func(KitHost) KitUI

var kitFactory kitUIFactoryFunc

// SetKitUIFactory installs the shell constructor. Passing nil disables kit
// activation even when the environment selects it (tests and minimal hosts),
// which falls back to the legacy renderer.
func SetKitUIFactory(f func(KitHost) KitUI) { kitFactory = f }

// initKit activates the retained shell unless MYAGENT_UI=legacy or no factory
// is registered. The legacy View path stays untouched when m.kit is nil.
func (m *model) initKit() {
	if !UIKitEnabled() || kitFactory == nil {
		return
	}
	m.kit = kitFactory(m)
}

// KitTranscriptRev reports the transcript mutation revision. Sync uses it to
// mark the shell dirty exactly once per event batch.
func (m *model) KitTranscriptRev() int { return m.transcript.rev }

// KitShowWelcome reports the transient empty-session state.
func (m *model) KitShowWelcome() bool { return m.showWelcome() }

// KitWelcome renders the welcome surface at the transcript content width and
// height. The existing welcome layout reads model.width; the temporary swap
// keeps one implementation for both shells.
func (m *model) KitWelcome(width, height int) string {
	oldWidth := m.width
	oldViewport := m.kitViewportH
	if width > 0 {
		m.width = width
	}
	m.kitViewportH = max(0, height)
	out := m.renderWelcome()
	m.width = oldWidth
	m.kitViewportH = oldViewport
	return out
}

// KitBlocks mirrors transcript.render's visible block sequence, including
// folded read runs and hidden thinking blocks.
func (m *model) KitBlocks() []KitBlock {
	t := m.transcript
	out := make([]KitBlock, 0, len(t.blocks))
	for i := 0; i < len(t.blocks); i++ {
		b := t.blocks[i]
		if b.kind == blockThinking && !t.showThinking {
			continue
		}
		if fold := t.foldReads(i); !t.expanded && fold != nil {
			out = append(out, &kitFoldRun{t: t, fold: fold})
			i = fold.end
			continue
		}
		out = append(out, &kitBlockRun{t: t, b: b})
	}
	return out
}

type kitBlockRun struct {
	t *transcript
	b *block
}

func (r *kitBlockRun) Render(width int) string { return r.t.renderBlock(r.b, width) }
func (r *kitBlockRun) Rev() int                { return r.t.rev }

// KitKind names the block for optional shell chrome (tool hover, thinking).
func (r *kitBlockRun) KitKind() string {
	switch r.b.kind {
	case blockUser:
		return "user"
	case blockAssistant:
		return "assistant"
	case blockTool:
		return "tool"
	case blockError:
		return "error"
	case blockNotice:
		return "notice"
	case blockThinking:
		return "thinking"
	default:
		return "block"
	}
}

type kitFoldRun struct {
	t    *transcript
	fold *readFold
}

func (r *kitFoldRun) Render(int) string { return r.t.renderFoldedReads(r.fold) }
func (r *kitFoldRun) Rev() int          { return r.t.rev }

// KitKind marks a folded read run as tools for the hover treatment.
func (r *kitFoldRun) KitKind() string { return "tool" }

// KitComposer renders the existing textarea chrome at the shell width.
func (m *model) KitComposer(width int) string { return m.renderComposerAt(width) }

// KitComposerHeight is the current composer block height.
func (m *model) KitComposerHeight() int { return m.composerHeight() }

// KitComposerEmpty reports whether the prompt is empty (Home/End scrolling is
// only active then, matching the legacy binding).
func (m *model) KitComposerEmpty() bool { return m.input.Value() == "" }

// KitComposerRev reports composer/content revision for the kit shell's dirty
// tracking. Idle ticks do not bump it; key and paste events do.
func (m *model) KitComposerRev() int { return m.kitComposerRev }

// KitKey feeds a key through the legacy onKey path and returns its command.
func (m *model) KitKey(ev tea.KeyPressMsg) tea.Cmd {
	m.kitComposerRev++
	_, cmd := m.onKey(ev)
	return cmd
}

// KitSetSize records the kit transcript geometry. Width invalidation stays on
// the transcript's existing invalidate path.
func (m *model) KitSetSize(contentWidth, viewportHeight int) {
	widthChanged := contentWidth > 0 && contentWidth != m.kitWidth
	m.kitWidth = contentWidth
	m.kitHeight = max(0, viewportHeight)
	if widthChanged {
		m.transcript.invalidate()
	}
}

// KitTabs exposes the session rail. myagent has no multi-session store yet, so
// the rail stays hidden until such a source exists (§4 "hidden until multiple
// sessions"); the kit widget and shell wiring are exercised in tests.
func (m *model) KitTabs() ([]KitTab, string) { return nil, "" }

// KitSelectTab is a no-op until the model owns more than one open session.
func (m *model) KitSelectTab(string) tea.Cmd { return nil }

// KitSidebar projects the footer's usage data into the right-hand rail.
func (m *model) KitSidebar() (string, []KitStat) {
	rows := []KitStat{
		{Label: "model", Value: m.modelID},
		{Label: "effort", Value: m.effortLabel()},
		{Label: "tokens", Value: "↑" + compact(m.usage.Input) + " ↓" + compact(m.usage.Output)},
		{Label: "cache", Value: "R" + compact(m.usage.CacheRead) + " W" + compact(m.usage.CacheWrite)},
		{Label: "cost", Value: fmt.Sprintf("$%.4f", m.usage.Cost.Total)},
	}
	if window := m.contextWindow(); window > 0 && m.lastInput > 0 {
		pct := min(100, m.lastInput*100/window)
		rows = append(rows, KitStat{
			Label: "context",
			Value: fmt.Sprintf("%d%%", pct),
			Alert: pct >= 70,
		})
	}
	return "Usage", rows
}

// KitTodos returns nil: myagent has no todo event source yet. The TodoBlock
// widget and shell wiring are covered by tests and light up when one lands.
func (m *model) KitTodos() []KitTodo { return nil }

// KitFooter reports the composer status row: busy state, cwd, and the active
// file-context chip (the mention being picked, else the first mention).
func (m *model) KitFooter() KitFooter {
	f := KitFooter{Busy: m.working, Cwd: m.cwd}
	if m.files.active {
		if path, ok := m.files.selected(); ok {
			f.File, f.FilePending = path, true
		}
	}
	if f.File == "" {
		if mentions := mentionedPaths(m.input.Value()); len(mentions) > 0 {
			f.File = mentions[0]
		}
	}
	return f
}

// KitNotice promotes the current status line to a toast. Severity is inferred
// from the existing status vocabulary; the host stays free of kit types.
func (m *model) KitNotice() KitNotice {
	text := strings.TrimSpace(m.statusMsg)
	if text == "" {
		return KitNotice{}
	}
	lower := strings.ToLower(text)
	severity := KitNoticeInfo
	switch {
	case strings.Contains(lower, "could not"), strings.Contains(lower, "unable"),
		strings.Contains(lower, "failed"), strings.Contains(lower, "error"):
		severity = KitNoticeError
	case strings.Contains(lower, "cancel"), strings.Contains(lower, "aborting"):
		severity = KitNoticeWarning
	case strings.Contains(lower, "copied"), strings.Contains(lower, "saved"),
		strings.Contains(lower, "configured"), strings.Contains(lower, "set to"),
		strings.Contains(lower, "started"), strings.Contains(lower, "resumed"),
		strings.Contains(lower, "exported"), strings.Contains(lower, "renamed"):
		severity = KitNoticeSuccess
	}
	return KitNotice{Text: text, Severity: severity}
}

// KitMouseClick starts a transcript selection at (row, col). The shell's block
// widgets translate their absolute bounds into content coordinates. It mirrors
// legacy onMouseClick: multi-click state resets on a fresh press, and
// Ctrl+click opens assistant-block URLs without starting a selection.
func (m *model) KitMouseClick(row, col int, mod tea.KeyMod) tea.Cmd {
	if m.showWelcome() {
		_, cmd := m.onWelcomeClick(row)
		return cmd
	}
	m.keepSelection = false
	if mod&tea.ModCtrl != 0 {
		m.ctrlClickURLAt(textPoint{row: row, col: col}, m.kitWidth)
		return nil
	}
	m.trackClick(row)
	point := textPoint{row: row, col: col}
	m.selection = &textSelection{anchor: point, current: point}
	return nil
}

// KitMouseMotion extends an active drag selection.
func (m *model) KitMouseMotion(row, col int, button tea.MouseButton) tea.Cmd {
	if button != tea.MouseLeft || m.selection == nil {
		return nil
	}
	point := textPoint{row: row, col: col}
	m.selection.current = point
	m.selection.dragged = point != m.selection.anchor
	return nil
}

// KitMouseRelease copies the rendered selection through the existing helpers,
// then applies legacy multi-click parity: double-click expands to a word,
// triple-click to a paragraph, and the highlight persists until the next press.
func (m *model) KitMouseRelease(row, col int) tea.Cmd {
	if m.selection == nil {
		return nil
	}
	point := textPoint{row: row, col: col}
	m.selection.current = point
	m.selection.dragged = m.selection.dragged || point != m.selection.anchor
	width := m.kitWidth
	if width <= 0 {
		width = m.width
	}
	content := m.transcript.render(width)
	selection := *m.selection
	if !selection.dragged {
		lines := strings.Split(ansi.Strip(content), "\n")
		switch {
		case m.clickCount >= 3:
			selection.anchor, selection.current = expandParagraph(lines, selection.anchor.row)
			selection.dragged = true
		case m.clickCount == 2:
			selection.anchor, selection.current = expandWord(lines, selection.anchor.row, selection.anchor.col)
			selection.dragged = selection.current != selection.anchor
		}
	}
	text := selectedRenderedText(content, selection)
	if text == "" {
		m.selection = nil
		return nil
	}
	if err := m.clipboardWrite(text); err != nil {
		m.statusMsg = "Could not copy selection: " + err.Error()
		m.selection = nil
		return nil
	}
	if !selection.dragged {
		m.selection = nil
		return nil
	}
	if m.clickCount >= 2 {
		m.keepSelection = true
		m.selection = &selection
		return m.pushToast(fmt.Sprintf("Copied %d characters (selection kept).", len([]rune(text))))
	}
	m.selection = nil
	return m.pushToast(fmt.Sprintf("Copied %d characters.", len([]rune(text))))
}

// KitOverlay clips the active selection to one block's local rows and applies
// selection.go's renderer without disturbing cells outside the range.
func (m *model) KitOverlay(content string, startRow int) string {
	if m.selection == nil || !m.selection.dragged {
		return content
	}
	start, end := normalizeSelection(*m.selection)
	lines := strings.Count(content, "\n") + 1
	if end.row < startRow || start.row >= startRow+lines {
		return content
	}
	local := textSelection{dragged: true}
	if start.row < startRow {
		local.anchor = textPoint{row: 0, col: 0}
	} else {
		local.anchor = textPoint{row: start.row - startRow, col: start.col}
	}
	const maxCol = int(^uint(0) >> 1)
	if end.row >= startRow+lines {
		local.current = textPoint{row: lines - 1, col: maxCol}
	} else {
		local.current = textPoint{row: end.row - startRow, col: end.col}
	}
	return renderTextSelection(content, &local, m.th.selection)
}

// renderComposerAt is the width-aware composer render shared by the legacy
// viewport path and the kit shell.
func (m *model) renderComposerAt(width int) string {
	if m.promptStyle != promptRuled {
		box := m.th.composerBox
		if m.input.Focused() {
			box = m.th.composerFocused
		}
		return box.Width(max(1, width)).Render(m.input.View())
	}
	rule := m.th.composerRule.Render(strings.Repeat("─", max(1, width)))
	return rule + "\n" + m.input.View() + "\n" + rule
}

// strippedLines is used by kit tests that need the plain content height.
func strippedLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(ansi.Strip(s), "\n") + 1
}
