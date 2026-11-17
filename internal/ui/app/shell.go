// Package app wires the retained kit shell into the legacy tui model. Since the
// S5 cutover it is the default frame; MYAGENT_UI=legacy keeps the old renderer
// byte for byte for one release.
package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/AlvinPlayz23/myagent/internal/ui/kit"
)

func init() { tui.SetKitUIFactory(New) }

// New builds the kit shell around the model host. The factory registration keeps
// internal/tui free of an import of kit/app (kit aliases tui tokens).
func New(host tui.KitHost) tui.KitUI { return newShell(host) }

// Shell is the vertical transcript + composer stack. It is deliberately a
// custom widget rather than kit.VBox: the composer height changes with input
// and terminal size, and S4 adds a tab rail, usage sidebar, todo block, jump
// row, and composer footer around the same two core bands.
type Shell struct {
	host     tui.KitHost
	base     *baseWidget
	root     *kit.Root
	scroll   *kit.ScrollBox
	composer *composerWidget
	welcome  *welcomeWidget
	dialogs  *kit.DialogStack
	toasts   *kit.ToastHost

	// S4 chrome. Every widget starts inert: the S3 default host state paints
	// no tabs/sidebar/todo/footer/jump, so the S3 goldens stay stable.
	tabs    *kit.Tabs
	sidebar *kit.Sidebar
	todo    *kit.TodoBlock
	footer  *kit.ComposerFooter
	jump    *kit.Jump

	width, height int
	contentW      int
	transcriptH   int
	composerH     int

	// S4 geometry, resolved by measure and committed by Layout.
	tabsH     int
	sidebarW  int
	mainW     int
	bodyH     int
	todoH     int
	jumpH     int
	footerH   int
	bodyRect  kit.Rect
	composerR kit.Rect
	footerR   kit.Rect
	todoR     kit.Rect
	jumpR     kit.Rect
	sideR     kit.Rect

	// S4 picker bridge (kit mode only; the legacy flow is untouched).
	picker               *kit.DialogSelect
	pickerLayout         *kit.DialogLayout
	pickerOpen           bool
	pickerKind           string
	pickerSig            string
	suppressPickerCancel bool

	// P6 panel bridge. Select-mode pickers keep the fields above; every other
	// panel kind uses this dialog body and forwards keys to the legacy path.
	panelList           *panelListBody
	panelInput          *panelInputBody
	panelLayout         *kit.DialogLayout
	panelFooter         *kit.DialogFooter
	panelOpen           bool
	panelKind           string
	panelMode           tui.KitPickerMode
	panelSig            string
	suppressPanelCancel bool

	// P6 dock: image attachments and queued follow-ups above the composer.
	dock  *kit.Dock
	dockH int
	dockR kit.Rect

	lastTranscriptRev int
	lastComposerRev   int
	lastWelcome       bool
	lastWelcomeRev    int
	lastContentW      int
	lastTranscriptH   int
	lastComposerH     int
	lastComposerW     int
	lastWidth         int
	lastNotice        string
	lastTick          time.Time
	haveState         bool

	cmds        []tea.Cmd
	layoutDirty bool
}

func newShell(host tui.KitHost) *Shell {
	s := &Shell{
		host:              host,
		lastTranscriptRev: -1,
		lastComposerRev:   -1,
		lastTick:          time.Now(),
	}
	s.scroll = kit.NewScrollBox()
	s.scroll.Sticky = true
	s.scroll.Start = kit.EdgeBottom
	s.composer = newComposer(s, host)
	s.welcome = newWelcome(s, host)
	s.tabs = kit.NewTabs(kit.TabsOpts{
		ID:       "kit.tabs",
		NoMotion: false,
		OnSelect: func(id string) bool {
			s.emit(s.host.KitSelectTab(id))
			return true
		},
	})
	s.sidebar = kit.NewSidebar(kit.SidebarOpts{ID: "kit.sidebar"})
	s.todo = kit.NewTodoBlock(kit.TodoBlockOpts{ID: "kit.todos"})
	s.footer = kit.NewComposerFooter(kit.ComposerFooterOpts{ID: "kit.footer", Animations: true})
	s.dock = kit.NewDock(kit.DockOpts{ID: "kit.dock"})
	s.jump = kit.NewJump(kit.JumpOpts{ID: "kit.jump", OnJump: s.scrollToEnd})
	s.jump.SetBounds(kit.Rect{})
	s.dialogs = kit.NewDialogStack()
	s.toasts = kit.NewToastHost(kit.ToastOpts{})
	s.base = &baseWidget{WidgetBase: kit.NewWidgetBase("kit.shell"), shell: s}
	s.root = kit.NewRoot(s.base,
		kit.WithToasts(s.toasts),
		kit.WithDialogs(s.dialogs),
		kit.WithInitialFocus("kit.composer"),
	)
	s.dialogs.Focus = s.root.Focus
	return s
}

