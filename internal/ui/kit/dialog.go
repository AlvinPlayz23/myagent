package kit

import (
	"fmt"
	"image/color"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucasb-eyer/go-colorful"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// Modal is implemented by a band host that owns input exclusively while open.
// Root consults it instead of the S1 "any visible dialog" placeholder gate.
// OpenTUI counterpart: the dialog manager's blocking flag.
type Modal interface {
	Modal() bool
}

// Dismissible is implemented by a dialog that wants to know when the stack
// removes it without the dialog's own close action (Esc, backdrop click,
// Replace). Confirm uses it to run its cancel action exactly once.
// OpenCode counterpart: dialog onClose with a reason.
type Dismissible interface {
	Dismissed()
}

// ScrimProvider is implemented by an overlay band host that dims everything
// painted beneath it. Root applies the blend to the composed cells before the
// host's own band paints, which is the only place the content band is readable.
// OpenCode counterpart: the dialog backdrop layer at z 3000.
type ScrimProvider interface {
	Scrim() (tk tui.Token, alpha float64, on bool)
}

// Dialog geometry from GOTUI.md §3.0.D.
const (
	// ScrimAlpha is the backdrop dim strength: 150/255 over the content band.
	ScrimAlpha = 150.0 / 255.0
	// dialogSideInset is both the horizontal inset of the dialog in the frame
	// and its interior padding.
	dialogSideInset = 3
	// DialogWidthSmall/Medium/Large are the three dialog width tiers.
	DialogWidthSmall  = 60
	DialogWidthMedium = 88
	DialogWidthLarge  = 116
	// minDialogHeight keeps header, one body row, and footer readable.
	minDialogHeight = 6
)

// dialogWidth keeps menus compact while preserving a terminal-edge inset.
func dialogWidth(w int) int { return max(1, min(DialogWidthSmall, w-2)) }

// applyScrim blends every painted cell in area toward tk by alpha. Both the
// background and foreground dim, which is what makes the content band read as
// "behind glass" rather than merely recolored.
// OpenCode: the alpha-150 backdrop over content.
func applyScrim(c *lipgloss.Canvas, area Rect, tk tui.Token, alpha float64) {
	if c == nil || area.Empty() || alpha <= 0 {
		return
	}
	base := tk.Color()
	for y := area.Y; y < area.Y+area.H; y++ {
		for x := area.X; x < area.X+area.W; x++ {
			cell := c.CellAt(x, y)
			if cell == nil {
				continue
			}
			out := *cell
			if out.Style.Bg != nil {
				out.Style.Bg = blendToward(out.Style.Bg, base, alpha)
			}
			if out.Style.Fg != nil {
				out.Style.Fg = blendToward(out.Style.Fg, base, alpha)
			}
			c.SetCell(x, y, &out)
		}
	}
}

// blendToward mixes a toward b in sRGB. It is the kit-local twin of
// tui.Decrease's blend step: the scrim needs raw color blending, not a token
// role, because it operates on already-painted cells.
func blendToward(a, b color.Color, level float64) color.Color {
	switch {
	case a == nil:
		return b
	case b == nil || level <= 0:
		return a
	case level >= 1:
		return b
	}
	ca, ok := colorful.MakeColor(a)
	if !ok {
		return b
	}
	cb, ok := colorful.MakeColor(b)
	if !ok {
		return a
	}
	r, g, bl := ca.BlendRgb(cb, level).RGB255()
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", r, g, bl))
}

// dialogGeometry is the optional sizing hook a dialog implements so the stack
// can place it without knowing its concrete type.
type dialogGeometry interface {
	// DialogHeight returns the preferred height for width inside availH.
	DialogHeight(width, availH int) int
	// DialogY returns the top offset of a dialogH-tall dialog in availH.
	DialogY(dialogH, availH int) int
}

// parentHeightAware is implemented by dialog bodies whose row cap depends on
// the dialog height (§3.0.D "height cap h/2 − 6").
type parentHeightAware interface {
	setParentHeight(h int)
}

// DialogStack is the provider-owned modal stack. It hosts the open dialogs,
// paints the scrim, gates input while non-empty, and restores focus on close.
//
// OpenTUI counterpart: the dialog manager. OpenCode counterpart:
// packages/tui/src/ui/dialog.tsx (stacked, Esc-eats-keys, dimmed backdrop).
type DialogStack struct {
	WidgetBase

	// Focus is the ring restored after the last dialog closes. Optional: keys
	// are routed to the stack by Root, so a stack without a ring still works.
	Focus *FocusRing
	// SelectionActive is the text-selection guard. When it reports true a
	// backdrop click is ignored, so a drag that started under the dialog never
	// dismisses it. internal/tui owns the policy; the stack only asks.
	SelectionActive func() bool
	// ScrimFill is the backdrop blend target (default background.base).
	ScrimFill tui.Token
	// Alpha is the scrim strength (default ScrimAlpha).
	Alpha float64

	stack     []Widget
	prevFocus string
	layoutRev int
	laidRev   int
	dialogBox Rect
	cache     RenderCache
}

// DialogStackOption customises NewDialogStack.
type DialogStackOption func(*DialogStack)

// WithDialogFocus restores the pre-dialog focus target on close.
func WithDialogFocus(f *FocusRing) DialogStackOption {
	return func(s *DialogStack) { s.Focus = f }
}

// WithDialogGuard installs the text-selection guard hook.
func WithDialogGuard(fn func() bool) DialogStackOption {
	return func(s *DialogStack) { s.SelectionActive = fn }
}

// NewDialogStack builds an empty, hidden stack. The ID is registered so the
// backdrop itself is hittable: a click outside the dialog resolves to the stack
// and closes it.
// OpenTUI: new DialogManager(); OpenCode: <DialogProvider>.
func NewDialogStack(opts ...DialogStackOption) *DialogStack {
	th := DefaultTheme()
	s := &DialogStack{
		WidgetBase: NewWidgetBase("dialogs"),
		ScrimFill:  th.Background.Base,
		Alpha:      ScrimAlpha,
	}
	s.Hide() // an empty stack takes no layout, paint, or hit space
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// Push opens a dialog above the current top. Only the top dialog is visible
// and focusable; lower entries stay in the stack but leave layout, paint, and
// the hit table.
// OpenCode: dialog.push().
func (s *DialogStack) Push(w Widget) {
	if w == nil {
		return
	}
	if len(s.stack) == 0 && s.Focus != nil {
		s.prevFocus = s.Focus.FocusedID()
	}
	if l, ok := w.(*DialogLayout); ok && l.OnClose == nil {
		l.OnClose = s.Pop
	}
	s.stack = append(s.stack, w)
	s.Show()
	s.syncVisibility()
	s.focusTop()
	s.layoutRev++
	s.MarkDirty()
}

// Replace clears the stack and opens w. This is the §3.0.D "replace() clears"
// provider semantic: a new picker never stacks on the previous one.
// OpenCode: dialog.replace().
func (s *DialogStack) Replace(w Widget) {
	s.Clear()
	s.Push(w)
}

// Pop closes the top dialog and restores focus when the stack empties. It
// reports whether anything was open.
// OpenCode: dialog.pop().
func (s *DialogStack) Pop() bool {
	if len(s.stack) == 0 {
		return false
	}
	popped := s.stack[len(s.stack)-1]
	s.stack = s.stack[:len(s.stack)-1]
	setWidgetVisible(popped, false)
	s.syncVisibility()
	if len(s.stack) == 0 {
		s.restoreFocus()
		s.Hide()
	} else {
		s.focusTop()
	}
	s.layoutRev++
	s.MarkDirty()
	// The dialog's own close action (header esc, footer action) has already
	// run by the time it calls Pop. Dismissed covers the other paths: Esc from
	// the stack, a backdrop click, and programmatic pop. Confirm uses it for
	// its cancel action exactly once.
	if d, ok := popped.(Dismissible); ok {
		d.Dismissed()
	}
	return true
}

// Clear closes every dialog (still restoring the previous focus).
// OpenCode: dialog.clear().
func (s *DialogStack) Clear() {
	if len(s.stack) == 0 {
		return
	}
	popped := s.stack
	s.stack = s.stack[:0]
	for _, w := range popped {
		setWidgetVisible(w, false)
	}
	s.restoreFocus()
	s.Hide()
	s.layoutRev++
	s.MarkDirty()
	for _, w := range popped {
		if d, ok := w.(Dismissible); ok {
			d.Dismissed()
		}
	}
}

// Len returns the number of stacked dialogs.
func (s *DialogStack) Len() int { return len(s.stack) }

// Top returns the visible dialog, or nil.
func (s *DialogStack) Top() Widget {
	if len(s.stack) == 0 {
		return nil
	}
	return s.stack[len(s.stack)-1]
}

// Modal reports whether the stack is blocking input beneath it.
func (s *DialogStack) Modal() bool { return len(s.stack) > 0 }

// NeedsLayout reports that the stack composition changed, so Root re-runs the
// geometry pass before the next composite (a dialog has never been laid out).
func (s *DialogStack) NeedsLayout() bool {
	if s.layoutRev != s.laidRev {
		return true
	}
	if top, ok := s.Top().(interface{ NeedsLayout() bool }); ok {
		return top.NeedsLayout()
	}
	return false
}

// Scrim reports the backdrop blend for Root's compositor.
func (s *DialogStack) Scrim() (tui.Token, float64, bool) {
	if !s.Modal() {
		return tui.Token{}, 0, false
	}
	return s.ScrimFill, s.Alpha, true
}

// syncVisibility leaves only the single-visible-dialog: lower entries stay in
// the stack but leave layout, paint, and the hit table.
func (s *DialogStack) syncVisibility() {
	for i, w := range s.stack {
		setWidgetVisible(w, i == len(s.stack)-1)
	}
}

// setWidgetVisible flips the base visibility flag. Custom widgets without a
// WidgetBase are left untouched (they render themselves unconditionally).
func setWidgetVisible(w Widget, v bool) {
	if b := baseOf(w); b != nil {
		b.SetVisible(v)
	}
}

// focusTop moves the ring to the top dialog's target when it is registered.
func (s *DialogStack) focusTop() {
	if s.Focus == nil {
		return
	}
	if t := s.Top(); t != nil {
		if ft := t.FocusTarget(); ft != nil && ft.ID != "" {
			s.Focus.Focus(ft.ID)
		}
	}
}

// restoreFocus returns keyboard focus to the widget focused before the stack
// opened, which is the §3.0.D "refocuses previous" rule.
func (s *DialogStack) restoreFocus() {
	if s.Focus != nil && s.prevFocus != "" {
		s.Focus.Focus(s.prevFocus)
	}
	s.prevFocus = ""
}

// FocusModal re-aims focus at the top dialog once layout has registered its
// target. Root calls it after every focus re-sync.
func (s *DialogStack) FocusModal(f *FocusRing) bool {
	top := s.Top()
	if top == nil || f == nil {
		return false
	}
	if id := f.FocusedID(); id != "" && containsFocusTarget(top, id) {
		return false
	}
	if t := top.FocusTarget(); t != nil && t.ID != "" {
		return f.Focus(t.ID)
	}
	return false
}

// containsFocusTarget reports whether id is a focus target inside w.
func containsFocusTarget(w Widget, id string) bool {
	if w == nil {
		return false
	}
	if t := w.FocusTarget(); t != nil && t.ID == id {
		return true
	}
	for _, c := range childWidgets(w) {
		if containsFocusTarget(c, id) {
			return true
		}
	}
	return false
}

// Layout places the top dialog: tier width centered horizontally, h/4 from the
// top unless the dialog asks to be centered.
// OpenTUI: dialog yoga node; OpenCode: width tiers + paddingTop h/4.
func (s *DialogStack) Layout(r Rect) {
	s.SetBounds(r)
	s.laidRev = s.layoutRev
	top := s.Top()
	if top == nil || !s.Visible {
		return
	}
	width := dialogWidth(r.W)
	height := min(max(minDialogHeight, 10), max(1, r.H-2))
	yOff := r.H / 4
	if g, ok := top.(dialogGeometry); ok {
		height = g.DialogHeight(width, r.H)
		yOff = g.DialogY(height, r.H)
	}
	height = min(max(minDialogHeight, height), max(1, r.H-2))
	yOff = min(max(0, yOff), max(0, r.H-height))
	s.dialogBox = Rect{X: r.X + (r.W-width)/2, Y: r.Y + yOff, W: width, H: height}
	top.Layout(s.dialogBox)
}

// Render paints the top dialog. The surrounding cells stay transparent so
// Root's scrim shows through; the dialog surface itself is opaque.
func (s *DialogStack) Render(w, h int) string {
	if !s.Visible || w <= 0 || h <= 0 {
		return ""
	}
	top := s.Top()
	if top == nil {
		return fitBlock("", w, h)
	}
	return s.cache.Render(w, h, subtreeRev(s), func() string {
		cv := lipgloss.NewCanvas(w, h)
		drawWidget(cv, top, s.dialogBox, s.Bounds())
		return padCanvas(cv, w, h)
	})
}

// Children exposes the visible dialog so Root's hit/focus/rev walks reach it.
func (s *DialogStack) Children() []Widget {
	if top := s.Top(); top != nil {
		return []Widget{top}
	}
	return nil
}

// Key routes to the top dialog, then handles stack keys. While open the stack
// consumes everything, including Esc, so nothing leaks to the transcript.
// OpenCode: Esc pops the top dialog / ctrl+c clears.
func (s *DialogStack) Key(ev tea.KeyPressMsg) bool {
	if len(s.stack) == 0 {
		return false
	}
	if top := s.Top(); top != nil && top.Key(ev) {
		s.MarkDirty()
		return true
	}
	switch ev.String() {
	case "esc", "ctrl+c":
		return s.Pop()
	}
	return true
}

// Mouse keeps clicks inside the dialog and closes on a backdrop click. A click
// during an active text selection is ignored (the drag belongs to the content
// beneath, not to the backdrop).
// OpenCode: backdrop click with the selection guard.
func (s *DialogStack) Mouse(ev tea.MouseMsg, _ Rect) bool {
	top := s.Top()
	if top == nil || ev == nil {
		return false
	}
	m := ev.Mouse()
	if WidgetBounds(top).Contains(m.X, m.Y) {
		if top.Mouse(ev, WidgetBounds(top)) {
			s.MarkDirty()
		}
		return true
	}
	if _, ok := ev.(tea.MouseClickMsg); !ok {
		return true
	}
	if s.SelectionActive != nil && s.SelectionActive() {
		return true
	}
	return s.Pop()
}

// FocusTarget is nil: the stack is a host, its dialog carries the focus stop.
func (s *DialogStack) FocusTarget() *FocusTarget { return nil }

// DialogLayout is the dialog chrome: padded raised surface, bold header with a
// right-aligned muted "esc", a body, and a footer, with 1-row gaps between the
// three bands.
//
// OpenTUI counterpart: the box+text composition a dialog renders. OpenCode
// counterpart: ui/dialog.tsx layout (borderless surface, BOLD title, esc hint).
type DialogLayout struct {
	WidgetBase

	Title  string
	Body   Widget
	Footer *DialogFooter
	// Fill is the dialog surface (default background.raised.base).
	Fill tui.Token
	// Height is the explicit dialog height; 0 sizes to content.
	Height int
	// Center places the dialog mid-screen instead of h/4 down.
	Center bool
	// EscHint is the right-aligned header label (default "esc").
	EscHint string
	// OnConfirm runs on Enter when the body did not consume the key.
	OnConfirm func() bool
	// OnClose runs when the header "esc" is clicked; DialogStack wires Pop.
	OnClose func() bool
	// OnDismiss runs when the stack removes this dialog without its own close
	// action (Esc, backdrop click, Replace). Confirm uses it for OnCancel.
	OnDismiss func()

	inner      Rect
	header     Rect
	bodyRect   Rect
	footerRect Rect
	availH     int
	target     *FocusTarget
	cache      RenderCache
}

// DialogLayoutOpts configures a DialogLayout.
type DialogLayoutOpts struct {
	ID        string
	Title     string
	Body      Widget
	Footer    *DialogFooter
	Fill      tui.Token
	Height    int
	Center    bool
	OnConfirm func() bool
	OnClose   func() bool
}

// NewDialogLayout builds a dialog surface. A zero ID defaults to "dialog" so the
// surface and its focus target are registered.
// OpenTUI: <box> composition; OpenCode: <Dialog> shell.
func NewDialogLayout(opts DialogLayoutOpts) *DialogLayout {
	id := opts.ID
	if id == "" {
		id = "dialog"
	}
	l := &DialogLayout{
		WidgetBase: NewWidgetBase(id),
		Title:      opts.Title,
		Body:       opts.Body,
		Footer:     opts.Footer,
		Fill:       opts.Fill,
		Height:     opts.Height,
		Center:     opts.Center,
		OnConfirm:  opts.OnConfirm,
		OnClose:    opts.OnClose,
	}
	if l.Footer != nil && l.Footer.ID == "" {
		l.Footer.ID = "dialog.footer"
	}
	l.target = &FocusTarget{ID: id, Widget: l, OnFocus: l.MarkDirty, OnBlur: l.MarkDirty}
	return l
}

// NeedsLayout propagates a filtered body's preferred-height change.
func (l *DialogLayout) NeedsLayout() bool {
	if body, ok := l.Body.(interface{ NeedsLayout() bool }); ok {
		return body.NeedsLayout()
	}
	return false
}

// SetTitle mutates the header label idempotently.
func (l *DialogLayout) SetTitle(title string) {
	if l.Title == title {
		return
	}
	l.Title = title
	l.MarkDirty()
}

// padding is the dialog interior: 1-row top/bottom, 3-cell sides (§3.0.D).
func (l *DialogLayout) padding() Insets {
	return InsetsTRBL(1, dialogSideInset, 1, dialogSideInset)
}

// DialogHeight sizes the dialog to its content when Height is unset.
func (l *DialogLayout) DialogHeight(width, availH int) int {
	l.availH = availH
	if l.Height > 0 {
		return l.Height
	}
	pad := l.padding()
	innerW := max(0, width-pad.Left-pad.Right)
	body := 4
	if hs, ok := l.Body.(interface{ PreferredHeight(width, maxH int) int }); ok {
		body = hs.PreferredHeight(innerW, availH)
	}
	h := pad.Top + pad.Bottom + body
	if l.Title != "" {
		h += 2 // header row + 1-row gap
	}
	if l.Footer != nil && WidgetVisible(l.Footer) {
		h += 2 // gap + footer row
	}
	return min(h, max(minDialogHeight, availH-2))
}

// DialogY is h/4 from the top unless the dialog asks to be centered.
func (l *DialogLayout) DialogY(dialogH, availH int) int {
	if l.Center {
		return max(0, (availH-dialogH)/2)
	}
	return availH / 4
}

// Layout assigns the header, body, and footer rects with their 1-row gaps.
// OpenTUI: dialog yoga layout pass.
func (l *DialogLayout) Layout(r Rect) {
	l.SetBounds(r)
	if !l.Visible {
		return
	}
	l.inner = r.Apply(l.padding())
	y := l.inner.Y
	if l.Title != "" {
		l.header = Rect{X: l.inner.X, Y: y, W: l.inner.W, H: 1}
		y += 2
	}
	bottom := l.inner.Y + l.inner.H
	bodyEnd := bottom
	if l.Footer != nil && WidgetVisible(l.Footer) {
		l.footerRect = Rect{X: l.inner.X, Y: bottom - 1, W: l.inner.W, H: 1}
		bodyEnd = bottom - 2 // footer row + 1-row gap above it
		l.Footer.Layout(l.footerRect)
	}
	l.bodyRect = Rect{X: l.inner.X, Y: y, W: l.inner.W, H: max(0, bodyEnd-y)}
	if l.Body == nil {
		return
	}
	// Bodies cap their rows against the band height (§3.0.D h/2 − 6), not the
	// dialog's own height, so the cap never shrinks with its own output.
	if ph, ok := l.Body.(parentHeightAware); ok {
		if l.availH > 0 {
			ph.setParentHeight(l.availH)
		} else {
			ph.setParentHeight(r.H)
		}
	}
	if WidgetVisible(l.Body) {
		l.Body.Layout(l.bodyRect)
	}
}

// Render paints the raised surface, BOLD title, muted esc hint, body, footer.
// OpenCode: the borderless dialog surface separated by background color.
func (l *DialogLayout) Render(w, h int) string {
	if !l.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return l.cache.Render(w, h, subtreeRev(l), func() string {
		th := DefaultTheme()
		cv := lipgloss.NewCanvas(w, h)
		fillRect(cv, 0, 0, w, h, tokenOr(l.Fill, th.Background.Raised.Base))
		origin := l.Bounds()
		if l.Title != "" && !l.header.Empty() {
			hx, hy := l.header.X-origin.X, l.header.Y-origin.Y
			hint := l.escHint()
			hintW := ansi.StringWidth(hint)
			titleW := l.header.W
			if hintW > 0 {
				titleW -= hintW + 1
			}
			if titleW >= 10 {
				putGlyph(cv, hx, hy, "◆", th.Text.Action.Primary.Color())
				hx += 2
				titleW -= 2
			}
			drawStyled(cv, hx, hy, titleW, l.Title,
				lipgloss.NewStyle().Bold(true).Foreground(th.Text.Strong.Color()))
			if hintW > 0 {
				drawStyled(cv, l.header.X-origin.X+l.header.W-hintW, hy, hintW, hint,
					lipgloss.NewStyle().Foreground(th.Text.Muted.Color()))
			}
		}
		if l.Body != nil && WidgetVisible(l.Body) {
			drawWidget(cv, l.Body, l.bodyRect, origin)
		}
		if l.Footer != nil && WidgetVisible(l.Footer) {
			drawWidget(cv, l.Footer, l.footerRect, origin)
		}
		return padCanvas(cv, w, h)
	})
}

// escHint is the header shortcut when the footer doesn't already show it.
func (l *DialogLayout) escHint() string {
	if l.Footer != nil && l.Footer.Visible && l.footerRect.Y > l.header.Y && l.Footer.showsEscape() {
		return ""
	}
	hint := l.EscHint
	if hint == "" {
		hint = "esc"
	}
	if l.header.W <= ansi.StringWidth(hint)+2 {
		return ""
	}
	return hint
}

// Children exposes body and footer for hit/focus/rev walks.
func (l *DialogLayout) Children() []Widget {
	out := make([]Widget, 0, 2)
	if l.Body != nil {
		out = append(out, l.Body)
	}
	if l.Footer != nil {
		out = append(out, l.Footer)
	}
	return out
}

// Key offers the key to the body, then to the dialog actions. Esc is left to
// DialogStack so a body that consumed it (an active search filter) keeps the
// dialog open.
// OpenCode: dialog key handling (Enter confirms, Esc bubbles to the stack).
func (l *DialogLayout) Key(ev tea.KeyPressMsg) bool {
	if l.Body != nil && WidgetVisible(l.Body) && l.Body.Key(ev) {
		l.MarkDirty()
		return true
	}
	switch ev.String() {
	case "enter":
		if l.OnConfirm != nil && l.OnConfirm() {
			l.MarkDirty()
			return true
		}
	case "tab", "shift+tab", "up", "down":
		return true // keep focus inside the modal
	}
	return false
}

// Mouse closes the dialog when the header esc hint is clicked.
// OpenCode: clickable esc affordance.
func (l *DialogLayout) Mouse(ev tea.MouseMsg, r Rect) bool {
	click, ok := ev.(tea.MouseClickMsg)
	if !ok || click.Button != tea.MouseLeft || l.header.Empty() {
		return false
	}
	hint := l.escHint()
	if hint == "" {
		return false
	}
	m := ev.Mouse()
	hintX := l.header.X + l.header.W - ansi.StringWidth(hint)
	if l.header.Contains(m.X, m.Y) && m.X >= hintX {
		if l.OnClose != nil {
			return l.OnClose()
		}
	}
	return false
}

// FocusTarget is the cached dialog focus stop.
func (l *DialogLayout) FocusTarget() *FocusTarget { return l.target }

// Dismissed implements Dismissible so stack-initiated closes can run cleanup
// (Confirm's cancel action) without the dialog calling Pop itself.
func (l *DialogLayout) Dismissed() {
	if l.OnDismiss != nil {
		l.OnDismiss()
	}
}

// DialogButton is one footer action. Label and Key render lowercase and
// space-separated ("cancel esc"); Primary actions are right-aligned.
type DialogButton struct {
	Label   string
	Key     string
	Primary bool
	// Action runs on click and returns whether it consumed the event.
	Action func() bool
}

// text is the rendered label: "cancel esc".
func (b DialogButton) text() string {
	s := strings.ToLower(strings.TrimSpace(b.Label))
	if k := strings.ToLower(strings.TrimSpace(b.Key)); k != "" {
		s += " " + k
	}
	return s
}

// width is the button's cell count including the 1-cell hover pad each side.
func (b DialogButton) width() int { return ansi.StringWidth(b.text()) + 2 }

// DialogFooter is the dialog action row: secondary buttons packed left, the
// primary action right-aligned, each padded by one hover cell.
//
// OpenTUI counterpart: a <box flexDirection="row"> of buttons. OpenCode
// counterpart: dialog footer hints.
type DialogFooter struct {
	WidgetBase

	Buttons []DialogButton
	// Fg is the secondary label color; PrimaryFg the primary one.
	Fg        tui.Token
	PrimaryFg tui.Token

	rects []Rect
	hover int
	cache RenderCache
}

// NewDialogFooter builds a footer from left-to-right declaration order.
func NewDialogFooter(buttons ...DialogButton) *DialogFooter {
	return &DialogFooter{
		WidgetBase: NewWidgetBase(""),
		Buttons:    buttons,
		hover:      -1,
	}
}

// Layout resolves each button's absolute rect: secondaries from the left, the
// primary flush right.
// OpenTUI: justify space-between footer row.
func (f *DialogFooter) Layout(r Rect) {
	f.SetBounds(r)
	f.rects = f.rects[:0]
	x := r.X
	for _, b := range f.Buttons {
		w := b.width()
		f.rects = append(f.rects, Rect{X: x, Y: r.Y, W: w, H: 1})
		if !b.Primary {
			x += w + 1
		}
	}
	rx := r.X + r.W
	for i := len(f.Buttons) - 1; i >= 0; i-- {
		if !f.Buttons[i].Primary {
			continue
		}
		w := f.Buttons[i].width()
		rx -= w
		f.rects[i] = Rect{X: rx, Y: r.Y, W: w, H: 1}
		rx -= 1
	}
}

func (f *DialogFooter) showsEscape() bool {
	for i, b := range f.Buttons {
		if !strings.EqualFold(strings.TrimSpace(b.Key), "esc") || i >= len(f.rects) {
			continue
		}
		r := f.rects[i]
		keyW := len("esc")
		key := Rect{X: r.X + r.W - 1 - keyW, Y: r.Y, W: keyW, H: 1}
		if key.Intersect(f.Bounds()) != key {
			continue
		}
		visible := true
		for _, later := range f.rects[i+1:] {
			if !key.Intersect(later).Empty() {
				visible = false
				break
			}
		}
		if visible {
			return true
		}
	}
	return false
}

// Render paints one footer row. Labels are lowercase and the primary action is
// right-aligned; hover paints the 1-cell pad behind the label.
// OpenCode: "cancel esc" / "confirm enter" hints.
func (f *DialogFooter) Render(w, h int) string {
	if !f.Visible || w <= 0 || h <= 0 || len(f.Buttons) == 0 {
		return ""
	}
	return f.cache.Render(w, h, f.Rev(), func() string {
		th := DefaultTheme()
		cv := lipgloss.NewCanvas(w, h)
		origin := f.Bounds()
		zero := Rect{}
		for i, b := range f.Buttons {
			if i >= len(f.rects) || f.rects[i] == zero {
				continue
			}
			r := f.rects[i]
			x, y := r.X-origin.X, r.Y-origin.Y
			if y < 0 || y >= h {
				continue
			}
			if i == f.hover {
				fillCells(cv, x, y, r.W, th.Background.Raised.High.Color())
			} else if b.Primary {
				surface := tui.Tint(th.Background.Raised.Base, th.Text.Action.Primary, 0.12)
				fillCells(cv, x, y, r.W, surface.Color())
			}
			fg := tokenOr(f.Fg, th.Text.Muted)
			style := lipgloss.NewStyle().Foreground(fg.Color())
			if b.Primary {
				style = lipgloss.NewStyle().Bold(true).
					Foreground(tokenOr(f.PrimaryFg, th.Text.Action.Primary).Color())
			}
			drawStyled(cv, x, y, r.W, " "+b.text()+" ", style)
		}
		return padCanvas(cv, w, h)
	})
}

// Mouse tracks the hovered button and runs its action on click.
// OpenCode: footer button hover pad + click.
func (f *DialogFooter) Mouse(ev tea.MouseMsg, r Rect) bool {
	if ev == nil {
		return false
	}
	idx := -1
	for i, br := range f.rects {
		if br.Contains(ev.Mouse().X, ev.Mouse().Y) {
			idx = i
		}
	}
	switch ev.(type) {
	case tea.MouseMotionMsg:
		if f.hover != idx {
			f.hover = idx
			f.MarkDirty()
		}
		return idx >= 0
	case tea.MouseClickMsg:
		if idx < 0 || idx >= len(f.Buttons) {
			return false
		}
		f.hover = -1
		f.MarkDirty()
		if a := f.Buttons[idx].Action; a != nil {
			return a()
		}
		return true
	}
	return false
}

// Key is a no-op: the dialog owns action keys (Enter/Esc).
func (f *DialogFooter) Key(tea.KeyPressMsg) bool { return false }

// FocusTarget is nil: buttons are click targets, not focus stops.
func (f *DialogFooter) FocusTarget() *FocusTarget { return nil }

// SelectItem is one DialogSelect choice.
type SelectItem struct {
	ID string
	// Title is the primary text and carries 2x fuzzy weight.
	Title string
	// Desc is muted secondary text and carries 1x weight.
	Desc string
	// Group is the category header the row is filed under.
	Group string
	// Footer is the per-row hint ("shift+tab cycle", "$2 left", …).
	Footer string
}

// DialogSelectOpts configures a DialogSelect.
type DialogSelectOpts struct {
	ID              string
	Items           []SelectItem
	Placeholder     string
	NoMotion        bool
	Palette         bool
	Details         bool
	SectionDividers bool
	OnSelect        func(SelectItem) bool
}

// DialogSelect is the fuzzy-filtered picker body: a search row, a 1-cell marker
// gutter, category-grouped rows, and a height cap with an "…" overflow row.
//
// OpenTUI counterpart: Select + Input. OpenCode counterpart: dialog-select.
type DialogSelect struct {
	WidgetBase

	Items       []SelectItem
	Placeholder string
	Palette     bool
	Details     bool
	OnSelect    func(SelectItem) bool

	list        *List
	query       []rune
	parentH     int
	layoutDirty bool
	target      *FocusTarget
	cache       RenderCache
}

// NewDialogSelect builds the picker body with the first row selected.
func NewDialogSelect(opts DialogSelectOpts) *DialogSelect {
	id := opts.ID
	if id == "" {
		id = "dialog.select"
	}
	s := &DialogSelect{
		WidgetBase:  NewWidgetBase(id),
		Items:       opts.Items,
		Placeholder: opts.Placeholder,
		Palette:     opts.Palette,
		Details:     opts.Details,
		OnSelect:    opts.OnSelect,
		list:        NewList(ListOpts{ID: id + ".rows", NoMotion: opts.NoMotion, Palette: opts.Palette, Compact: true, SectionDividers: opts.SectionDividers}),
	}
	s.list.OnChoose = s.choose
	s.target = &FocusTarget{ID: id, Widget: s, OnFocus: s.MarkDirty, OnBlur: s.MarkDirty}
	s.refilter()
	return s
}

// SetParentHeight records the dialog height; the row cap is h/2 − 6.
func (s *DialogSelect) setParentHeight(h int) { s.parentH = h }

// capRows is the §3.0.D row cap: floor(h/2) − 6, or unlimited without a parent.
func (s *DialogSelect) capRows() int {
	if s.parentH <= 0 {
		return 0
	}
	return max(1, s.parentH/2-6)
}

// Query returns the current filter text.
func (s *DialogSelect) Query() string { return string(s.query) }

// SetItems replaces the choices and re-filters.
func (s *DialogSelect) SetItems(items []SelectItem) {
	s.Items = items
	s.refilter()
}

// SetQuery replaces the filter text and re-ranks the rows. It is the mirror
// hook used by an owner picker that keeps its own query (the S4 bridge); keys
// still type into the dialog when it owns input directly.
func (s *DialogSelect) SetQuery(query string) {
	if s.Query() == query {
		return
	}
	s.query = []rune(query)
	s.refilter()
}

// SelectID moves the row marker to id when a visible row carries it. It keeps
// the owner's selection when the dialog opens over an already-positioned
// picker. Returns true when the marker moved.
func (s *DialogSelect) SelectID(id string) bool {
	for i, row := range s.list.Rows {
		if row.Header || row.ID != id {
			continue
		}
		if s.list.Selected == i {
			return false
		}
		s.list.Selected = i
		s.list.MarkDirty()
		s.MarkDirty()
		return true
	}
	return false
}

// SelectedID returns the current row identity, or "".
func (s *DialogSelect) SelectedID() string {
	if row, ok := s.list.SelectedRow(); ok {
		return row.ID
	}
	return ""
}

// PreferredHeight is the height this body wants for the dialog sizing pass.
func (s *DialogSelect) PreferredHeight(_, maxH int) int {
	capRows := s.capRows()
	if capRows == 0 {
		capRows = max(1, maxH/2-6)
	}
	if s.Palette {
		return 4 + s.detailHeight() + min(max(1, len(s.list.displayLines())), capRows)
	}
	return 2 + s.detailHeight() + min(len(s.filteredRows()), capRows)
}

// filteredRows returns the currently rendered rows (headers included).
func (s *DialogSelect) filteredRows() []ListRow { return s.list.Rows }

// refilter re-ranks the items for the current query and regroups them.
func (s *DialogSelect) refilter() {
	rows := SelectRows(s.Items, string(s.query))
	s.list.SetRows(rows)
	if s.Palette {
		s.layoutDirty = true
	}
	s.MarkDirty()
}

// choose reports the current row to the owner.
func (s *DialogSelect) choose(row ListRow) bool {
	for _, it := range s.Items {
		if it.ID == row.ID {
			if s.OnSelect != nil {
				return s.OnSelect(it)
			}
			return true
		}
	}
	return false
}

// NeedsLayout reports a search-driven height change.
func (s *DialogSelect) NeedsLayout() bool { return s.layoutDirty }

func (s *DialogSelect) detailHeight() int {
	if s.Details {
		return MenuDetailsHeight
	}
	return 0
}

// Layout puts the search row at the top and the capped list below.
func (s *DialogSelect) Layout(r Rect) {
	s.layoutDirty = false
	s.SetBounds(r)
	if !s.Visible || r.Empty() {
		return
	}
	capRows := s.capRows()
	avail := max(0, r.H-2)
	if s.Palette {
		avail = max(0, avail-2)
	}
	avail = max(0, avail-s.detailHeight())
	if capRows > 0 && avail > capRows {
		avail = capRows
	}
	s.list.MaxRows = capRows
	s.list.Layout(Rect{X: r.X, Y: r.Y + 2, W: r.W, H: avail})
}

// Render paints the search field and the list.
func (s *DialogSelect) Render(w, h int) string {
	if !s.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return s.cache.Render(w, h, subtreeRev(s), func() string {
		th := DefaultTheme()
		cv := lipgloss.NewCanvas(w, h)
		label, fg := s.Query(), th.Text.Base
		if label == "" {
			label, fg = s.Placeholder, th.Text.Muted
			if label == "" {
				label = "Search"
			}
		}
		searchX := 0
		if s.Palette && w >= 10 {
			fillCells(cv, 0, 0, w, th.Background.Raised.High.Color())
			putGlyph(cv, 1, 0, "⌕", th.Text.Action.Primary.Color())
			searchX = 3
		}
		drawStyled(cv, searchX, 0, w-searchX, label, lipgloss.NewStyle().Foreground(fg.Color()))
		drawWidget(cv, s.list, s.list.Bounds(), s.Bounds())
		if s.Details && h >= 8 {
			if row, ok := s.list.SelectedRow(); ok {
				drawCells(cv, RenderMenuDetails(row.Desc, w, s.detailHeight()), 0, h-1-s.detailHeight(), w, s.detailHeight())
			}
		}
		if s.Palette {
			count := 0
			for _, row := range s.list.Rows {
				if !row.Header {
					count++
				}
			}
			if count == 0 && h > 2 {
				message := "No items available"
				if s.Query() != "" {
					message = "No matches found"
				}
				drawText(cv, 0, 2, w, message, th.Text.Muted.Color())
			}
			if h >= 4 {
				hint := "↑↓ navigate  enter select"
				if w < 48 {
					hint = "↑↓ move  ↵ select"
				}
				drawText(cv, 0, h-1, w, hint, th.Text.Muted.Color())
				countText := strconv.Itoa(count) + " results"
				if count == 1 {
					countText = "1 result"
				}
				if w > len(hint)+len(countText)+2 {
					drawText(cv, w-len(countText), h-1, len(countText), countText, th.Text.Muted.Color())
				}
			}
		}
		return padCanvas(cv, w, h)
	})
}

// Children exposes the row list for hit registration.
func (s *DialogSelect) Children() []Widget { return []Widget{s.list} }

// Key is the dialog-select keyboard subset: type to filter, up/down to move,
// Enter to pick, and Esc to clear the filter (consumed, so the stack keeps the
// dialog open) before a second Esc pops.
// OpenCode: dialog-select keymap.
func (s *DialogSelect) Key(ev tea.KeyPressMsg) bool {
	switch ev.String() {
	case "esc":
		if len(s.query) == 0 {
			return false
		}
		s.query = nil
		s.refilter()
		return true
	case "ctrl+u":
		s.SetQuery("")
		return true
	case "pgup", "pgdown":
		delta := max(1, s.list.Bounds().H-2)
		if ev.String() == "pgup" {
			delta = -delta
		}
		if s.list.Move(delta) {
			s.MarkDirty()
		}
		return true
	case "up", "ctrl+p":
		if s.list.Move(-1) {
			s.MarkDirty()
		}
		return true
	case "down", "ctrl+n":
		if s.list.Move(1) {
			s.MarkDirty()
		}
		return true
	case "enter":
		if row, ok := s.list.SelectedRow(); ok {
			return s.choose(row)
		}
		return true
	case "backspace":
		if len(s.query) == 0 {
			return false
		}
		s.query = s.query[:len(s.query)-1]
		s.refilter()
		return true
	}
	if txt := ev.Text; txt != "" && !strings.ContainsAny(txt, "\r\n\t") {
		s.query = append(s.query, []rune(txt)...)
		s.refilter()
		return true
	}
	return false
}

// Mouse is unconsumed: the row list handles clicks itself.
func (s *DialogSelect) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is the cached picker focus stop.
func (s *DialogSelect) FocusTarget() *FocusTarget { return s.target }

// SelectRows ranks items for query and groups them under header rows. Query
// matching weights the title 2x against the description, per §3.0.D.
// OpenCode: dialog-select fuzzy filter with title weighting.
func SelectRows(items []SelectItem, query string) []ListRow {
	type scored struct {
		item  SelectItem
		score int
	}
	kept := make([]scored, 0, len(items))
	for _, it := range items {
		sc, ok := scoreItem(query, it)
		if !ok {
			continue
		}
		kept = append(kept, scored{item: it, score: sc})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].score > kept[j].score })
	rows := make([]ListRow, 0, len(kept)+2)
	group := ""
	for _, s := range kept {
		if s.item.Group != group {
			group = s.item.Group
			if group != "" {
				rows = append(rows, ListRow{Title: group, Header: true})
			}
		}
		rows = append(rows, ListRow{
			ID:     s.item.ID,
			Title:  s.item.Title,
			Desc:   s.item.Desc,
			Footer: s.item.Footer,
		})
	}
	return rows
}

