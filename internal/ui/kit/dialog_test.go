package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// --- helpers ---------------------------------------------------------------

func keyEsc() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyEsc} }
func keyEnter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func keyDown() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: tea.KeyDown} }
func keyUp() tea.KeyPressMsg    { return tea.KeyPressMsg{Code: tea.KeyUp} }

func keyRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

// testFooter is the §3.0.D footer: "cancel esc" left, "confirm enter" primary
// right-aligned.
func testFooter() *DialogFooter {
	return NewDialogFooter(
		DialogButton{Label: "cancel", Key: "esc"},
		DialogButton{Label: "confirm", Key: "enter", Primary: true},
	)
}

func testDialog(title string, body Widget) *DialogLayout {
	return NewDialogLayout(DialogLayoutOpts{
		ID:     "dialog",
		Title:  title,
		Body:   body,
		Footer: testFooter(),
	})
}

func sampleItems() []SelectItem {
	return []SelectItem{
		{ID: "opus", Title: "Claude Opus", Desc: "most capable", Group: "Anthropic", Footer: "ctrl+1"},
		{ID: "sonnet", Title: "Claude Sonnet", Desc: "balanced", Group: "Anthropic", Footer: "ctrl+2"},
		{ID: "gpt", Title: "GPT-5", Desc: "OpenAI flagship", Group: "OpenAI"},
		{ID: "flash", Title: "Gemini Flash", Desc: "fast and cheap", Group: "Google"},
	}
}

// manyItems builds n ungrouped rows.
func manyItems(n int) []SelectItem {
	items := make([]SelectItem, n)
	for i := range items {
		items[i] = SelectItem{ID: "m" + itoa(i), Title: "model " + itoa(i)}
	}
	return items
}

// --- dialog stack ----------------------------------------------------------

// TestDialogWidthTiers pins the 60/88/116 tiers and the width-2 clamp.
func TestDialogWidthTiers(t *testing.T) {
	cases := []struct{ w, want int }{{40, 38}, {66, 60}, {80, 60}, {120, 88}, {128, 116}, {200, 116}}
	for _, tc := range cases {
		if got := dialogWidth(tc.w); got != tc.want {
			t.Errorf("dialogWidth(%d) = %d, want %d", tc.w, got, tc.want)
		}
	}
}

// TestDialogStackPushPopReplace covers the provider stack rules: push stacks,
// replace clears, pop restores, and only the top dialog is visible.
func TestDialogStackPushPopReplace(t *testing.T) {
	s := NewDialogStack()
	if s.Modal() || s.Len() != 0 {
		t.Fatal("new stack is not empty")
	}
	a, b, c := newProbe("a"), newProbe("b"), newProbe("c")
	s.Push(a)
	if !s.Modal() || s.Len() != 1 || s.Top() != a {
		t.Fatalf("after Push: modal=%v len=%d top=%v", s.Modal(), s.Len(), s.Top())
	}
	s.Push(b)
	if !WidgetVisible(b) || WidgetVisible(a) {
		t.Fatal("single-visible-dialog rule broken: both dialogs visible")
	}
	s.Replace(c)
	if s.Len() != 1 || s.Top() != c {
		t.Fatalf("Replace did not clear the stack: len=%d", s.Len())
	}
	if WidgetVisible(b) {
		t.Fatal("replaced dialog stayed visible")
	}
	if !s.Pop() || s.Modal() {
		t.Fatal("Pop did not empty the stack")
	}
	if s.Pop() {
		t.Fatal("Pop on an empty stack reported a dialog")
	}
	if s.Key(keyEsc()) {
		t.Fatal("an empty stack consumed Esc")
	}
}

