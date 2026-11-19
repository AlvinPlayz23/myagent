package kit

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// List marker glyphs. The 1-cell gutter shows the current row marker; NoMotion
// uses the geometric fallback (§3.0.E no-animation fallbacks).
const (
	listMarker         = "●"
	listMarkerFallback = "▪"
)

// ListRow is one display line in a List. Header rows group selectable rows and
// are never focusable. Titles carry the row identity; Desc/Footer are muted
// metadata and the per-row hint.
//
// OpenTUI counterpart: <select> options + <tabselect> categories. OpenCode
// counterpart: dialog-select rows.
type ListRow struct {
	ID     string
	Title  string
	Desc   string
	Footer string
	Header bool
	// Error paints the per-row footer in text.feedback.error ("$2 left").
	Error bool
}

// ListOpts configures a List.
type ListOpts struct {
	ID   string
	Rows []ListRow
	// MaxRows caps the painted rows independently of the assigned height.
	// DialogSelect sets this to floor(h/2)-6.
	MaxRows int
	// NoMotion swaps the ● marker for the ▪ fallback.
	NoMotion bool
	// HideOverflow lets a surrounding scrollbar indicate additional rows.
	HideOverflow bool
	// Palette uses compact command-palette selection chrome.
	Compact         bool
	SectionDividers bool
	Palette         bool
}

// List is the shared scrollable-rows widget used by DialogSelect. It owns
// cursor movement, the visible window, category headers, the 1-cell marker
// gutter, per-row footers, and the "…" overflow row. It never reaches into
// internal/tui or the transcript.
//
// OpenTUI counterpart: <select> over a scrollbox. OpenCode counterpart: the
// list body of dialog-select.tsx.
type List struct {
	WidgetBase

	Rows            []ListRow
	MaxRows         int
	NoMotion        bool
	Palette         bool
	Compact         bool
	SectionDividers bool
	Selected        int
	hideOverflow    bool
	OnChoose        func(ListRow) bool
	Offset          int
	cache           RenderCache
}

// NewList builds a list with the OpenCode marker gutter.
func NewList(opts ListOpts) *List {
	l := &List{
		WidgetBase:      NewWidgetBase(opts.ID),
		MaxRows:         opts.MaxRows,
		NoMotion:        opts.NoMotion,
		Palette:         opts.Palette,
		Compact:         opts.Compact,
		SectionDividers: opts.SectionDividers,
		hideOverflow:    opts.HideOverflow,
		Selected:        -1,
	}
	l.BindRows(opts.Rows)
	return l
}

// BindRows replaces the rows without dropping the cursor when its ID survives.
// It is used by DialogSelect after every refilter.
func (l *List) BindRows(rows []ListRow) {
	prev := ""
	if row, ok := l.SelectedRow(); ok {
		prev = row.ID
	}
	l.Rows = rows
	l.Selected = l.firstSelectable()
	if prev != "" {
		for i, row := range rows {
			if !row.Header && row.ID == prev {
				l.Selected = i
				break
			}
		}
	}
	l.MarkDirty()
}

// SetRows is the named setter form of BindRows.
func (l *List) SetRows(rows []ListRow) { l.BindRows(rows) }

// Move shifts the cursor by delta, skipping headers and wrapping.
func (l *List) Move(delta int) bool {
	idx := l.selectableIndices()
	if len(idx) == 0 {
		return false
	}
	pos := -1
	for i, row := range idx {
		if row == l.Selected {
			pos = i
			break
		}
	}
	if pos < 0 {
		pos = 0
		if delta < 0 {
			pos = len(idx) - 1
		}
	} else {
		pos = ((pos+delta)%len(idx) + len(idx)) % len(idx)
	}
	if idx[pos] == l.Selected {
		return false
	}
	l.Selected = idx[pos]
	l.MarkDirty()
	return true
}

// SelectedRow returns the focused row, or false when nothing is selectable.
func (l *List) SelectedRow() (ListRow, bool) {
	if l.Selected < 0 || l.Selected >= len(l.Rows) || l.Rows[l.Selected].Header {
		return ListRow{}, false
	}
	return l.Rows[l.Selected], true
}

