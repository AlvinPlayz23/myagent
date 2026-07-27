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
