package kit

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"sort"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// Edge is the sticky edge of a ScrollBox.
type Edge int

const (
	// EdgeBottom pins the viewport to the newest content (transcript tail).
	EdgeBottom Edge = iota
	// EdgeTop pins the viewport to the start.
	EdgeTop
)

type targetKind uint8

const (
	targetNone targetKind = iota
	targetAbs
	targetEnd // persists across content growth, like OpenTUI's scrollTo(scrollHeight)
)

// ScrollItem is one direct child of a ScrollBox. H is the row height the child
// occupies in the scroll content. A zero H asks the child to measure itself
// through MeasureHeight(width); negative values are clamped to zero.
type ScrollItem struct {
	W Widget
	H int
}

// ScrollBox is a retained vertical viewport over fixed-height children. It is
// the S3 port of the OpenTUI ScrollBox state machine verified in
// spikes/gotui-s0/sticky: sticky tail-follow pauses when the user scrolls away
// and resumes at the edge, end targets persist across appends while absolute
// targets are one-shot, and offscreen children are culled from paint.
//
// OpenTUI counterpart: ScrollBox + ScrollBar. OpenCode counterpart: the
// session transcript scroll region.
type ScrollBox struct {
	WidgetBase

	// Items is the staged content, committed by Layout (content is staged
	// before layout so same-tick growth flushes pending targets correctly).
	Items []ScrollItem
	// Sticky enables tail-follow when Start is EdgeBottom.
	Sticky bool
	// Start is the sticky edge. EdgeBottom is the transcript default.
	Start Edge
	// ShowScrollbar reserves the right column and paints the track/thumb.
	ShowScrollbar bool
	// Track and Thumb are semantic tokens. Zero values fall back to the theme.
	Track tui.Token
	Thumb tui.Token

	viewportH int
	contentH  int
	contentW  int
	scrollTop int

	stickyActive bool
	target       targetKind
	targetAbs    int

	heights []int
	offsets []int
	placed  []Rect
	staged  bool
	cache   RenderCache
}

// NewScrollBox builds a sticky-bottom scroll box. Call SetItems before the
// first Layout; the Shell wires the transcript into it.
func NewScrollBox(items ...ScrollItem) *ScrollBox {
	b := &ScrollBox{
		WidgetBase:    NewWidgetBase("scrollbox"),
		Sticky:        true,
		Start:         EdgeBottom,
		ShowScrollbar: true,
		stickyActive:  true,
	}
	b.SetItems(items)
	return b
}

// SetItems replaces the staged content. Layout commits the new heights and
// applies any pending end target against them.
func (b *ScrollBox) SetItems(items []ScrollItem) {
	b.Items = append(b.Items[:0], items...)
	b.staged = true
	b.MarkDirty()
}

// Append stages additional items. It is sugar over SetItems for streaming
// appends; the target/layout rules are identical.
func (b *ScrollBox) Append(items ...ScrollItem) {
	b.Items = append(b.Items, items...)
	b.staged = true
	b.MarkDirty()
}

// NeedsLayout reports staged content, so Root runs the geometry pass before
// composing an append.
func (b *ScrollBox) NeedsLayout() bool { return b.staged }

// ContentWidth is the paint width available to children. The scrollbar column
// is excluded while ShowScrollbar is set.
func (b *ScrollBox) ContentWidth() int { return b.contentW }

// ContentHeight is the committed scroll height in rows.
func (b *ScrollBox) ContentHeight() int { return b.contentH }

// ViewportHeight is the committed viewport height in rows.
func (b *ScrollBox) ViewportHeight() int { return b.viewportH }

// ScrollTop is the committed first visible content row.
func (b *ScrollBox) ScrollTop() int { return b.scrollTop }

// ItemOffsets returns each item's committed content-row offset after Layout.
// The shell uses it to translate block-local rows into transcript coordinates.
func (b *ScrollBox) ItemOffsets() []int {
	out := make([]int, len(b.Items))
	copy(out, b.offsets)
	return out
}

// StickyActive reports whether tail-follow is currently engaged.
func (b *ScrollBox) StickyActive() bool { return b.stickyActive }

// MaxScroll is contentH - viewportH, floored at zero.
func (b *ScrollBox) MaxScroll() int {
	if b.contentH <= b.viewportH {
		return 0
	}
	return b.contentH - b.viewportH
}

// AtBottom reports whether the viewport is at the end of the content.
func (b *ScrollBox) AtBottom() bool { return b.scrollTop >= b.MaxScroll() }

func (b *ScrollBox) atStickyEdge() bool {
	if !b.Sticky {
		return false
	}
	if b.Start == EdgeTop {
		return b.scrollTop <= 0
	}
	return b.scrollTop >= b.MaxScroll()
}

