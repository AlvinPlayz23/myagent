package kit

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// Toast geometry from GOTUI.md §3.0.D: top 1, right 2, width ≤ min(60, w−6).
// The surface is content-fit up to that cap, so a one-line notice reads as a
// notification instead of a full-width dialog.
const (
	toastTopInset   = 1
	toastRightInset = 2
	toastMaxWidth   = 60
	// toastPadX is the body padding inside the left ┃ rail.
	toastPadX = 2
	// toastRailW is the single variant-tinted accent rail on the left edge.
	// The card idiom elsewhere uses a left-only heavy border, so the toast does
	// too; the right edge is the raised surface itself.
	toastRailW = 1
	// toastMinWidth is the narrowest renderable surface: one rail, two cells of
	// padding on each side, and one content cell.
	toastMinWidth = 7
	// toastMinTextWidth keeps short notices from collapsing into a sliver.
	// Narrow bands still win: text is clamped to the available surface.
	toastMinTextWidth = 12
	// toastGlyphW is the width reserved for the variant glyph in the first row.
	toastGlyphW = 1
)

// ToastDuration is the default auto-dismiss timer (§3.0.D: 5000 ms).
const ToastDuration = 5 * time.Second

// ToastVariant tints the left rail and glyph. It maps 1:1 onto text.feedback
// roles, so the toast never invents a color of its own.
type ToastVariant uint8

const (
	ToastInfo ToastVariant = iota
	ToastSuccess
	ToastWarning
	ToastError
)

// ToastAction is the trailing "› label" affordance. A click anywhere on the
// toast dismisses it and runs the action, matching OpenCode's onMouseUp.
type ToastAction struct {
	Label string
	Run   func()
}

// Toast is one message plus its options. Duration 0 uses ToastDuration.
type Toast struct {
	Title    string
	Message  string
	Variant  ToastVariant
	Action   *ToastAction
	Duration time.Duration
}

// duration resolves the per-toast timer.
func (t Toast) duration() time.Duration {
	if t.Duration > 0 {
		return t.Duration
	}
	return ToastDuration
}

// ToastOpts configures a ToastHost.
type ToastOpts struct {
	ID string
	// MaxWidth caps the surface; 0 means the §3.0.D cap of 60.
	MaxWidth int
}

// ToastHost is the band-10 overlay host: one visible toast plus a FIFO queue,
// positioned top-right inside the band. It is deliberately passive about keys
// (register it with WithBandCapture(false)) so typing still reaches the
// composer while a toast is up.
//
// OpenTUI counterpart: a position:absolute box with borders. OpenCode
// counterpart: packages/tui/src/ui/toast.tsx (Surface + provider queue).
type ToastHost struct {
	WidgetBase

	// MaxWidth caps the surface width; the band-derived cap still applies.
	MaxWidth int
	// AnchorWidth bounds the toast column to a sub-rect of the band (for
	// example the main content column beside a sidebar). Zero uses the full
	// band. Right inset still applies inside the anchor.
	AnchorWidth int
	// Animations toggles the affordance emphasis (bold) on hover. It mirrors
	// config.animations; the toast has no motion of its own.
	Animations bool

	queue     []Toast
	current   *Toast
	remaining time.Duration
	paused    bool
	hover     bool

	layoutDirty bool
	cache       RenderCache
}

// NewToastHost builds an empty host. Show enqueues; the host hides itself by
// reporting an empty rect while the queue is empty.
func NewToastHost(opts ToastOpts) *ToastHost {
	return &ToastHost{
		WidgetBase: NewWidgetBase(opts.ID),
		MaxWidth:   opts.MaxWidth,
		Animations: true,
	}
}

// SetMaxWidth updates the surface cap. It marks the host for relayout because a
// width change is a geometry change; values above 60 are clamped by width.
func (t *ToastHost) SetMaxWidth(width int) {
	if t.MaxWidth == width {
		return
	}
	t.MaxWidth = width
	t.layoutDirty = true
	t.MarkDirty()
}

// SetAnchorWidth keeps the toast inside a sub-rect of its band (the main column
// beside the sidebar). Zero restores full-band placement. A width change is a
// geometry change, so it marks the host for relayout.
func (t *ToastHost) SetAnchorWidth(width int) {
	if t.AnchorWidth == width {
		return
	}
	t.AnchorWidth = width
	t.layoutDirty = true
	t.MarkDirty()
}

