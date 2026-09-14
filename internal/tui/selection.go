package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// textPoint identifies a terminal cell in the fully rendered transcript.
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

// renderTextSelection overlays a selection style without disturbing text
// outside the selected cells. Its coordinates refer to ANSI-stripped rows.
func renderTextSelection(content string, selection *textSelection, style lipgloss.Style) string {
	if selection == nil || !selection.dragged {
		return content
	}
	start, end := normalizeSelection(*selection)
	lines := strings.Split(content, "\n")
	if start.row < 0 || start.row >= len(lines) {
		return content
	}
	end.row = min(end.row, len(lines)-1)
	for row := start.row; row <= end.row; row++ {
		plainWidth := ansi.StringWidth(lines[row])
		from, to := 0, plainWidth
		if row == start.row {
			from = min(max(0, start.col), plainWidth)
		}
		if row == end.row {
			to = min(max(0, end.col+1), plainWidth)
		}
		if to > from {
			lines[row] = lipgloss.StyleRanges(lines[row], lipgloss.NewRange(from, to, style))
		}
	}
	return strings.Join(lines, "\n")
}

// isWordRune reports whether r belongs in a double-click word selection:
// identifiers, paths, and URLs select as one unit.
func isWordRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	switch r {
	case '_', '-', '.', '/', '@', ':', '~', '+', '%', '#':
		return true
	}
	return false
}

// expandWord grows a press at (row, col) to its word boundaries on plain
// (ANSI-stripped) lines. col is a display cell; wide cells map to their
// owning rune. A press on whitespace selects nothing (anchor == current).
func expandWord(lines []string, row, col int) (textPoint, textPoint) {
	if row < 0 || row >= len(lines) {
		return textPoint{row: row, col: col}, textPoint{row: row, col: col}
	}
	runes := []rune(lines[row])
	if len(runes) == 0 {
		return textPoint{row: row, col: 0}, textPoint{row: row, col: 0}
	}
	// Map the display cell to a rune index.
	ri, cell := 0, 0
	for ri < len(runes) && cell+ansi.StringWidth(string(runes[ri])) <= col {
		cell += ansi.StringWidth(string(runes[ri]))
		ri++
	}
	if ri >= len(runes) {
		ri = len(runes) - 1
	}
	if !isWordRune(runes[ri]) {
		return textPoint{row: row, col: col}, textPoint{row: row, col: col}
	}
	start, end := ri, ri
	for start > 0 && isWordRune(runes[start-1]) {
		start--
	}
	for end+1 < len(runes) && isWordRune(runes[end+1]) {
		end++
	}
	// Selection end is inclusive; convert rune bounds back to cells.
	startCell := 0
	for _, r := range runes[:start] {
		startCell += ansi.StringWidth(string(r))
	}
	endCell := startCell
	for _, r := range runes[start : end+1] {
		endCell += ansi.StringWidth(string(r))
	}
	return textPoint{row: row, col: startCell}, textPoint{row: row, col: endCell - 1}
}

// expandParagraph grows a press to the blank-line-delimited paragraph
// (triple-click) on plain lines. Blank rows select nothing.
func expandParagraph(lines []string, row int) (textPoint, textPoint) {
	if row < 0 || row >= len(lines) || strings.TrimSpace(lines[row]) == "" {
		return textPoint{row: row, col: 0}, textPoint{row: row, col: 0}
	}
	start, end := row, row
	for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	for end+1 < len(lines) && strings.TrimSpace(lines[end+1]) != "" {
		end++
	}
	lastWidth := ansi.StringWidth(lines[end])
	return textPoint{row: start, col: 0}, textPoint{row: end, col: max(0, lastWidth-1)}
}

// selectedRenderedText extracts exactly what the user selected visually,
// excluding ANSI styling while retaining displayed line breaks and wrapping.
func selectedRenderedText(content string, selection textSelection) string {
	if !selection.dragged {
		return ""
	}
	start, end := normalizeSelection(selection)
	lines := strings.Split(ansi.Strip(content), "\n")
	if start.row < 0 || start.row >= len(lines) {
		return ""
	}
	end.row = min(end.row, len(lines)-1)
	selected := make([]string, 0, end.row-start.row+1)
	for row := start.row; row <= end.row; row++ {
		width := ansi.StringWidth(lines[row])
		from, to := 0, width
		if row == start.row {
			from = min(max(0, start.col), width)
		}
		if row == end.row {
			to = min(max(0, end.col+1), width)
		}
		if to < from {
			to = from
		}
		selected = append(selected, ansi.Cut(lines[row], from, to))
	}
	return strings.Join(selected, "\n")
}