// TestDialogStackBackdropClose is the backdrop rule: a click outside the dialog
// closes it and never reaches the transcript beneath.
func TestDialogStackBackdropClose(t *testing.T) {
	base := newProbe("transcript")
	stack := NewDialogStack()
	root := NewRoot(base, RegisterBandHost(BandDialogs, stack))
	root.Render(80, 24)

	stack.Push(testDialog("Pick a model", NewText(TextOpts{Content: "body"})))
	root.Render(80, 24)
	if stack.Len() != 1 {
		t.Fatal("dialog did not stay open")
	}
	if !root.Mouse(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft}, Rect{}) {
		t.Fatal("backdrop click was not consumed")
	}
	if stack.Len() != 0 {
		t.Fatal("backdrop click did not close the dialog")
	}
	if base.mouseEvents != 0 {
		t.Fatalf("transcript received %d backdrop events", base.mouseEvents)
	}
	// With the stack empty the same point belongs to the transcript again.
	root.Render(80, 24)
	root.Mouse(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft}, Rect{})
	if base.mouseEvents != 1 {
		t.Fatalf("transcript events after close = %d, want 1", base.mouseEvents)
	}
}

// TestDialogStackSelectionGuard: a drag in progress under the dialog keeps it
// open (the hook is the only coupling with internal/tui selection).
func TestDialogStackSelectionGuard(t *testing.T) {
	base := newProbe("transcript")
	stack := NewDialogStack()
	root := NewRoot(base, RegisterBandHost(BandDialogs, stack))
	stack.Push(testDialog("Pick", nil))
	root.Render(80, 24)

	dragging := true
	stack.SelectionActive = func() bool { return dragging }
	if !root.Mouse(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft}, Rect{}) {
		t.Fatal("guarded backdrop click was not consumed")
	}
	if stack.Len() != 1 {
		t.Fatal("backdrop click closed the dialog during a text selection")
	}
	dragging = false
	root.Mouse(tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft}, Rect{})
	if stack.Len() != 0 {
		t.Fatal("backdrop click did not close after the selection ended")
	}
}

// TestDialogStackHitGating: the dialog band owns the hit table; clicks inside
// the dialog reach band-20 widgets and never the transcript.
func TestDialogStackHitGating(t *testing.T) {
	base := newProbe("transcript")
	stack := NewDialogStack()
	root := NewRoot(base, RegisterBandHost(BandDialogs, stack))
	stack.Push(testDialog("Pick a model", NewText(TextOpts{Content: "body"})))
	root.Render(80, 24)

	hit, ok := root.Hit(40, 2)
	if !ok || hit.Band != BandDialogs || hit.ID != "dialogs" {
		t.Fatalf("backdrop hit = %+v ok=%v, want the dialog band", hit, ok)
	}
	hit, ok = root.Hit(40, 10)
	if !ok || hit.Band != BandDialogs || hit.ID != "dialog" {
		t.Fatalf("dialog hit = %+v ok=%v, want band-20 dialog", hit, ok)
	}
	if !root.Mouse(tea.MouseClickMsg{X: 40, Y: 10, Button: tea.MouseLeft}, Rect{}) {
		t.Fatal("dialog click was not consumed")
	}
	if base.mouseEvents != 0 {
		t.Fatalf("transcript received %d clicks through the dialog", base.mouseEvents)
	}
	if stack.Len() != 1 {
		t.Fatal("a click inside the dialog closed it")
	}
}

// TestDialogStackRefocus asserts opening focuses the dialog and closing returns
// focus to the previous target.
func TestDialogStackRefocus(t *testing.T) {
	base := newProbe("composer")
	stack := NewDialogStack()
	root := NewRoot(base, RegisterBandHost(BandDialogs, stack))
	stack.Focus = root.Focus
	root.Render(80, 24)
	root.Focus.Focus("composer")

	stack.Push(testDialog("Pick", nil))
	root.Render(80, 24)
	if got := root.Focus.FocusedID(); got != "dialog" {
		t.Fatalf("focus after open = %q, want dialog", got)
	}
	if !stack.Pop() {
		t.Fatal("pop")
	}
	root.Render(80, 24)
	if got := root.Focus.FocusedID(); got != "composer" {
		t.Fatalf("focus after close = %q, want composer", got)
	}
}

