package kit

import (
	tea "charm.land/bubbletea/v2"
)

// Widget is the retained-mode contract every kit node implements. Widgets
// persist across frames: state is mutated through setters that mark them dirty,
// never rebuilt per frame.
//
// OpenTUI counterpart: Renderable (yoga node + render buffer). OpenCode
// counterpart: the Solid component tree in packages/tui/src — components own
// their state and request re-render through signals.
//
// This interface is frozen for the S1 kit contract. Do not change it without
// the human; Phase 2-5 widgets are written against it verbatim.
type Widget interface {
	// Layout assigns absolute bounds. It is called top-down once per frame
	// before any Render. Implementations must only compute geometry: never
	// paint, never mutate content state.
	//
	// OpenTUI: YogaNode.calculateLayout() + setPosition/setDimensions.
	// OpenCode: the computed layout object passed to <box>.
	Layout(r Rect)

	// Render paints the widget into an exact w x h cell block and returns it.
	// w and h are the W/H from the last Layout. Rendering must be pure with
	// respect to widget state (a second call with the same size/rev may return
	// a cache hit). Height must equal h; callers may clip, but goldens do not.
	//
	// OpenTUI: Renderable.render(buffer, dt). OpenCode: <text>/<box> output.
	Render(w, h int) string

	// Key feeds a key press to the widget. Return true if consumed. The focus
	// target receives keys before its ancestors and before global handlers.
	//
	// OpenTUI: Renderable.handleKeyPress. OpenCode: keymap dispatch on the
	// focused element.
	Key(ev tea.KeyPressMsg) bool

	// Mouse feeds a mouse event to the widget. r is the widget's absolute
	// bounds from the last Layout. Return true if consumed; otherwise the
	// registry bubbles the event to the widget's ancestors.
	//
	// OpenTUI: Renderable.handleMouse. OpenCode: onMouseDown/onMouseMove.
	Mouse(ev tea.MouseMsg, r Rect) bool

	// FocusTarget returns non-nil when this widget can take keyboard focus.
	// The returned target is normally cached on the widget so repeated calls
	// are allocation-free.
	//
	// OpenTUI: focusable/focused props. OpenCode: the focus signal consumed by
	// dialog/list components.
	FocusTarget() *FocusTarget
}

// WidgetBase is the identity/state block embedded by every kit widget. It is
// not itself a Widget because Render is widget-specific.
//
// Visible false keeps the node in the tree but removes it from layout and
// focus traversal (OpenTUI: visible=false; OpenCode: a hidden Solid branch).
// Z is the sibling band used for paint and hit ordering (OpenTUI: zIndex).
type WidgetBase struct {
	// ID is the stable identity used by the hit registry and focus ring.
	// Widgets with an empty ID never register for hit testing.
	ID string
	// Visible controls layout participation and focusability. Constructors set
	// it true; a zero-value WidgetBase is hidden by design, so custom widgets
	// must call Show() or set Visible=true.
	Visible bool
	// Z orders siblings for paint and hit resolution. Higher paints later and
	// wins ties in the hit registry. Z does not inherit to children.
	Z int

	rect  Rect
	dirty bool
	rev   int
}

// NewWidgetBase builds an initially visible, clean base. Prefer this over a
// struct literal so the Visible footgun cannot bite.
func NewWidgetBase(id string) WidgetBase {
	return WidgetBase{ID: id, Visible: true}
}

// Base exposes the embedded state block; it lets containers inspect widgets
// that embed WidgetBase through the small Base() interface.
// OpenTUI counterpart: Renderable's common props object.
func (b *WidgetBase) Base() *WidgetBase { return b }

// SetBounds records the absolute rect assigned by Layout. It marks the widget
// dirty only when the rect actually changes, so a steady 60 fps layout pass
// does not invalidate render caches.
// OpenTUI counterpart: setPosition/setDimensions writing the yoga result.
func (b *WidgetBase) SetBounds(r Rect) {
	if b.rect == r {
		return
	}
	b.rect = r
	b.MarkDirty()
}

// Bounds returns the last rect assigned by Layout.
// OpenTUI counterpart: Renderable.bounds.
func (b *WidgetBase) Bounds() Rect { return b.rect }