// baseWidget is Root's band-0 layer. It forwards geometry/input to the shell
// without recursively calling KitUI.Render.
type baseWidget struct {
	kit.WidgetBase
	shell *Shell
}

func (b *baseWidget) Layout(r kit.Rect) {
	b.SetBounds(r)
	b.shell.Layout(r)
}
func (b *baseWidget) Render(w, h int) string { return b.shell.renderBands(w, h) }

// Key/Mouse return false: the model calls Shell.Key/Mouse directly, so Root
// must not bounce base-band input back into the shell entry points.
func (b *baseWidget) Key(ev tea.KeyPressMsg) bool            { return false }
func (b *baseWidget) Mouse(ev tea.MouseMsg, _ kit.Rect) bool { return false }
func (b *baseWidget) Children() []kit.Widget                 { return b.shell.Children() }
func (b *baseWidget) NeedsLayout() bool                      { return b.shell.NeedsLayout() }
func (b *baseWidget) FocusTarget() *kit.FocusTarget          { return nil }

// Render returns the exact full-screen frame. Root owns layout, dirty tracking,
// and the compose cache; idle frames return the cached string untouched. The
// initial focus is queued through Root, so it applies after the first layout
// discovers the composer target (no manual layout/focus pre-pass).
func (s *Shell) Render(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	return s.root.Render(w, h)
}

// Key follows the composer-first routing contract: scroll keys bubble to the
// ScrollBox; everything else goes through Root, where a modal band captures
// first and the focused composer forwards to the legacy onKey path.
func (s *Shell) Key(ev tea.KeyPressMsg) bool {
	if s.dialogs.Modal() {
		return s.root.HandleKey(ev)
	}
	if s.jump.Active() && ev.String() == "enter" && s.host.KitComposerEmpty() {
		s.scrollToEnd()
		return true
	}
	switch ev.Keystroke() {
	case "pgup", "pgdown", "ctrl+g":
		return s.scroll.Key(ev)
	case "home", "end":
		if s.host.KitComposerEmpty() {
			return s.scroll.Key(ev)
		}
	}
	return s.root.HandleKey(ev)
}

// Mouse routes through Root first so block widgets own their rows, chrome owns
// its bands, and an open dialog gates every lower-band hit. Only events that
// miss every widget fall through to the shell's content-coordinate handler.
func (s *Shell) Mouse(ev tea.MouseMsg) bool {
	if ev == nil {
		return false
	}
	if m, ok := ev.(tea.MouseMotionMsg); ok {
		if s.toasts.Active() {
			s.toasts.Pointer(m.X, m.Y)
		}
		if m.Button != tea.MouseLeft {
			s.syncHover(m.X, m.Y)
		}
	}
	if s.root.HandleMouse(ev, kit.Rect{}) {
		return true
	}
	if s.dialogs.Modal() {
		return false
	}
	m := ev.Mouse()
	if _, wheel := kit.WheelDelta(ev); wheel {
		if s.inScroll(m.X, m.Y) {
			return s.scroll.Mouse(ev, s.scroll.Bounds())
		}
		return false
	}
	switch msg := ev.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return false
		}
		if s.composerR.Contains(m.X, m.Y) || s.footerR.Contains(m.X, m.Y) {
			s.root.Focus.Focus(s.composer.FocusTarget().ID)
			return true
		}
		if !s.inScroll(m.X, m.Y) {
			return false
		}
		row, col := s.transcriptPoint(m.X, m.Y)
		s.emit(s.host.KitMouseClick(row, col, msg.Mouse().Mod))
	case tea.MouseMotionMsg:
		if msg.Mouse().Button != tea.MouseLeft || !s.inScroll(m.X, m.Y) {
			return false
		}
		row, col := s.transcriptPoint(m.X, m.Y)
		s.emit(s.host.KitMouseMotion(row, col, msg.Mouse().Button))
	case tea.MouseReleaseMsg:
		if !s.inScroll(m.X, m.Y) {
			return false
		}
		row, col := s.transcriptPoint(m.X, m.Y)
		s.emit(s.host.KitMouseRelease(row, col))
	default:
		return false
	}
	s.markBlocksDirty()
	return true
}

