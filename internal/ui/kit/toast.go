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
const (
	toastTopInset   = 1
	toastRightInset = 2
	toastMaxWidth   = 60
	// toastPadX is the body padding inside the two ┃ border columns.
	toastPadX = 2
	// toastMinWidth is the narrowest renderable surface: two borders, two
	// cells of padding on each side, and one content cell.
	toastMinWidth = 7
)

// ToastDuration is the default auto-dismiss timer (§3.0.D: 5000 ms).
const ToastDuration = 5 * time.Second

// ToastVariant tints the ┃ borders. It maps 1:1 onto text.feedback roles, so
// the toast never invents a color of its own.
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

// Layout places the surface top-right inside the band. Root hands the host the
// full band rect; the host owns its own placement.
// OpenTUI: position:absolute top={1} right={2} + maxWidth.
func (t *ToastHost) Layout(r Rect) {
	t.layoutDirty = false
	if t.current == nil || !t.Visible {
		t.SetBounds(Rect{X: r.X, Y: r.Y})
		return
	}
	w := t.width(r.W)
	if w < toastMinWidth {
		t.SetBounds(Rect{X: r.X, Y: r.Y})
		return
	}
	rows := t.rows(w)
	t.SetBounds(Rect{
		X: r.X + r.W - toastRightInset - w,
		Y: r.Y + toastTopInset,
		W: w,
		H: len(rows) + 2,
	})
}

// width resolves min(MaxWidth or 60, bandW−6). It returns 0 when the band is
// too narrow for a renderable surface, so Bounds never advertises a width that
// exceeds the §3.0.D cap.
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
	return min(cap, avail)
}

// textWidth is the usable message width: borders 2 + padding 2 each side.
func (t *ToastHost) textWidth(surfaceW int) int {
	return max(1, surfaceW-2-2*toastPadX)
}

// toastRowKind selects the row style.
type toastRowKind uint8

const (
	toastRowText toastRowKind = iota
	toastRowTitle
	toastRowMuted
)

// toastRow is one painted content line inside the surface.
type toastRow struct {
	text string
	kind toastRowKind
}

// rows lays out the content: bold title, a 1-row gap, the word-wrapped message,
// and the muted "+N more" counter after its own gap.
// OpenCode: the ToastSurface body with marginBottom={1} under the title.
func (t *ToastHost) rows(surfaceW int) []toastRow {
	if t.current == nil {
		return nil
	}
	tw := t.textWidth(surfaceW)
	var rows []toastRow
	if t.current.Title != "" {
		rows = append(rows, toastRow{text: t.current.Title, kind: toastRowTitle}, toastRow{})
	}
	for _, line := range wrapWords(t.current.Message, tw) {
		rows = append(rows, toastRow{text: line, kind: toastRowText})
	}
	if n := len(t.queue); n > 0 {
		rows = append(rows, toastRow{}, toastRow{text: fmt.Sprintf("+%d more", n), kind: toastRowMuted})
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

// actionLabel is the rendered affordance, including the space after ›.
func (t *ToastHost) actionLabel() string {
	if t.current == nil || t.current.Action == nil {
		return ""
	}
	return "› " + t.current.Action.Label
}

// actionFits reports whether the affordance can be painted without colliding
// with the content column. A click must not run an action the user cannot see.
func (t *ToastHost) actionFits(surfaceW int) bool {
	label := t.actionLabel()
	return label != "" && ansi.StringWidth(label) <= t.textWidth(surfaceW)-4
}

// Render paints the raised surface, the two variant-tinted ┃ borders, the
// title/message/counter rows, and the right-aligned "› action" affordance.
// OpenCode: SplitBorder(["left","right"]) + raised.high body.
func (t *ToastHost) Render(w, h int) string {
	if !t.Visible || t.current == nil || w < toastMinWidth || h < 3 {
		return ""
	}
	return t.cache.Render(w, h, t.Rev(), func() string {
		th := DefaultTheme()
		tint := toastTint(th, t.current.Variant)
		cv := lipgloss.NewCanvas(w, h)
		fillRect(cv, 0, 0, w, h, th.Background.Raised.High)
		for y := 0; y < h; y++ {
			putGlyph(cv, 0, y, "┃", tint.Color())
			putGlyph(cv, w-1, y, "┃", tint.Color())
		}

		rows := t.rows(w)
		x := 1 + toastPadX
		tw := t.textWidth(w)

		// The affordance owns the right edge of the first content row; the row
		// text is clipped around it so the two never overlap.
		aw := 0
		if t.actionFits(w) {
			label := t.actionLabel()
			aw = ansi.StringWidth(label)
			style := lipgloss.NewStyle().Foreground(th.Text.Muted.Color())
			if t.hover && t.Animations {
				style = lipgloss.NewStyle().Bold(true).
					Foreground(th.Text.Action.Primary.Color())
			}
			drawStyled(cv, x+tw-aw, 1, aw, label, style)
		}

		for i, row := range rows {
			if row.text == "" {
				continue
			}
			avail := tw
			if i == 0 && aw > 0 {
				avail = max(1, tw-aw-2)
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
			drawStyled(cv, x, 1+i, avail, row.text, style)
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