// SetAnimations toggles hover emphasis and drops the cached paint.
func (t *ToastHost) SetAnimations(on bool) {
	if t.Animations == on {
		return
	}
	t.Animations = on
	t.MarkDirty()
}

// Show enqueues a toast. When nothing is visible it becomes current and starts
// its timer immediately; otherwise it waits its turn. Show shadows
// WidgetBase.Show (which toggles visibility) by design: the OpenCode API name
// wins, and constructors already start visible.
func (t *ToastHost) Show(toast Toast) {
	t.WidgetBase.Show()
	if t.current == nil {
		t.activate(toast)
	} else {
		t.queue = append(t.queue, toast)
	}
	t.layoutDirty = true
	t.MarkDirty()
}

// activate promotes a toast to current and restarts the timer.
func (t *ToastHost) activate(toast Toast) {
	t.current = &toast
	t.remaining = toast.duration()
	t.hover = false
	t.paused = false
}

// Dismiss drops the visible toast and promotes the queue head. It reports
// whether anything was showing.
func (t *ToastHost) Dismiss() bool {
	if t.current == nil {
		return false
	}
	t.promote()
	return true
}

// promote advances the queue after a dismissal.
func (t *ToastHost) promote() {
	if len(t.queue) == 0 {
		t.current = nil
		t.remaining = 0
	} else {
		next := t.queue[0]
		t.queue = t.queue[1:]
		t.activate(next)
	}
	t.layoutDirty = true
	t.MarkDirty()
}

// Current returns the visible toast.
func (t *ToastHost) Current() (Toast, bool) {
	if t.current == nil {
		return Toast{}, false
	}
	return *t.current, true
}

// Pending is the "+N more" count: queued toasts behind the visible one.
func (t *ToastHost) Pending() int { return len(t.queue) }

// Len counts the visible toast plus everything queued.
func (t *ToastHost) Len() int {
	if t.current == nil {
		return len(t.queue)
	}
	return 1 + len(t.queue)
}

// Active reports whether a toast is showing; it is the natural gate for the
// TickDriver subscription.
func (t *ToastHost) Active() bool { return t.current != nil }

// Paused reports the hover pause state.
func (t *ToastHost) Paused() bool { return t.paused }

// SetPaused pauses or resumes the auto-dismiss timer idempotently.
// OpenCode: toast.pause()/resume() on mouse over/out.
func (t *ToastHost) SetPaused(paused bool) {
	if t.paused == paused {
		return
	}
	t.paused = paused
	t.MarkDirty()
}

// Advance consumes elapsed animation time. It reports whether the visible
// toast changed (a dismissal), so a tick loop can skip a repaint otherwise.
// OpenCode: the setTimeout behind ToastSurface, driven by the kit TickDriver.
func (t *ToastHost) Advance(d time.Duration) bool {
	if t.current == nil || t.paused || d <= 0 {
		return false
	}
	t.remaining -= d
	if t.remaining > 0 {
		return false
	}
	t.promote()
	return true
}

// Pointer syncs the hover state from an app-level mouse-motion event. Root only
// routes motion to the widget under the pointer, so the integration layer (or a
// test) feeds coordinates here to notice the pointer leaving the toast.
func (t *ToastHost) Pointer(x, y int) bool {
	inside := t.current != nil && t.Bounds().Contains(x, y)
	changed := false
	if t.hover != inside {
		t.hover = inside
		t.MarkDirty()
		changed = true
	}
	if inside != t.paused {
		t.SetPaused(inside)
		changed = true
	}
	return changed
}

// NeedsLayout reports a queue change: a promoted toast can change the surface
// height without a terminal resize, so Root must re-run the geometry pass.
func (t *ToastHost) NeedsLayout() bool { return t.layoutDirty }

// Layout places the content-fit surface top-right inside the anchor (or the
// full band when no anchor is set). Root hands the host the full band rect; the
// host owns its own placement.
// OpenTUI: position:absolute top={1} right={2} + maxWidth.
func (t *ToastHost) Layout(r Rect) {
	t.layoutDirty = false
	if t.current == nil || !t.Visible {
		t.SetBounds(Rect{X: r.X, Y: r.Y})
		return
	}
	bandW := r.W
	if t.AnchorWidth > 0 && t.AnchorWidth < bandW {
		bandW = t.AnchorWidth
	}
	w := t.width(bandW)
	if w < toastMinWidth {
		t.SetBounds(Rect{X: r.X, Y: r.Y})
		return
	}
	rows := t.rows(w)
	t.SetBounds(Rect{
		X: r.X + bandW - toastRightInset - w,
		Y: r.Y + toastTopInset,
		W: w,
		H: len(rows) + 2,
	})
}

