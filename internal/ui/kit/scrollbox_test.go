package kit

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// countWidget is a fixed-height leaf that records how often it is painted and
// the width it was measured/painted at. Culling tests assert on renders alone:
// an offscreen child must never reach Render.
type countWidget struct {
	WidgetBase

	renders  int
	lastW    int
	lastH    int
	measureW int
	measureH int
}

func newCountWidget(id string) *countWidget {
	return &countWidget{WidgetBase: NewWidgetBase(id)}
}

func (c *countWidget) Layout(r Rect) { c.SetBounds(r) }

func (c *countWidget) Render(w, h int) string {
	c.renders++
	c.lastW, c.lastH = w, h
	if w <= 0 || h <= 0 {
		return ""
	}
	lines := make([]string, h)
	row := strings.Repeat("x", w)
	for i := range lines {
		lines[i] = row
	}
	return strings.Join(lines, "\n")
}

func (c *countWidget) MeasureHeight(w int) int {
	c.measureW = w
	return c.measureH
}

func (c *countWidget) Key(tea.KeyPressMsg) bool { return false }

func (c *countWidget) Mouse(tea.MouseMsg, Rect) bool { return false }

func (c *countWidget) FocusTarget() *FocusTarget { return nil }

func TestScrollBoxStickyFollowPauseResume(t *testing.T) {
	box := NewScrollBox()
	box.Sticky = true
	box.SetItems(fixedItems(3))
	box.Layout(Rect{W: 20, H: 10})
	if got := box.ScrollTop(); got != 20 {
		t.Fatalf("initial scrollTop = %d, want 20 (tail)", got)
	}
	if !box.StickyActive() {
		t.Fatal("sticky should start active at the tail")
	}

	box.Append(countItem(10))
	box.Layout(Rect{W: 20, H: 10})
	if got := box.ScrollTop(); got != 30 {
		t.Fatalf("scrollTop after append = %d, want 30", got)
	}

	// Scrolling away from the edge pauses follow.
	box.ScrollBy(-5)
	if box.StickyActive() {
		t.Fatal("sticky should pause after scrolling away from the tail")
	}
	box.Append(countItem(10))
	box.Layout(Rect{W: 20, H: 10})
	if got := box.ScrollTop(); got != 25 {
		t.Fatalf("paused scrollTop after append = %d, want 25", got)
	}

	// An intermediate step stays paused; landing on the edge resumes follow.
	box.ScrollBy(5)
	if box.StickyActive() {
		t.Fatalf("sticky resumed early at %d of %d", box.ScrollTop(), box.MaxScroll())
	}
	box.ScrollBy(10)
	if !box.StickyActive() {
		t.Fatal("sticky should resume on the tail")
	}
	box.Append(countItem(10))
	box.Layout(Rect{W: 20, H: 10})
	if got := box.ScrollTop(); got != box.MaxScroll() {
		t.Fatalf("sticky scrollTop after append = %d, want %d", got, box.MaxScroll())
	}
}

func TestScrollBoxPersistentEndVsOneShotAbsolute(t *testing.T) {
	box := NewScrollBox(fixedItems(10)...)
	box.Layout(Rect{W: 20, H: 10})
	if !box.AtBottom() {
		t.Fatal("expected to start at the tail")
	}

	// A middle target is one-shot: content growth must not move it.
	box.ScrollTo(30)
	box.Append(countItem(10))
	box.Layout(Rect{W: 20, H: 10})
	if got := box.ScrollTop(); got != 30 {
		t.Fatalf("one-shot absolute scrollTop = %d, want 30", got)
	}

	// A target at the end persists: it follows every later append.
	box.ScrollTo(box.MaxScroll())
	box.Append(countItem(10))
	box.Layout(Rect{W: 20, H: 10})
	if got := box.ScrollTop(); got != box.MaxScroll() {
		t.Fatalf("persistent end scrollTop = %d, want %d", got, box.MaxScroll())
	}

	// ScrollTo(0) is always absolute, even when the content fits and even
	// when the box was following the end.
	small := NewScrollBox(fixedItems(1)...)
	small.Layout(Rect{W: 20, H: 10})
	small.ScrollTo(0)
	small.Append(fixedItems(3)...)
	small.Layout(Rect{W: 20, H: 10})
	if got := small.ScrollTop(); got != 0 {
		t.Fatalf("top target scrollTop = %d, want 0", got)
	}
	if small.StickyActive() {
		t.Fatal("top target should not re-enable tail follow")
	}
}

