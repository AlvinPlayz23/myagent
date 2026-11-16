package kit

// CacheKey is the render-cache identity required by GOTUI.md §3.5: a widget's
// paint is reused until its width, height, or revision changes.
// OpenTUI counterpart: the render version + framebuffer dimensions.
type CacheKey struct {
	W, H, Rev int
}

// RenderCache is the rev-keyed memo every painted widget embeds. It is a value
// type with no synchronization: kit rendering is single-goroutine, owned by
// the Bubble Tea update loop.
//
// OpenCode counterpart: the per-part streaming render cache keyed by width;
// §3.5 extends that key with height and revision.
type RenderCache struct {
	key   CacheKey
	out   string
	valid bool
}

// Lookup returns the cached string for (w, h, rev) when it is still valid.
// OpenTUI counterpart: memoized Renderable.render output.
func (c *RenderCache) Lookup(w, h, rev int) (string, bool) {
	if !c.valid || c.key != (CacheKey{W: w, H: h, Rev: rev}) {
		return "", false
	}
	return c.out, true
}

// Store records a freshly painted string under (w, h, rev).
func (c *RenderCache) Store(w, h, rev int, out string) {
	c.key = CacheKey{W: w, H: h, Rev: rev}
	c.out = out
	c.valid = true
}

// Render returns the cached output or calls fn and stores the result. It is
// the one-liner widgets use at the top of Render.
func (c *RenderCache) Render(w, h, rev int, fn func() string) string {
	if out, ok := c.Lookup(w, h, rev); ok {
		return out
	}
	out := fn()
	c.Store(w, h, rev, out)
	return out
}

// Invalidate drops the cached frame. Used when style fields are mutated
// without a revision bump (the exported-field escape hatch).
func (c *RenderCache) Invalidate() {
	c.valid = false
	c.out = ""
}

// Key returns the key of the cached frame, or the zero key.
func (c *RenderCache) Key() CacheKey { return c.key }

// subtreeRev folds a widget and its descendants' revisions into one number for
// container caches. A child change must invalidate the parent's composed
// frame, and this is the cheapest way to express that without parent pointers.
// OpenTUI counterpart: dirty propagation up the renderable tree.
func subtreeRev(w Widget) int {
	if w == nil {
		return 0
	}
	rev := WidgetRev(w)
	for _, c := range childWidgets(w) {
		rev += subtreeRev(c)
	}
	return rev
}

// anyDirty reports whether the widget subtree needs a composite.
// OpenTUI counterpart: the render request bit anywhere in the tree.
func anyDirty(w Widget) bool {
	if w == nil {
		return false
	}
	if WidgetDirty(w) {
		return true
	}
	for _, c := range childWidgets(w) {
		if anyDirty(c) {
			return true
		}
	}
	return false
}

// clearDirtyTree acknowledges a composite for the whole subtree.
func clearDirtyTree(w Widget) {
	if w == nil {
		return
	}
	ClearWidgetDirty(w)
	for _, c := range childWidgets(w) {
		clearDirtyTree(c)
	}
}