// TestDialogStackModalKeyGate: the modal stack consumes keys before the base.
func TestDialogStackModalKeyGate(t *testing.T) {
	base := newProbe("composer")
	stack := NewDialogStack()
	root := NewRoot(base, RegisterBandHost(BandDialogs, stack))
	root.Render(60, 20)

	root.Key(keyRune('x'))
	if len(base.keys) != 1 {
		t.Fatalf("closed stack consumed keys: %v", base.keys)
	}
	stack.Push(testDialog("Pick", NewText(TextOpts{Content: "b"})))
	root.Render(60, 20)
	if !root.Key(keyRune('x')) {
		t.Fatal("modal stack did not consume the key")
	}
	if len(base.keys) != 1 {
		t.Fatal("key leaked to the base while a dialog was open")
	}
}

// TestDialogStackEscPops: Esc pops the top dialog.
func TestDialogStackEscPops(t *testing.T) {
	stack := NewDialogStack()
	stack.Push(testDialog("Pick", nil))
	if !stack.Key(keyEsc()) {
		t.Fatal("Esc was not consumed")
	}
	if stack.Len() != 0 {
		t.Fatal("Esc did not pop the dialog")
	}
}

// TestConfirmActionsDismiss covers the small Confirm helper: Enter runs
// OnConfirm and closes; Esc pops and runs OnCancel through Dismissible.
func TestConfirmActionsDismiss(t *testing.T) {
	open := func(t *testing.T) (*Root, *DialogStack, *int, *int) {
		t.Helper()
		base := newProbe("composer")
		stack := NewDialogStack()
		root := NewRoot(base, RegisterBandHost(BandDialogs, stack))
		root.Render(80, 24)
		confirmed, cancelled := 0, 0
		stack.Push(NewConfirm(ConfirmOpts{
			Title: "Delete model?",
			Body:  "This cannot be undone.",
			OnConfirm: func() bool {
				confirmed++
				return true
			},
			OnCancel: func() bool {
				cancelled++
				return true
			},
		}))
		root.Render(80, 24)
		return root, stack, &confirmed, &cancelled
	}

	t.Run("enter confirms", func(t *testing.T) {
		root, stack, confirmed, cancelled := open(t)
		if !root.Key(keyEnter()) {
			t.Fatal("Enter was not consumed")
		}
		if *confirmed != 1 || *cancelled != 0 || stack.Len() != 0 {
			t.Fatalf("confirmed=%d cancelled=%d len=%d, want 1/0/0", *confirmed, *cancelled, stack.Len())
		}
	})

	t.Run("esc cancels", func(t *testing.T) {
		root, stack, confirmed, cancelled := open(t)
		if !root.Key(keyEsc()) {
			t.Fatal("Esc was not consumed")
		}
		if *confirmed != 0 || *cancelled != 1 || stack.Len() != 0 {
			t.Fatalf("confirmed=%d cancelled=%d len=%d, want 0/1/0", *confirmed, *cancelled, stack.Len())
		}
	})
}

// TestDialogSelectEscClearsThenPops is the "Esc consumed when handling input"
// case: the first Esc clears the filter, the second pops.
func TestDialogSelectEscClearsThenPops(t *testing.T) {
	sel := NewDialogSelect(DialogSelectOpts{Items: sampleItems(), Placeholder: "search"})
	base := newProbe("composer")
	stack := NewDialogStack()
	root := NewRoot(base, RegisterBandHost(BandDialogs, stack))
	stack.Push(testDialog("Models", sel))
	root.Render(80, 24)

	if !root.Key(keyRune('s')) {
		t.Fatal("typing was not consumed by the picker")
	}
	if sel.Query() != "s" {
		t.Fatalf("query = %q, want s", sel.Query())
	}
	if !root.Key(keyEsc()) {
		t.Fatal("Esc was not consumed")
	}
	if stack.Len() != 1 {
		t.Fatal("Esc with an active filter closed the dialog")
	}
	if sel.Query() != "" {
		t.Fatalf("Esc did not clear the filter: %q", sel.Query())
	}
	root.Key(keyEsc())
	if stack.Len() != 0 {
		t.Fatal("second Esc did not pop the dialog")
	}
}

