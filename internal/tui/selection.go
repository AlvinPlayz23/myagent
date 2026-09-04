package tui

import (
	"charm.land/lipgloss/v2"
)

// textPoint identifies a terminal cell in the laid-out transcript: row is an
// index into the layout rows (which map one-to-one onto viewport lines
// because wrapping is applied when rows are built), col is a display cell.
type textPoint struct {
	row int
	col int
}

type textSelection struct {
	anchor  textPoint
	current textPoint
	dragged bool
}

func normalizeSelection(s textSelection) (textPoint, textPoint) {
	if s.current.row < s.anchor.row || (s.current.row == s.anchor.row && s.current.col < s.anchor.col) {
		return s.current, s.anchor
	}
	return s.anchor, s.current
}

// renderRowsSelection renders every row, overlaying the selection style on
// the selected cell range of each. Gutter chrome (padding, diff prefixes,
// markers) is never highlighted, so what looks selected is what gets copied.
func renderRowsSelection(rows []layoutRow, sel *textSelection, style lipgloss.Style) []string {
	lines := make([]string, len(rows))
	if sel == nil || !sel.dragged {
		for i, r := range rows {
			lines[i] = r.render()
		}
		return lines
	}
	start, end := normalizeSelection(*sel)
	end.row = min(end.row, len(rows)-1)
	for i, r := range rows {
		if i < start.row || i > end.row || !r.selectable() {
			lines[i] = r.render()
			continue
		}
		from, to := 0, r.width()
		if i == start.row {
			from = max(0, start.col)
		}
		if i == end.row {
			to = min(max(0, end.col+1), r.width())
		}
		lines[i] = renderRowSelection(r, from, to, style)
	}
	return lines
}

// selectedRowsText copies the selection as plain text. Wrapped continuation
// rows join their parent row without a newline, and gutter chrome is
// excluded, so copied text reads like the original input.
func selectedRowsText(rows []layoutRow, sel textSelection) string {
	return copyRowsText(rows, sel)
}

// rowsFromLines builds single-span rows from plain lines; a test helper.
func rowsFromLines(lines ...string) []layoutRow {
	rows := make([]layoutRow, len(lines))
	for i, line := range lines {
		rows[i] = layoutRow{
			kind:    rowAssistant,
			blockID: i + 1,
			lineIdx: i,
			spans:   []layoutSpan{{text: line, raw: true}},
		}
	}
	return rows
}
