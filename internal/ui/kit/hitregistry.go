package kit

import (
	tea "charm.land/bubbletea/v2"
)

// Hit bands owned by Root. They mirror GOTUI.md §3.3: transcript Z0, toasts
// Z10, dialogs Z20. The band is global/flat like lipgloss Z (S0 finding); it
// is assigned by Root, not inherited from widget nesting.
const (
	BandBase    = 0
	BandToasts  = 10
	BandDialogs = 20
)

// Registration is one widget's contribution to the hit table. Root fills this
// during the post-layout walk; tests can register directly.
type Registration struct {
	ID     string
	Widget Widget
	// Rect is the widget's full absolute bounds (passed to Mouse).
	Rect Rect
	// Clip is the ancestor clip chain, including the widget's own rect. The
	// clickable area is Rect.Intersect(Clip).
	Clip Rect
	// Z orders siblings inside a band. Band orders Root's layers.
	Z    int
	Band int
	// Chain is root-to-parent ancestry, deepest last. Mouse routing bubbles
	// through it in reverse order when the widget returns false.
	Chain []Widget
}

type hitEntry struct {
	id     string
	widget Widget
	rect   Rect
	hit    Rect
	z      int
	band   int
	depth  int
	seq    int
	chain  []Widget
}

// Hit is a resolved hit: the widget plus the geometry and ancestry needed to
// dispatch and bubble the event.
type Hit struct {
	ID     string
	Widget Widget
	Rect   Rect
	Z      int
	Band   int
	Chain  []Widget
}

// HitRegistry is the id -> rect table built during Layout and consumed by
// mouse routing. It deliberately mirrors lipgloss.Compositor.Hit semantics
// (S0-verified) without creating a compositor layer per widget:
//
//   - higher band wins; inside a band higher Z wins;
//   - then deeper widgets win; then later-registered siblings win;
//   - ID-less widgets never register;
//   - the clickable rect is clipped by every ancestor;
//   - zero-size rects are not hittable.
//
// OpenTUI counterpart: the renderable tree's hit test. OpenCode counterpart:
// element-level mouse targets (replaces internal/tui/hit.go).
type HitRegistry struct {
	entries []hitEntry
	byID    map[string]int
	seq     int
}

// NewHitRegistry builds an empty table. Root owns one for its lifetime and
// resets it at the start of every layout pass.
func NewHitRegistry() *HitRegistry {
	return &HitRegistry{byID: make(map[string]int)}
}

// Reset clears the table. Layout calls it before walking the tree.
func (h *HitRegistry) Reset() {
	h.entries = h.entries[:0]
	clear(h.byID)
	h.seq = 0
}

// Register adds one widget. It silently ignores nil widgets, empty IDs,
// invisible widgets, and clips that cover no cells.
func (h *HitRegistry) Register(reg Registration) {
	if h == nil || reg.Widget == nil || reg.ID == "" {
		return
	}
	if !WidgetVisible(reg.Widget) {
		return
	}
	clipped := reg.Rect.Intersect(reg.Clip)
	if clipped.Empty() {
		return
	}
	h.seq++
	e := hitEntry{
		id:     reg.ID,
		widget: reg.Widget,
		rect:   reg.Rect,
		hit:    clipped,
		z:      reg.Z,
		band:   reg.Band,
		depth:  len(reg.Chain),
		seq:    h.seq,
		chain:  reg.Chain,
	}
	h.entries = append(h.entries, e)
	h.byID[reg.ID] = len(h.entries) - 1
}

// Hit resolves the topmost clickable widget at (x, y).
// OpenTUI counterpart: Renderable.hitTest at the root; OpenCode: mouse target
// resolution from the element tree.
func (h *HitRegistry) Hit(x, y int) (Hit, bool) {
	if h == nil {
		return Hit{}, false
	}
	best := -1
	for i := range h.entries {
		e := &h.entries[i]
		if !e.hit.Contains(x, y) {
			continue
		}
		if best < 0 || hitWins(e, &h.entries[best]) {
			best = i
		}
	}
	if best < 0 {
		return Hit{}, false
	}
	e := h.entries[best]
	return Hit{ID: e.id, Widget: e.widget, Rect: e.rect, Z: e.z, Band: e.band, Chain: e.chain}, true
}

// hitWins orders candidates: band, then ancestry (a descendant always paints
// over its ancestor regardless of Z), then Z, then depth, then registration
// order. This is the "highest z band -> deepest/last-added" rule from §3.4.
func hitWins(a, b *hitEntry) bool {
	if a.band != b.band {
		return a.band > b.band
	}
	if ancestorOf(a.widget, b.chain) {
		return false // b is inside a and paints later
	}
	if ancestorOf(b.widget, a.chain) {
		return true
	}
	if a.z != b.z {
		return a.z > b.z
	}
	if a.depth != b.depth {
		return a.depth > b.depth
	}
	return a.seq > b.seq
}

// ancestorOf reports whether want appears in a root-to-parent chain.
func ancestorOf(want Widget, chain []Widget) bool {
	for _, w := range chain {
		if w == want {
			return true
		}
	}
	return false
}

// Rect returns the last registered full rect for an id. It is the S1 assertion
// surface for layout tests ("assert registry rects").
// OpenTUI counterpart: Renderable.bounds lookup.
func (h *HitRegistry) Rect(id string) (Rect, bool) {
	if h == nil {
		return Rect{}, false
	}
	i, ok := h.byID[id]
	if !ok {
		return Rect{}, false
	}
	return h.entries[i].rect, true
}

// Len returns the number of registered widgets.
func (h *HitRegistry) Len() int {
	if h == nil {
		return 0
	}
	return len(h.entries)
}

// WheelDelta normalises a bubbletea v2.0.8 wheel event to one line step:
// wheel up/left = -1, wheel down/right = +1. The S0 probe confirmed v2.0.8
// delivers wheel input as a button event with no fractional delta; widgets
// that scroll should call this instead of inventing their own mapping.
// OpenTUI counterpart: ScrollBox scrollBy(delta).
func WheelDelta(ev tea.MouseMsg) (int, bool) {
	if ev == nil {
		return 0, false
	}
	switch ev.Mouse().Button {
	case tea.MouseWheelUp, tea.MouseWheelLeft:
		return -1, true
	case tea.MouseWheelDown, tea.MouseWheelRight:
		return 1, true
	default:
		return 0, false
	}
}
