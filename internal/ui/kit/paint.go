package kit

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// fillRect paints a background over a rectangle of the canvas. Every painted
// surface starts here so trailing-space trimming can never expose the previous
// layer's cells.
// OpenTUI counterpart: Renderable.backgroundColor fill.
func fillRect(c *lipgloss.Canvas, x, y, w, h int, token tui.Token) {
	if c == nil || w <= 0 || h <= 0 {
		return
	}
	fillRectColor(c, x, y, w, h, token.Color())
}

// fillRectColor is fillRect for callers that already resolved a state color
// (e.g. List selection uses background.action.primary.focused). Widgets still
// choose the color through a semantic token; only the state accessor ran early.
// OpenTUI counterpart: Renderable backgroundColor fill.
func fillRectColor(c *lipgloss.Canvas, x, y, w, h int, col color.Color) {
	if c == nil || w <= 0 || h <= 0 {
		return
	}
	cell := uv.Cell{Content: " ", Width: 1}
	cell.Style.Bg = col
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			c.SetCell(xx, yy, &cell)
		}
	}
}

// drawCells stamps a rendered string into the canvas without erasing the cells
// under transparent whitespace. That distinction is what lets a Text widget
// paint over a Card's raised background instead of punching holes in it.
// OpenTUI counterpart: renderable paint order; OpenCode: layered <box> nesting.
func drawCells(c *lipgloss.Canvas, s string, x, y, w, h int) {
	if c == nil || s == "" || w <= 0 || h <= 0 {
		return
	}
	// Parse through a scratch canvas: StyledString.Lines in this ultraviolet
	// version stops after the first sequence when built without bounds, while
	// StyledString.Draw honors the explicit area.
	tmp := lipgloss.NewCanvas(w, h)
	uv.NewStyledString(s).Draw(tmp, uv.Rect(0, 0, w, h))
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			cell := tmp.CellAt(col, row)
			if cell == nil || (cell.Content == "" && cell.Width == 0) {
				continue // empty buffer cell or wide-grapheme placeholder
			}
			if cell.Content == " " && cell.Style.IsZero() {
				continue // transparent: keep the parent surface
			}
			out := *cell
			// Keep the parent background behind glyphs that only set a
			// foreground (Text over a Card/Pane surface).
			if out.Style.Bg == nil {
				if under := c.CellAt(x+col, y+row); under != nil {
					out.Style.Bg = under.Style.Bg
				}
			}
			c.SetCell(x+col, y+row, &out)
		}
	}
}

// drawWidget renders a child into its absolute rect. Origin is the parent
// canvas origin, so nested boxes can share placement math.
// OpenTUI counterpart: Renderable.render into the parent buffer.
func drawWidget(c *lipgloss.Canvas, w Widget, abs, origin Rect) {
	if w == nil || abs.Empty() {
		return
	}
	out := w.Render(abs.W, abs.H)
	drawCells(c, out, abs.X-origin.X, abs.Y-origin.Y, abs.W, abs.H)
}

// PaintWidgets paints visible widgets in order at their laid-out screen bounds.
func PaintWidgets(c *lipgloss.Canvas, widgets ...Widget) {
	for _, w := range widgets {
		if w != nil && WidgetVisible(w) {
			drawWidget(c, w, WidgetBounds(w), Rect{})
		}
	}
}

// drawText paints a styled one-line label at a fixed cell.
// OpenTUI counterpart: fg/bg on a text renderable.
func drawText(c *lipgloss.Canvas, x, y, w int, s string, fg color.Color) {
	if c == nil || w <= 0 || s == "" {
		return
	}
	styled := lipgloss.NewStyle().Foreground(fg).Render(ansi.Truncate(s, w, ""))
	drawCells(c, styled, x, y, w, 1)
}

// putGlyph writes one terminal cell with a foreground color.
// OpenTUI counterpart: border glyph painting.
func putGlyph(c *lipgloss.Canvas, x, y int, glyph string, fg color.Color) {
	if c == nil || glyph == "" {
		return
	}
	cell := uv.NewCell(ansi.GraphemeWidth, glyph)
	cell.Style.Fg = fg
	c.SetCell(x, y, cell)
}

// PaintBlock composites a rendered block over a solid token background. Unlike
// wrapping the block in a background style, this fills every cell first and
// then stamps the block with drawCells, so foreground-only glyphs keep the
// surface color behind them and embedded style resets cannot punch holes.
// Inline palettes use it to share the composer's raised surface.
// OpenTUI counterpart: a parent renderable background behind child text.
func PaintBlock(s string, w, h int, token tui.Token) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	cv := lipgloss.NewCanvas(w, h)
	fillRect(cv, 0, 0, w, h, token)
	drawCells(cv, s, 0, 0, w, h)
	return padCanvas(cv, w, h)
}

// fitBlock makes an arbitrary rendered string exactly w by h cells: each line
// is truncated to w and padded with spaces, missing lines are added, and extra
// lines are dropped. ANSI styles are preserved by ansi.Truncate.
// OpenTUI counterpart: the fixed character frame a renderable owns.
func fitBlock(s string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i := range lines {
		lw := ansi.StringWidth(lines[i])
		if lw > w {
			lines[i] = ansi.Truncate(lines[i], w, "")
			lw = ansi.StringWidth(lines[i])
		}
		if lw < w {
			lines[i] += strings.Repeat(" ", w-lw)
		}
	}
	return strings.Join(lines, "\n")
}

// padCanvas renders a canvas and repairs the two things uv.TrimSpace removes:
// trailing spaces and blank tail lines. Root and every container use it so
// their output is always exactly w by h (GOTUI §3.3 base-layer rule).
// OpenTUI counterpart: the explicit full-screen frame buffer.
func padCanvas(c *lipgloss.Canvas, w, h int) string {
	if c == nil {
		return fitBlock("", w, h)
	}
	return fitBlock(c.Render(), w, h)
}

// fillCells paints a 1-row run with a solid background. Footer buttons use it
// for the 1-cell hover pad; it never touches an already-painted glyph.
// OpenTUI counterpart: a row background fill behind a <text>.
func fillCells(c *lipgloss.Canvas, x, y, w int, col color.Color) {
	fillRectColor(c, x, y, w, 1, col)
}

// drawStyled paints one styled, width-clipped line at (x,y).
// OpenTUI counterpart: <text fg/bold> at an absolute cell.
func drawStyled(c *lipgloss.Canvas, x, y, w int, s string, style lipgloss.Style) {
	if c == nil || w <= 0 || s == "" {
		return
	}
	drawCells(c, style.Render(ansi.Truncate(s, w, "…")), x, y, w, 1)
}
