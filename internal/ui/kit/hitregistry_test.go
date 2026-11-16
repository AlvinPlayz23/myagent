package kit

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestHitZBandTopmostWins is the core G3 case: a dialog-band hit beats a
// transcript-band hit even when the transcript registered later/deeper.
func TestHitZBandTopmostWins(t *testing.T) {
	h := NewHitRegistry()
	base := newProbe("transcript")
	overlay := newProbe("dialog")
	h.Register(Registration{ID: "transcript", Widget: base, Rect: Rect{0, 0, 20, 10}, Clip: Rect{0, 0, 20, 10}, Band: BandBase})
	h.Register(Registration{ID: "dialog", Widget: overlay, Rect: Rect{0, 0, 20, 10}, Clip: Rect{0, 0, 20, 10}, Band: BandDialogs})

	hit, ok := h.Hit(4, 4)
	if !ok || hit.ID != "dialog" {
		t.Fatalf("hit = %+v, ok=%v; want dialog", hit, ok)
	}
}

// TestHitZWithinBand: inside one band, higher sibling Z wins.
func TestHitZWithinBand(t *testing.T) {
	h := NewHitRegistry()
	low := newProbe("low")
	high := newProbe("high")
	h.Register(Registration{ID: "low", Widget: low, Rect: Rect{0, 0, 10, 10}, Clip: Rect{0, 0, 10, 10}, Z: 0})
	h.Register(Registration{ID: "high", Widget: high, Rect: Rect{2, 2, 4, 4}, Clip: Rect{0, 0, 10, 10}, Z: 5})

	if hit, _ := h.Hit(3, 3); hit.ID != "high" {
		t.Fatalf("overlap hit = %q, want high", hit.ID)
	}
	if hit, _ := h.Hit(0, 0); hit.ID != "low" {
		t.Fatalf("outside high Z hit = %q, want low", hit.ID)
	}
}

// TestHitChildBeatsHighZParent: Z orders siblings, not a widget against its own
// descendants; the child paints last and must win.
func TestHitChildBeatsHighZParent(t *testing.T) {
	h := NewHitRegistry()
	parent := newProbe("parent")
	child := newProbe("child")
	rect := Rect{0, 0, 10, 10}
	h.Register(Registration{ID: "parent", Widget: parent, Rect: rect, Clip: rect, Z: 10})
	h.Register(Registration{ID: "child", Widget: child, Rect: rect, Clip: rect, Chain: []Widget{parent}})
	if hit, _ := h.Hit(1, 1); hit.ID != "child" {
		t.Fatalf("nested hit = %q, want child", hit.ID)
	}
}

// TestHitIDLessSkipped: widgets without an ID never register or hit.
func TestHitIDLessSkipped(t *testing.T) {
	h := NewHitRegistry()
	w := newProbe("")
	h.Register(Registration{Widget: w, Rect: Rect{0, 0, 10, 10}, Clip: Rect{0, 0, 10, 10}})
	if h.Len() != 0 {
		t.Fatalf("ID-less registration added %d entries", h.Len())
	}
	if _, ok := h.Hit(1, 1); ok {
		t.Fatal("ID-less widget was hittable")
	}
}

// TestHitClipHonored: ancestor clipping removes cells from the hit area while
// preserving the widget's full Rect for dispatch.
func TestHitClipHonored(t *testing.T) {
	h := NewHitRegistry()
	w := newProbe("clipped")
	full := Rect{0, 0, 10, 10}
	clip := Rect{2, 2, 3, 3}
	h.Register(Registration{ID: "clipped", Widget: w, Rect: full, Clip: clip})

	if _, ok := h.Hit(3, 3); !ok {
		t.Fatal("inside-clip point did not hit")
	}
	if _, ok := h.Hit(7, 7); ok {
		t.Fatal("outside-clip point hit")
	}
	hit, _ := h.Hit(3, 3)
	if hit.Rect != full {
		t.Fatalf("dispatch rect = %+v, want full %+v", hit.Rect, full)
	}
}

// TestHitDeepestThenLastAdded: equal band/Z resolves deepest, then latest.
func TestHitDeepestThenLastAdded(t *testing.T) {
	h := NewHitRegistry()
	parent := newProbe("parent")
	child := newProbe("child")
	first := newProbe("first")
	second := newProbe("second")
	rect := Rect{0, 0, 10, 10}
	h.Register(Registration{ID: "parent", Widget: parent, Rect: rect, Clip: rect})
	h.Register(Registration{ID: "child", Widget: child, Rect: rect, Clip: rect, Chain: []Widget{parent}})
	if hit, _ := h.Hit(1, 1); hit.ID != "child" {
		t.Fatalf("deepest hit = %q, want child", hit.ID)
	}

	h.Reset()
	h.Register(Registration{ID: "first", Widget: first, Rect: rect, Clip: rect})
	h.Register(Registration{ID: "second", Widget: second, Rect: rect, Clip: rect})
	if hit, _ := h.Hit(1, 1); hit.ID != "second" {
		t.Fatalf("last-added hit = %q, want second", hit.ID)
	}
}

// TestHitRectLookup asserts the id -> rect table built during layout.
func TestHitRectLookup(t *testing.T) {
	h := NewHitRegistry()
	w := newProbe("box")
	h.Register(Registration{ID: "box", Widget: w, Rect: Rect{1, 2, 3, 4}, Clip: Rect{0, 0, 20, 20}})
	if got, ok := h.Rect("box"); !ok || got != (Rect{1, 2, 3, 4}) {
		t.Fatalf("Rect(box) = %+v, %v", got, ok)
	}
	if _, ok := h.Rect("missing"); ok {
		t.Fatal("missing id resolved")
	}
}

// TestWheelDeltaNormalized is the S0 wheel rule: every wheel event becomes
// exactly one line step, regardless of the terminal's button encoding.
func TestWheelDeltaNormalized(t *testing.T) {
	cases := []struct {
		button tea.MouseButton
		want   int
		ok     bool
	}{
		{tea.MouseWheelUp, -1, true},
		{tea.MouseWheelDown, 1, true},
		{tea.MouseWheelLeft, -1, true},
		{tea.MouseWheelRight, 1, true},
		{tea.MouseLeft, 0, false},
	}
	for _, tc := range cases {
		var ev tea.MouseMsg = tea.MouseWheelMsg{X: 1, Y: 1, Button: tc.button}
		got, ok := WheelDelta(ev)
		if got != tc.want || ok != tc.ok {
			t.Errorf("button %v: delta=%d ok=%v, want %d/%v", tc.button, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := WheelDelta(nil); ok {
		t.Fatal("nil mouse message normalized as wheel")
	}
}
