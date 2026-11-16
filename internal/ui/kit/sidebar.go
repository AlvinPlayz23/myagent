package kit

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// StatRow is one sidebar metadata line: a muted label and a right-aligned
// value. Alert paints the value in the error feedback role.
type StatRow struct {
	Label string
	Value string
	Alert bool
}

// SidebarOpts configures the usage rail.
type SidebarOpts struct {
	ID    string
	Title string
	Rows  []StatRow
}

// Sidebar is the right-hand usage/context rail. It is a plain metadata column:
// a hairline left edge, a BOLD title, and `label … value` rows (§3.0.B's
// `·`-separated metadata grammar, stacked vertically). It owns no data source;
// the shell maps host stats into Rows.
//
// OpenTUI counterpart: a fixed-width box column. OpenCode counterpart:
// component/sidebar.tsx (usage/context panel).
type Sidebar struct {
	WidgetBase

	Title string
	Rows  []StatRow
	cache RenderCache
}

// NewSidebar builds a rail with the given stats.
func NewSidebar(opts SidebarOpts) *Sidebar {
	return &Sidebar{
		WidgetBase: NewWidgetBase(opts.ID),
		Title:      opts.Title,
		Rows:       append([]StatRow(nil), opts.Rows...),
	}
}

// SetStats replaces the title and rows idempotently.
func (s *Sidebar) SetStats(title string, rows []StatRow) {
	if s.Title == title && sameStats(s.Rows, rows) {
		return
	}
	s.Title = title
	s.Rows = append(s.Rows[:0], rows...)
	s.MarkDirty()
}

// sameStats compares two stat sets.
func sameStats(a, b []StatRow) bool {
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

// Len is the number of rows including the optional title row.
func (s *Sidebar) Len() int {
	n := len(s.Rows)
	if s.Title != "" {
		n++
	}
	return n
}

// Layout records the rail rect.
func (s *Sidebar) Layout(r Rect) { s.SetBounds(r) }

// Render paints the title and rows, clipping to the assigned height.
func (s *Sidebar) Render(w, h int) string {
	if !s.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return s.cache.Render(w, h, subtreeRev(s), func() string {
		th := DefaultTheme()
		cv := lipgloss.NewCanvas(w, h)
		for y := 0; y < h; y++ {
			putGlyph(cv, 0, y, "│", th.Border.Base.Color())
		}
		y := 0
		if s.Title != "" && y < h {
			drawStyled(cv, 2, y, max(0, w-3), s.Title,
				lipgloss.NewStyle().Bold(true).Foreground(th.Text.Strong.Color()))
			y++
		}
		for _, row := range s.Rows {
			if y >= h {
				break
			}
			labelW := ansi.StringWidth(row.Label)
			valueW := ansi.StringWidth(row.Value)
			if valueW > 0 && labelW+valueW+3 <= w {
				drawStyled(cv, 2, y, labelW, row.Label,
					lipgloss.NewStyle().Foreground(th.Text.Muted.Color()))
				fg := th.Text.Base.Color()
				if row.Alert {
					fg = th.Text.Feedback.Error.Base.Color()
				}
				drawStyled(cv, w-valueW-1, y, valueW, row.Value,
					lipgloss.NewStyle().Foreground(fg))
			} else {
				line := row.Label
				if row.Value != "" {
					line += " " + row.Value
				}
				drawStyled(cv, 2, y, max(0, w-3), line,
					lipgloss.NewStyle().Foreground(th.Text.Muted.Color()))
			}
			y++
		}
		return padCanvas(cv, w, h)
	})
}

// Key is a no-op: the rail is read-only.
func (s *Sidebar) Key(tea.KeyPressMsg) bool { return false }

// Mouse is a no-op: stat rows are display-only.
func (s *Sidebar) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil.
func (s *Sidebar) FocusTarget() *FocusTarget { return nil }
