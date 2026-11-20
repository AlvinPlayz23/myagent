package kit

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// renderLine returns the stripped first painted row.
func renderLine(t *testing.T, w Widget, width, height int) string {
	t.Helper()
	raw := w.Render(width, height)
	lines := strings.Split(ansi.Strip(raw), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimRight(lines[0], " ")
}

func TestJumpInactiveAndActive(t *testing.T) {
	j := NewJump(JumpOpts{})
	if j.Active() || j.Render(30, 1) != "" {
		t.Fatal("new jump row should start inactive and paint nothing")
	}

	j.SetActive(true)
	line := renderLine(t, j, 30, 1)
	if !strings.HasSuffix(line, "Jump to latest ↓") {
		t.Fatalf("jump row = %q, want right-aligned label", line)
	}
	if got := len([]rune(line)); got >= 30 {
		t.Fatalf("label width = %d, want a 2-cell right gutter", got)
	}

	j.SetActive(false)
	if j.Active() || j.Render(30, 1) != "" {
		t.Fatal("inactive jump row still paints")
	}
}

func TestJumpHoverClickAndEnter(t *testing.T) {
	jumps := 0
	j := NewJump(JumpOpts{ID: "jump", OnJump: func() { jumps++ }})
	j.SetActive(true)
	j.Layout(Rect{X: 0, Y: 4, W: 30, H: 1})
	before := j.Render(30, 1)

	if !j.Mouse(tea.MouseMotionMsg{X: 20, Y: 4}, j.Bounds()) {
		t.Fatal("motion inside the active row not consumed")
	}
	if !j.hover {
		t.Fatal("hover state not set")
	}
	if after := j.Render(30, 1); after == before {
		t.Fatal("hover did not repaint the label")
	}

	if !j.Mouse(tea.MouseClickMsg{X: 20, Y: 4, Button: tea.MouseLeft}, j.Bounds()) {
		t.Fatal("click on the active row not consumed")
	}
	if jumps != 1 {
		t.Fatalf("jump callbacks = %d, want 1", jumps)
	}
	if j.Mouse(tea.MouseClickMsg{X: 20, Y: 9, Button: tea.MouseLeft}, j.Bounds()) {
		t.Fatal("click outside the row consumed")
	}
	if j.Key(tea.KeyPressMsg{Code: tea.KeyEnter}) {
		t.Fatal("jump row should leave Enter to the owner")
	}
	if j.FocusTarget() != nil {
		t.Fatal("jump row should not be a focus stop")
	}
}

func TestTabsClipsAndSelects(t *testing.T) {
	var calls []string
	tabs := NewTabs(TabsOpts{
		ID: "tabs",
		Items: []TabItem{
			{ID: "one", Title: "one"},
			{ID: "two", Title: "two", Busy: true},
			{ID: "three", Title: "", Unread: true},
		},
		OnSelect: func(id string) bool {
			calls = append(calls, id)
			return true
		},
	})
	tabs.Layout(Rect{X: 0, Y: 2, W: 40, H: 1})

	line := renderLine(t, tabs, 40, 1)
	for _, want := range []string{"one", "two", "(untitled)", "│", "●"} {
		if !strings.Contains(line, want) {
			t.Fatalf("rail = %q, missing %q", line, want)
		}
	}
	if tabs.Len() != 3 || tabs.ActiveID() != "one" {
		t.Fatalf("Len/Active = %d/%q, want 3/one", tabs.Len(), tabs.ActiveID())
	}
	if w := ansi.StringWidth(line); w > 40 {
		t.Fatalf("rail width = %d, want <= 40", w)
	}

	// Click the second tab's rect.
	if !tabs.Mouse(tea.MouseClickMsg{X: 7, Y: 2, Button: tea.MouseLeft}, tabs.Bounds()) {
		t.Fatal("tab click not consumed")
	}
	if len(calls) != 1 || calls[0] != "two" {
		t.Fatalf("select calls = %v, want [two]", calls)
	}
	if tabs.ActiveID() != "two" {
		t.Fatalf("active tab = %q, want two", tabs.ActiveID())
	}

	// Right arrow wraps from the last tab to the first.
	if !tabs.Key(tea.KeyPressMsg{Code: tea.KeyRight}) || tabs.ActiveID() != "three" {
		t.Fatalf("right arrow active = %q, want three", tabs.ActiveID())
	}
	if !tabs.Key(tea.KeyPressMsg{Code: tea.KeyRight}) || tabs.ActiveID() != "one" {
		t.Fatalf("wrapped active = %q, want one", tabs.ActiveID())
	}
}

func TestTabsNoMotionGlyph(t *testing.T) {
	moving := NewTabs(TabsOpts{Items: []TabItem{{ID: "a", Title: "a", Busy: true}}})
	moving.Layout(Rect{W: 20, H: 1})
	if got := renderLine(t, moving, 20, 1); !strings.Contains(got, BrailleFrames[0]) {
		t.Fatalf("animated rail = %q, want braille frame", got)
	}

	still := NewTabs(TabsOpts{NoMotion: true, Items: []TabItem{{ID: "a", Title: "a", Busy: true}}})
	still.Layout(Rect{W: 20, H: 1})
	if got := renderLine(t, still, 20, 1); !strings.Contains(got, SpinnerStillGlyph) {
		t.Fatalf("still rail = %q, want %q", got, SpinnerStillGlyph)
	}
}

func TestSidebarStatsAndAlert(t *testing.T) {
	s := NewSidebar(SidebarOpts{
		ID:    "side",
		Title: "Usage",
		Rows: []StatRow{
			{Label: "model", Value: "muse"},
			{Label: "context", Value: "82%", Alert: true},
		},
	})
	s.Layout(Rect{W: 24, H: 6})
	if s.Len() != 3 {
		t.Fatalf("Len = %d, want 3 (title + rows)", s.Len())
	}
	plain := ansi.Strip(s.Render(24, 6))
	lines := strings.Split(plain, "\n")
	if !strings.Contains(lines[0], "│") || !strings.Contains(lines[0], "Usage") {
		t.Fatalf("header = %q, want rail edge + title", lines[0])
	}
	if !strings.Contains(lines[1], "model") || !strings.HasSuffix(strings.TrimRight(lines[1], " "), "muse") {
		t.Fatalf("model row = %q", lines[1])
	}
	if !strings.Contains(lines[2], "82%") {
		t.Fatalf("alert row = %q, want 82%%", lines[2])
	}

	// Alert rows paint with the error feedback role.
	s.SetStats("Usage", []StatRow{{Label: "context", Value: "82%", Alert: true}})
	alert := s.Render(24, 6)
	s.SetStats("Usage", []StatRow{{Label: "context", Value: "82%"}})
	normal := s.Render(24, 6)
	if alert == normal {
		t.Fatal("alert value painted identically to a normal value")
	}

	if s.Key(tea.KeyPressMsg{Code: 'x', Text: "x"}) || s.Mouse(tea.MouseClickMsg{}, Rect{}) {
		t.Fatal("sidebar should be display-only")
	}
	if s.FocusTarget() != nil {
		t.Fatal("sidebar should not be a focus stop")
	}
}

func TestTodoBlockStatesAndOverflow(t *testing.T) {
	todo := NewTodoBlock(TodoBlockOpts{
		ID: "todos",
		Items: []TodoItem{
			{Text: "pending item", Status: TodoPending},
			{Text: "active item", Status: TodoActive},
			{Text: "done item", Status: TodoDone},
			{Text: "cancelled item", Status: TodoCancelled},
		},
	})
	todo.Layout(Rect{W: 40, H: 4})
	plain := ansi.Strip(todo.Render(40, 4))
	for _, want := range []string{"[ ] pending item", "[•] active item", "[✓] done item", "[-] cancelled item"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("todo output missing %q:\n%s", want, plain)
		}
	}
	if todo.Height() != 4 {
		t.Fatalf("Height = %d, want 4", todo.Height())
	}

	capped := NewTodoBlock(TodoBlockOpts{
		MaxRows: 3,
		Items: []TodoItem{
			{Text: "one"}, {Text: "two"}, {Text: "three"}, {Text: "four"}, {Text: "five"},
		},
	})
	capped.Layout(Rect{W: 40, H: 3})
	if capped.Height() != 3 {
		t.Fatalf("capped Height = %d, want 3", capped.Height())
	}
	plain = ansi.Strip(capped.Render(40, 3))
	if !strings.Contains(plain, "… +3 more") {
		t.Fatalf("capped output = %q, want overflow summary", plain)
	}
	if strings.Contains(plain, "three") {
		t.Fatalf("capped output painted clipped rows:\n%s", plain)
	}
}