// selectableIndices returns Rows indices that can take the cursor.
func (l *List) selectableIndices() []int {
	out := make([]int, 0, len(l.Rows))
	for i, row := range l.Rows {
		if !row.Header {
			out = append(out, i)
		}
	}
	return out
}

// firstSelectable returns the first non-header row index, or -1.
func (l *List) firstSelectable() int {
	for i, row := range l.Rows {
		if !row.Header {
			return i
		}
	}
	return -1
}

// listLine is one painted display line: either a blank category separator or a
// reference back into Rows.
type listLine struct {
	row   int // -1 for a separator
	blank bool
}

// displayLines expands Rows with the blank separators that sit above category
// headers (§3.0.D "category headers group rows with blank-row separation").
func (l *List) displayLines() []listLine {
	out := make([]listLine, 0, len(l.Rows)+4)
	for i, row := range l.Rows {
		if row.Header && len(out) > 0 && !l.SectionDividers {
			out = append(out, listLine{row: -1, blank: true})
		}
		out = append(out, listLine{row: i})
	}
	return out
}

// limit is the number of painted rows for height h, capped by MaxRows.
func (l *List) limit(h int) int {
	if h <= 0 {
		h = l.Bounds().H
	}
	if l.MaxRows > 0 && (h <= 0 || l.MaxRows < h) {
		h = l.MaxRows
	}
	if h < 0 {
		h = 0
	}
	return h
}

// windowState resolves the first visible display line and the number of data
// lines shown before the "…" overflow row.
func (l *List) windowState(h int) (start, slots int, overflow bool) {
	display := l.displayLines()
	limit := l.limit(h)
	if limit <= 0 {
		return 0, 0, false
	}
	// Keep a selectable row visible in a one-line viewport. There is no room
	// for both a row and the overflow marker at that height.
	overflow = !l.hideOverflow && len(display) > limit && limit > 1
	slots = limit
	if overflow {
		slots--
	}
	if slots < 0 {
		slots = 0
	}
	maxStart := max(0, len(display)-slots)
	start = min(max(0, l.Offset), maxStart)
	selLine := -1
	for i, line := range display {
		if line.row == l.Selected {
			selLine = i
			break
		}
	}
	if selLine >= 0 && slots > 0 {
		if selLine < start {
			start = selLine
		}
		if selLine >= start+slots {
			start = selLine - slots + 1
		}
	}
	start = min(max(0, start), maxStart)
	return start, slots, overflow
}

// Window returns the visible display range (start, data-line count). The
// overflow row is not counted; tests and DialogSelect use it for hit math.
func (l *List) Window(h int) (start, count int) {
	start, count, _ = l.windowState(h)
	return start, count
}

// Layout records the assigned rect and keeps the cursor valid.
func (l *List) Layout(r Rect) {
	l.SetBounds(r)
	if l.Selected < 0 || l.Selected >= len(l.Rows) || l.Rows[l.Selected].Header {
		l.Selected = l.firstSelectable()
	}
}

// Render paints the visible window. The final row becomes "…" on overflow.
func (l *List) Render(w, h int) string {
	if !l.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return l.cache.Render(w, h, subtreeRev(l), func() string {
		start, slots, overflow := l.windowState(h)
		display := l.displayLines()
		lines := make([]string, 0, h)
		for i := 0; i < slots && start+i < len(display); i++ {
			line := display[start+i]
			if line.blank {
				lines = append(lines, "")
				continue
			}
			lines = append(lines, l.renderRow(line.row, w))
		}
		if overflow && len(lines) < h {
			lines = append(lines, l.renderOverflow(w))
		}
		for len(lines) < h {
			lines = append(lines, "")
		}
		if len(lines) > h {
			lines = lines[:h]
		}
		return fitBlock(strings.Join(lines, "\n"), w, h)
	})
}