// scoreItem returns the weighted score for one item.
func scoreItem(query string, it SelectItem) (int, bool) {
	if strings.TrimSpace(query) == "" {
		return 0, true
	}
	titleScore, titleOK := fuzzyScore(query, it.Title)
	descScore, descOK := fuzzyScore(query, it.Desc)
	identityScore, identityOK := fuzzyScore(query, it.ID+" "+it.Group)
	if !titleOK && !descOK && !identityOK {
		return 0, false
	}
	return 2*titleScore + max(descScore, identityScore), true
}

// fuzzyScore is a minimal fuzzysort-style subsequence score: +1 per match, +2
// for consecutive characters, +3 for a word-start match, and a small length
// penalty so shorter targets win ties.
// OpenCode: fuzzysort over dialog-select rows (ported, no dependency).
func fuzzyScore(query, target string) (int, bool) {
	q := []rune(strings.ToLower(query))
	t := []rune(strings.ToLower(target))
	if len(q) == 0 {
		return 0, true
	}
	score, ti, last := 0, 0, -2
	for _, qr := range q {
		found := -1
		for ; ti < len(t); ti++ {
			if t[ti] == qr {
				found = ti
				break
			}
		}
		if found < 0 {
			return 0, false
		}
		score++
		if found == last+1 {
			score += 2
		}
		if found == 0 || isWordBreak(t[found-1]) {
			score += 3
		}
		last = found
		ti = found + 1
	}
	return score - len(t)/8, true
}