func TestComposerFooterStateAndLoadingWave(t *testing.T) {
	f := NewComposerFooter(ComposerFooterOpts{ID: "footer", Animations: true})
	if f.Any() {
		t.Fatal("empty footer should not claim a row")
	}
	f.Layout(Rect{W: 40, H: 1})
	if got := f.Render(40, 1); strings.TrimSpace(ansi.Strip(got)) != "" {
		t.Fatalf("empty footer painted %q", got)
	}

	f.SetState(true, "~/src/myagent", "main.go", true, "")
	if !f.Any() {
		t.Fatal("busy footer should claim a row")
	}
	plain := ansi.Strip(f.Render(40, 1))
	if !strings.Contains(plain, LoadingWaveGlyph) {
		t.Fatalf("footer = %q, want loading comet", plain)
	}
	if !strings.Contains(plain, "~/src/myagent") || !strings.Contains(plain, "main.go") {
		t.Fatalf("footer = %q, want cwd and file chip", plain)
	}

	if !f.Advance(LoadingWaveInterval) {
		t.Fatal("busy loading wave did not advance")
	}
	if f.Advance(0) {
		t.Fatal("zero-duration advance reported a painted change")
	}
	f.SetAnimations(false)
	if f.Advance(LoadingWaveInterval) {
		t.Fatal("disabled loading wave still advanced")
	}
	if got := ansi.Strip(f.Render(40, 1)); !strings.Contains(got, OneCellStillGlyph) {
		t.Fatalf("still footer = %q, want the ▪ fallback", got)
	}

	f.SetState(false, "", "", false, "ctrl+j newline")
	if !f.Any() {
		t.Fatal("hint footer should claim a row")
	}
	if got := ansi.Strip(f.Render(40, 1)); !strings.Contains(got, "ctrl+j newline") {
		t.Fatalf("hint footer = %q, want hint text", got)
	}

	f.SetState(false, "", "", false, "")
	if f.Any() {
		t.Fatal("cleared footer still claims a row")
	}
}

