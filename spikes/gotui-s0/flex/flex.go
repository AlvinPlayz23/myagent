// Package flex is the S0 probe for the kit's flex subset: fixed bases plus
// flex-grow weights on an integer terminal-cell grid, with a constant gap.
//
// Rounding mirrors Yoga/OpenTUI's point-scale-factor-1 contract: compute exact
// edges, then round each cumulative edge to the nearest cell. Rounding edges
// (not individual widths) preserves the layout total and gives the documented
// 31-column / three-grow-child result: 10, 11, 10 (middle gets the extra cell).
package flex

import "math"

// Child is one main-axis item. Fixed is its base size in cells; Grow is its
// flex-grow weight (0 = does not grow). A child may have both (flex basis plus
// growth), matching flexBasis + flexGrow.
type Child struct {
	Fixed int
	Grow  float64
}

// Span is a child's computed cell range: [Start, Start+Width).
type Span struct {
	Start int
	Width int
}

// Distribute lays out children across w cells with gap cells between adjacent
// children. It never shrinks: when fixed bases plus gaps exceed w, spans
// overflow past w instead of going negative (flexShrink is not part of S0).
// With no growers, unused space is left blank at the end.
func Distribute(w, gap int, children []Child) []Span {
	n := len(children)
	if n == 0 {
		return nil
	}
	if w < 0 {
		w = 0
	}
	if gap < 0 {
		gap = 0
	}
	gaps := 0
	if n > 1 {
		gaps = gap * (n - 1)
	}
	available := w - gaps

	base := 0
	growTotal := 0.0
	for _, c := range children {
		b := c.Fixed
		if b < 0 {
			b = 0
		}
		base += b
		if c.Grow > 0 {
			growTotal += c.Grow
		}
	}

	extra := 0.0
	if available > base && growTotal > 0 {
		extra = float64(available - base)
	}

	spans := make([]Span, n)
	pos := 0.0
	for i, c := range children {
		b := c.Fixed
		if b < 0 {
			b = 0
		}
		exact := float64(b)
		if c.Grow > 0 && growTotal > 0 {
			exact += extra * c.Grow / growTotal
		}
		start := int(math.Round(pos))
		pos += exact
		end := int(math.Round(pos))
		spans[i] = Span{Start: start, Width: end - start}
		pos += float64(gap)
	}
	return spans
}