// Layout measures staged items, commits the scroll target against the new
// content height, then assigns every child an absolute rect. Layout never
// paints.
func (b *ScrollBox) Layout(r Rect) {
	b.SetBounds(r)
	b.placed = b.placed[:0]
	if !b.Visible {
		return
	}
	b.viewportH = max(0, r.H)
	b.contentW = max(0, r.W)
	if b.ShowScrollbar && b.contentW > 0 {
		b.contentW--
	}

	if cap(b.heights) < len(b.Items) {
		b.heights = make([]int, len(b.Items))
	} else {
		b.heights = b.heights[:len(b.Items)]
	}
	if cap(b.offsets) < len(b.Items)+1 {
		b.offsets = make([]int, len(b.Items)+1)
	} else {
		b.offsets = b.offsets[:len(b.Items)+1]
	}
	total := 0
	for i, it := range b.Items {
		h := it.H
		if h == 0 {
			if m, ok := it.W.(interface{ MeasureHeight(int) int }); ok {
				h = m.MeasureHeight(b.contentW)
			}
		}
		h = max(0, h)
		b.heights[i] = h
		b.offsets[i] = total
		total += h
	}
	b.offsets[len(b.Items)] = total
	b.contentH = total
	b.commit()

	y := r.Y - b.scrollTop
	for i, it := range b.Items {
		h := b.heights[i]
		cr := Rect{X: r.X, Y: y, W: b.contentW, H: h}
		if it.W != nil {
			it.W.Layout(cr)
		}
		b.placed = append(b.placed, cr)
		y += h
	}
	b.staged = false
}

// commit applies the S0 target/sticky rules to the freshly measured content.
func (b *ScrollBox) commit() {
	maxScroll := b.MaxScroll()
	switch b.target {
	case targetEnd:
		b.scrollTop = maxScroll
	case targetAbs:
		b.scrollTop = clampInt(b.targetAbs, 0, maxScroll)
		b.target = targetNone
	default:
		if b.stickyActive && b.Sticky {
			if b.Start == EdgeTop {
				b.scrollTop = 0
			} else {
				b.scrollTop = maxScroll
			}
		} else {
			b.scrollTop = clampInt(b.scrollTop, 0, maxScroll)
		}
	}
	b.stickyActive = b.atStickyEdge()
}

// ScrollTo scrolls to an absolute row now and queues it for the next layout. A
// target at/past the current end becomes a persistent end target; ScrollTo(0)
// is always an absolute top target.
func (b *ScrollBox) ScrollTo(y int) {
	maxScroll := b.MaxScroll()
	switch {
	case y <= 0:
		b.target, b.targetAbs = targetAbs, 0
		b.scrollTop = 0
	case y >= maxScroll:
		b.target = targetEnd
		b.scrollTop = maxScroll
	default:
		b.target, b.targetAbs = targetAbs, y
		b.scrollTop = y
	}
	b.stickyActive = b.atStickyEdge()
	b.MarkDirty()
}

// ScrollToEnd pins to the bottom and keeps following content growth.
func (b *ScrollBox) ScrollToEnd() {
	b.target = targetEnd
	b.scrollTop = b.MaxScroll()
	b.stickyActive = b.atStickyEdge()
	b.MarkDirty()
}

// ScrollBy applies a wheel/key step. Positive scrolls down. Scrolling away from
// the sticky edge pauses stickiness; landing on it resumes.
func (b *ScrollBox) ScrollBy(delta int) {
	if delta == 0 {
		return
	}
	before := b.scrollTop
	maxScroll := b.MaxScroll()
	switch b.target {
	case targetEnd:
		if delta > 0 {
			b.scrollTop = maxScroll
		} else {
			b.target = targetAbs
			b.targetAbs = clampInt(maxScroll+delta, 0, maxScroll)
			b.scrollTop = b.targetAbs
		}
	case targetAbs:
		n := b.targetAbs + delta
		if n > 0 && n >= maxScroll {
			b.target = targetEnd
			b.scrollTop = maxScroll
		} else {
			b.targetAbs = clampInt(n, 0, maxScroll)
			b.scrollTop = b.targetAbs
		}
	default:
		n := b.scrollTop + delta
		if n > 0 && n >= maxScroll {
			b.target = targetEnd
			b.scrollTop = maxScroll
		} else {
			b.target = targetAbs
			b.targetAbs = clampInt(n, 0, maxScroll)
			b.scrollTop = b.targetAbs
		}
	}
	b.stickyActive = b.atStickyEdge()
	if b.scrollTop != before {
		b.MarkDirty()
	}
}