func TestHoverableSurfaceMarkerAndDelegation(t *testing.T) {
	child := NewText(TextOpts{Content: "tool output", Fg: DefaultTheme().Text.Base})
	h := NewHoverable(HoverableOpts{ID: "hover", Child: child, Surface: DefaultTheme().Background.Raised.High})
	h.Layout(Rect{X: 2, Y: 3, W: 12, H: 1})

	before := h.Render(12, 1)
	if h.Hovered() {
		t.Fatal("wrapper started hovered")
	}

	entered := 0
	h.OnHover = func(on bool) {
		if on {
			entered++
		}
	}
	if !h.Mouse(tea.MouseMotionMsg{X: 4, Y: 3}, h.Bounds()) {
		t.Fatal("motion inside wrapper not consumed")
	}
	if !h.Hovered() || entered != 1 {
		t.Fatalf("hover = %v, callbacks = %d", h.Hovered(), entered)
	}
	after := h.Render(12, 1)
	if after == before {
		t.Fatal("hover surface did not repaint")
	}
	if ansi.Strip(after) != ansi.Strip(before) {
		t.Fatal("hover surface changed the child's plain text")
	}

	h.SetHovered(false)
	if h.Render(12, 1) != before {
		t.Fatal("leaving hover did not restore the child paint")
	}

	// A configured marker paints over column 0.
	marked := NewHoverable(HoverableOpts{ID: "marked", Child: NewText(TextOpts{Content: "row"}), Marker: "┃"})
	marked.Layout(Rect{W: 8, H: 1})
	marked.SetHovered(true)
	if got := renderLine(t, marked, 8, 1); !strings.HasPrefix(got, "┃") {
		t.Fatalf("marked row = %q, want ┃ prefix", got)
	}

	// Layout and measurement forward to the child; key/mouse gestures delegate.
	probe := newProbe("child")
	wrapped := NewHoverable(HoverableOpts{ID: "wrap", Child: probe})
	wrapped.Layout(Rect{X: 1, Y: 2, W: 9, H: 3})
	if probe.Bounds() != (Rect{X: 1, Y: 2, W: 9, H: 3}) {
		t.Fatalf("child bounds = %+v", probe.Bounds())
	}
	if wrapped.MeasureHeight(9) != 0 {
		t.Fatal("probe has no MeasureHeight, want 0")
	}
	probe.consumeKey = true
	if !wrapped.Key(tea.KeyPressMsg{Code: 'k', Text: "k"}) {
		t.Fatal("key did not delegate to the child")
	}
	if len(probe.keys) != 1 || probe.keys[0] != "k" {
		t.Fatalf("child keys = %v", probe.keys)
	}
	if len(wrapped.Children()) != 1 || wrapped.FocusTarget() != nil {
		t.Fatal("wrapper children/focus contract changed")
	}
}