// renderRow paints one row at an explicit Rows index.
func (l *List) renderRow(idx, w int) string {
	row := l.Rows[idx]
	th := DefaultTheme()
	cv := lipgloss.NewCanvas(w, 1)

	if row.Header && l.SectionDividers {
		title := ansi.Truncate(row.Title, w, "")
		ruleW := w - ansi.StringWidth(title) - 1
		line := strings.Repeat(" ", max(0, w-ansi.StringWidth(title))) + title
		if ruleW > 0 {
			line = strings.Repeat("─", ruleW) + " " + title
		}
		drawStyled(cv, 0, 0, w, line, lipgloss.NewStyle().Foreground(th.Text.Muted.Color()))
		return padCanvas(cv, w, 1)
	}
	if row.Header {
		drawStyled(cv, 0, 0, w, row.Title,
			lipgloss.NewStyle().Bold(true).Foreground(th.Text.Strong.Color()))
		return padCanvas(cv, w, 1)
	}

	selected := idx == l.Selected
	if selected {
		bg := th.Background.Action.Primary.Focused()
		if l.Palette {
			bg = th.Background.Selection.Color()
		}
		fillCells(cv, 0, 0, w, bg)
	}
	textFg := th.Text.Base.Color()
	descFg := th.Text.Muted.Color()
	if selected {
		textFg = th.Text.Action.Primary.Focused()
		descFg = th.Text.Action.Secondary.Focused()
		marker := l.marker()
		if l.Palette {
			marker = "›"
			descFg = textFg
		}
		drawText(cv, 0, 0, 1, marker, textFg)
	}

	footerW := ansi.StringWidth(row.Footer)
	avail := w - 2
	if footerW > 0 {
		avail -= footerW + 2
	}
	if avail < 1 {
		avail = 1
	}
	titleW := ansi.StringWidth(row.Title)
	drawText(cv, 2, 0, avail, ansi.Truncate(row.Title, avail, "…"), textFg)
	if !l.Compact && row.Desc != "" && titleW+2 < avail {
		drawText(cv, 2+titleW+2, 0, avail-titleW-2, row.Desc, descFg)
	}
	if footerW > 0 && footerW < w-2 {
		fg := th.Text.Muted.Color()
		if row.Error {
			fg = th.Text.Feedback.Error.Base.Color()
		}
		if selected {
			fg = textFg
		}
		drawText(cv, w-footerW, 0, footerW, row.Footer, fg)
	}
	return padCanvas(cv, w, 1)
}

// renderOverflow paints the "…" overflow row at the gutter column.
func (l *List) renderOverflow(w int) string {
	cv := lipgloss.NewCanvas(w, 1)
	drawText(cv, 0, 0, w, "…", DefaultTheme().Text.Muted.Color())
	return padCanvas(cv, w, 1)
}

// marker resolves the gutter glyph, with the NoMotion/width fallback.
func (l *List) marker() string {
	if l.NoMotion || ansi.StringWidth(listMarker) != 1 {
		return listMarkerFallback
	}
	return listMarker
}

// Key implements the shared list subset: up/down (plus vim keys) and enter.
func (l *List) Key(ev tea.KeyPressMsg) bool {
	switch ev.String() {
	case "up", "k":
		return l.Move(-1)
	case "down", "j":
		return l.Move(1)
	case "enter":
		if row, ok := l.SelectedRow(); ok && l.OnChoose != nil {
			l.OnChoose(row)
		}
		return true
	default:
		return false
	}
}

// Mouse handles wheel scrolling and row clicks. A first click selects; a click
// on the selected row activates it.
func (l *List) Mouse(ev tea.MouseMsg, r Rect) bool {
	if ev == nil || !l.Visible || r.Empty() {
		return false
	}
	if delta, ok := WheelDelta(ev); ok {
		return l.Move(delta)
	}
	click, ok := ev.(tea.MouseClickMsg)
	if !ok || click.Button != tea.MouseLeft {
		return false
	}
	m := ev.Mouse()
	if !r.Contains(m.X, m.Y) {
		return false
	}
	start, slots, _ := l.windowState(r.H)
	display := l.displayLines()
	y := m.Y - r.Y
	if y < 0 || y >= slots || start+y >= len(display) {
		return true
	}
	line := display[start+y]
	if line.blank || line.row < 0 || l.Rows[line.row].Header {
		return true
	}
	if line.row == l.Selected {
		if l.OnChoose != nil {
			l.OnChoose(l.Rows[line.row])
		}
		return true
	}
	l.Selected = line.row
	l.MarkDirty()
	if l.Palette && l.OnChoose != nil {
		l.OnChoose(l.Rows[line.row])
	}
	return true
}

// FocusTarget is nil: DialogSelect owns the dialog focus stop.
func (l *List) FocusTarget() *FocusTarget { return nil }