// width resolves a content-fit width clamped to min(MaxWidth or 60, bandW−6).
// A short message gets a narrow surface; a long one wraps at the cap. It
// returns 0 when the band is too narrow for a renderable surface.
// OpenCode: maxWidth={Math.min(60, dimensions().width - 6)}.
func (t *ToastHost) width(bandW int) int {
	cap := t.MaxWidth
	if cap <= 0 {
		cap = toastMaxWidth
	}
	cap = min(cap, toastMaxWidth)
	avail := bandW - 6
	if avail < toastMinWidth {
		return 0
	}
	cap = min(cap, avail)
	textCap := max(1, cap-toastRailW-2*toastPadX)
	textW := max(t.naturalWidth(), toastMinTextWidth)
	textW = min(textW, textCap)
	w := textW + toastRailW + 2*toastPadX
	if w < toastMinWidth {
		return 0
	}
	return w
}

// naturalWidth is the width the content would like before wrapping: the widest
// title/message line, the action affordance, and the queue counter. Title and
// message rows share the glyph gutter, so their widest line gets the prefix.
func (t *ToastHost) naturalWidth() int {
	if t.current == nil {
		return 0
	}
	body := ansi.StringWidth(t.current.Title)
	for _, line := range strings.Split(t.current.Message, "\n") {
		body = max(body, ansi.StringWidth(line))
	}
	if body > 0 {
		body += toastGlyphW + 1
	}
	width := body
	if label := t.actionLabel(); label != "" {
		width = max(width, ansi.StringWidth(label))
	}
	if n := len(t.queue); n > 0 {
		// The counter shares the glyph gutter with title/message rows.
		width = max(width, ansi.StringWidth(fmt.Sprintf("+%d more", n))+toastGlyphW+1)
	}
	return width
}

// textWidth is the usable content width inside the rail and horizontal padding.
func (t *ToastHost) textWidth(surfaceW int) int {
	return max(1, surfaceW-toastRailW-2*toastPadX)
}

// toastRowKind selects the row style.
type toastRowKind uint8

const (
	toastRowText toastRowKind = iota
	toastRowTitle
	toastRowMuted
	toastRowAction
)

// toastRow is one painted content line inside the surface.
type toastRow struct {
	text string
	kind toastRowKind
}

// rows lays out the content: an optional bold title, the word-wrapped message,
// the muted "+N more" counter, and the trailing action affordance. Title and
// message rows share the glyph gutter; there is no blank spacer row, because
// the raised background and padding already separate the toast from the app.
func (t *ToastHost) rows(surfaceW int) []toastRow {
	if t.current == nil {
		return nil
	}
	tw := t.textWidth(surfaceW)
	textW := tw
	if t.current.Title != "" || t.current.Message != "" {
		// The first content row carries the variant glyph; every title/message
		// row aligns under the text that follows it.
		textW = max(1, tw-toastGlyphW-1)
	}
	var rows []toastRow
	if t.current.Title != "" {
		rows = append(rows, toastRow{text: t.current.Title, kind: toastRowTitle})
	}
	for _, line := range wrapWords(t.current.Message, textW) {
		rows = append(rows, toastRow{text: line, kind: toastRowText})
	}
	if n := len(t.queue); n > 0 {
		rows = append(rows, toastRow{text: fmt.Sprintf("+%d more", n), kind: toastRowMuted})
	}
	if label := t.actionLabel(); label != "" && t.actionFits(surfaceW) {
		rows = append(rows, toastRow{text: label, kind: toastRowAction})
	}
	if len(rows) == 0 {
		rows = append(rows, toastRow{})
	}
	return rows
}