func TestRootInitialFocusWaitsForTarget(t *testing.T) {
	head := newProbe("head")
	base := VBox(Child{W: head, Fixed: 1})
	root := NewRoot(base, WithInitialFocus("composer"))

	root.Render(40, 5)
	if got := root.PendingFocus(); got != "composer" {
		t.Fatalf("pending focus = %q, want composer", got)
	}
	if got := root.Focus.FocusedID(); got != "" {
		t.Fatalf("focused id = %q before the target exists", got)
	}

	composer := newProbe("composer")
	base.Add(Child{W: composer, Fixed: 1})
	root.Layout(Rect{X: 0, Y: 0, W: 40, H: 5})
	if got := root.Focus.FocusedID(); got != "composer" {
		t.Fatalf("focused id after layout = %q, want composer", got)
	}
	if root.PendingFocus() != "" {
		t.Fatal("pending focus survived application")
	}
	if !composer.focused {
		t.Fatal("queued target did not receive focus")
	}
}

func TestRootHandleKeySkipsBase(t *testing.T) {
	base := newProbe("base")
	base.consumeKey = true
	root := NewRoot(base)
	root.Render(20, 4)

	if !root.Key(tea.KeyPressMsg{Code: 'a', Text: "a"}) {
		t.Fatal("Root.Key did not dispatch to the base")
	}
	if len(base.keys) != 1 {
		t.Fatalf("base keys = %v, want one dispatch", base.keys)
	}

	if root.HandleKey(tea.KeyPressMsg{Code: 'b', Text: "b"}) {
		t.Fatal("HandleKey consumed a key through the base")
	}
	if len(base.keys) != 1 {
		t.Fatalf("base keys = %v after HandleKey, want no new dispatch", base.keys)
	}
}

func TestRootHandleKeyReachesFocusedChild(t *testing.T) {
	head := newProbe("head")
	child := newProbe("child")
	child.consumeKey = true
	root := NewRoot(VBox(Child{W: head, Fixed: 1}, Child{W: child, Grow: 1}))
	root.Render(20, 6)
	root.Focus.Focus("child")

	if !root.HandleKey(tea.KeyPressMsg{Code: 'c', Text: "c"}) {
		t.Fatal("HandleKey did not reach the focused child")
	}
	if len(child.keys) != 1 || child.keys[0] != "c" {
		t.Fatalf("child keys = %v", child.keys)
	}
}

func TestRootHandleMouseSkipsBase(t *testing.T) {
	base := newProbe("base")
	base.consumeMouse = true
	root := NewRoot(base)
	root.Render(20, 4)

	click := tea.MouseClickMsg{X: 3, Y: 2, Button: tea.MouseLeft}
	if !root.Mouse(click, Rect{}) {
		t.Fatal("Root.Mouse did not dispatch to the base")
	}
	if base.mouseEvents != 1 {
		t.Fatalf("base mouse events = %d, want 1", base.mouseEvents)
	}

	if root.HandleMouse(click, Rect{}) {
		t.Fatal("HandleMouse consumed a hit through the base")
	}
	if base.mouseEvents != 1 {
		t.Fatalf("base mouse events = %d after HandleMouse, want no new dispatch", base.mouseEvents)
	}
}
