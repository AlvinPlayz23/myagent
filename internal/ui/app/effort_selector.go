package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/AlvinPlayz23/myagent/internal/ui/kit"
)

type effortChoiceHit struct {
	rect kit.Rect
	id   string
}

// effortSelector is a compact horizontal control attached to the composer.
type effortSelector struct {
	kit.WidgetBase
	shell    *Shell
	items    []tui.KitPickerItem
	selected string
	hits     []effortChoiceHit
}

func newEffortSelector(s *Shell) *effortSelector {
	return &effortSelector{WidgetBase: kit.NewWidgetBase("kit.effort.selector"), shell: s}
}

func (e *effortSelector) setState(state tui.KitPickerState) {
	e.items = append(e.items[:0], state.Items...)
	e.selected = state.SelectedID
	e.MarkDirty()
}

func (e *effortSelector) Layout(r kit.Rect) {
	e.SetBounds(r)
	e.hits = e.hits[:0]
	if r.Empty() || !e.Visible || len(e.items) == 0 || r.W < 3 {
		return
	}
	selected := 0
	for i, item := range e.items {
		if item.ID == e.selected {
			selected = i
			break
		}
	}
	start := 0
	available := max(0, r.W-2)
	if ansi.StringWidth(e.items[selected].Title)+2 > available {
		e.hits = append(e.hits, effortChoiceHit{rect: kit.Rect{X: r.X + 1, Y: r.Y, W: available, H: 1}, id: e.items[selected].ID})
		return
	}
	width := 0
	for i := 0; i <= selected; i++ {
		width += ansi.StringWidth(e.items[i].Title) + 2
	}
	for start < selected && width > available {
		width -= ansi.StringWidth(e.items[start].Title) + 2
		start++
	}
	x := r.X + 1
	for _, item := range e.items[start:] {
		w := ansi.StringWidth(item.Title) + 2
		if x+w > r.X+r.W-1 {
			break
		}
		e.hits = append(e.hits, effortChoiceHit{rect: kit.Rect{X: x, Y: r.Y, W: w, H: 1}, id: item.ID})
		x += w
	}
}

func (e *effortSelector) Render(w, h int) string {
	if !e.Visible || w <= 0 || h <= 0 {
		return ""
	}
	th := kit.DefaultTheme()
	line := strings.Builder{}
	x := 0
	if len(e.hits) > 0 && e.hits[0].id != e.items[0].ID {
		line.WriteString(lipgloss.NewStyle().Foreground(th.Text.Muted.Color()).Render("‹"))
		x = 1
	}
	for _, hit := range e.hits {
		localX := hit.rect.X - e.Bounds().X
		if localX > x {
			line.WriteString(strings.Repeat(" ", localX-x))
		}
		var item tui.KitPickerItem
		for _, candidate := range e.items {
			if candidate.ID == hit.id {
				item = candidate
				break
			}
		}
		label := " " + ansi.Truncate(item.Title, max(0, hit.rect.W-2), "…") + " "
		if ansi.StringWidth(label) > hit.rect.W {
			label = ansi.Truncate(item.Title, hit.rect.W, "…")
		}
		label += strings.Repeat(" ", max(0, hit.rect.W-ansi.StringWidth(label)))
		style := lipgloss.NewStyle().Foreground(th.Text.Muted.Color())
		if hit.id == e.selected {
			style = style.Foreground(th.Text.Selection.Color()).Background(th.Background.Selection.Color()).Bold(true)
		}
		line.WriteString(style.Render(label))
		x = localX + hit.rect.W
	}
	if len(e.hits) > 0 && e.hits[len(e.hits)-1].id != e.items[len(e.items)-1].ID && x < w {
		line.WriteString(strings.Repeat(" ", w-x-1))
		line.WriteString(lipgloss.NewStyle().Foreground(th.Text.Muted.Color()).Render("›"))
	}
	lines := []string{line.String()}
	if h > 1 {
		for _, item := range e.items {
			if item.ID == e.selected {
				lines = append(lines, " "+lipgloss.NewStyle().Foreground(th.Text.Muted.Color()).Render(ansi.Truncate(item.Desc, max(0, w-2), "…")))
				break
			}
		}
	}
	if h > 2 {
		hint := " ←/→ change  enter save  esc cancel"
		lines = append(lines, lipgloss.NewStyle().Foreground(th.Text.Faint.Color()).Render(ansi.Truncate(hint, w, "")))
	}
	return kit.PaintBlock(strings.Join(lines, "\n"), w, h, th.Background.Raised.Base)
}

func (e *effortSelector) Mouse(ev tea.MouseMsg, _ kit.Rect) bool {
	click, ok := ev.(tea.MouseClickMsg)
	if !ok || click.Button != tea.MouseLeft {
		return false
	}
	for _, hit := range e.hits {
		if hit.rect.Contains(click.X, click.Y) {
			e.shell.emit(e.shell.host.KitPickerAccept(hit.id))
			return true
		}
	}
	return false
}

func (e *effortSelector) Key(tea.KeyPressMsg) bool      { return false }
func (e *effortSelector) FocusTarget() *kit.FocusTarget { return nil }