// --- dialog chrome ---------------------------------------------------------

// TestDialogLayoutBands pins the header/body/footer rects and their 1-row gaps
// at the 60-cell tier with the 3-cell side inset.
func TestDialogLayoutBands(t *testing.T) {
	d := testDialog("Pick a model", NewText(TextOpts{Content: "body"}))
	d.Layout(Rect{X: 0, Y: 0, W: 60, H: 14})

	cases := []struct {
		name string
		got  Rect
		want Rect
	}{
		{"header", d.header, Rect{X: 3, Y: 1, W: 54, H: 1}},
		{"body", d.bodyRect, Rect{X: 3, Y: 3, W: 54, H: 8}},
		{"footer", d.footerRect, Rect{X: 3, Y: 12, W: 54, H: 1}},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %+v, want %+v", tc.name, tc.got, tc.want)
		}
	}
	if got := d.bodyRect.Y + d.bodyRect.H; got != 11 {
		t.Errorf("body ends at %d, want the gap row at 11", got)
	}
}

// TestDialogChromeText asserts the BOLD title, the right-aligned muted "esc",
// and the lowercase footer hints with their 1-cell hover pads.
func TestDialogChromeText(t *testing.T) {
	d := testDialog("Pick a model", NewText(TextOpts{Content: "body"}))
	d.Layout(Rect{X: 0, Y: 0, W: 60, H: 14})
	lines := strings.Split(ansi.Strip(d.Render(60, 14)), "\n")

	header := strings.TrimRight(lines[1], " ")
	if got := strings.TrimLeft(header, " "); !strings.HasPrefix(got, "Pick a model") {
		t.Fatalf("header = %q, want the title first", header)
	}
	if !strings.HasSuffix(header, " esc") {
		t.Fatalf("header = %q, want a right-aligned esc hint", header)
	}

	footer := strings.TrimRight(lines[12], " ")
	if !strings.Contains(footer, " cancel esc ") {
		t.Fatalf("footer = %q, want a padded cancel esc left", footer)
	}
	if !strings.HasSuffix(footer, " confirm enter") {
		t.Fatalf("footer = %q, want a padded primary confirm enter right", footer)
	}
	if strings.Contains(footer, "CANCEL") || strings.Contains(footer, "Confirm") {
		t.Fatalf("footer hints are not lowercase: %q", footer)
	}
}

// TestScrimBlend checks the alpha-150 blend math on a painted cell.
func TestScrimBlend(t *testing.T) {
	th := DefaultTheme()
	cv := lipgloss.NewCanvas(3, 2)
	fillRect(cv, 0, 0, 3, 2, th.Background.Raised.Base)
	before := cv.CellAt(1, 1).Style.Bg
	applyScrim(cv, Rect{W: 3, H: 2}, th.Background.Base, ScrimAlpha)
	after := cv.CellAt(1, 1).Style.Bg
	want := blendToward(before, th.Background.Base.Color(), ScrimAlpha)

	br, bg, bb, ba := before.RGBA()
	ar, ag, ab, aa := after.RGBA()
	wr, wg, wb, wa := want.RGBA()
	if ar != wr || ag != wg || ab != wb || aa != wa {
		t.Fatalf("scrimmed cell = %d/%d/%d/%d, want %d/%d/%d/%d", ar, ag, ab, aa, wr, wg, wb, wa)
	}
	if ar == br && ag == bg && ab == bb && aa == ba {
		t.Fatal("scrim did not change the cell")
	}
}

// TestScrimDimsWithoutRelayout proves Root applies the scrim to the composed
// content band: only styling changes, never layout.
func TestScrimDimsWithoutRelayout(t *testing.T) {
	with := dialogGoldenRoot(ScrimAlpha)
	without := dialogGoldenRoot(0)
	dimmed := with.Render(80, 24)
	plain := without.Render(80, 24)
	if ansi.Strip(dimmed) != ansi.Strip(plain) {
		t.Fatal("scrim changed the frame layout")
	}
	if dimmed == plain {
		t.Fatal("scrim changed no cell styling")
	}
}

