package kit

import (
	"testing"
)

// TestDistributeOpenTUI31 is the G2 gate: the S0-verified OpenTUI example.
// 31 cells across three equal growers must round cumulative edges, not
// individual widths, producing 10, 11, 10 (middle gets the extra cell).
func TestDistributeOpenTUI31(t *testing.T) {
	cases := []struct {
		name  string
		main  int
		gap   int
		items []flexItem
		want  []int
	}{
		{
			name:  "31 three growers",
			main:  31,
			items: []flexItem{{grow: 1}, {grow: 1}, {grow: 1}},
			want:  []int{10, 11, 10},
		},
		{
			name:  "30 three growers",
			main:  30,
			items: []flexItem{{grow: 1}, {grow: 1}, {grow: 1}},
			want:  []int{10, 10, 10},
		},
		{
			name:  "32 three growers",
			main:  32,
			items: []flexItem{{grow: 1}, {grow: 1}, {grow: 1}},
			want:  []int{11, 10, 11},
		},
		{
			name:  "fixed plus growers with gap",
			main:  21,
			gap:   1,
			items: []flexItem{{base: 5}, {grow: 1}, {grow: 1}},
			want:  []int{5, 7, 7},
		},
		{
			name:  "one to two weight",
			main:  20,
			items: []flexItem{{base: 4}, {grow: 1}, {grow: 2}},
			want:  []int{4, 5, 11},
		},
		{
			name:  "fixed no growers leaves trailing blank",
			main:  20,
			gap:   1,
			items: []flexItem{{base: 4}, {base: 5}},
			want:  []int{4, 5},
		},
		{
			name:  "overflow no shrink",
			main:  10,
			gap:   1,
			items: []flexItem{{base: 6}, {base: 7}},
			want:  []int{6, 7},
		},
		{
			name:  "grow child collapses at zero",
			main:  5,
			gap:   1,
			items: []flexItem{{base: 6}, {grow: 1}},
			want:  []int{6, 0},
		},
		{
			name:  "shrink distributes deficit",
			main:  10,
			items: []flexItem{{base: 10, shrink: 1}, {base: 10, shrink: 1}},
			want:  []int{5, 5},
		},
		{
			name:  "min clamps shrink",
			main:  8,
			items: []flexItem{{base: 10, shrink: 1, min: 6}, {base: 10, shrink: 1}},
			want:  []int{6, 4},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spans := distribute(tc.main, tc.gap, tc.items)
			if len(spans) != len(tc.want) {
				t.Fatalf("got %d spans, want %d", len(spans), len(tc.want))
			}
			for i, want := range tc.want {
				if spans[i].Width != want {
					t.Fatalf("span %d width = %d, want %d (spans %+v)", i, spans[i].Width, want, spans)
				}
			}
		})
	}
}

// TestDistributeEdges pins the cumulative-edge invariant: the final edge of
// the line is exactly main whenever growers absorb the space, and gaps are
// exactly gap cells.
func TestDistributeEdges(t *testing.T) {
	spans := distribute(31, 2, []flexItem{{grow: 1}, {grow: 1}, {grow: 1}})
	if got, want := spans[2].Start+spans[2].Width, 31; got != want {
		t.Fatalf("final edge = %d, want %d", got, want)
	}
	if got, want := spans[1].Start-(spans[0].Start+spans[0].Width), 2; got != want {
		t.Fatalf("first gap = %d, want %d", got, want)
	}
	if got, want := spans[2].Start-(spans[1].Start+spans[1].Width), 2; got != want {
		t.Fatalf("second gap = %d, want %d", got, want)
	}
}

// TestBoxLayout31 reproduces the OpenTUI example through the actual Box layout
// pass (row direction, no padding), asserting child bounds rather than just
// the distribution helper.
func TestBoxLayout31(t *testing.T) {
	mk := func(id string) *probeWidget {
		return newProbe(id)
	}
	a, b, c := mk("a"), mk("b"), mk("c")
	row := HBox(
		Child{W: a, Grow: 1},
		Child{W: b, Grow: 1},
		Child{W: c, Grow: 1},
	)
	row.Layout(Rect{X: 0, Y: 0, W: 31, H: 1})

	want := []Rect{
		{X: 0, Y: 0, W: 10, H: 1},
		{X: 10, Y: 0, W: 11, H: 1},
		{X: 21, Y: 0, W: 10, H: 1},
	}
	for i, w := range []*probeWidget{a, b, c} {
		if got := w.Bounds(); got != want[i] {
			t.Errorf("child %d bounds = %+v, want %+v", i, got, want[i])
		}
	}
}

// TestBoxLayoutDefaults pins the ported defaults: column direction, no
// shrink for numeric sizes, stretch cross axis, fixed children overflow.
func TestBoxLayoutDefaults(t *testing.T) {
	child := newProbe("child")
	box := NewBox(BoxOpts{}).Add(Child{W: child, Fixed: 10})
	box.Layout(Rect{X: 0, Y: 0, W: 5, H: 8})
	if got, want := child.Bounds(), (Rect{X: 0, Y: 0, W: 5, H: 10}); got != want {
		t.Fatalf("fixed child = %+v, want %+v (no shrink default)", got, want)
	}
}

// TestBoxPaddingMarginAbsolute covers the shorthand + absolute positioning
// path used by overlay chrome.
func TestBoxPaddingMarginAbsolute(t *testing.T) {
	flow := newProbe("flow")
	overlay := newProbe("overlay")
	box := NewBox(BoxOpts{
		Direction: Row,
		Padding:   InsetsTRBL(1, 2, 3, 4),
		Margin:    InsetsAll(1),
	}).Add(
		Child{W: flow, Grow: 1},
		Child{W: overlay, Position: Position{Absolute: true, X: 2, Y: 1}, Width: 6, Height: 2},
	)
	box.Layout(Rect{X: 0, Y: 0, W: 20, H: 10})

	// Surface = (1,1)-(19,9); padding -> content (5,2) size 12x4.
	if got, want := flow.Bounds(), (Rect{X: 5, Y: 2, W: 12, H: 4}); got != want {
		t.Fatalf("flow = %+v, want %+v", got, want)
	}
	if got, want := overlay.Bounds(), (Rect{X: 7, Y: 3, W: 6, H: 2}); got != want {
		t.Fatalf("overlay = %+v, want %+v", got, want)
	}
}

// TestJustify applies the main-axis subset.
func TestJustify(t *testing.T) {
	cases := []struct {
		name    string
		justify Justify
		want    int
	}{
		{"start", JustifyStart, 0},
		{"center", JustifyCenter, 3},
		{"end", JustifyEnd, 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			child := newProbe("child")
			box := NewBox(BoxOpts{Direction: Row, Justify: tc.justify}).
				Add(Child{W: child, Fixed: 4})
			box.Layout(Rect{X: 0, Y: 0, W: 10, H: 1})
			if got := child.Bounds().X; got != tc.want {
				t.Fatalf("x = %d, want %d", got, tc.want)
			}
		})
	}
}