// wrapWords greedily wraps each explicit line to w cells. Long words overflow
// and are truncated at paint time.
func wrapWords(s string, w int) []string {
	if s == "" {
		return nil
	}
	if w <= 0 {
		return []string{s}
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case ansi.StringWidth(line)+1+ansi.StringWidth(word) <= w:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return out
}

// toastTint maps a variant to its feedback border role.
func toastTint(th Theme, v ToastVariant) tui.Token {
	switch v {
	case ToastSuccess:
		return th.Text.Feedback.Success.Base
	case ToastWarning:
		return th.Text.Feedback.Warning.Base
	case ToastError:
		return th.Text.Feedback.Error.Base
	default:
		return th.Text.Feedback.Info.Base
	}
}

// toastGlyph is the width-1 status mark on the first content row. It carries
// the variant independently of color, so the notice survives a monochrome
// terminal or a color-vision difference.
func toastGlyph(v ToastVariant) string {
	switch v {
	case ToastSuccess:
		return "✓"
	case ToastWarning:
		return "!"
	case ToastError:
		return "✗"
	default:
		return "◈"
	}
}

// actionLabel is the rendered affordance, including the space after ›.
func (t *ToastHost) actionLabel() string {
	if t.current == nil || t.current.Action == nil {
		return ""
	}
	return "› " + t.current.Action.Label
}

// actionFits reports whether the affordance can be painted inside the content
// column. A click must not run an action the user cannot see.
func (t *ToastHost) actionFits(surfaceW int) bool {
	label := t.actionLabel()
	return label != "" && ansi.StringWidth(label) <= t.textWidth(surfaceW)
}

// Render paints the raised surface, the variant-tinted left rail, the glyph +
// title/message/counter rows, and the right-aligned "› action" row.
func (t *ToastHost) Render(w, h int) string {
	if !t.Visible || t.current == nil || w < toastMinWidth || h < 3 {
		return ""
	}
	return t.cache.Render(w, h, t.Rev(), func() string {
		th := DefaultTheme()
		tint := toastTint(th, t.current.Variant)
		cv := lipgloss.NewCanvas(w, h)
		fillRect(cv, 0, 0, w, h, th.Background.Raised.High)
		// Paint the rail and glyph with the surface behind them. putGlyph
		// replaces the cell and drops its background, which would punch a
		// terminal-default rectangle through the raised surface.
		glyphStyle := lipgloss.NewStyle().
			Foreground(tint.Color()).
			Background(th.Background.Raised.High.Color())
		for y := 0; y < h; y++ {
			drawStyled(cv, 0, y, toastRailW, "┃", glyphStyle)
		}

		rows := t.rows(w)
		x := toastRailW + toastPadX
		tw := t.textWidth(w)
		hasGlyph := len(rows) > 0 && rows[0].text != "" && rows[0].kind != toastRowAction
		if hasGlyph {
			drawStyled(cv, x, 1, toastGlyphW, toastGlyph(t.current.Variant), glyphStyle)
		}

		for i, row := range rows {
			if row.text == "" {
				continue
			}
			if row.kind == toastRowAction {
				aw := ansi.StringWidth(row.text)
				style := lipgloss.NewStyle().Foreground(th.Text.Muted.Color())
				if t.hover && t.Animations {
					style = lipgloss.NewStyle().Bold(true).
						Foreground(th.Text.Action.Primary.Color())
				}
				drawStyled(cv, x+tw-aw, 1+i, aw, row.text, style)
				continue
			}
			var style lipgloss.Style
			switch row.kind {
			case toastRowTitle:
				style = lipgloss.NewStyle().Bold(true).Foreground(th.Text.Base.Color())
			case toastRowMuted:
				style = lipgloss.NewStyle().Foreground(th.Text.Muted.Color())
			default:
				style = lipgloss.NewStyle().Foreground(th.Text.Base.Color())
			}
			tx, avail := x, tw
			if hasGlyph {
				tx = x + toastGlyphW + 1
				avail = max(1, tw-toastGlyphW-1)
			}
			drawStyled(cv, tx, 1+i, avail, row.text, style)
		}
		return padCanvas(cv, w, h)
	})
}

// Mouse pauses the timer while hovering and runs the action on click.
// OpenTUI: onMouseOver/onMouseOut/onMouseUp on the toast box.
func (t *ToastHost) Mouse(ev tea.MouseMsg, r Rect) bool {
	if ev == nil || t.current == nil {
		return false
	}
	m := ev.Mouse()
	inside := r.Contains(m.X, m.Y)
	switch ev.(type) {
	case tea.MouseMotionMsg:
		if t.Pointer(m.X, m.Y) || inside {
			return true
		}
		return false
	case tea.MouseClickMsg:
		if !inside {
			return false
		}
		action := t.current.Action
		run := action != nil && action.Run != nil && t.actionFits(r.W)
		t.promote()
		if run {
			action.Run()
		}
		return true
	}
	return false
}

// Key is a no-op: a toast is a notification, never a keyboard target.
func (t *ToastHost) Key(tea.KeyPressMsg) bool { return false }

// FocusTarget is nil: toasts are not focus stops.
func (t *ToastHost) FocusTarget() *FocusTarget { return nil }
