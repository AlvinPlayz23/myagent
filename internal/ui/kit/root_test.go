package kit

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

func testTree() (*Root, *probeWidget, *probeWidget, *Box) {
	head := newProbe("head")
	body := newProbe("body")
	base := VBox(
		Child{W: head, Fixed: 1},
		Child{W: body, Grow: 1},
	)
	base.ID = "base"
	return NewRoot(base), head, body, base
}

// TestRootFullScreenBaseLayer is the S0 rule: Root always returns exactly
// W x H, even when every child is blank, because Compositor.Render would
// otherwise shift/trim the frame.
func TestRootFullScreenBaseLayer(t *testing.T) {
	root := NewRoot(nil)
	out := ansi.Strip(root.Render(40, 10))
	lines := strings.Split(out, "\n")
	if len(lines) != 10 {
		t.Fatalf("height = %d, want 10", len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != 40 {
			t.Fatalf("line %d width = %d, want 40 (%q)", i, got, line)
		}
	}
	if root.ComposeCount() != 1 {
		t.Fatalf("compose count = %d, want 1", root.ComposeCount())
	}
}

// TestRootDirtyCounterIdle proves G5: an idle app produces zero composites.
func TestRootDirtyCounterIdle(t *testing.T) {
	root, _, body, _ := testTree()
	first := root.Render(40, 10)
	if root.ComposeCount() != 1 {
		t.Fatalf("first compose count = %d, want 1", root.ComposeCount())
	}
	second := root.Render(40, 10)
	if root.ComposeCount() != 1 {
		t.Fatalf("idle render composed again: count = %d", root.ComposeCount())
	}
	if first != second {
		t.Fatal("idle frame changed")
	}

	body.MarkDirty()
	root.Render(40, 10)
	if root.ComposeCount() != 2 {
		t.Fatalf("dirty child compose count = %d, want 2", root.ComposeCount())
	}

	root.Render(41, 10)
	if root.ComposeCount() != 3 {
		t.Fatalf("resize compose count = %d, want 3", root.ComposeCount())
	}
	root.Render(41, 10)
	if root.ComposeCount() != 3 {
		t.Fatalf("idle after resize composed again: count = %d", root.ComposeCount())
	}
}

func TestRootRefreshesCachedTheme(t *testing.T) {
	tui.SetThemeName("tokyonight")
	t.Cleanup(func() { tui.SetThemeName("default") })

	scroll := NewScrollBox(fixedItems(12)...)
	focusable := newProbe("focused")
	root := NewRoot(VBox(
		Child{W: scroll, Grow: 1},
		Child{W: focusable, Fixed: 1},
	))
	first := root.Render(20, 6)
	if !root.Focus.Focus("focused") {
		t.Fatal("could not focus test widget")
	}
	scroll.ScrollTo(20)
	root.Render(20, 6)
	composed := root.ComposeCount()
	scrollTop := scroll.ScrollTop()
	scrollFrame := scroll.Render(scroll.Bounds().W, scroll.Bounds().H)
	if scrollTop == 0 {
		t.Fatal("test scroll position was not established")
	}

	previousRevision := tui.ThemeRevision()
	tui.SetThemeName("catppuccin")
	if tui.ThemeRevision() == previousRevision {
		t.Fatal("theme revision did not change")
	}
	second := root.Render(20, 6)
	if second == first {
		t.Fatal("theme change reused the previous ANSI frame")
	}
	if got := scroll.Render(scroll.Bounds().W, scroll.Bounds().H); got == scrollFrame {
		t.Fatal("nested scrollbox cache retained the previous theme")
	}
	if root.Fill != tui.CurrentThemeTokens().Background.Base {
		t.Fatal("default root fill did not follow the selected theme")
	}
	if root.ComposeCount() != composed+1 {
		t.Fatalf("theme change compose count = %d, want %d", root.ComposeCount(), composed+1)
	}
	if got := root.Focus.FocusedID(); got != "focused" {
		t.Fatalf("focused widget changed to %q", got)
	}
	if got := scroll.ScrollTop(); got != scrollTop {
		t.Fatalf("scroll position changed from %d to %d", scrollTop, got)
	}
	if idle := root.Render(20, 6); idle != second || root.ComposeCount() != composed+1 {
		t.Fatal("idle render after theme refresh did not reuse the refreshed frame")
	}
}

func TestRootThemeRefreshPreservesCustomFill(t *testing.T) {
	tui.SetThemeName("tokyonight")
	t.Cleanup(func() { tui.SetThemeName("default") })

	custom := tui.CurrentThemeTokens().Background.Raised.Base
	root := NewRoot(nil, WithRootFill(custom))
	root.Render(8, 2)
	tui.SetThemeName("catppuccin")
	root.Render(8, 2)
	if root.Fill != custom {
		t.Fatal("theme refresh replaced an explicit root fill")
	}
}

func TestRootThemeRefreshUpdatesDefaultDialogScrim(t *testing.T) {
	tui.SetThemeName("tokyonight")
	t.Cleanup(func() { tui.SetThemeName("default") })

	defaultScrim := tui.CurrentThemeTokens().Background.Base
	stack := NewDialogStack()
	if stack.ScrimFill != defaultScrim {
		t.Fatal("dialog stack did not start with the default theme scrim")
	}
	root := NewRoot(nil, WithDialogs(stack))
	root.Render(8, 2)

	tui.SetThemeName("catppuccin")
	root.Render(8, 2)
	if got, want := stack.ScrimFill, tui.CurrentThemeTokens().Background.Base; got != want {
		t.Fatal("dialog stack scrim did not follow the selected theme")
	}
}

func TestRootThemeRefreshPreservesCustomDialogScrim(t *testing.T) {
	tui.SetThemeName("tokyonight")
	t.Cleanup(func() { tui.SetThemeName("default") })

	custom := tui.CurrentThemeTokens().Background.Raised.Base
	stack := NewDialogStack()
	stack.ScrimFill = custom
	root := NewRoot(nil, WithDialogs(stack))
	root.Render(8, 2)

	tui.SetThemeName("catppuccin")
	root.Render(8, 2)
	if stack.ScrimFill != custom {
		t.Fatal("theme refresh replaced an explicit dialog scrim")
	}
}

// TestRootRegistryRects asserts the layout -> registry handoff.
func TestRootRegistryRects(t *testing.T) {
	root, _, body, _ := testTree()
	root.Render(40, 10)

	if got, ok := root.HitRegistry().Rect("head"); !ok || got != (Rect{0, 0, 40, 1}) {
		t.Fatalf("head rect = %+v, %v", got, ok)
	}
	if got, ok := root.HitRegistry().Rect("body"); !ok || got != (Rect{0, 1, 40, 9}) {
		t.Fatalf("body rect = %+v, %v", got, ok)
	}
	if got, ok := root.Hit(3, 5); !ok || got.ID != "body" {
		t.Fatalf("hit(3,5) = %+v, %v; want body", got, ok)
	}
	if hit, ok := root.Hit(3, 5); ok && (hit.Widget != body) {
		t.Fatal("hit returned the wrong widget")
	}
	if _, ok := root.Hit(3, 10); ok {
		t.Fatal("row 10 is outside the frame")
	}
}

// TestRootClickFocusAndBubble covers click-to-focus and ancestor bubbling.
func TestRootClickFocusAndBubble(t *testing.T) {
	root, head, body, _ := testTree()
	root.Render(40, 10)

	click := tea.MouseClickMsg{X: 3, Y: 0, Button: tea.MouseLeft}
	root.Mouse(click, Rect{})
	if root.Focus.FocusedID() != "head" || !head.focused {
		t.Fatalf("click focus = %q (focused=%v), want head", root.Focus.FocusedID(), head.focused)
	}
	if head.mouseEvents != 1 {
		t.Fatalf("head mouse events = %d, want 1", head.mouseEvents)
	}

	root.Mouse(tea.MouseClickMsg{X: 3, Y: 5, Button: tea.MouseLeft}, Rect{})
	if root.Focus.FocusedID() != "body" || !body.focused || !head.blurred {
		t.Fatalf("second click focus = %q, want body", root.Focus.FocusedID())
	}

	// An unconsumed leaf event bubbles to its Box ancestor; Box.Mouse is a
	// no-op, so Root reports false without losing the focus change.
	root.Focus.Blur()
	if root.Mouse(tea.MouseClickMsg{X: 3, Y: 5, Button: tea.MouseLeft}, Rect{}) {
		t.Fatal("unconsumed mouse event reported consumed")
	}
	if root.Focus.FocusedID() != "body" {
		t.Fatalf("focus after click = %q, want body", root.Focus.FocusedID())
	}
}

// TestRootKeyFirstResponder: the focused widget sees keys before the base and
// the focus ring.
func TestRootKeyFirstResponder(t *testing.T) {
	root, head, body, _ := testTree()
	root.Render(40, 10)
	root.Focus.Focus("body")

	body.consumeKey = true
	if !root.Key(tea.KeyPressMsg{Code: 'x', Text: "x"}) {
		t.Fatal("focused widget consumption did not reach Root")
	}
	if len(body.keys) != 1 || body.keys[0] != "x" {
		t.Fatalf("body keys = %v", body.keys)
	}
	if len(head.keys) != 0 {
		t.Fatal("unfocused head received the key")
	}

	body.consumeKey = false
	root.Focus.Blur()
	if !root.Key(tea.KeyPressMsg{Code: tea.KeyTab}) {
		t.Fatal("Tab did not move focus")
	}
	if root.Focus.FocusedID() != "head" {
		t.Fatalf("Tab focus = %q, want head", root.Focus.FocusedID())
	}
}

// TestRootModalGating: a visible dialog band discards lower-band mouse hits.
func TestRootModalGating(t *testing.T) {
	head := newProbe("head")
	body := newProbe("body")
	base := VBox(Child{W: head, Fixed: 1}, Child{W: body, Grow: 1})
	dialog := newProbe("dialog")

	// The dialog layer fills the screen but only its child is hittable.
	dialogs := NewBox(BoxOpts{}).Add(
		Child{W: dialog, Position: Position{Absolute: true}, Width: 5, Height: 5},
	)
	root := NewRoot(base, WithDialogs(dialogs))
	root.Render(40, 10)

	// Outside the dialog: a base-band hit exists, but modal gating drops it.
	before := body.mouseEvents
	if root.Mouse(tea.MouseClickMsg{X: 20, Y: 8, Button: tea.MouseLeft}, Rect{}) {
		t.Fatal("gated mouse event reported consumed")
	}
	if body.mouseEvents != before {
		t.Fatal("base widget received a mouse event while a dialog was open")
	}
	if root.Focus.FocusedID() != "" {
		t.Fatalf("gated click focused %q", root.Focus.FocusedID())
	}

	// Inside the dialog: band-20 routing wins.
	root.Mouse(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft}, Rect{})
	if root.Focus.FocusedID() != "dialog" {
		t.Fatalf("dialog click focus = %q, want dialog", root.Focus.FocusedID())
	}
	if dialog.mouseEvents != 1 {
		t.Fatalf("dialog mouse events = %d, want 1", dialog.mouseEvents)
	}
}

