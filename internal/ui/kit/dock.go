package kit

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// DockOpts configures the composer-adjacent dock.
type DockOpts struct {
	ID string
}

// DockRow is one passive dock line: an accent label chip, body text, and an
// optional right-aligned muted hint.
type DockRow struct {
	Label string
	Text  string
	Hint  string
}

// Dock is the bottom-stack row group that sits above the composer (§3.0.D:
// queued-prompt dock → composer). It uses the card idiom: a left-only heavy
// border over a raised surface. Rows are painted, never clicked.
//
// OpenTUI counterpart: the queued-prompt dock box. OpenCode counterpart: the
// queued message rows above the prompt.
type Dock struct {
	WidgetBase

	Rows []DockRow

	cache RenderCache
}

// NewDock builds an empty dock. An empty dock has zero height and paints
// nothing, so S3/S4 frames stay identical.
func NewDock(opts DockOpts) *Dock {
	return &Dock{WidgetBase: NewWidgetBase(opts.ID)}
}

// SetRows replaces the dock contents idempotently.
func (d *Dock) SetRows(rows []DockRow) {
	if dockRowsEqual(d.Rows, rows) {
		return
	}
	d.Rows = append(d.Rows[:0], rows...)
	d.MarkDirty()
}

// Any reports whether the dock has rows to paint.
func (d *Dock) Any() bool { return len(d.Rows) > 0 }

// Height returns the row count the shell should reserve.
func (d *Dock) Height() int { return len(d.Rows) }

// Layout records the assigned band.
func (d *Dock) Layout(r Rect) { d.SetBounds(r) }

// Render paints one row per dock entry, clipped to the assigned cell block.
func (d *Dock) Render(w, h int) string {
	if !d.Visible || w <= 0 || h <= 0 || len(d.Rows) == 0 {
		return ""
	}
	return d.cache.Render(w, h, d.Rev(), func() string {
		lines := make([]string, 0, h)
		for i := 0; i < h && i < len(d.Rows); i++ {
			lines = append(lines, d.renderRow(d.Rows[i], w))
		}
		return fitBlock(strings.Join(lines, "\n"), w, h)
	})
}

// renderRow paints one dock line: ┃ gutter, accent label chip, base text, and
// a muted right-aligned hint.
func (d *Dock) renderRow(row DockRow, w int) string {
	th := DefaultTheme()
	cv := lipgloss.NewCanvas(w, 1)
	fillCells(cv, 0, 0, w, th.Background.Raised.Base.Color())
	putGlyph(cv, 0, 0, "┃", th.Border.Base.Color())

	x := 2
	if label := strings.TrimSpace(row.Label); label != "" {
		chip := " " + label + " "
		cw := ansi.StringWidth(chip)
		if x+cw < w {
			fillCells(cv, x, 0, cw, th.Background.Raised.High.Color())
			drawStyled(cv, x, 0, cw, chip, lipgloss.NewStyle().
				Bold(true).Foreground(th.Text.Action.Primary.Color()))
			x += cw + 2
		}
	}

	right := w - 1
	if hint := strings.TrimSpace(row.Hint); hint != "" {
		hw := ansi.StringWidth(hint)
		if hw > 0 && right-hw > x+1 {
			drawStyled(cv, right-hw, 0, hw, hint,
				lipgloss.NewStyle().Foreground(th.Text.Muted.Color()))
			right -= hw + 2
		}
	}

	if text := strings.TrimSpace(row.Text); text != "" && right > x {
		drawStyled(cv, x, 0, right-x, ansi.Truncate(text, right-x, "…"),
			lipgloss.NewStyle().Foreground(th.Text.Base.Color()))
	}
	return padCanvas(cv, w, 1)
}

// Key is a no-op: dock rows are passive.
func (d *Dock) Key(tea.KeyPressMsg) bool { return false }

// Mouse is a no-op: dock rows are passive.
func (d *Dock) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil: the dock is not focusable.
func (d *Dock) FocusTarget() *FocusTarget { return nil }

// dockRowsEqual compares the paint-relevant fields.
func dockRowsEqual(a, b []DockRow) bool {
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
