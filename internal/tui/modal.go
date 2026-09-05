package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// modalBounds computes a centered, cleared rectangle with enough room for the
// title, body, and one shortcut footer row. It degrades to the full terminal
// width/height instead of producing negative geometry on short terminals.
func modalBounds(width, height, contentWidth, contentHeight int) tuiRect {
	if width <= 0 || height <= 0 {
		return tuiRect{}
	}
	w := min(width, max(20, contentWidth+4))
	h := min(height, max(3, contentHeight+4))
	return tuiRect{
		X:      max(0, (width-w)/2),
		Y:      max(0, (height-h)/2),
		Width:  w,
		Height: h,
	}
}

func renderModal(title, body, footer string, width, height int, th *theme) string {
	_, frame := renderModalFrame(title, body, footer, width, height, th)
	return frame
}

func renderModalFrame(title, body, footer string, width, height int, th *theme) (tuiRect, string) {
	trimmedBody := strings.TrimRight(body, "\n")
	var bodyLines []string
	if trimmedBody != "" {
		bodyLines = strings.Split(trimmedBody, "\n")
	}
	contentWidth := max(1, visibleMaxWidth(bodyLines))
	contentWidth = max(contentWidth, max(ansi.StringWidth(footer), ansi.StringWidth(title)+2))
	contentHeight := len(bodyLines)
	if footer != "" {
		contentHeight++
	}
	r := modalBounds(width, height, contentWidth, contentHeight)
	if r.empty() {
		return tuiRect{}, ""
	}
	innerWidth := max(1, r.Width-4)
	lines := make([]string, 0, r.Height)
	border := th.modalBorder.Render(strings.Repeat("─", max(1, r.Width-2)))
	caption := title
	if caption != "" && r.Width >= 4 {
		caption = " " + truncateColumns(caption, max(1, r.Width-6)) + " "
		captionWidth := ansi.StringWidth(caption)
		border = th.modalBorder.Render("─") + th.modalTitle.Render(caption) + th.modalBorder.Render(strings.Repeat("─", max(0, r.Width-3-captionWidth)))
	}
	if r.Height == 1 {
		lines = append(lines, border)
	} else {
		// Reserve the footer and bottom border before emitting body rows. A
		// short terminal may clip the picker, but it must never lose the frame
		// boundary or paint into the rows below it.
		lines = append(lines, border)
		bodyRows := r.Height - 2
		if footer != "" && bodyRows > 0 {
			bodyRows--
		}
		for i := 0; i < bodyRows; i++ {
			line := ""
			if i < len(bodyLines) {
				line = "  " + truncateColumns(bodyLines[i], innerWidth)
			}
			lines = append(lines, line)
		}
		if footer != "" && len(lines) < r.Height-1 {
			lines = append(lines, "  "+truncateColumns(footer, innerWidth))
		}
		for len(lines) < r.Height-1 {
			lines = append(lines, "")
		}
		lines = append(lines, th.modalBorder.Render(strings.Repeat("─", max(1, r.Width-2))))
	}
	if len(lines) > r.Height {
		lines = lines[:r.Height]
	}
	for i, line := range lines {
		lines[i] = fitModalLine(line, r.Width)
	}
	return r, strings.Join(lines, "\n")
}

func fitModalLine(line string, width int) string {
	line = truncateColumns(line, width)
	if padding := width - ansi.StringWidth(line); padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}

// overlayModal clears the modal rectangle in the already-rendered frame and
// then paints the frame over it. Replacing complete terminal rows gives the
// string-based Bubble Tea renderer the same no-bleed behavior as ratatui's
// Clear widget.
func overlayModal(base string, width, height int, bounds tuiRect, frame string) string {
	if bounds.empty() || width <= 0 || height <= 0 {
		return base
	}
	rows := strings.Split(strings.TrimSuffix(base, "\n"), "\n")
	if base == "" {
		rows = nil
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	for i := range rows {
		rows[i] = fitModalLine(rows[i], width)
	}
	frameRows := strings.Split(strings.TrimSuffix(frame, "\n"), "\n")
	for row, frameLine := range frameRows {
		y := bounds.Y + row
		if y < 0 || y >= height {
			continue
		}
		frameLine = fitModalLine(frameLine, bounds.Width)
		prefix := strings.Repeat(" ", bounds.X)
		visibleWidth := ansi.StringWidth(frameLine)
		suffix := max(0, width-bounds.X-visibleWidth)
		rows[y] = prefix + frameLine + strings.Repeat(" ", suffix)
	}
	return strings.Join(rows, "\n")
}

func visibleMaxWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		width = max(width, ansi.StringWidth(line))
	}
	return width
}