func TestScrollBoxCullingOverscan(t *testing.T) {
	widgets := make([]*countWidget, 10)
	items := make([]ScrollItem, len(widgets))
	for i := range widgets {
		widgets[i] = newCountWidget("item." + itoa(i))
		items[i] = ScrollItem{W: widgets[i], H: 10}
	}
	box := NewScrollBox(items...)
	box.Sticky = false
	box.ScrollTo(0)

	root := NewRoot(box)
	root.Render(20, 10)
	if got := renderedCounts(widgets); !equalInts(got, []int{1, 1, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("top frame renders = %v, want items 0..1 only (overscan)", got)
	}

	// Items 4..5 are visible; 3 and 6 are the one-item overscan.
	box.ScrollTo(45)
	root.Render(20, 10)
	want := []int{1, 1, 0, 1, 1, 1, 1, 0, 0, 0}
	if got := renderedCounts(widgets); !equalInts(got, want) {
		t.Fatalf("mid frame renders = %v, want %v", got, want)
	}

	// Returning to the top repaints 0..1 and leaves the mid items untouched.
	box.ScrollTo(0)
	root.Render(20, 10)
	want = []int{2, 2, 0, 1, 1, 1, 1, 0, 0, 0}
	if got := renderedCounts(widgets); !equalInts(got, want) {
		t.Fatalf("back-to-top renders = %v, want %v", got, want)
	}

	if got, want := widgets[0].lastW, 19; got != want {
		t.Fatalf("child paint width = %d, want %d (scrollbar column reserved)", got, want)
	}
}

func TestScrollBoxDynamicAppendViaRoot(t *testing.T) {
	box := NewScrollBox(fixedItems(4)...)
	box.Sticky = false
	box.ScrollTo(0)
	root := NewRoot(box)
	root.Render(30, 10)
	composed := root.ComposeCount()

	// Appending stages content; Root's NeedsLayout path must commit it before
	// composing, without a resize or an explicit layout call.
	appended := newCountWidget("appended")
	box.Append(ScrollItem{W: appended, H: 10})
	root.Render(30, 10)
	if got := box.ContentHeight(); got != 50 {
		t.Fatalf("content height after append = %d, want 50", got)
	}
	if appended.renders != 0 {
		t.Fatalf("offscreen appended item rendered %d times, want 0", appended.renders)
	}
	if root.ComposeCount() != composed+1 {
		t.Fatalf("append composed %d frames, want 1", root.ComposeCount()-composed)
	}

	// Idle frames are pure cache hits: no compose and no child paint.
	root.Render(30, 10)
	root.Render(30, 10)
	if root.ComposeCount() != composed+1 {
		t.Fatalf("idle frames composed %d extra times", root.ComposeCount()-composed-1)
	}
	if appended.renders != 0 {
		t.Fatalf("idle frame painted appended item %d times", appended.renders)
	}
}

func TestScrollBoxMeasuredHeightAndWidth(t *testing.T) {
	child := newCountWidget("measured")
	child.measureH = 3
	box := NewScrollBox(ScrollItem{W: child})
	box.Sticky = false
	box.Layout(Rect{W: 20, H: 5})
	if got := child.measureW; got != 19 {
		t.Fatalf("MeasureHeight width = %d, want 19", got)
	}
	if got := box.ContentHeight(); got != 3 {
		t.Fatalf("measured content height = %d, want 3", got)
	}
	box.Render(20, 5)
	if child.lastW != 19 || child.lastH != 3 {
		t.Fatalf("child render size = %dx%d, want 19x3", child.lastW, child.lastH)
	}

	box.ShowScrollbar = false
	box.SetItems([]ScrollItem{{W: child}})
	box.Layout(Rect{W: 20, H: 5})
	if got := child.measureW; got != 20 {
		t.Fatalf("MeasureHeight width without scrollbar = %d, want 20", got)
	}
	if got := box.ContentWidth(); got != 20 {
		t.Fatalf("content width without scrollbar = %d, want 20", got)
	}
}

func TestScrollBoxScrollbarColumn(t *testing.T) {
	box := NewScrollBox(ScrollItem{W: newCountWidget("short"), H: 3})
	box.Sticky = false
	box.ShowScrollbar = true
	box.Layout(Rect{W: 20, H: 5})
	frame := ansi.Strip(box.Render(20, 5))
	lines := strings.Split(frame, "\n")
	if len(lines) != 5 {
		t.Fatalf("frame height = %d, want 5", len(lines))
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != 20 {
			t.Fatalf("line %d width = %d, want 20", i, got)
		}
		if !strings.HasSuffix(line, " ") {
			t.Fatalf("line %d should reserve a blank column when content fits: %q", i, line)
		}
	}

	// Tall content shrinks the thumb and positions it from the scroll offset.
	tall := NewScrollBox(fixedItems(10)...)
	tall.Sticky = false
	tall.ShowScrollbar = true
	tall.Layout(Rect{W: 20, H: 5})
	tall.ScrollTo(0)
	tall.Render(20, 5)
	lines = strings.Split(ansi.Strip(tall.Render(20, 5)), "\n")
	if !strings.HasSuffix(lines[0], "█") {
		t.Fatalf("thumb should start at the top, got %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "│") {
		t.Fatalf("track should follow the 1-row thumb, got %q", lines[1])
	}
	tall.ScrollToEnd()
	lines = strings.Split(ansi.Strip(tall.Render(20, 5)), "\n")
	if !strings.HasSuffix(lines[4], "█") {
		t.Fatalf("thumb should reach the bottom, got %q", lines[4])
	}
}

func TestScrollBoxKeysAndWheel(t *testing.T) {
	box := NewScrollBox(fixedItems(10)...)
	box.Layout(Rect{W: 20, H: 10})
	if got := box.ScrollTop(); got != 90 {
		t.Fatalf("initial scrollTop = %d, want 90", got)
	}

	steps := []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{tea.KeyPressMsg{Code: tea.KeyPgUp}, 82},                    // h-2 = 8
		{tea.KeyPressMsg{Code: tea.KeyUp}, 81},                      // line
		{tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl}, 76},    // 5-cell
		{tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift}, 81}, // 5-cell
		{tea.KeyPressMsg{Code: tea.KeyHome}, 0},                     // absolute top
		{tea.KeyPressMsg{Code: tea.KeyPgDown}, 8},                   // page
		{tea.KeyPressMsg{Code: tea.KeyDown}, 9},                     // line
		{tea.KeyPressMsg{Code: tea.KeyEnd}, box.MaxScroll()},        // persistent end
	}
	for _, step := range steps {
		if !box.Key(step.key) {
			t.Fatalf("key %q not consumed", step.key.Keystroke())
		}
		if got := box.ScrollTop(); got != step.want {
			t.Fatalf("after %q scrollTop = %d, want %d", step.key.Keystroke(), got, step.want)
		}
	}
	if !box.StickyActive() {
		t.Fatal("end key should re-engage sticky follow")
	}

	// Wheel down at the tail stays pinned; wheel up pauses follow.
	if !box.Mouse(tea.MouseWheelMsg{Button: tea.MouseWheelDown}, Rect{}) {
		t.Fatal("wheel down not consumed")
	}
	if got := box.ScrollTop(); got != box.MaxScroll() {
		t.Fatalf("wheel down at tail scrollTop = %d, want %d", got, box.MaxScroll())
	}
	if !box.Mouse(tea.MouseWheelMsg{Button: tea.MouseWheelUp}, Rect{}) {
		t.Fatal("wheel up not consumed")
	}
	if got := box.ScrollTop(); got != box.MaxScroll()-1 {
		t.Fatalf("wheel up scrollTop = %d, want %d", got, box.MaxScroll()-1)
	}
	if box.StickyActive() {
		t.Fatal("wheel up should pause sticky follow")
	}

	if box.Key(tea.KeyPressMsg{Code: 'x', Text: "x"}) {
		t.Fatal("rune key should not be consumed by the scrollbox")
	}
	if box.Mouse(tea.MouseClickMsg{Button: tea.MouseLeft}, Rect{}) {
		t.Fatal("click should bubble, not scroll")
	}
}