// TestScrimClosedStackHasNone keeps the backdrop off when no dialog is open.
func TestScrimClosedStackHasNone(t *testing.T) {
	s := NewDialogStack()
	if _, _, on := s.Scrim(); on {
		t.Fatal("empty stack reported a scrim")
	}
	s.Push(testDialog("Pick", nil))
	tok, alpha, on := s.Scrim()
	if !on || alpha != ScrimAlpha || tok != DefaultTheme().Background.Base {
		t.Fatalf("scrim = %v/%v/%v", tok, alpha, on)
	}
}

// --- dialog select ---------------------------------------------------------

// TestDialogSelectFuzzyWeighting: a title match outranks a description match.
func TestDialogSelectFuzzyWeighting(t *testing.T) {
	items := []SelectItem{
		{ID: "desc", Title: "Other", Desc: "gpt based"},
		{ID: "title", Title: "GPT-5", Desc: "flagship"},
	}
	rows := SelectRows(items, "gpt")
	if len(rows) != 2 || rows[0].ID != "title" {
		t.Fatalf("rows = %+v; the title match must rank first", rows)
	}
	if _, ok := fuzzyScore("gpt", "Other"); ok {
		t.Fatal("fuzzy matched a target without the subsequence")
	}
	if rows := SelectRows(items, "zzz"); len(rows) != 0 {
		t.Fatalf("non-matching query returned %+v", rows)
	}
}

// TestDialogSelectMarker pins the 1-cell gutter marker and its ▪ fallback.
func TestDialogSelectMarker(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  bool
		want string
	}{
		{"marker", false, "● Claude Opus"},
		{"no-motion", true, "▪ Claude Opus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sel := NewDialogSelect(DialogSelectOpts{Items: sampleItems(), Placeholder: "search", NoMotion: tc.mut})
			sel.setParentHeight(24)
			sel.Layout(Rect{X: 0, Y: 0, W: 40, H: 10})
			lines := strings.Split(ansi.Strip(sel.Render(40, 10)), "\n")
			// Row 0 is the search line, row 1 is blank, row 2 the first group.
			if !strings.HasPrefix(lines[3], tc.want) {
				t.Fatalf("selected row = %q, want prefix %q", lines[3], tc.want)
			}
			if strings.HasPrefix(lines[4], "●") || strings.HasPrefix(lines[4], "▪") {
				t.Fatalf("unselected row carries a marker: %q", lines[4])
			}
			if !strings.HasPrefix(lines[2], "Anthropic") {
				t.Fatalf("category header = %q", lines[2])
			}
		})
	}
}

func TestDialogSelectSearchSpacing(t *testing.T) {
	selectDialog := NewDialogSelect(DialogSelectOpts{
		Placeholder: "Search commands",
		Items:       []SelectItem{{ID: "help", Title: "Help"}},
	})
	selectDialog.Layout(Rect{W: 30, H: 4})
	lines := strings.Split(ansi.Strip(selectDialog.Render(30, 4)), "\n")
	if len(lines) < 3 {
		t.Fatalf("rendered dialog has %d rows, want search, gap, and list: %q", len(lines), lines)
	}
	if got := strings.TrimSpace(lines[0]); got != "Search commands" {
		t.Fatalf("search placeholder = %q, want plain placeholder", got)
	}
	if strings.TrimSpace(lines[1]) != "" {
		t.Fatalf("search/list gap = %q, want blank row", lines[1])
	}
	if !strings.Contains(lines[2], "Help") {
		t.Fatalf("list row missing after search gap: %q", lines[2])
	}
}