// isWordBreak reports a separator that starts a new word for scoring.
func isWordBreak(r rune) bool {
	switch r {
	case ' ', '-', '_', '/', '.', ':', '(':
		return true
	}
	return false
}

// ConfirmOpts configures the small confirmation dialog helper.
type ConfirmOpts struct {
	Title string
	Body  string
	// ConfirmLabel/CancelLabel default to "confirm"/"cancel".
	ConfirmLabel string
	CancelLabel  string
	OnConfirm    func() bool
	OnCancel     func() bool
}

// NewConfirm builds a confirmation dialog on top of DialogLayout: title, body
// text, and cancel/confirm actions in the standard footer grammar. The helper
// dismisses itself after a successful action; DialogStack wires OnClose on Push,
// so the same code works standalone (no stack) as a layout preview.
// OpenCode counterpart: the confirm/abort permission prompt.
func NewConfirm(opts ConfirmOpts) *DialogLayout {
	th := DefaultTheme()
	body := NewText(TextOpts{Content: opts.Body, Fg: th.Text.Base})
	confirm := opts.ConfirmLabel
	if confirm == "" {
		confirm = "confirm"
	}
	cancel := opts.CancelLabel
	if cancel == "" {
		cancel = "cancel"
	}
	var dialog *DialogLayout
	done := false
	closeDialog := func() {
		if dialog != nil && dialog.OnClose != nil {
			dialog.OnClose()
		}
	}
	// finish runs the confirm action once and closes on success. A false return
	// keeps the dialog open so callers can veto (e.g. a failed validation).
	finish := func() bool {
		if done {
			return true
		}
		done = true
		if opts.OnConfirm != nil && !opts.OnConfirm() {
			done = false
			return false
		}
		closeDialog()
		return true
	}
	// cancel always closes; the optional hook is notification, not a veto.
	cancelDialog := func() bool {
		if done {
			return true
		}
		done = true
		if opts.OnCancel != nil {
			opts.OnCancel()
		}
		closeDialog()
		return true
	}
	footer := NewDialogFooter(
		DialogButton{Label: cancel, Key: "esc", Action: cancelDialog},
		DialogButton{Label: confirm, Key: "enter", Primary: true, Action: finish},
	)
	dialog = NewDialogLayout(DialogLayoutOpts{
		ID:        "dialog.confirm",
		Title:     opts.Title,
		Body:      body,
		Footer:    footer,
		Center:    true,
		OnConfirm: finish,
	})
	// Esc/backdrop come through DialogStack.Pop, not OnClose.
	dialog.OnDismiss = func() {
		if done {
			return
		}
		done = true
		if opts.OnCancel != nil {
			opts.OnCancel()
		}
	}
	return dialog
}