// inScroll reports whether a screen cell is inside the transcript viewport.
func (s *Shell) inScroll(x, y int) bool {
	return s.scroll.Bounds().Contains(x, y)
}

// scrollToEnd resumes sticky follow, the click/Enter action of the jump row.
func (s *Shell) scrollToEnd() {
	s.scroll.ScrollToEnd()
	s.markBlocksDirty()
}

// syncHover paints and clears tool-card wrappers from the pointer cell. Root
// only routes motion to the widget under the pointer, so an app-level pass is
// what makes hover leave work when the pointer crosses a block boundary.
func (s *Shell) syncHover(x, y int) {
	for _, item := range s.scroll.Items {
		h, ok := item.W.(*kit.Hoverable)
		if !ok {
			continue
		}
		h.SetHovered(h.Bounds().Contains(x, y))
	}
}

// transcriptPoint converts a screen cell inside the scroll viewport into
// transcript content coordinates.
func (s *Shell) transcriptPoint(x, y int) (row, col int) {
	b := s.scroll.Bounds()
	return s.scroll.ScrollTop() + y - b.Y, x - b.X
}

// Resize publishes the new terminal size, then syncs content and geometry.
func (s *Shell) Resize(w, h int) {
	s.width, s.height = w, h
	s.layoutDirty = true
	s.root.Invalidate()
	s.Sync()
}

// Sync refreshes host-driven chrome, rebuilds transcript items from the host
// revision, refreshes the composer, and stages a layout when content moved. The
// model calls it once per update batch, so streaming composes once.
func (s *Shell) Sync() {
	if s.width <= 0 || s.height <= 0 {
		return
	}
	now := time.Now()
	elapsed := now.Sub(s.lastTick)
	if elapsed < 0 || elapsed > 250*time.Millisecond {
		elapsed = 100 * time.Millisecond
	}
	s.lastTick = now

	welcome := s.host.KitShowWelcome()
	s.syncChrome(elapsed)
	s.syncJump(welcome)
	s.syncPicker()
	s.syncDock()
	s.emitNotice(s.host.KitNotice())

	m := s.measure(s.width, s.height)
	promptW := m.promptWidth(welcome)
	if host, ok := s.host.(interface{ KitSetComposerWidth(int) }); ok {
		host.KitSetComposerWidth(promptW)
		m = s.measure(s.width, s.height)
	}
	contentW := m.contentWidth(welcome)
	s.host.KitSetSize(contentW, m.transcriptH)
	welcomeRev := 0
	if host, ok := s.host.(interface{ KitWelcomeRev() int }); ok && welcome {
		welcomeRev = host.KitWelcomeRev()
	}
	rev := s.host.KitTranscriptRev()
	crev := s.host.KitComposerRev()

	dirty := false
	if !s.haveState ||
		welcome != s.lastWelcome ||
		(welcome && welcomeRev != s.lastWelcomeRev) ||
		rev != s.lastTranscriptRev ||
		contentW != s.lastContentW ||
		(welcome && m.transcriptH != s.lastTranscriptH) {
		s.rebuildContent(welcome, contentW, m.transcriptH)
		dirty = true
	}
	if !s.haveState ||
		crev != s.lastComposerRev ||
		s.width != s.lastWidth ||
		m.composerH != s.lastComposerH || promptW != s.lastComposerW {
		s.composer.setContent(s.host.KitComposer(promptW))
		dirty = true
	}
	if m.transcriptH != s.lastTranscriptH || m.composerH != s.lastComposerH ||
		m.tabsH != s.tabsH || m.sidebarW != s.sidebarW ||
		m.todoH != s.todoH || m.jumpH != s.jumpH || m.footerH != s.footerH ||
		m.dockH != s.dockH {
		s.layoutDirty = true
		dirty = true
	}

	s.lastTranscriptRev = rev
	s.lastComposerRev = crev
	s.lastWelcome = welcome
	s.lastWelcomeRev = welcomeRev
	s.lastContentW = contentW
	s.lastTranscriptH = m.transcriptH
	s.lastComposerH = m.composerH
	s.lastComposerW = promptW
	s.lastWidth = s.width
	s.contentW = contentW
	s.transcriptH = m.transcriptH
	s.composerH = m.composerH
	s.tabsH, s.sidebarW = m.tabsH, m.sidebarW
	s.todoH, s.jumpH, s.footerH = m.todoH, m.jumpH, m.footerH
	s.dockH = m.dockH
	s.haveState = true
	if dirty {
		s.base.MarkDirty()
	}
}

