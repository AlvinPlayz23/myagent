package kit

import (
	"math"
	"sort"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// Direction is the main axis of a Box. Column is the zero value, matching
// Yoga/OpenTUI's default flexDirection (GOTUI §3.0.1).
// OpenTUI: flexDirection.
type Direction uint8

const (
	// Column stacks children top-to-bottom.
	Column Direction = iota
	// Row places children left-to-right.
	Row
)

// Align is the cross-axis alignment subset (GOTUI §3.0.1).
// OpenTUI: alignItems / alignSelf.
type Align uint8

const (
	// AlignAuto inherits from the container; a container's auto resolves to
	// stretch (Yoga's default).
	AlignAuto Align = iota
	// AlignStretch makes the child fill the cross axis.
	AlignStretch
	// AlignStart pins the child to the cross-axis start.
	AlignStart
	// AlignCenter centers the child on the cross axis.
	AlignCenter
	// AlignEnd pins the child to the cross-axis end.
	AlignEnd
)

// Justify is the main-axis distribution subset.
// OpenTUI: justifyContent.
type Justify uint8

const (
	// JustifyStart packs children at the start (Yoga default).
	JustifyStart Justify = iota
	// JustifyCenter centers the packed line.
	JustifyCenter
	// JustifyEnd packs children at the end.
	JustifyEnd
	// JustifySpaceBetween keeps the first/last at the edges and spreads the
	// leftover cells between children.
	JustifySpaceBetween
)

// Insets are per-edge cell counts. Box uses them for padding/margin; keeping
// one type avoids the Margin/MarginTop/MarginLeft explosion while still
// allowing the CSS one/two/four-value shorthand through InsetsFrom.
// OpenTUI: padding/margin shorthands.
type Insets struct {
	Top, Right, Bottom, Left int
}

// InsetsAll applies n to every edge.
func InsetsAll(n int) Insets { return Insets{Top: n, Right: n, Bottom: n, Left: n} }

// InsetsXY sets horizontal x and vertical y.
func InsetsXY(x, y int) Insets { return Insets{Top: y, Right: x, Bottom: y, Left: x} }

// InsetsTRBL sets the four edges explicitly.
func InsetsTRBL(top, right, bottom, left int) Insets {
	return Insets{Top: top, Right: right, Bottom: bottom, Left: left}
}

// InsetsFrom implements the CSS-style shorthand: 1 value = all edges,
// 2 = vertical/horizontal, 4 = top/right/bottom/left. Other counts return the
// zero insets rather than panicking.
// OpenTUI: padding={2} / paddingX / paddingY.
func InsetsFrom(values ...int) Insets {
	switch len(values) {
	case 1:
		return InsetsAll(values[0])
	case 2:
		return InsetsXY(values[1], values[0])
	case 4:
		return InsetsTRBL(values[0], values[1], values[2], values[3])
	default:
		return Insets{}
	}
}

// Position places a child absolutely inside the parent's content rect.
// The zero value means "participate in flex flow".
// OpenTUI: position="absolute" + left/top.
type Position struct {
	Absolute bool
	X, Y     int
}

// Child is one entry in a Box's flex line. The Grow/Fixed fields are the
// compact §3.2 vocabulary; FlexGrow/FlexBasis are the explicit §3.0.1 names
// and win when non-zero. FlexShrink defaults to 0 (numeric size never shrinks,
// the S0-verified OpenTUI default).
type Child struct {
	W Widget

	// Grow is the integer flex-grow weight (legacy compact spelling, GOTUI
	// §3.2). FlexGrow wins when non-zero.
	Grow int
	// Fixed is the main-axis base in cells (legacy compact spelling).
	Fixed int

	// FlexGrow is the explicit flex-grow weight. It wins over Grow.
	FlexGrow float64
	// FlexShrink is the flex-shrink factor. 0 disables shrinking, matching
	// "numeric size => flexShrink: 0" in §3.0.1.
	FlexShrink float64
	// FlexBasis is the main-axis base in cells. It wins over Fixed.
	FlexBasis int

	// MinW/MaxW/MinH/MaxH clamp the computed rect. Zero means unset (Max is
	// "unbounded", not "zero cells").
	MinW, MaxW, MinH, MaxH int

	// Width/Height give an explicit cross-axis size in flow (0 = fill), and
	// the absolute frame in Position.Absolute.
	Width, Height int

	// CrossAlign overrides the box's Align for this child. AlignAuto inherits.
	CrossAlign Align

	// Position moves the child out of flow.
	Position Position
}

func (c Child) grow() float64 {
	if c.FlexGrow != 0 {
		return c.FlexGrow
	}
	return float64(c.Grow)
}

func (c Child) basis() int {
	if c.FlexBasis != 0 {
		return c.FlexBasis
	}
	return c.Fixed
}

func (c Child) minMain(dir Direction) int {
	if dir == Row {
		return max(0, c.MinW)
	}
	return max(0, c.MinH)
}

func (c Child) maxMain(dir Direction) int {
	if dir == Row {
		return c.MaxW
	}
	return c.MaxH
}

// BoxOpts is the struct-first constructor form from GOTUI §3.0.1.
type BoxOpts struct {
	ID        string
	Direction Direction
	Justify   Justify
	Align     Align
	Gap       int
	Padding   Insets
	Margin    Insets
	Fill      tui.Token
	Filled    bool
}

// Box is the flex container: the Go implementation of OpenTUI's <box>, with
// the Yoga subset pinned in §3.0.1 and no measure pass. The zero value is
// invisible; always construct with NewBox/VBox/HBox.
type Box struct {
	WidgetBase

	Direction Direction
	Justify   Justify
	Align     Align
	Gap       int
	Padding   Insets
	Margin    Insets

	// Fill is the surface color painted before children when Filled is true.
	Fill   tui.Token
	Filled bool

	children []Child
	placed   []placement
	cache    RenderCache
}

type placement struct {
	w Widget
	r Rect
}

// NewBox builds an empty flex container. OpenTUI: <box>.
func NewBox(opts BoxOpts) *Box {
	return &Box{
		WidgetBase: NewWidgetBase(opts.ID),
		Direction:  opts.Direction,
		Justify:    opts.Justify,
		Align:      opts.Align,
		Gap:        opts.Gap,
		Padding:    opts.Padding,
		Margin:     opts.Margin,
		Fill:       opts.Fill,
		Filled:     opts.Filled,
	}
}

// VBox builds a column with the given children. OpenTUI: <box flexDirection="column">.
func VBox(children ...Child) *Box {
	return NewBox(BoxOpts{Direction: Column}).Add(children...)
}

// HBox builds a row with the given children. OpenTUI: <box flexDirection="row">.
func HBox(children ...Child) *Box {
	return NewBox(BoxOpts{Direction: Row}).Add(children...)
}

// Add appends children in document order (OpenTUI's add() reparenting
// semantics: the child is owned by this box from now on).
func (b *Box) Add(children ...Child) *Box {
	for _, c := range children {
		if c.W == nil {
			continue
		}
		b.children = append(b.children, c)
	}
	b.MarkDirty()
	return b
}

// Children exposes document order for the hit registry and focus discovery.
// It implements the optional Container extension of the Widget contract.
func (b *Box) Children() []Widget {
	out := make([]Widget, 0, len(b.children))
	for _, c := range b.children {
		out = append(out, c.W)
	}
	return out
}

// Layout distributes the assigned rect with the Yoga-subset algorithm and
// recurses into every child, including zero-size ones (S0 zero-size rule).
// OpenTUI: box yoga layout pass.
func (b *Box) Layout(r Rect) {
	b.SetBounds(r)
	b.placed = b.placed[:0]
	if !b.Visible {
		return
	}

	surface := r.Apply(b.Margin)
	content := surface.Apply(b.Padding)

	flow := make([]int, 0, len(b.children))
	items := make([]flexItem, 0, len(b.children))
	for i := range b.children {
		c := &b.children[i]
		if !WidgetVisible(c.W) {
			continue // hidden nodes stay in the tree but take no space
		}
		if c.Position.Absolute {
			cr := b.absoluteRect(c, content)
			c.W.Layout(cr)
			b.placed = append(b.placed, placement{w: c.W, r: cr})
			continue
		}
		flow = append(flow, i)
		items = append(items, flexItem{
			base:   c.basis(),
			grow:   c.grow(),
			shrink: c.FlexShrink,
			min:    c.minMain(b.Direction),
			max:    c.maxMain(b.Direction),
		})
	}

	main, cross := content.H, content.W
	if b.Direction == Row {
		main, cross = content.W, content.H
	}
	spans := distribute(main, b.Gap, items)
	applyJustify(spans, main, b.Justify)

	for n, idx := range flow {
		c := &b.children[idx]
		span := spans[n]
		size := crossSize(c, b.Align, b.Direction, cross)
		offset := crossOffset(c, b.Align, b.Direction, cross, size)
		var cr Rect
		if b.Direction == Row {
			cr = Rect{X: content.X + span.Start, Y: content.Y + offset, W: span.Width, H: size}
		} else {
			cr = Rect{X: content.X + offset, Y: content.Y + span.Start, W: size, H: span.Width}
		}
		c.W.Layout(cr)
		b.placed = append(b.placed, placement{w: c.W, r: cr})
	}
}

// absoluteRect resolves an out-of-flow child against the content rect.
// OpenTUI: position absolute + left/top/width/height.
func (b *Box) absoluteRect(c *Child, content Rect) Rect {
	x := content.X + c.Position.X
	y := content.Y + c.Position.Y
	w := c.Width
	if w <= 0 {
		w = max(0, content.W-c.Position.X)
	}
	h := c.Height
	if h <= 0 {
		h = max(0, content.H-c.Position.Y)
	}
	if c.MinW > 0 && w < c.MinW {
		w = c.MinW
	}
	if c.MaxW > 0 && w > c.MaxW {
		w = c.MaxW
	}
	if c.MinH > 0 && h < c.MinH {
		h = c.MinH
	}
	if c.MaxH > 0 && h > c.MaxH {
		h = c.MaxH
	}
	return Rect{X: x, Y: y, W: max(0, w), H: max(0, h)}
}

// crossSize resolves a child's cross-axis extent. Explicit Width/Height wins;
// otherwise auto/stretch fills the container, and the other alignments still
// need an explicit size to have a visible effect (the kit has no measure pass).
// OpenTUI: alignItems stretch vs flex-start/center/flex-end.
func crossSize(c *Child, boxAlign Align, dir Direction, available int) int {
	explicit := c.Height
	limitMin, limitMax := c.MinH, c.MaxH
	if dir == Column {
		explicit = c.Width
		limitMin, limitMax = c.MinW, c.MaxW
	}
	size := available
	if explicit > 0 {
		size = explicit
	}
	align := c.CrossAlign
	if align == AlignAuto {
		align = boxAlign
	}
	if align == AlignStretch && explicit <= 0 {
		size = available
	}
	if limitMin > 0 && size < limitMin {
		size = limitMin
	}
	if limitMax > 0 && size > limitMax {
		size = limitMax
	}
	return max(0, size)
}

// crossOffset places the child inside the cross axis. Stretch/start = 0.
func crossOffset(c *Child, boxAlign Align, dir Direction, available, size int) int {
	align := c.CrossAlign
	if align == AlignAuto {
		align = boxAlign
	}
	switch align {
	case AlignCenter:
		return max(0, (available-size)/2)
	case AlignEnd:
		return max(0, available-size)
	default:
		return 0
	}
}

// flexItem is the per-child input to the rounding algorithm.
type flexItem struct {
	base         int
	grow, shrink float64
	min, max     int
}

// span is the main-axis range assigned to one child.
type span struct {
	Start int
	Width int
}

// distribute is the ported S0 algorithm (spikes/gotui-s0/flex/flex.go):
// compute exact cell sizes, then round each cumulative edge with math.Round.
// Rounding edges rather than widths preserves the total and reproduces the
// OpenTUI-documented 31-cell / 3-grow result 10, 11, 10.
//
// Extension over S0: optional flex-shrink and min/max clamps. When no child
// shrinks, overflow is left past the container edge (the S0 default). Grow
// space is distributed only when there is positive free space.
func distribute(main, gap int, items []flexItem) []span {
	n := len(items)
	if n == 0 {
		return nil
	}
	if main < 0 {
		main = 0
	}
	if gap < 0 {
		gap = 0
	}
	gaps := 0
	if n > 1 {
		gaps = gap * (n - 1)
	}
	available := main - gaps

	base := make([]int, n)
	totalBase := 0
	growTotal := 0.0
	shrinkTotal := 0.0
	for i, it := range items {
		b := max(it.base, 0)
		if it.max > 0 && b > it.max {
			b = it.max
		}
		if b < it.min {
			b = it.min
		}
		base[i] = b
		totalBase += b
		if it.grow > 0 {
			growTotal += it.grow
		}
		if it.shrink > 0 && b > 0 {
			shrinkTotal += it.shrink * float64(b)
		}
	}

	exact := make([]float64, n)
	for i := range exact {
		exact[i] = float64(base[i])
	}
	switch {
	case available > totalBase && growTotal > 0:
		extra := float64(available - totalBase)
		for i, it := range items {
			if it.grow > 0 {
				exact[i] += extra * it.grow / growTotal
			}
		}
	case available < totalBase && shrinkTotal > 0:
		deficit := float64(totalBase - available)
		for i, it := range items {
			if it.shrink <= 0 || base[i] == 0 {
				continue
			}
			cut := deficit * (it.shrink * float64(base[i])) / shrinkTotal
			if cut > exact[i] {
				cut = exact[i]
			}
			exact[i] -= cut
		}
	}

	for i, it := range items {
		if exact[i] < 0 {
			exact[i] = 0
		}
		if it.max > 0 && exact[i] > float64(it.max) {
			exact[i] = float64(it.max)
		}
		if exact[i] < float64(it.min) {
			exact[i] = float64(it.min)
		}
	}

	spans := make([]span, n)
	pos := 0.0
	for i := range exact {
		start := int(math.Round(pos))
		pos += exact[i]
		end := int(math.Round(pos))
		spans[i] = span{Start: start, Width: max(0, end-start)}
		pos += float64(gap)
	}
	return spans
}

// applyJustify distributes leftover main-axis cells after flex growth. It runs
// on the spans so its math is independent of grow/shrink. Space-between adds
// the remainder to each inter-child gap using cumulative rounding, keeping the
// line exactly within main when possible.
// OpenTUI: justifyContent.
func applyJustify(spans []span, main int, justify Justify) {
	if len(spans) == 0 {
		return
	}
	used := 0
	for _, s := range spans {
		used = max(used, s.Start+s.Width)
	}
	extra := main - used
	if extra <= 0 {
		return
	}
	switch justify {
	case JustifyCenter:
		shift := extra / 2
		for i := range spans {
			spans[i].Start += shift
		}
	case JustifyEnd:
		for i := range spans {
			spans[i].Start += extra
		}
	case JustifySpaceBetween:
		if len(spans) < 2 {
			return
		}
		last := len(spans) - 1
		for i := range spans {
			spans[i].Start += int(math.Round(float64(extra*i) / float64(last)))
		}
	}
}

// Render paints the surface and children exactly w by h. Children publish
// themselves in Z order; equal Z keeps document order (stable sort).
// OpenTUI: box render + child render buffers.
func (b *Box) Render(w, h int) string {
	if !b.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return b.cache.Render(w, h, subtreeRev(b), func() string {
		c := lipgloss.NewCanvas(w, h)
		if b.Filled {
			fill := tokenOr(b.Fill, DefaultTheme().Background.Base)
			surface := b.Bounds().Apply(b.Margin)
			fillRect(c, surface.X-b.Bounds().X, surface.Y-b.Bounds().Y, surface.W, surface.H, fill)
		}
		order := make([]int, len(b.placed))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(i, j int) bool {
			return WidgetZ(b.placed[order[i]].w) < WidgetZ(b.placed[order[j]].w)
		})
		for _, i := range order {
			p := b.placed[i]
			drawWidget(c, p.w, p.r, b.Bounds())
		}
		return padCanvas(c, w, h)
	})
}

// Key gives containers no text-input behavior; Root routes the focused leaf
// first and bubbles mouse events through the registry.
// OpenTUI: box handleKeyPress (usually no-op).
func (b *Box) Key(tea.KeyPressMsg) bool { return false }

// Mouse is a no-op: children are hit-tested individually by the registry, and
// an unconsumed event bubbles to this box from a child.
// OpenTUI: box handleMouse.
func (b *Box) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil: boxes are not focusable by themselves.
// OpenTUI: focusable=false default.
func (b *Box) FocusTarget() *FocusTarget { return nil }