// TestDialogSelectHeightCap: rows cap at floor(h/2)-6 with an "…" overflow row.
func TestDialogSelectHeightCap(t *testing.T) {
	sel := NewDialogSelect(DialogSelectOpts{Items: manyItems(12), Placeholder: "search"})
	sel.setParentHeight(24)
	if got, want := sel.capRows(), 6; got != want {
		t.Fatalf("capRows = %d, want %d", got, want)
	}
	sel.Layout(Rect{X: 0, Y: 0, W: 30, H: 12})
	lines := strings.Split(ansi.Strip(sel.Render(30, 12)), "\n")
	if got := strings.TrimSpace(lines[7]); got != "…" {
		t.Fatalf("overflow row = %q, want …", got)
	}
	if got := strings.TrimSpace(lines[8]); got != "" {
		t.Fatalf("row past the cap painted: %q", got)
	}
}

// TestDialogSelectKeyboard covers up/down/enter and type-to-filter.
func TestDialogSelectKeyboard(t *testing.T) {
	picked := ""
	sel := NewDialogSelect(DialogSelectOpts{
		Items:    sampleItems(),
		OnSelect: func(it SelectItem) bool { picked = it.ID; return true },
	})
	sel.setParentHeight(40)
	sel.Layout(Rect{X: 0, Y: 0, W: 40, H: 24})

	sel.Key(keyDown())
	if row, ok := sel.list.SelectedRow(); !ok || row.ID != "sonnet" {
		t.Fatalf("after down = %+v, want sonnet", row)
	}
	sel.Key(keyUp())
	if row, _ := sel.list.SelectedRow(); row.ID != "opus" {
		t.Fatalf("after up = %q, want opus", row.ID)
	}
	sel.Key(keyEnter())
	if picked != "opus" {
		t.Fatalf("enter picked %q, want opus", picked)
	}
	// Typing filters; the title match ranks above a description match.
	for _, r := range "flash" {
		sel.Key(keyRune(r))
	}
	if sel.Query() != "flash" {
		t.Fatalf("query = %q", sel.Query())
	}
	rows := sel.filteredRows()
	if len(rows) != 4 || !rows[0].Header || rows[0].Title != "Google" || rows[1].ID != "flash" {
		t.Fatalf("filtered rows = %+v; the title match must rank first", rows)
	}
}

// --- shared list -----------------------------------------------------------

// TestListGroupHeaders: headers group rows with a blank separator and are not
// selectable.
func TestListGroupHeaders(t *testing.T) {
	l := NewList(ListOpts{Rows: []ListRow{
		{Title: "Group A", Header: true},
		{ID: "a", Title: "a1"},
		{Title: "Group B", Header: true},
		{ID: "b", Title: "b1"},
	}})
	l.Layout(Rect{X: 0, Y: 0, W: 20, H: 6})
	lines := strings.Split(ansi.Strip(l.Render(20, 6)), "\n")
	if got := strings.TrimSpace(lines[0]); got != "Group A" {
		t.Fatalf("line 0 = %q", got)
	}
	if got := strings.TrimSpace(lines[2]); got != "" {
		t.Fatalf("blank separator missing at line 2: %q", got)
	}
	if got := strings.TrimSpace(lines[3]); got != "Group B" {
		t.Fatalf("line 3 = %q", got)
	}
	if row, _ := l.SelectedRow(); row.ID != "a" {
		t.Fatalf("initial selection = %q, want a", row.ID)
	}
	if !l.Move(1) {
		t.Fatal("Move did not skip the headers")
	}
	if row, _ := l.SelectedRow(); row.ID != "b" {
		t.Fatalf("selection after move = %q, want b", row.ID)
	}
	if l.Move(-1) && l.Selected != 1 {
		t.Fatalf("move up landed on %d, want a1", l.Selected)
	}
}

// TestListWindowKeepsSelectionVisible drives the scroll window past the rect.
func TestListWindowKeepsSelectionVisible(t *testing.T) {
	rows := make([]ListRow, 10)
	for i := range rows {
		rows[i] = ListRow{ID: "r" + itoa(i), Title: "row " + itoa(i)}
	}
	l := NewList(ListOpts{Rows: rows, MaxRows: 4})
	l.Layout(Rect{X: 0, Y: 0, W: 20, H: 4})
	for i := 0; i < 6; i++ {
		l.Move(1)
	}
	lines := strings.Split(ansi.Strip(l.Render(20, 4)), "\n")
	if !strings.HasPrefix(lines[2], "● row 6") {
		t.Fatalf("window = %q; selection must stay visible", lines)
	}
	if got := strings.TrimRight(lines[3], " "); got != "…" {
		t.Fatalf("overflow row = %q, want …", got)
	}
	if start, count := l.Window(4); start != 4 || count != 3 {
		t.Fatalf("window = %d/%d, want 4/3", start, count)
	}
}

