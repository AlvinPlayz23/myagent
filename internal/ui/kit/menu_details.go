package kit

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const MenuDetailsHeight = 4

// RenderMenuDetails keeps only the highlighted item's help below the list.
func RenderMenuDetails(description string, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	if description == "" {
		return fitBlock("", w, h)
	}
	th := DefaultTheme()
	if h == 1 {
		line, _, _ := strings.Cut(description, "\n")
		line = ansi.Truncate(line, w, "…")
		return fitBlock(lipgloss.NewStyle().Foreground(th.Text.Muted.Color()).Render(line), w, h)
	}
	rule := lipgloss.NewStyle().Foreground(th.Border.Base.Color()).Render(strings.Repeat("─", w))
	lines := strings.Split(ansi.Wrap(description, w, ""), "\n")
	if len(lines) > h-1 {
		lines = lines[:h-1]
		if len(lines) > 0 {
			lines[len(lines)-1] = ansi.Truncate(lines[len(lines)-1], max(0, w-1), "") + "…"
		}
	}
	help := lipgloss.NewStyle().Foreground(th.Text.Muted.Color()).Render(strings.Join(lines, "\n"))
	return fitBlock(rule+"\n"+help, w, h)
}