// Key implements the OpenTUI scroll keymap: line +/-1, ctrl/shift/alt + arrow
// +/-5, page h-2, home/end. It returns true when consumed.
func (b *ScrollBox) Key(ev tea.KeyPressMsg) bool {
	page := max(1, b.viewportH-2)
	switch ev.Keystroke() {
	case "up":
		b.ScrollBy(-1)
	case "down":
		b.ScrollBy(1)
	case "ctrl+up", "shift+up", "alt+up":
		b.ScrollBy(-5)
	case "ctrl+down", "shift+down", "alt+down":
		b.ScrollBy(5)
	case "pgup":
		b.ScrollBy(-page)
	case "pgdown":
		b.ScrollBy(page)
	case "home":
		b.ScrollTo(0)
	case "end", "ctrl+g":
		b.ScrollToEnd()
	default:
		return false
	}
	return true
}

// Mouse handles wheel scrolling through WheelDelta (±1). Other mouse gestures
// bubble: blocks own selection and the shell owns empty-space clicks.
func (b *ScrollBox) Mouse(ev tea.MouseMsg, _ Rect) bool {
	delta, ok := WheelDelta(ev)
	if !ok {
		return false
	}
	b.ScrollBy(delta)
	return true
}

// Render paints the visible children (+1 overscan on each side) and the
// scrollbar column. Offscreen children are never rendered.
func (b *ScrollBox) Render(w, h int) string {
	if !b.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return b.cache.Render(w, h, subtreeRev(b), func() string {
		cv := lipgloss.NewCanvas(w, h)
		b.paint(cv, w, h)
		return padCanvas(cv, w, h)
	})
}

func (b *ScrollBox) paint(cv *lipgloss.Canvas, w, h int) {
	if len(b.Items) == 0 || b.contentH == 0 {
		b.drawScrollbar(cv, w, h)
		return
	}
	top := b.scrollTop
	bottom := top + h
	start := sort.Search(len(b.Items), func(i int) bool {
		return b.offsets[i]+b.heights[i] > top
	})
	end := sort.Search(len(b.Items), func(i int) bool {
		return b.offsets[i] >= bottom
	})
	if start > 0 {
		start-- // one item of overscan above
	}
	if end < len(b.Items) {
		end++ // one item of overscan below
	}
	contentW := w
	if b.ShowScrollbar && contentW > 0 {
		contentW--
	}
	for i := start; i < end && i < len(b.Items); i++ {
		it := b.Items[i]
		if it.W == nil || b.heights[i] <= 0 {
			continue
		}
		y := b.offsets[i] - top
		drawCells(cv, it.W.Render(contentW, b.heights[i]), 0, y, contentW, b.heights[i])
	}
	b.drawScrollbar(cv, w, h)
}

// drawScrollbar paints a one-cell right-edge track with a proportional thumb.
// TODO(s3): token review — resolved-wontfix in S5. The launch notes contain no
// scrollbar.thumb approval, so the thumb keeps borrowing Text.Strong; revisit
// only if a dedicated scrollbar.thumb role is approved.
func (b *ScrollBox) drawScrollbar(cv *lipgloss.Canvas, w, h int) {
	if !b.ShowScrollbar || w <= 0 || h <= 0 || b.contentH <= b.viewportH {
		return
	}
	theme := DefaultTheme()
	track := tokenOr(b.Track, theme.Scrollbar.Base)
	thumb := tokenOr(b.Thumb, theme.Text.Strong)
	thumbH := h
	if b.contentH > b.viewportH && b.contentH > 0 {
		thumbH = max(1, h*b.viewportH/b.contentH)
	}
	if thumbH > h {
		thumbH = h
	}
	offset := 0
	if maxScroll := b.MaxScroll(); maxScroll > 0 {
		offset = (h - thumbH) * b.scrollTop / maxScroll
	}
	x := w - 1
	for y := 0; y < h; y++ {
		glyph, tok := "│", track
		if y >= offset && y < offset+thumbH {
			glyph, tok = "█", thumb
		}
		putGlyph(cv, x, y, glyph, tok.Color())
	}
}

// Children exposes scroll items to the hit/focus/revision walks.
func (b *ScrollBox) Children() []Widget {
	out := make([]Widget, 0, len(b.Items))
	for _, it := range b.Items {
		if it.W != nil {
			out = append(out, it.W)
		}
	}
	return out
}

// FocusTarget is nil: the ScrollBox is scrolled by bindings owned by the shell.
func (b *ScrollBox) FocusTarget() *FocusTarget { return nil }

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