// syncChrome pulls the optional S4 rails from the host. Idle ticks only advance
// the spinner/toast clocks; widgets and the frame cache stay idle otherwise.
func (s *Shell) syncChrome(elapsed time.Duration) {
	tabs, active := s.host.KitTabs()
	s.tabs.SetItems(mapTabs(tabs), active)
	title, rows := s.host.KitSidebar()
	s.sidebar.SetStats(title, mapStats(rows))
	s.todo.SetItems(mapTodos(s.host.KitTodos()))
	f := s.host.KitFooter()
	s.footer.SetState(f.Busy, f.Cwd, f.File, f.FilePending, f.Hint)
	if s.footer.Advance(elapsed) && s.haveState {
		s.base.MarkDirty()
	}
	if s.toasts.Active() {
		s.toasts.Advance(elapsed)
	}
}

// syncJump shows the resume affordance while tail-follow is suspended.
func (s *Shell) syncJump(welcome bool) {
	s.jump.SetActive(!welcome && !s.scroll.StickyActive())
}

// syncDock pulls the optional P6 dock projection. Hosts without the extension
// (including the S3/S4 test doubles) paint no dock rows.
func (s *Shell) syncDock() {
	host, ok := s.host.(tui.KitDockHost)
	if !ok {
		s.dock.SetRows(nil)
		return
	}
	projection := host.KitDock()
	rows := make([]kit.DockRow, len(projection.Rows))
	for i, row := range projection.Rows {
		rows[i] = kit.DockRow{Label: row.Label, Text: row.Text, Hint: row.Hint}
	}
	s.dock.SetRows(rows)
}

// emitNotice promotes a changed status line to one toast. Info-level picker
// instructions are suppressed while that picker is open (the dialog already
// says what to do); errors and warnings still surface.
func (s *Shell) emitNotice(n tui.KitNotice) {
	if n.Text == "" {
		s.lastNotice = ""
		return
	}
	if n.Text == s.lastNotice {
		return
	}
	s.lastNotice = n.Text
	if (s.pickerOpen || s.panelOpen) && n.Severity == tui.KitNoticeInfo {
		return
	}
	variant := kit.ToastInfo
	switch n.Severity {
	case tui.KitNoticeSuccess:
		variant = kit.ToastSuccess
	case tui.KitNoticeWarning:
		variant = kit.ToastWarning
	case tui.KitNoticeError:
		variant = kit.ToastError
	}
	s.toasts.Show(kit.Toast{Title: "myagent", Message: n.Text, Variant: variant})
}

// mapTabs converts the host projection into kit rail items.
func mapTabs(tabs []tui.KitTab) []kit.TabItem {
	out := make([]kit.TabItem, len(tabs))
	for i, tab := range tabs {
		out[i] = kit.TabItem{ID: tab.ID, Title: tab.Title, Busy: tab.Busy, Unread: tab.Unread}
	}
	return out
}

// mapStats converts the host projection into kit rows.
func mapStats(rows []tui.KitStat) []kit.StatRow {
	out := make([]kit.StatRow, len(rows))
	for i, row := range rows {
		out[i] = kit.StatRow{Label: row.Label, Value: row.Value, Alert: row.Alert}
	}
	return out
}

// mapTodos converts the host projection into kit rows.
func mapTodos(items []tui.KitTodo) []kit.TodoItem {
	out := make([]kit.TodoItem, len(items))
	for i, item := range items {
		status := kit.TodoPending
		switch item.Status {
		case tui.KitTodoActive:
			status = kit.TodoActive
		case tui.KitTodoDone:
			status = kit.TodoDone
		case tui.KitTodoCancelled:
			status = kit.TodoCancelled
		}
		out[i] = kit.TodoItem{Text: item.Text, Status: status}
	}
	return out
}

// shellMetrics resolves the home group and session bands in terminal cells.
type shellMetrics struct {
	tabsH       int
	sidebarW    int
	mainW       int
	bodyH       int
	transcriptH int
	todoH       int
	jumpH       int
	dockH       int
	composerH   int
	footerH     int
}

func (m shellMetrics) contentWidth(welcome bool) int {
	if welcome {
		return m.promptWidth(true)
	}
	return max(0, m.mainW-2*m.inset()-1)
}

func (m shellMetrics) inset() int { return min(2, m.mainW/20) }

func (m shellMetrics) promptWidth(welcome bool) int {
	w := max(0, m.mainW-2*m.inset())
	if welcome {
		return min(75, w)
	}
	return w
}

