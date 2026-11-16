package kit

import (
	tea "charm.land/bubbletea/v2"
)

// FocusTarget is a widget's keyboard-focus handle. Returning a non-nil target
// from Widget.FocusTarget marks the widget as focusable; Root collects targets
// in document order after every layout pass.
//
// OpenTUI counterpart: focusable/focused props + focus()/blur() methods.
// OpenCode counterpart: the focus signal consumed by DialogSelect/Textarea.
type FocusTarget struct {
	// ID must match WidgetBase.ID for click-to-focus and stable re-sync.
	ID string
	// Widget is the handler that receives keys while focused.
	Widget Widget
	// OnFocus/OnBlur let the widget update its local focus styling. They run
	// after the ring's index changes; a widget should call MarkDirty here.
	OnFocus func()
	OnBlur  func()
}

// FocusRing owns keyboard focus order. Tab/Shift+Tab and the arrow keys cycle
// focusables; a click focuses the hit widget; hiding or removing the focused
// widget blurs it on the next Sync.
//
// OpenTUI counterpart: focus manager. OpenCode counterpart: the focus signal
// plus keymap focus traversal.
type FocusRing struct {
	targets []*FocusTarget
	index   int // -1 = blurred
}

// NewFocusRing builds a blurred ring.
func NewFocusRing() *FocusRing { return &FocusRing{index: -1} }

// SetTargets replaces the focus order, preserving the focused target when it
// still exists. It returns true when the focused identity changed (Root uses
// that to invalidate the frame). An existing focus whose widget vanished or
// became invisible is blurred, with its OnBlur callback fired.
//
// OpenTUI: focus manager re-registration after layout; hidden focused nodes
// blur (GOTUI §3.0.1).
func (f *FocusRing) SetTargets(targets []*FocusTarget) bool {
	if f == nil {
		return false
	}
	prevID := f.FocusedID()
	var prev *FocusTarget
	if f.index >= 0 && f.index < len(f.targets) {
		prev = f.targets[f.index]
	}
	f.targets = targets
	if prevID == "" {
		if f.index >= len(f.targets) {
			f.index = -1
		}
		return false
	}
	for i, t := range f.targets {
		if t != nil && t.ID == prevID {
			f.index = i
			return false
		}
	}
	f.index = -1
	fireBlurTarget(prev)
	return true
}

// Current returns the focused target, or nil.
// OpenTUI: focused renderable.
func (f *FocusRing) Current() *FocusTarget {
	if f == nil || f.index < 0 || f.index >= len(f.targets) {
		return nil
	}
	return f.targets[f.index]
}

// FocusedID returns the focused widget id, or "".
func (f *FocusRing) FocusedID() string {
	if t := f.Current(); t != nil {
		return t.ID
	}
	return ""
}

// Focus moves the ring to id. It returns true when focus changed. Focusing the
// already-focused target is a no-op.
// OpenTUI: focus(id); OpenCode: setFocus(id).
func (f *FocusRing) Focus(id string) bool {
	if f == nil {
		return false
	}
	if f.FocusedID() == id && f.index >= 0 {
		return false
	}
	for i, t := range f.targets {
		if t == nil || t.ID != id {
			continue
		}
		old := f.index
		f.index = i
		f.fireBlur(old)
		if t.OnFocus != nil {
			t.OnFocus()
		}
		return true
	}
	return false
}

// Blur clears focus and fires the previous target's OnBlur.
// OpenTUI: blur(); OpenCode: focus(null).
func (f *FocusRing) Blur() bool {
	if f == nil || f.index < 0 {
		return false
	}
	old := f.index
	f.index = -1
	f.fireBlur(old)
	return true
}

// Next focuses the next target, wrapping. From a blurred ring it focuses the
// first target. Returns true when focus changed.
// OpenTUI: focusNext(); OpenCode: Tab traversal.
func (f *FocusRing) Next() bool {
	if f == nil || len(f.targets) == 0 {
		return false
	}
	if f.index < 0 {
		return f.focusIndex(0)
	}
	return f.focusIndex((f.index + 1) % len(f.targets))
}

// Prev focuses the previous target, wrapping. From a blurred ring it focuses
// the last target.
// OpenTUI: focusPrevious(); OpenCode: Shift+Tab traversal.
func (f *FocusRing) Prev() bool {
	if f == nil || len(f.targets) == 0 {
		return false
	}
	if f.index < 0 {
		return f.focusIndex(len(f.targets) - 1)
	}
	return f.focusIndex((f.index - 1 + len(f.targets)) % len(f.targets))
}

// focusIndex moves to a valid index and fires blur/focus callbacks.
func (f *FocusRing) focusIndex(i int) bool {
	if i < 0 || i >= len(f.targets) || f.targets[i] == nil {
		return false
	}
	if i == f.index {
		return false
	}
	old := f.index
	f.index = i
	f.fireBlur(old)
	if t := f.targets[i]; t.OnFocus != nil {
		t.OnFocus()
	}
	return true
}

// fireBlur invokes the old target's blur callback.
func (f *FocusRing) fireBlur(index int) {
	if index < 0 || index >= len(f.targets) {
		return
	}
	fireBlurTarget(f.targets[index])
}

// fireBlurTarget fires one target's blur callback.
func fireBlurTarget(t *FocusTarget) {
	if t != nil && t.OnBlur != nil {
		t.OnBlur()
	}
}

// Key handles the focus-navigation subset: Tab/Shift+Tab and arrows. The
// focused widget gets a first chance to consume keys before Root calls this,
// so a textarea can keep arrow movement inside its own buffer.
//
// OpenTUI: keymap focus traversal. OpenCode: Tab/Shift+Tab and arrow focus
// cycling.
func (f *FocusRing) Key(ev tea.KeyPressMsg) bool {
	switch ev.String() {
	case "tab", "down", "right":
		return f.Next()
	case "shift+tab", "up", "left":
		return f.Prev()
	default:
		return false
	}
}
