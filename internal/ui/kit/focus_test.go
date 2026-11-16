package kit

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestFocusRingTransitions is the G4 gate: Tab/arrow cycling, click focus,
// blur, and re-sync all move the ring and fire callbacks in order.
func TestFocusRingTransitions(t *testing.T) {
	a, b, c := newProbe("a"), newProbe("b"), newProbe("c")
	ring := NewFocusRing()
	ring.SetTargets([]*FocusTarget{a.target, b.target, c.target})

	if ring.Current() != nil {
		t.Fatal("new ring should be blurred")
	}
	if !ring.Next() || ring.FocusedID() != "a" || !a.focused {
		t.Fatalf("Next from blurred = %q, focused=%v; want a", ring.FocusedID(), a.focused)
	}
	if !ring.Next() || ring.FocusedID() != "b" || !a.blurred || !b.focused {
		t.Fatalf("Next a->b failed (id=%q aBlur=%v bFocus=%v)", ring.FocusedID(), a.blurred, b.focused)
	}
	if !ring.Prev() || ring.FocusedID() != "a" {
		t.Fatalf("Prev b->a failed: %q", ring.FocusedID())
	}
	if ring.Focus("a") {
		t.Fatal("focusing the focused target reported a change")
	}
	if !ring.Focus("c") || !c.focused {
		t.Fatal("Focus(c) failed")
	}
	if !ring.Blur() || ring.Current() != nil || !c.blurred {
		t.Fatal("Blur failed")
	}
}

// TestFocusRingKeyNavigation covers the key subset used by Root.
func TestFocusRingKeyNavigation(t *testing.T) {
	a, b, c := newProbe("a"), newProbe("b"), newProbe("c")
	ring := NewFocusRing()
	ring.SetTargets([]*FocusTarget{a.target, b.target, c.target})

	key := func(s string) tea.KeyPressMsg {
		switch s {
		case "tab":
			return tea.KeyPressMsg{Code: tea.KeyTab}
		case "shift+tab":
			return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
		case "down":
			return tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			return tea.KeyPressMsg{Code: tea.KeyUp}
		case "right":
			return tea.KeyPressMsg{Code: tea.KeyRight}
		case "left":
			return tea.KeyPressMsg{Code: tea.KeyLeft}
		default:
			return tea.KeyPressMsg{Code: 'x'}
		}
	}

	if !ring.Key(key("tab")) || ring.FocusedID() != "a" {
		t.Fatalf("tab from blurred: %q", ring.FocusedID())
	}
	if !ring.Key(key("down")) || ring.FocusedID() != "b" {
		t.Fatalf("down: %q", ring.FocusedID())
	}
	if !ring.Key(key("right")) || ring.FocusedID() != "c" {
		t.Fatalf("right: %q", ring.FocusedID())
	}
	if !ring.Key(key("tab")) || ring.FocusedID() != "a" {
		t.Fatalf("tab wrap: %q", ring.FocusedID())
	}
	if !ring.Key(key("shift+tab")) || ring.FocusedID() != "c" {
		t.Fatalf("shift+tab wrap: %q", ring.FocusedID())
	}
	if !ring.Key(key("up")) || ring.FocusedID() != "b" {
		t.Fatalf("up: %q", ring.FocusedID())
	}
	if !ring.Key(key("left")) || ring.FocusedID() != "a" {
		t.Fatalf("left: %q", ring.FocusedID())
	}
	if ring.Key(key("x")) {
		t.Fatal("plain rune consumed by focus ring")
	}
}

// TestFocusRingSyncPreservesOrBlurs pins remount semantics: a surviving ID
// keeps focus, a vanished/hidden target blurs.
func TestFocusRingSyncPreservesOrBlurs(t *testing.T) {
	a, b := newProbe("a"), newProbe("b")
	ring := NewFocusRing()
	ring.SetTargets([]*FocusTarget{a.target, b.target})
	ring.Focus("b")

	if ring.SetTargets([]*FocusTarget{a.target, b.target}) {
		t.Fatal("sync with the same targets changed focus")
	}
	if ring.FocusedID() != "b" {
		t.Fatalf("focus = %q, want b", ring.FocusedID())
	}

	if !ring.SetTargets([]*FocusTarget{a.target}) {
		t.Fatal("sync dropping focused target did not report change")
	}
	if ring.Current() != nil || !b.blurred {
		t.Fatalf("dropped target stayed focused (id=%q, blur=%v)", ring.FocusedID(), b.blurred)
	}
}
