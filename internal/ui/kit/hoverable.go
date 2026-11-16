package kit

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// HoverableOpts configures a Hoverable wrapper.
type HoverableOpts struct {
	ID    string
	Child Widget
	// Marker is painted over column 0 of every row while hovered. Empty paints
	// no marker (use Surface for blocks that already own column 0).
	Marker string
	// MarkerFg paints the marker; zero falls back to text.strong.
	MarkerFg tui.Token
	// Surface, when set, fills the child's cells before paint while hovered.
	// The card idiom's hover = decrease(bg); the 1-cell marker is for children
	// with a reserved gutter.
	Surface tui.Token
	// OnHover receives enter (true) and leave (false) transitions.
	OnHover func(bool)
}

// Hoverable wraps a child and swaps its paint on mouse-motion enter/leave.
// The wrapper owns no layout of its own: the child keeps the assigned rect, so
// wrapping a transcript block costs no geometry. The hover treatment is the
// OpenCode card idiom — a 1-cell `┃` accent bar on the left edge — rather than
// re-coloring child ANSI, which would fight the block's own cached style.
//
// OpenTUI counterpart: onMouseOver/onMouseOut renderable props. OpenCode
// counterpart: the `$hovered` state accessor on cards and tool rows.
type Hoverable struct {
	WidgetBase

	Child    Widget
	Marker   string
	MarkerFg tui.Token
	Surface  tui.Token
	OnHover  func(bool)

	hovered bool
	cache   RenderCache
}

// NewHoverable builds a wrapper around child. A nil child is allowed so an
// owner can bind it later (the wrapper paints blank until then).
func NewHoverable(opts HoverableOpts) *Hoverable {
	h := &Hoverable{
		WidgetBase: NewWidgetBase(opts.ID),
		Child:      opts.Child,
		Marker:     opts.Marker,
		MarkerFg:   opts.MarkerFg,
		Surface:    opts.Surface,
		OnHover:    opts.OnHover,
	}
	if h.Child == nil {
		h.Hide()
	}
	return h
}

// SetChild replaces the wrapped child and shows the wrapper.
func (h *Hoverable) SetChild(child Widget) {
	h.Child = child
	if child == nil {
		h.Hide()
		return
	}
	h.Show()
	h.MarkDirty()
}

// Hovered reports the current hover state.
func (h *Hoverable) Hovered() bool { return h.hovered }

// SetHovered mutates the hover state idempotently. It returns true when the
// painted state changed.
func (h *Hoverable) SetHovered(on bool) bool {
	if h.hovered == on {
		return false
	}
	h.hovered = on
	h.MarkDirty()
	if h.OnHover != nil {
		h.OnHover(on)
	}
	return true
}

// Layout forwards the absolute rect to the child.
func (h *Hoverable) Layout(r Rect) {
	h.SetBounds(r)
	if h.Child != nil {
		h.Child.Layout(r)
	}
}

// MeasureHeight forwards the scroll measurement hook.
func (h *Hoverable) MeasureHeight(width int) int {
	if m, ok := h.Child.(interface{ MeasureHeight(int) int }); ok {
		return m.MeasureHeight(width)
	}
	return 0
}

// Render fills the hover surface (when configured), paints the child, then the
// marker over its first column when one is configured.
func (h *Hoverable) Render(w, hgt int) string {
	if !h.Visible || w <= 0 || hgt <= 0 || h.Child == nil {
		return ""
	}
	return h.cache.Render(w, hgt, subtreeRev(h), func() string {
		cv := lipgloss.NewCanvas(w, hgt)
		if h.hovered && h.Surface != (tui.Token{}) {
			fillRect(cv, 0, 0, w, hgt, h.Surface)
		}
		drawWidget(cv, h.Child, h.Bounds(), h.Bounds())
		if h.hovered && h.Marker != "" {
			fg := tokenOr(h.MarkerFg, DefaultTheme().Text.Strong).Color()
			for y := 0; y < hgt; y++ {
				putGlyph(cv, 0, y, h.Marker, fg)
			}
		}
		return padCanvas(cv, w, hgt)
	})
}

// Children exposes the wrapped child to Root's hit/focus/rev walks.
func (h *Hoverable) Children() []Widget {
	if h.Child == nil {
		return nil
	}
	return []Widget{h.Child}
}

// Key delegates to the child; hover is pointer-only state.
func (h *Hoverable) Key(ev tea.KeyPressMsg) bool {
	return h.Child != nil && h.Child.Key(ev)
}

// Mouse tracks motion and delegates every other gesture. A left-button drag is
// passed straight through so text selection is never delayed by a hover repaint.
func (h *Hoverable) Mouse(ev tea.MouseMsg, r Rect) bool {
	if ev == nil {
		return false
	}
	if m, ok := ev.(tea.MouseMotionMsg); ok && m.Button != tea.MouseLeft {
		inside := r.Contains(m.X, m.Y)
		h.SetHovered(inside)
		if inside {
			return true
		}
		return false
	}
	return h.Child != nil && h.Child.Mouse(ev, r)
}

// FocusTarget is nil: the wrapper is not itself a focus stop.
func (h *Hoverable) FocusTarget() *FocusTarget { return nil }
