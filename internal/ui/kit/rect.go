package kit

// Rect is an integer cell rectangle in absolute screen coordinates. The
// zero value is an empty rectangle at the origin.
//
// It is deliberately not image.Rectangle: the kit works in W/H cell counts
// (what lipgloss and the terminal already speak), while image.Rectangle
// exposes Half-open Min/Max coordinates and drags an image dependency into a
// layout API that has nothing to do with pixels.
type Rect struct {
	X, Y, W, H int
}

// Empty reports whether the rectangle covers no cells. A negative axis is also
// empty so subtraction during layout can never produce a "backwards" hit area.
func (r Rect) Empty() bool { return r.W <= 0 || r.H <= 0 }

// Contains reports whether the cell at (x, y) lies inside the rectangle.
// The right and bottom edges are exclusive, matching terminal cell addressing.
// OpenTUI counterpart: Renderable.bounds + Yoga computed rectangle hit tests.
func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Intersect returns the overlap of r and o. The result is empty (W or H zero)
// when the rectangles do not overlap. OpenTUI counterpart: overflow:hidden
// clip propagation during absolute layout.
func (r Rect) Intersect(o Rect) Rect {
	x0 := max(r.X, o.X)
	y0 := max(r.Y, o.Y)
	x1 := min(r.X+r.W, o.X+o.W)
	y1 := min(r.Y+r.H, o.Y+o.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{X: x0, Y: y0}
	}
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// Apply removes the given insets from the rectangle, clamping each axis at
// zero. It is the geometry behind Box padding/margin and Pane border cuts.
func (r Rect) Apply(in Insets) Rect {
	return Rect{
		X: r.X + in.Left,
		Y: r.Y + in.Top,
		W: max(0, r.W-in.Left-in.Right),
		H: max(0, r.H-in.Top-in.Bottom),
	}
}
