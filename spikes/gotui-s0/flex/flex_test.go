package flex

import (
	"reflect"
	"testing"
)

func widths(spans []Span) []int {
	out := make([]int, len(spans))
	for i, s := range spans {
		out[i] = s.Width
	}
	return out
}

// OpenTUI's documented cell-rounding example: a 32-column bordered row leaves
// 30 inner columns (10/10/10); adding one column gives the middle child the
// extra cell (10/11/10). Our W is the inner width, so W=31 is the 33-column
// case and W=30 is the 32-column case. Gap is 0 in the documented example.
func TestOpenTUIDocumentedExample(t *testing.T) {
	kids := []Child{{Grow: 1}, {Grow: 1}, {Grow: 1}}
	if got := widths(Distribute(30, 0, kids)); !reflect.DeepEqual(got, []int{10, 10, 10}) {
		t.Fatalf("W=30 widths = %v, want [10 10 10]", got)
	}
	if got := widths(Distribute(31, 0, kids)); !reflect.DeepEqual(got, []int{10, 11, 10}) {
		t.Fatalf("W=31 widths = %v, want [10 11 10] (middle gets the extra cell)", got)
	}
	if got := widths(Distribute(32, 0, kids)); !reflect.DeepEqual(got, []int{11, 10, 11}) {
		t.Fatalf("W=32 widths = %v, want [11 10 11]", got)
	}
}

func TestFixedAndGrowMix(t *testing.T) {
	// 21 cells, two one-cell gaps => 19 for content; 5 fixed + 14 split 7/7.
	kids := []Child{{Fixed: 5}, {Grow: 1}, {Grow: 1}}
	got := Distribute(21, 1, kids)
	want := []Span{{0, 5}, {6, 7}, {14, 7}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spans = %v, want %v", got, want)
	}

	// Weighted 1:2 after a fixed base. Exact edges 0,4,9.33,20 round to 0,4,9,20.
	weighted := Distribute(20, 0, []Child{{Fixed: 4}, {Grow: 1}, {Grow: 2}})
	if want := []Span{{0, 4}, {4, 5}, {9, 11}}; !reflect.DeepEqual(weighted, want) {
		t.Fatalf("weighted spans = %v, want %v", weighted, want)
	}
}

func TestGrowZeroAndNegativeSpace(t *testing.T) {
	// No growers: leftover space stays blank at the end.
	got := Distribute(20, 1, []Child{{Fixed: 4}, {Fixed: 5}})
	if want := []Span{{0, 4}, {5, 5}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("no-grow spans = %v, want %v (10 trailing cells unused)", got, want)
	}

	// Fixed bases + gaps exceed W: no shrink, spans overflow to the right.
	over := Distribute(10, 1, []Child{{Fixed: 6}, {Fixed: 7}})
	if want := []Span{{0, 6}, {7, 7}}; !reflect.DeepEqual(over, want) {
		t.Fatalf("overflow spans = %v, want %v", over, want)
	}
	if end := over[1].Start + over[1].Width; end != 14 {
		t.Fatalf("overflow end = %d, want 14 (> W=10)", end)
	}

	// A grow child gets zero, never a negative width, when there is no space.
	zeroGrow := Distribute(5, 1, []Child{{Fixed: 6}, {Grow: 1}})
	if want := []Span{{0, 6}, {7, 0}}; !reflect.DeepEqual(zeroGrow, want) {
		t.Fatalf("negative-space grow spans = %v, want %v", zeroGrow, want)
	}
}

// A zero-width child collapses to an empty span, but it is still in the
// layout sequence and its siblings still paint at the same coordinate.
//
// For the kit: Layout must recurse into a zero-size child (its children keep
// their own bounds), and Render/Hit must not early-return just because the
// parent's W/H is zero. The parent only erases them when overflow:hidden clips.
func TestZeroWidthChildCollapsesButStaysInFlow(t *testing.T) {
	got := Distribute(10, 0, []Child{{Fixed: 3}, {Fixed: 0}, {Grow: 1}})
	want := []Span{{0, 3}, {3, 0}, {3, 7}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("spans = %v, want %v", got, want)
	}
	if got[1].Width != 0 {
		t.Fatalf("zero-width child width = %d, want 0", got[1].Width)
	}
	if got[2].Start != got[1].Start {
		t.Fatalf("sibling starts at %d, want %d (zero-width child takes no cells)", got[2].Start, got[1].Start)
	}
}

func TestInvariants(t *testing.T) {
	cases := []struct {
		w    int
		gap  int
		kids []Child
	}{
		{31, 0, []Child{{Grow: 1}, {Grow: 1}, {Grow: 1}}},
		{80, 1, []Child{{Fixed: 20}, {Grow: 1}, {Grow: 3}, {Fixed: 10}}},
		{7, 2, []Child{{Grow: 1}, {Grow: 1}}},
		{2, 5, []Child{{Fixed: 1}, {Fixed: 1}, {Fixed: 1}}},
		{0, 0, []Child{{Grow: 1}, {Grow: 1}}},
	}
	for _, tc := range cases {
		spans := Distribute(tc.w, tc.gap, tc.kids)
		if len(spans) != len(tc.kids) {
			t.Fatalf("%+v: got %d spans, want %d", tc, len(spans), len(tc.kids))
		}
		for i, s := range spans {
			if s.Width < 0 {
				t.Errorf("%+v: span %d width %d < 0", tc, i, s.Width)
			}
			if i == 0 {
				continue
			}
			prev := spans[i-1]
			if s.Start != prev.Start+prev.Width+tc.gap {
				t.Errorf("%+v: span %d starts at %d, want %d (prev end %d + gap %d)",
					tc, i, s.Start, prev.Start+prev.Width+tc.gap, prev.Start+prev.Width, tc.gap)
			}
		}
		// When growers absorb the space, the final edge is exactly W.
		hasGrow := false
		for _, k := range tc.kids {
			hasGrow = hasGrow || k.Grow > 0
		}
		last := spans[len(spans)-1]
		if hasGrow && tc.w > 0 && last.Start+last.Width != tc.w {
			t.Errorf("%+v: final edge = %d, want W=%d", tc, last.Start+last.Width, tc.w)
		}
	}
}
