package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/AlvinPlayz23/myagent/internal/ui/kit"
)

// maxCommandMenuRows matches OpenCode's slash menu: a tall list that covers
// most of the transcript instead of a five-row peek.
const maxCommandMenuRows = 10

// commandMenu is the inline slash menu: command rows above the composer, with
// the composer itself acting as the filter input. It stays host-driven, so
// typing, arrows, tab, enter, and esc keep running through the legacy onKey
// path; the shell only paints rows and forwards clicks.
type commandMenu struct {
	kit.WidgetBase

	list *kit.List
}

func newCommandMenu(s *Shell) *commandMenu {
	m := &commandMenu{
		WidgetBase: kit.NewWidgetBase("kit.commands"),
		list: kit.NewList(kit.ListOpts{
			ID:           "kit.commands.rows",
			Palette:      true,
			HideOverflow: true,
		}),
	}
	m.list.OnChoose = func(row kit.ListRow) bool {
		s.emit(s.host.KitPickerAccept(row.ID))
		return true
	}
	return m
}

// Layout reserves the last column for the scrollbar, matching the OpenCode
// palette.
func (m *commandMenu) Layout(r kit.Rect) {
	m.SetBounds(r)
	m.list.Layout(kit.Rect{X: r.X, Y: r.Y, W: max(0, r.W-1), H: r.H})
}

// Render paints the rows and a one-column scrollbar when they overflow.
func (m *commandMenu) Render(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	listW := max(1, w-1)
	lines := strings.Split(m.list.Render(listW, h), "\n")
	if len(lines) < h {
		lines = append(lines, make([]string, h-len(lines))...)
	}
	if len(lines) > h {
		lines = lines[:h]
	}

	thumbStart, thumbSize := m.thumb(h)
	th := kit.DefaultTheme()
	track := lipgloss.NewStyle().Foreground(th.Text.Faint.Color()).Render("│")
	thumb := lipgloss.NewStyle().Foreground(th.Text.Muted.Color()).Render("┃")
	for i := range lines {
		if pad := listW - ansi.StringWidth(lines[i]); pad > 0 {
			lines[i] += strings.Repeat(" ", pad)
		}
		switch {
		case thumbStart < 0:
			lines[i] += " "
		case i >= thumbStart && i < thumbStart+thumbSize:
			lines[i] += thumb
		default:
			lines[i] += track
		}
	}
	// The menu sits on the same raised surface as the composer for every
	// theme; the fill also backs the scrollbar column.
	return kit.PaintBlock(strings.Join(lines, "\n"), w, h, th.Background.Raised.Base)
}

// thumb resolves the scrollbar thumb from the list's visible window.
func (m *commandMenu) thumb(h int) (start, size int) {
	total := len(m.list.Rows)
	if total <= h || h <= 0 {
		return -1, 0
	}
	first, count := m.list.Window(h)
	size = max(1, h*max(1, count)/total)
	if size > h {
		size = h
	}
	maxStart := max(0, h-size)
	lastFirst := max(1, total-count)
	start = min(maxStart, first*maxStart/lastFirst)
	return start, size
}

// Mouse forwards row clicks so first-click selection matches the dialog path.
func (m *commandMenu) Mouse(ev tea.MouseMsg, _ kit.Rect) bool {
	return m.list.Mouse(ev, m.list.Bounds())
}

// Key is a no-op: the composer owns every key while the menu is open.
func (m *commandMenu) Key(tea.KeyPressMsg) bool { return false }

// FocusTarget is nil: the composer keeps focus.
func (m *commandMenu) FocusTarget() *kit.FocusTarget { return nil }

// Children exposes the row list for hit registration.
func (m *commandMenu) Children() []kit.Widget { return []kit.Widget{m.list} }

// syncCommandMenu mirrors the host-filtered command rows onto the inline menu.
func (s *Shell) syncCommandMenu(state tui.KitPickerState) {
	sig := pickerSignature(state)
	if s.menuOpen && sig == s.menuSig {
		return
	}
	rows := make([]kit.ListRow, 0, len(state.Items))
	for _, item := range state.Items {
		rows = append(rows, kit.ListRow{ID: item.ID, Title: item.Title, Desc: item.Desc})
	}
	s.layoutDirty = s.layoutDirty || !s.menuOpen || s.menuRows != len(rows)
	s.menu.list.SetRows(rows)
	s.menuRows = len(rows)
	if state.SelectedID != "" {
		for i, row := range rows {
			if row.ID != state.SelectedID {
				continue
			}
			s.menu.list.Selected = i
			break
		}
	}
	s.menu.list.MarkDirty()
	s.menuSig = sig
	if !s.menuOpen {
		s.menuOpen = true
		s.menu.Show()
	}
}

// closeCommandMenu hides the band and reclaims its rows.
func (s *Shell) closeCommandMenu() {
	if !s.menuOpen {
		return
	}
	s.menuOpen = false
	s.menuRows = 0
	s.menuSig = ""
	s.menu.Hide()
	s.layoutDirty = true
}
