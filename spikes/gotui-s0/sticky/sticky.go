// Package sticky is the S0 probe for OpenTUI's ScrollBox scroll state:
// stickyScroll + stickyStart, pause on scrolling away, resume at the edge, and
// scroll targets that are applied again after the next layout.
//
// The box owns no rendering. It models only the arithmetic a kit ScrollBox
// needs, so the state machine can be tested headlessly.
package sticky

// Edge is the sticky edge. S0 implements the vertical edges.
type Edge int

const (
	Bottom Edge = iota
	Top
)

type targetKind int

const (
	targetNone targetKind = iota
	targetAbs
	targetEnd // a position at/past the end; survives later content growth
)

// Box is a vertical scroll state machine. Content changes are staged with
// Append/SetContent and committed by Layout, mirroring a frame where content
// mutates before the layout pass.
type Box struct {
	viewportH int
	contentH  int // committed layout content height
	wantH     int // staged content height for the next Layout

	scrollTop int

	stickyScroll bool
	stickyStart  Edge
	stickyActive bool

	target    targetKind
	targetAbs int
}

// New creates a scroll box. A sticky box starts pinned to its sticky edge.
func New(viewportH, contentH int, stickyScroll bool, start Edge) *Box {
	if viewportH < 0 {
		viewportH = 0
	}
	if contentH < 0 {
		contentH = 0
	}
	b := &Box{
		viewportH:    viewportH,
		contentH:     contentH,
		wantH:        contentH,
		stickyScroll: stickyScroll,
		stickyStart:  start,
		stickyActive: stickyScroll,
	}
	b.Layout(viewportH)
	return b
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

// MaxScroll is scrollHeight - viewport.height, floored at zero.
func (b *Box) MaxScroll() int {
	if b.contentH <= b.viewportH {
		return 0
	}
	return b.contentH - b.viewportH
}

func (b *Box) ScrollTop() int      { return b.scrollTop }
func (b *Box) ScrollHeight() int   { return b.contentH }
func (b *Box) ViewportHeight() int { return b.viewportH }
func (b *Box) StickyActive() bool  { return b.stickyActive }
func (b *Box) AtEnd() bool         { return b.scrollTop >= b.MaxScroll() }
func (b *Box) atStickyEdge() bool {
	if !b.stickyScroll {
		return false
	}
	if b.stickyStart == Top {
		return b.scrollTop <= 0
	}
	return b.scrollTop >= b.MaxScroll()
}

// Append stages n more content rows (n may be negative to model removal).
func (b *Box) Append(n int) { b.SetContent(b.wantH + n) }

// SetContent stages the next content height. It is committed by Layout.
func (b *Box) SetContent(h int) {
	if h < 0 {
		h = 0
	}
	b.wantH = h
}

// Layout commits staged content/viewport sizes, then applies any queued scroll
// target against the new layout. A sticky box that is still active follows its
// edge; a paused box keeps its absolute position (clamped).
func (b *Box) Layout(viewportH int) {
	if viewportH < 0 {
		viewportH = 0
	}
	b.viewportH = viewportH
	b.contentH = b.wantH
	max := b.MaxScroll()

	switch b.target {
	case targetEnd:
		b.scrollTop = max
	case targetAbs:
		b.scrollTop = clamp(b.targetAbs, 0, max)
		b.target = targetNone
	default:
		if b.stickyActive && b.stickyScroll {
			if b.stickyStart == Top {
				b.scrollTop = 0
			} else {
				b.scrollTop = max
			}
		} else {
			b.scrollTop = clamp(b.scrollTop, 0, max)
		}
	}
	b.stickyActive = b.atStickyEdge()
}

// ScrollTo scrolls to an absolute position now and queues that position for
// the next layout. A position at/past the current end becomes a persistent
// end target, so it stays at the end after content grows. ScrollTo(0) is
// always an absolute top target, even while the content fits the viewport.
func (b *Box) ScrollTo(y int) {
	max := b.MaxScroll()
	switch {
	case y <= 0:
		b.target, b.targetAbs = targetAbs, 0
		b.scrollTop = 0
	case y >= max:
		b.target = targetEnd
		b.scrollTop = max
	default:
		b.target, b.targetAbs = targetAbs, y
		b.scrollTop = y
	}
	b.stickyActive = b.atStickyEdge()
}

// ScrollToEnd pins to the bottom and keeps following content growth.
func (b *Box) ScrollToEnd() {
	b.target = targetEnd
	b.scrollTop = b.MaxScroll()
	b.stickyActive = b.atStickyEdge()
}

// ScrollBy is a wheel/key step: positive scrolls down. It applies immediately
// against the current layout and adds to a target still waiting for Layout.
// Scrolling away from the sticky edge pauses stickiness; landing on it resumes.
func (b *Box) ScrollBy(delta int) {
	if delta == 0 {
		return
	}
	max := b.MaxScroll()

	switch b.target {
	case targetEnd:
		if delta > 0 {
			b.scrollTop = max // already at the end; stay pinned there
		} else {
			b.target = targetAbs
			b.targetAbs = clamp(max+delta, 0, max)
			b.scrollTop = b.targetAbs
		}
	case targetAbs:
		n := b.targetAbs + delta
		if n > 0 && n >= max {
			b.target = targetEnd
			b.scrollTop = max
		} else {
			b.targetAbs = clamp(n, 0, max)
			b.scrollTop = b.targetAbs
		}
	default:
		n := b.scrollTop + delta
		if n > 0 && n >= max {
			b.target = targetEnd
			b.scrollTop = max
		} else {
			b.target = targetAbs
			b.targetAbs = clamp(n, 0, max)
			b.scrollTop = b.targetAbs
		}
	}
	b.stickyActive = b.atStickyEdge()
}
