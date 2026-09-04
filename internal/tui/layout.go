package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// rowKind classifies one laid-out transcript row for styling, selection, and
// hit-testing.
type rowKind int

const (
	rowUser rowKind = iota
	rowAssistant
	rowThinking
	rowThinkingHeader
	rowToolHeader
	rowToolOutput
	rowDiff
	rowDiffMeta
	rowError
	rowNotice
	rowSpacer
)

// layoutSpan is one styled run of visible text within a row. text may carry
// ANSI sequences (Markdown output); raw spans render verbatim without a
// lipgloss pass so renderer output is preserved byte-for-byte.
type layoutSpan struct {
	text  string
	style lipgloss.Style
	raw   bool
}

// layoutRow is one terminal row of the transcript. Wrapping is applied while
// rows are built, so viewport line offsets map one-to-one onto row indexes
// and hit-testing is exact. wrapped marks a continuation of the previous
// row's source line: copying joins it without a newline. gutterCols and
// gutterSuffixCols count leading and trailing visual-only cells (padding,
// markers, diff prefixes, fold indicators) that selection highlights over
// but never copies. toggle marks rows whose click toggles the owning block's
// fold.
type layoutRow struct {
	kind             rowKind
	blockID          int
	lineIdx          int
	spans            []layoutSpan
	gutterCols       int
	gutterSuffixCols int
	wrapped          bool
	toggle           bool
}

// selectable reports whether the row participates in mouse selection.
func (r layoutRow) selectable() bool { return r.kind != rowSpacer }

// width is the row's visible width in terminal cells, padding included.
func (r layoutRow) width() int {
	w := 0
	for _, s := range r.spans {
		w += ansi.StringWidth(s.text)
	}
	return w
}

// renderSpan draws one span, honoring raw spans.
func renderSpan(s layoutSpan) string {
	if s.raw {
		return s.text
	}
	return s.style.Render(s.text)
}

// render draws the row's spans in order.
func (r layoutRow) render() string {
	if len(r.spans) == 1 {
		return renderSpan(r.spans[0])
	}
	var sb strings.Builder
	for _, s := range r.spans {
		sb.WriteString(renderSpan(s))
	}
	return sb.String()
}

// wrapRow splits a row wider than limit into a base row plus continuation
// rows, preserving span styles and raw flags. Cuts are display-width and
// grapheme aware, so wide characters and ANSI styling survive intact.
func wrapRow(r layoutRow, limit int) []layoutRow {
	if limit < 1 || r.width() <= limit {
		return []layoutRow{r}
	}
	var rows []layoutRow
	cur := layoutRow{kind: r.kind, blockID: r.blockID, lineIdx: r.lineIdx}
	used := 0
	flush := func() {
		rows = append(rows, cur)
		cur = layoutRow{kind: r.kind, blockID: r.blockID, lineIdx: r.lineIdx, wrapped: true}
		used = 0
	}
	for _, s := range r.spans {
		text := s.text
		for text != "" {
			avail := limit - used
			if avail <= 0 {
				flush()
				continue
			}
			w := ansi.StringWidth(text)
			if w <= avail {
				cur.spans = append(cur.spans, layoutSpan{text: text, style: s.style, raw: s.raw})
				used += w
				break
			}
			head := ansi.Cut(text, 0, avail)
			rest := ansi.Cut(text, avail, w)
			cur.spans = append(cur.spans, layoutSpan{text: head, style: s.style, raw: s.raw})
			used += avail
			if rest == "" {
				break
			}
			text = rest
			flush()
		}
	}
	if len(cur.spans) > 0 || len(rows) == 0 {
		rows = append(rows, cur)
	}
	return rows
}

// wrapRows wraps every row, preserving order.
func wrapRows(rows []layoutRow, limit int) []layoutRow {
	var out []layoutRow
	for _, r := range rows {
		out = append(out, wrapRow(r, limit)...)
	}
	return out
}

// plainRows builds one selectable row per line of text, all styled the same,
// wrapped to limit.
func plainRows(text string, style lipgloss.Style, kind rowKind, blockID int, limit int) []layoutRow {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	rows := make([]layoutRow, 0, len(lines))
	for i, line := range lines {
		rows = append(rows, layoutRow{
			kind:    kind,
			blockID: blockID,
			lineIdx: i,
			spans:   []layoutSpan{{text: line, style: style}},
		})
	}
	return wrapRows(rows, limit)
}

// renderRows joins rendered rows with newlines.
func renderRows(rows []layoutRow) string {
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = r.render()
	}
	return strings.Join(lines, "\n")
}

// renderRowSelection draws one row with the selection style overlaid on the
// half-open [from, to) cell range. Leading gutter cells stay unhighlighted so
// visual-only chrome never appears selected.
func renderRowSelection(r layoutRow, from, to int, selStyle lipgloss.Style) string {
	if to <= from {
		return r.render()
	}
	from = max(from, r.gutterCols)
	to = min(to, r.width()-r.gutterSuffixCols)
	if to <= from {
		return r.render()
	}
	var ranges []lipgloss.Range
	var sb strings.Builder
	off := 0
	for _, s := range r.spans {
		w := ansi.StringWidth(s.text)
		lo, hi := off, off+w
		off = hi
		if lo < to && hi > from {
			ranges = append(ranges, lipgloss.NewRange(max(lo, from), min(hi, to), selStyle))
		}
		sb.WriteString(renderSpan(s))
	}
	line := sb.String()
	if len(ranges) == 0 {
		return line
	}
	return lipgloss.StyleRanges(line, ranges...)
}

// rowSelectedCells extracts the [from, to) cell range of one row as plain
// text, skipping the leading gutter cells. Cuts are display-cell based and
// ANSI is stripped, so the result is copyable text; trailing padding is not
// trimmed here because wrapped rows must keep their boundary spaces.
func rowSelectedCells(r layoutRow, from, to int) string {
	if to <= from {
		return ""
	}
	from = max(from, r.gutterCols)
	to = min(to, r.width()-r.gutterSuffixCols)
	var sb strings.Builder
	off := 0
	for _, s := range r.spans {
		w := ansi.StringWidth(s.text)
		lo, hi := off, off+w
		off = hi
		if hi <= from || lo >= to {
			continue
		}
		sb.WriteString(ansi.Strip(ansi.Cut(s.text, max(0, from-lo), min(w, to-lo))))
	}
	return sb.String()
}

// copyRowsText copies a normalized selection across laid-out rows. Wrapped
// continuations join the previous row directly — keeping the space at the
// wrap boundary — so copied prose reads as it was typed rather than as it
// was wrapped. Each logical line is right-trimmed only at its end.
func copyRowsText(rows []layoutRow, sel textSelection) string {
	if !sel.dragged {
		return ""
	}
	start, end := normalizeSelection(sel)
	if start.row < 0 || start.row >= len(rows) {
		return ""
	}
	end.row = min(end.row, len(rows)-1)
	var parts []string
	var cur strings.Builder
	started := false
	for i := start.row; i <= end.row; i++ {
		r := rows[i]
		if !r.selectable() {
			continue
		}
		from, to := 0, r.width()
		if i == start.row {
			from = max(0, start.col)
		}
		if i == end.row {
			to = min(max(0, end.col+1), r.width())
		}
		text := rowSelectedCells(r, from, to)
		if started && !r.wrapped {
			parts = append(parts, strings.TrimRight(cur.String(), " "))
			cur.Reset()
		}
		started = true
		cur.WriteString(text)
	}
	if started {
		parts = append(parts, strings.TrimRight(cur.String(), " "))
	}
	return strings.TrimRight(strings.Join(parts, "\n"), "\n")
}