// Show makes the widget visible and dirties it.
// OpenTUI counterpart: visible=true; OpenCode counterpart: show signal.
func (b *WidgetBase) Show() {
	if b.Visible {
		return
	}
	b.Visible = true
	b.MarkDirty()
}

// Hide removes the widget from layout and blurs it via the focus ring's next
// Sync. OpenTUI counterpart: visible=false (hidden focused node blurs).
func (b *WidgetBase) Hide() {
	if !b.Visible {
		return
	}
	b.Visible = false
	b.MarkDirty()
}

// SetVisible is the idempotent setter behind Show/Hide.
// OpenTUI counterpart: Renderable.visible setter.
func (b *WidgetBase) SetVisible(v bool) {
	if b.Visible == v {
		return
	}
	b.Visible = v
	b.MarkDirty()
}

// MarkDirty bumps the revision and raises the dirty flag. Every state mutation
// that affects paint or geometry must call it; render caches key on Rev.
// OpenTUI counterpart: requestRender(); OpenCode: signal setter.
func (b *WidgetBase) MarkDirty() {
	b.rev++
	b.dirty = true
}

// Dirty reports whether the widget changed since the last composite.
// OpenTUI counterpart: the render request bit.
func (b *WidgetBase) Dirty() bool { return b.dirty }

// ClearDirty acknowledges a composite. Called by Root after painting the tree.
// OpenTUI counterpart: clearing the render request after render().
func (b *WidgetBase) ClearDirty() { b.dirty = false }

// Rev returns the monotonic revision used as the third cache key component.
// OpenTUI counterpart: the render version/generation counter.
func (b *WidgetBase) Rev() int { return b.rev }

// Touch is the escape hatch for widgets that expose exported style/content
// fields without setters: mutate the field, then call Touch so caches notice.
// OpenTUI: requestRender(); OpenCode: a signal setter.
func (b *WidgetBase) Touch() { b.MarkDirty() }

// baseOf returns the embedded base for a widget, or nil for custom widgets
// that implement the interface from scratch (they can still render; they just
// do not participate in bounds/hit-id bookkeeping).
func baseOf(w Widget) *WidgetBase {
	if b, ok := w.(interface{ Base() *WidgetBase }); ok {
		return b.Base()
	}
	return nil
}

// WidgetID returns the widget's hit/focus identity, or "".
// OpenTUI counterpart: Renderable.id.
func WidgetID(w Widget) string {
	if b := baseOf(w); b != nil {
		return b.ID
	}
	return ""
}

// WidgetVisible reports whether the widget participates in layout. Widgets
// without a WidgetBase are treated as visible.
// OpenTUI counterpart: Renderable.visible.
func WidgetVisible(w Widget) bool {
	if b := baseOf(w); b != nil {
		return b.Visible
	}
	return true
}

// WidgetZ returns the widget's sibling band.
// OpenTUI counterpart: Renderable.zIndex.
func WidgetZ(w Widget) int {
	if b := baseOf(w); b != nil {
		return b.Z
	}
	return 0
}

// WidgetBounds returns the absolute rect recorded during Layout.
// OpenTUI counterpart: Renderable.bounds.
func WidgetBounds(w Widget) Rect {
	if b := baseOf(w); b != nil {
		return b.Bounds()
	}
	return Rect{}
}

// WidgetRev returns the widget's paint revision (0 for custom widgets).
// OpenTUI counterpart: the render version used by render caches.
func WidgetRev(w Widget) int {
	if b := baseOf(w); b != nil {
		return b.Rev()
	}
	return 0
}

// WidgetDirty reports the widget's compose dirty flag.
// OpenTUI counterpart: pending render request.
func WidgetDirty(w Widget) bool {
	if b := baseOf(w); b != nil {
		return b.Dirty()
	}
	return false
}

// ClearWidgetDirty acknowledges the widget's compose dirty flag.
func ClearWidgetDirty(w Widget) {
	if b := baseOf(w); b != nil {
		b.ClearDirty()
	}
}

// childWidgets returns the direct children when the widget is a kit
// container. It is an optional extension of the frozen Widget contract used by
// layout-independent bookkeeping (hit registry, focus discovery, subtree
// revisions). Custom containers should implement Children() []Widget.
func childWidgets(w Widget) []Widget {
	if c, ok := w.(interface{ Children() []Widget }); ok {
		return c.Children()
	}
	return nil
}