// TestRootDialogKeyGate: the modal band consumes keys before a focused base
// widget.
func TestRootDialogKeyGate(t *testing.T) {
	head := newProbe("head")
	body := newProbe("body")
	base := VBox(Child{W: head, Fixed: 1}, Child{W: body, Grow: 1})
	dialog := newProbe("dialog")
	dialog.consumeKey = true

	root := NewRoot(base, WithDialogs(dialog))
	root.Render(40, 10)
	root.Focus.Focus("body")

	if !root.Key(tea.KeyPressMsg{Code: 'z', Text: "z"}) {
		t.Fatal("dialog key was not consumed")
	}
	if len(body.keys) != 0 {
		t.Fatal("focused base widget received a key while a dialog was open")
	}
	if len(dialog.keys) != 1 {
		t.Fatalf("dialog keys = %v", dialog.keys)
	}
}

// TestRootHiddenWidgetLeavesFocus pins hide/blur semantics through a real
// layout sync.
func TestRootHiddenWidgetLeavesFocus(t *testing.T) {
	head := newProbe("head")
	body := newProbe("body")
	base := VBox(Child{W: head, Fixed: 1}, Child{W: body, Grow: 1})
	root := NewRoot(base)
	root.Render(40, 10)
	root.Focus.Focus("head")
	if !head.focused {
		t.Fatal("head did not focus")
	}

	head.Hide()
	root.Layout(Rect{X: 0, Y: 0, W: 40, H: 10})
	if root.Focus.Current() != nil {
		t.Fatalf("hidden widget stayed focused: %q", root.Focus.FocusedID())
	}
	if !head.blurred {
		t.Fatal("hidden widget did not blur")
	}
	if _, ok := root.HitRegistry().Rect("head"); ok {
		t.Fatal("hidden widget remained hittable")
	}
}

func TestRootComposesToastInsideItsOwnBounds(t *testing.T) {
	toast := NewToastHost(ToastOpts{ID: "toast", MaxWidth: 40})
	toast.Show(Toast{Message: "Choose a model"})
	root := NewRoot(nil, WithToasts(toast))
	lines := strings.Split(ansi.Strip(root.Render(100, 30)), "\n")
	bounds := toast.Bounds()
	for y, line := range lines {
		if y < bounds.Y || y >= bounds.Y+bounds.H {
			if strings.TrimSpace(line) != "" {
				t.Fatalf("toast painted outside its rows at %d", y)
			}
		} else if strings.TrimSpace(string([]rune(line)[:bounds.X])) != "" {
			t.Fatalf("toast painted left of its bounds at %d", y)
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), "Choose a model") {
		t.Fatal("toast content missing")
	}
}
