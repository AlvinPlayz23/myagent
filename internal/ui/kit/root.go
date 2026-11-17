package kit

import (
	"sort"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// Root owns the frame pipeline: layout pass -> band composition -> exact
// full-screen string. It is deliberately not a scroll/dialog host; those land
// in later phases and plug into the Base/Toasts/Dialogs slots.
//
// OpenTUI counterpart: the root renderer + render buffer. OpenCode counterpart:
// the app shell in routes/session (`<box flexGrow={1} backgroundColor=...>`).
type Root struct {
	WidgetBase

	// Base is the main screen (transcript / shell), band 0.
	Base Widget
	// Toasts is the toast host, band 10.
	Toasts Widget
	// Dialogs is the modal stack, band 20.
	Dialogs Widget

	// Focus is the keyboard ring. Root rebuilds its target list after layout.
	Focus *FocusRing
	// Fill is the full-screen base layer color (default background.base).
	Fill   tui.Token
	Filled bool

	themeRevision uint64
	themeFill     tui.Token
	followTheme   bool

	hits     *HitRegistry
	cache    RenderCache
	composed int
	// pendingFocus is an app-queued initial focus id. Layout applies it once
	// the target exists; until then it survives every pass (S4 contract).
	pendingFocus string
	// hosts holds every non-base band host, keyed by band. Toasts and Dialogs
	// are mirrors of the band-10 and band-20 entries so existing call sites
	// keep working; layers() is built from this map alone.
	hosts map[int]bandHost
}

// RootOption customises NewRoot.
type RootOption func(*Root)

// bandHost is one overlay band: its widget plus its input policy.
type bandHost struct {
	band    int
	widget  Widget
	capture bool
}

// BandHostOption customises a band registered with RegisterBandHost.
type BandHostOption func(*bandHost)

// WithBandCapture controls whether the host sees keys before lower bands.
// Modal chrome (the dialog stack) keeps the default true; passive overlays such
// as a toast host pass false so typing still reaches the composer.
func WithBandCapture(on bool) BandHostOption {
	return func(h *bandHost) { h.capture = on }
}

// RegisterBandHost installs w as the host of an overlay band. It is the one
// registration path for every overlay (dialog stack, toast host, future rails),
// so Root never needs a per-widget field. Bands paint and hit in ascending
// order; BandBase is the base screen and is never registered here.
//
//	root := kit.NewRoot(main, kit.RegisterBandHost(kit.BandDialogs, stack))
func RegisterBandHost(band int, w Widget, opts ...BandHostOption) RootOption {
	h := bandHost{band: band, widget: w, capture: band > BandBase}
	for _, opt := range opts {
		if opt != nil {
			opt(&h)
		}
	}
	return func(r *Root) { r.setBandHost(h) }
}

// WithToasts installs the band-10 overlay. Toasts are passive — ToastHost is
// non-focusable with a no-op Key — so the band must not capture keys; capture
// stays off so typing reaches the composer beneath (see toast.go).
func WithToasts(w Widget) RootOption {
	return RegisterBandHost(BandToasts, w, WithBandCapture(false))
}

// WithDialogs installs the band-20 modal overlay.
func WithDialogs(w Widget) RootOption { return RegisterBandHost(BandDialogs, w) }

// WithRootFill overrides the full-screen base-layer token.
func WithRootFill(t tui.Token) RootOption {
	return func(r *Root) {
		r.Fill = t
		r.followTheme = false
	}
}

// WithInitialFocus queues a focus target to be applied after the first layout
// pass. The id may be supplied before the target exists — Root holds it until
// a later layout registers it. It is sugar over QueueFocus.
//
//	root := kit.NewRoot(base, kit.WithInitialFocus("kit.composer"))
func WithInitialFocus(id string) RootOption {
	return func(r *Root) {
		if id != "" {
			r.pendingFocus = id
		}
	}
}

// QueueFocus stages the next focus target. Layout applies it once the target
// is registered (immediately when it already is). A queued id that never
// appears is harmless; it costs one string comparison per layout pass.
func (r *Root) QueueFocus(id string) {
	if r == nil || id == "" {
		return
	}
	r.pendingFocus = id
	r.MarkDirty()
}

// PendingFocus returns the queued focus id, or "" when nothing is staged.
// Tests use it as the assertion surface for the pre-layout state.
func (r *Root) PendingFocus() string { return r.pendingFocus }

// NewRoot builds the pipeline root. The base widget is required; overlays are
// optional and compose over it only when visible.
//
// Root always paints a full-screen base layer at (0,0), the S0 rule: lipgloss
// Compositor.Render sizes its canvas from bounds but draws at absolute
// coordinates, and trims output. Root therefore pad-repairs every frame to
// exactly w x h before returning it.
func NewRoot(base Widget, opts ...RootOption) *Root {
	theme := DefaultTheme()
	r := &Root{
		WidgetBase:    NewWidgetBase("root"),
		Base:          base,
		Focus:         NewFocusRing(),
		Fill:          theme.Background.Base,
		Filled:        true,
		hits:          NewHitRegistry(),
		hosts:         make(map[int]bandHost),
		themeRevision: tui.ThemeRevision(),
		themeFill:     theme.Background.Base,
		followTheme:   true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

// setBandHost records one band host and mirrors it into the legacy fields.
func (r *Root) setBandHost(h bandHost) {
	if h.band == BandBase {
		r.Base = h.widget
	} else {
		r.hosts[h.band] = h
		switch h.band {
		case BandToasts:
			r.Toasts = h.widget
		case BandDialogs:
			r.Dialogs = h.widget
		}
	}
	r.MarkDirty()
}

// Layout runs the top-down geometry pass, resets the hit registry, rebuilds it
// from the resulting bounds, and re-syncs the focus ring. It never paints.
// OpenTUI: root calculateLayout; OpenCode: layout effect.
func (r *Root) Layout(bounds Rect) {
	r.SetBounds(bounds)
	r.hits.Reset()
	if !r.Visible {
		return
	}
	for _, l := range r.layers() {
		if l.w != nil {
			l.w.Layout(bounds)
		}
	}
	r.rebuildHits(bounds)
	if r.Focus.SetTargets(r.collectFocus()) {
		r.MarkDirty()
	}
	r.applyPendingFocus()
	r.syncModalFocus()
}

// applyPendingFocus moves the ring to the queued id once its target is part of
// the tree. Modal focus still wins afterwards: a dialog that opened before the
// base target existed keeps the keyboard.
func (r *Root) applyPendingFocus() {
	if r.pendingFocus == "" || r.Focus == nil {
		return
	}
	if r.Focus.FocusedID() == r.pendingFocus {
		r.pendingFocus = ""
		return
	}
	if r.Focus.Focus(r.pendingFocus) {
		r.pendingFocus = ""
		r.MarkDirty()
	}
}

// layer pairs a root band with its widget.
type layer struct {
	w    Widget
	band int
}

func (r *Root) layers() []layer {
	bands := make([]int, 0, len(r.hosts))
	for band := range r.hosts {
		bands = append(bands, band)
	}
	sort.Ints(bands)
	out := make([]layer, 0, len(bands)+1)
	out = append(out, layer{w: r.Base, band: BandBase})
	for _, band := range bands {
		out = append(out, layer{w: r.hosts[band].widget, band: band})
	}
	return out
}

// keyLayers returns the input-capturing hosts, highest band first.
// OpenTUI: z-ordered key dispatch; GOTUI §3.4 first-responder order.
func (r *Root) keyLayers() []layer {
	var out []layer
	for _, l := range r.layers() {
		if h, ok := r.hosts[l.band]; ok && h.capture {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].band > out[j].band })
	return out
}

// modalOpen reports whether the dialog band owns input exclusively. It asks the
// host itself (DialogStack.Modal) instead of assuming any visible widget blocks
// the app; a host without the Modal extension keeps the S1 conservative gate.
func (r *Root) modalOpen() bool {
	if r.Dialogs == nil || !WidgetVisible(r.Dialogs) {
		return false
	}
	if m, ok := r.Dialogs.(Modal); ok {
		return m.Modal()
	}
	return true
}

// needsLayout reports a host that changed composition without a resize (a
// dialog opened), so the geometry pass must run before composing.
func (r *Root) needsLayout() bool {
	for _, l := range r.layers() {
		if h, ok := l.w.(interface{ NeedsLayout() bool }); ok && h.NeedsLayout() {
			return true
		}
	}
	return false
}

// syncModalFocus hands keyboard focus to the modal band after the ring has been
// rebuilt, covering the target that only became focusable in this pass.
func (r *Root) syncModalFocus() {
	if !r.modalOpen() {
		return
	}
	if f, ok := r.Dialogs.(interface{ FocusModal(*FocusRing) bool }); ok && f.FocusModal(r.Focus) {
		r.MarkDirty()
	}
}

// rebuildHits walks each band in document order. Bounds come from WidgetBase,
// so no layout callbacks or parent pointers are needed.
// OpenTUI: hit-test tree construction; S0: ID-less layers are invisible to hit.
func (r *Root) rebuildHits(bounds Rect) {
	for _, l := range r.layers() {
		r.walkHit(l.w, nil, bounds, l.band)
	}
}

func (r *Root) walkHit(w Widget, chain []Widget, clip Rect, band int) {
	if w == nil || !WidgetVisible(w) {
		return
	}
	b := WidgetBounds(w)
	clipped := b.Intersect(clip)
	if id := WidgetID(w); id != "" && !clipped.Empty() {
		r.hits.Register(Registration{
			ID:     id,
			Widget: w,
			Rect:   b,
			Clip:   clip,
			Z:      WidgetZ(w),
			Band:   band,
			Chain:  chain,
		})
	}
	nextClip := clip.Intersect(b)
	nextChain := append(append([]Widget(nil), chain...), w)
	for _, c := range childWidgets(w) {
		r.walkHit(c, nextChain, nextClip, band)
	}
}

// collectFocus walks the same tree in document order. Invisible/zero-size
// widgets are skipped, which is what blurs a hidden focused node.
// OpenTUI: focusable discovery; OpenCode: focus signal registration.
func (r *Root) collectFocus() []*FocusTarget {
	var out []*FocusTarget
	var walk func(Widget)
	walk = func(w Widget) {
		if w == nil || !WidgetVisible(w) {
			return
		}
		if t := w.FocusTarget(); t != nil && t.ID != "" && !WidgetBounds(w).Empty() {
			out = append(out, t)
		}
		for _, c := range childWidgets(w) {
			walk(c)
		}
	}
	for _, l := range r.layers() {
		walk(l.w)
	}
	return out
}

// Render lays out when the terminal size changed, composes when the tree is
// dirty, and otherwise returns the cached full frame. An idle app performs
// zero composites; ComposeCount exposes that for G5.
//
// OpenTUI: root render loop. OpenCode: View() on the retained tree.
func (r *Root) Render(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	r.refreshTheme()
	if !r.Visible {
		return fitBlock("", w, h)
	}
	if out, ok := r.cache.Lookup(w, h, r.Rev()); ok && !anyDirty(r) {
		return out
	}
	if r.Bounds() != (Rect{X: 0, Y: 0, W: w, H: h}) || r.needsLayout() {
		r.Layout(Rect{X: 0, Y: 0, W: w, H: h})
	}
	out := r.compose(w, h)
	r.composed++
	r.cache.Store(w, h, r.Rev(), out)
	clearDirtyTree(r)
	return out
}

// refreshTheme invalidates retained paint without disturbing widget state or
// geometry. Every descendant cache gets a revision bump before Root looks up
// its full-frame cache.
func (r *Root) refreshTheme() {
	revision := tui.ThemeRevision()
	if revision == r.themeRevision {
		return
	}
	theme := tui.CurrentThemeTokens()
	for _, layer := range r.layers() {
		stack, ok := layer.w.(*DialogStack)
		if ok && stack.ScrimFill == r.themeFill {
			stack.ScrimFill = theme.Background.Base
		}
	}
	if r.followTheme && r.Fill == r.themeFill {
		r.Fill = theme.Background.Base
	} else {
		r.followTheme = false
	}
	r.themeRevision = revision
	r.themeFill = theme.Background.Base
	markDirtyTree(r)
}

func markDirtyTree(w Widget) {
	if w == nil {
		return
	}
	if b := baseOf(w); b != nil {
		b.MarkDirty()
	}
	for _, child := range childWidgets(w) {
		markDirtyTree(child)
	}
}

// compose paints the full-screen base then bands 20/10/0 in ascending order.
// Every layer is stamped through a cell canvas, so a higher band only replaces
// the cells it actually paints (transparent blanks never punch holes).
// OpenTUI: render buffer compose; GOTUI §3.3 pipeline.
func (r *Root) compose(w, h int) string {
	theme := DefaultTheme()
	cv := lipgloss.NewCanvas(w, h)
	if r.Filled {
		fillRect(cv, 0, 0, w, h, tokenOr(r.Fill, theme.Background.Base))
	}
	full := Rect{X: 0, Y: 0, W: w, H: h}
	for _, l := range r.layers() {
		if l.w == nil || !WidgetVisible(l.w) {
			continue
		}
		// A scrim host dims everything painted beneath it (§3.0.D alpha 150).
		if sp, ok := l.w.(ScrimProvider); ok {
			if tok, alpha, on := sp.Scrim(); on {
				applyScrim(cv, full, tok, alpha)
			}
		}
		drawWidget(cv, l.w, WidgetBounds(l.w), full)
	}
	return padCanvas(cv, w, h)
}

// Key routes keyboard input in first-responder order: modal band, focused
// widget, base, then focus navigation. It is the standalone-root entry point;
// an app that owns its base widget's input calls HandleKey instead so the base
// is never dispatched twice.
// OpenTUI: key dispatch; OpenCode: §3.4 input routing.
func (r *Root) Key(ev tea.KeyPressMsg) bool { return r.dispatchKey(ev, true) }

// HandleKey is the top-level entry for an app shell whose base widget already
// receives keys from its owner (the myagent shell forwards to the legacy onKey
// path). It runs the same first-responder order as Key but never dispatches to
// Base, eliminating the S3 "baseWidget returns false" sharp edge.
//
// Order: capturing bands (highest first), the focused widget, then focus
// navigation. The app decides what an unconsumed key means.
func (r *Root) HandleKey(ev tea.KeyPressMsg) bool { return r.dispatchKey(ev, false) }

func (r *Root) dispatchKey(ev tea.KeyPressMsg, includeBase bool) bool {
	for _, l := range r.keyLayers() {
		if l.w != nil && WidgetVisible(l.w) && l.w.Key(ev) {
			r.MarkDirty()
			return true
		}
	}
	if t := r.Focus.Current(); t != nil && t.Widget != nil {
		if t.Widget.Key(ev) {
			r.MarkDirty()
			return true
		}
	}
	if includeBase && r.Base != nil && WidgetVisible(r.Base) && r.Base.Key(ev) {
		r.MarkDirty()
		return true
	}
	if r.Focus.Key(ev) {
		r.MarkDirty()
		return true
	}
	return false
}

// Mouse resolves the topmost registry hit, sets focus when the hit chain is
// focusable, then dispatches deepest-first and bubbles to ancestors. When a
// dialog band is visible, lower-band hits are discarded (modal gating).
// OpenTUI: mouse dispatch; OpenCode: §3.4 routing.
func (r *Root) Mouse(ev tea.MouseMsg, rect Rect) bool { return r.dispatchMouse(ev, rect, true) }

// HandleMouse is the shell-owned counterpart to HandleKey: it resolves hits and
// bubbles through the widget chain but skips the base widget itself, so a miss
// inside the transcript can fall through to the shell's content-coordinate
// handlers without re-entering the shell widget.
func (r *Root) HandleMouse(ev tea.MouseMsg, rect Rect) bool {
	return r.dispatchMouse(ev, rect, false)
}

func (r *Root) dispatchMouse(ev tea.MouseMsg, _ Rect, includeBase bool) bool {
	if ev == nil || !r.Visible {
		return false
	}
	m := ev.Mouse()
	hit, ok := r.hits.Hit(m.X, m.Y)
	if !ok {
		return false
	}
	if r.modalOpen() && hit.Band < BandDialogs {
		return false
	}
	if r.focusHit(hit) {
		r.MarkDirty()
	}
	if hit.Widget != nil && (includeBase || hit.Widget != r.Base) && hit.Widget.Mouse(ev, hit.Rect) {
		r.MarkDirty()
		return true
	}
	for i := len(hit.Chain) - 1; i >= 0; i-- {
		w := hit.Chain[i]
		if w == nil || !WidgetVisible(w) || (!includeBase && w == r.Base) {
			continue
		}
		if w.Mouse(ev, WidgetBounds(w)) {
			r.MarkDirty()
			return true
		}
	}
	return false
}

// focusHit focuses the closest focusable widget on the hit path.
func (r *Root) focusHit(hit Hit) bool {
	candidates := make([]Widget, 0, len(hit.Chain)+1)
	candidates = append(candidates, hit.Widget)
	for i := len(hit.Chain) - 1; i >= 0; i-- {
		candidates = append(candidates, hit.Chain[i])
	}
	for _, w := range candidates {
		if w == nil {
			continue
		}
		if t := w.FocusTarget(); t != nil && t.ID != "" {
			return r.Focus.Focus(t.ID)
		}
	}
	return false
}

// Children exposes the bands in paint order for generic dirty/rev walks.
func (r *Root) Children() []Widget {
	layers := r.layers()
	out := make([]Widget, 0, len(layers))
	for _, l := range layers {
		out = append(out, l.w)
	}
	return out
}

// HitRegistry returns the live table (tests assert rects against it).
func (r *Root) HitRegistry() *HitRegistry { return r.hits }

// Hit resolves a click at screen coordinates.
func (r *Root) Hit(x, y int) (Hit, bool) { return r.hits.Hit(x, y) }

// ComposeCount returns how many full frames Root has composed. It is the G5
// assertion surface: two consecutive Render calls on an idle tree leave it
// unchanged.
func (r *Root) ComposeCount() int { return r.composed }

// Invalidate forces the next Render to compose even when no widget revision
// changed (the app uses this when terminal mode/theme changes externally).
func (r *Root) Invalidate() { r.MarkDirty() }

// FocusTarget is nil: Root is the router, never a focus stop.
func (r *Root) FocusTarget() *FocusTarget { return nil }