func (s *Shell) measure(w, h int) shellMetrics {
	m := shellMetrics{}
	m.composerH = clamp(s.host.KitComposerHeight(), 0, h)
	if s.tabs.Len() > 1 {
		m.tabsH = 1
	}
	if !s.host.KitShowWelcome() && w >= 106 && s.sidebar.Len() > 0 {
		m.sidebarW = 28
	}
	m.mainW = max(0, w-m.sidebarW)
	m.bodyH = max(0, h-m.tabsH)
	if s.footer.Any() {
		m.footerH = 1
	}
	m.dockH = min(s.dock.Height(), max(0, m.bodyH-m.composerH-m.footerH))
	m.todoH = min(s.todo.Height(), max(0, m.bodyH-m.composerH-m.footerH-m.dockH))
	if s.jump.Active() {
		m.jumpH = 1
	}
	m.transcriptH = max(0, m.bodyH-m.todoH-m.jumpH-m.dockH-m.composerH-m.footerH)
	if s.host.KitShowWelcome() {
		if host, ok := s.host.(interface{ KitWelcomeHeight(int) int }); ok {
			m.transcriptH = clamp(host.KitWelcomeHeight(m.transcriptH), 0, m.transcriptH)
		} else {
			m.transcriptH = min(8, m.transcriptH)
		}
	}
	return m
}

// Layout places the chrome and core bands, then publishes each block's
// content-row origin for selection overlay and mouse translation.
func (s *Shell) Layout(r kit.Rect) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	s.syncJump(s.host.KitShowWelcome())
	m := s.measure(r.W, r.H)
	s.width, s.height = r.W, r.H
	s.tabsH, s.sidebarW, s.mainW, s.bodyH = m.tabsH, m.sidebarW, m.mainW, m.bodyH
	s.todoH, s.jumpH, s.footerH = m.todoH, m.jumpH, m.footerH
	s.dockH = m.dockH
	s.composerH, s.transcriptH = m.composerH, m.transcriptH
	s.contentW = m.contentWidth(s.host.KitShowWelcome())

	s.tabs.Layout(kit.Rect{X: r.X, Y: r.Y, W: r.W, H: m.tabsH})
	body := kit.Rect{X: r.X, Y: r.Y + m.tabsH, W: m.mainW, H: m.bodyH}
	s.bodyRect = body

	x := body.X + m.inset()
	width := m.promptWidth(s.host.KitShowWelcome())
	y := body.Y
	if s.host.KitShowWelcome() {
		x = body.X + (body.W-width)/2
		y += max(0, (body.H-m.footerH-m.transcriptH-m.dockH-m.composerH)/2)
	}
	s.scroll.Layout(kit.Rect{X: x, Y: y, W: width, H: m.transcriptH})
	y += m.transcriptH
	s.jumpR = kit.Rect{X: x, Y: y, W: width, H: m.jumpH}
	s.jump.Layout(s.jumpR)
	y += m.jumpH
	s.todoR = kit.Rect{X: x, Y: y, W: width, H: m.todoH}
	s.todo.Layout(s.todoR)
	y += m.todoH
	s.dockR = kit.Rect{X: x, Y: y, W: width, H: m.dockH}
	s.dock.Layout(s.dockR)
	y += m.dockH
	s.composerR = kit.Rect{X: x, Y: y, W: width, H: m.composerH}
	s.composer.Layout(s.composerR)
	y += m.composerH
	s.footerR = kit.Rect{X: body.X + m.inset(), Y: body.Y + body.H - m.footerH, W: max(0, body.W-2*m.inset()), H: m.footerH}
	s.footer.Layout(s.footerR)
	s.sideR = kit.Rect{X: body.X + body.W, Y: body.Y, W: m.sidebarW, H: m.bodyH}
	s.sidebar.Layout(s.sideR)

	s.host.KitSetSize(s.contentW, s.transcriptH)
	if off := s.scroll.ItemOffsets(); len(off) > 0 {
		for i, item := range s.scroll.Items {
			if b := blockWidgetOf(item.W); b != nil && i < len(off) {
				b.setContentStart(off[i])
			}
		}
	}
	s.layoutDirty = false
}