func fixedItems(n int) []ScrollItem {
	items := make([]ScrollItem, n)
	for i := range items {
		items[i] = countItem(10)
	}
	return items
}

func countItem(h int) ScrollItem {
	return ScrollItem{W: newCountWidget("item"), H: h}
}

func renderedCounts(ws []*countWidget) []int {
	out := make([]int, len(ws))
	for i, w := range ws {
		out[i] = w.renders
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestScrollBoxUpdatesHitBoundsOnlyWhenViewportMoves(t *testing.T) {
	first, second := newCountWidget("first"), newCountWidget("second")
	box := NewScrollBox(ScrollItem{W: first, H: 4}, ScrollItem{W: second, H: 4})
	box.Start = EdgeTop
	root := NewRoot(box)
	root.Render(10, 4)
	if hit, ok := root.Hits.Hit(0, 0); !ok || hit.Widget != first {
		t.Fatalf("initial hit = %#v, want first item", hit)
	}

	box.ScrollBy(4)
	root.Render(10, 4)
	if hit, ok := root.Hits.Hit(0, 0); !ok || hit.Widget != second {
		t.Fatalf("hit after scroll = %#v, want second item", hit)
	}
	count := root.ComposeCount()
	box.Key(tea.KeyPressMsg{Code: tea.KeyDown})
	root.Render(10, 4)
	if root.ComposeCount() != count {
		t.Fatal("scrolling against the viewport edge recomposed the frame")
	}
}