// --- band host -------------------------------------------------------------

// TestBandHostRegistration covers the reusable RegisterBandHost API: a
// non-capturing host paints but never takes keys.
func TestBandHostRegistration(t *testing.T) {
	base := newProbe("base")
	toast := newProbe("toast")
	toast.consumeKey = true
	root := NewRoot(base, RegisterBandHost(BandToasts, toast, WithBandCapture(false)))
	root.Render(40, 10)

	root.Key(keyRune('x'))
	if len(toast.keys) != 0 {
		t.Fatal("a non-capturing band host received keys")
	}
	if len(base.keys) != 1 {
		t.Fatalf("base keys = %v, want one", base.keys)
	}
	rect, ok := root.HitRegistry().Rect("toast")
	if !ok || rect != (Rect{0, 0, 40, 10}) {
		t.Fatalf("toast rect = %+v ok=%v", rect, ok)
	}
	if hit, ok := root.Hit(5, 5); !ok || hit.Band != BandToasts {
		t.Fatalf("hit = %+v ok=%v, want the toast band", hit, ok)
	}
}

// --- goldens ---------------------------------------------------------------

// dialogGoldenRoot builds the golden shell with a picker dialog open. It is the
// s2a dialog-open fixture: title, search, grouped rows, footer, and scrim.
func dialogGoldenRoot(alpha float64) *Root {
	stack := NewDialogStack()
	stack.Alpha = alpha
	sel := NewDialogSelect(DialogSelectOpts{
		Items: []SelectItem{
			{ID: "opus", Title: "Claude Opus", Desc: "most capable", Group: "Anthropic", Footer: "ctrl+1"},
			{ID: "sonnet", Title: "Claude Sonnet", Desc: "balanced", Group: "Anthropic", Footer: "ctrl+2"},
			{ID: "gpt", Title: "GPT-5", Desc: "OpenAI flagship", Group: "OpenAI"},
			{ID: "flash", Title: "Gemini Flash", Desc: "fast and cheap", Group: "Google"},
		},
		Placeholder: "search models",
	})
	stack.Push(NewDialogLayout(DialogLayoutOpts{
		ID:     "dialog",
		Title:  "Select a model",
		Body:   sel,
		Footer: testFooter(),
	}))
	base := goldenTree()
	return NewRoot(base.Base, RegisterBandHost(BandDialogs, stack))
}

// TestGoldenDialogFrames is the s2a fixture set: the dialog-open state at
// 80x24 and 120x40, plaintext (ANSI stripped) for reviewable diffs.
func TestGoldenDialogFrames(t *testing.T) {
	sizes := []struct{ w, h int }{{80, 24}, {120, 40}}
	for _, size := range sizes {
		name := "s2a_dialog_" + itoa(size.w) + "x" + itoa(size.h)
		t.Run(name, func(t *testing.T) {
			root := dialogGoldenRoot(ScrimAlpha)
			raw := root.Render(size.w, size.h)
			assertFrameSize(t, raw, size.w, size.h)
			plain := trimRightLines(ansi.Strip(raw))
			for _, want := range []string{"Select a model", "search models", "Anthropic", "● Claude Opus", "cancel esc", "confirm enter"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("frame is missing %q\n%s", want, plain)
				}
			}
			path := filepath.Join("testdata", name+".txt")
			if os.Getenv("UPDATE_GOLDENS") == "1" {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(plain+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run with UPDATE_GOLDENS=1 to create)", err)
			}
			wantStr := strings.TrimSuffix(strings.TrimSuffix(string(want), "\n"), "\r")
			if plain != wantStr {
				t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, plain, wantStr)
			}
		})
	}
}