// renderBands uses the same bounds for painting and hit testing.
func (s *Shell) renderBands(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	layers := []*lipgloss.Layer{}
	for _, child := range []kit.Widget{s.scroll, s.jump, s.todo, s.dock, s.composer, s.footer, s.sidebar, s.tabs} {
		r := kit.WidgetBounds(child)
		if r.Empty() {
			continue
		}
		layers = append(layers, lipgloss.NewLayer(child.Render(r.W, r.H)).X(r.X).Y(r.Y))
	}
	return fitBlock(lipgloss.NewCompositor(layers...).Render(), w, h)
}

// NeedsLayout reports staged scroll content, a composer/chrome height change,
// or a jump-row visibility drift discovered after a scroll key.
func (s *Shell) NeedsLayout() bool {
	desired := !s.host.KitShowWelcome() && !s.scroll.StickyActive()
	return s.layoutDirty || s.scroll.NeedsLayout() || s.jump.Active() != desired
}

// rebuildContent replaces the ScrollBox items. Every block stays a child
// widget delegating to transcript.renderBlock; blank separator rows are spacer
// widgets so the content matches transcript.render's one-row gaps. Tool runs are
// wrapped in Hoverable so motion paints the raised hover surface (§3.0.D tools
// duality) without re-coloring the block's cached ANSI.
//
// S5 note: the rounded/title-in-border/traffic-light tool header restyle is not
// applied here. The block paints opaque ANSI from a cache keyed by (width,
// expand); inserting a border or title row would either rewrite that cached
// paint (breaking the legacy cache contract and selection row math) or require
// a parallel kit-only renderer. The Hoverable Surface pass is the sanctioned
// kit-mode treatment for this release.
func (s *Shell) rebuildContent(welcome bool, contentW, transcriptH int) {
	s.scroll.ShowScrollbar = !welcome
	var items []kit.ScrollItem
	if welcome {
		s.welcome.setContent(s.host.KitWelcome(contentW, transcriptH))
		items = []kit.ScrollItem{{W: s.welcome, H: 0}}
	} else {
		runs := s.host.KitBlocks()
		for i, run := range runs {
			if i > 0 {
				items = append(items, kit.ScrollItem{W: newSpacer(), H: 1})
			}
			var w kit.Widget = newBlock(s, s.host, run, i)
			if kind, ok := run.(tui.KitBlockKind); ok && kind.KitKind() == "tool" {
				w = kit.NewHoverable(kit.HoverableOpts{
					ID:      "kit.hover." + itoa(i),
					Child:   w,
					Surface: kit.DefaultTheme().Background.Raised.High,
				})
			}
			items = append(items, kit.ScrollItem{W: w, H: 0})
		}
	}
	s.scroll.SetItems(items)
}

// Children exposes the chrome + core bands for hit testing and revision walks.
func (s *Shell) Children() []kit.Widget {
	return []kit.Widget{s.tabs, s.scroll, s.jump, s.todo, s.dock, s.composer, s.footer, s.sidebar}
}

// FocusTarget is nil: the shell routes but never takes focus itself.
func (s *Shell) FocusTarget() *kit.FocusTarget { return nil }

// ComposeCount is the dirty-frame assertion surface.
func (s *Shell) ComposeCount() int { return s.root.ComposeCount() }

// TakeCmd drains the commands produced by host key/mouse handlers.
func (s *Shell) TakeCmd() tea.Cmd {
	if len(s.cmds) == 0 {
		return nil
	}
	out := s.cmds
	s.cmds = nil
	if len(out) == 1 {
		return out[0]
	}
	return tea.Batch(out...)
}

func (s *Shell) emit(cmd tea.Cmd) {
	if cmd != nil {
		s.cmds = append(s.cmds, cmd)
	}
}

func (s *Shell) markBlocksDirty() {
	for _, item := range s.scroll.Items {
		if b := blockWidgetOf(item.W); b != nil {
			b.MarkDirty()
		}
	}
	s.scroll.MarkDirty()
	s.base.MarkDirty()
}

// blockWidgetOf unwraps the optional Hoverable tool-card wrapper.
func blockWidgetOf(w kit.Widget) *blockWidget {
	switch v := w.(type) {
	case *blockWidget:
		return v
	case *kit.Hoverable:
		b, _ := v.Child.(*blockWidget)
		return b
	}
	return nil
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// fitBlock makes a string exactly w by h cells (ANSI styles preserved).
func fitBlock(content string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i := range lines {
		lw := ansi.StringWidth(lines[i])
		if lw > w {
			lines[i] = ansi.Truncate(lines[i], w, "")
			lw = ansi.StringWidth(lines[i])
		}
		if lw < w {
			lines[i] += strings.Repeat(" ", w-lw)
		}
	}
	return strings.Join(lines, "\n")
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
