package tui

import (
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
)

// theme holds the lipgloss styles for the UI. Colors mirror pi's token roles
// (userMessageBg, toolPending/Success/ErrorBg, muted, accent, error) at a
// coarse level; we keep a small palette rather than pi's ~50 tokens.
type theme struct {
	userBlock    lipgloss.Style
	assistantTxt lipgloss.Style
	toolPending  lipgloss.Style
	toolSuccess  lipgloss.Style
	toolError    lipgloss.Style
	toolTitle    lipgloss.Style
	muted        lipgloss.Style
	accent       lipgloss.Style
	errorText    lipgloss.Style
	footer       lipgloss.Style
	footerRight  lipgloss.Style
	spinner      lipgloss.Style
}

func newTheme() *theme {
	return &theme{
		userBlock:    lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255")).Padding(0, 1),
		assistantTxt: lipgloss.NewStyle(),
		toolPending:  lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		toolSuccess:  lipgloss.NewStyle().Foreground(lipgloss.Color("35")),
		toolError:    lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		toolTitle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		muted:        lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		accent:       lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		errorText:    lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		footer:       lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		footerRight:  lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		spinner:      lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
	}
}

// mdRenderer caches a glamour renderer per word-wrap width. glamour is not
// reactive, so we rebuild (and cache) a renderer whenever the width changes.
type mdRenderer struct {
	mu    sync.Mutex
	width int
	r     *glamour.TermRenderer
}

func newMDRenderer() *mdRenderer { return &mdRenderer{} }

// render renders markdown to ANSI wrapped at width. A width <= 0 falls back to
// returning the raw markdown so we never panic on an unsized terminal.
func (m *mdRenderer) render(md string, width int) string {
	if width <= 0 {
		return md
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.r == nil || m.width != width {
		r, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return md
		}
		m.r = r
		m.width = width
	}
	out, err := m.r.Render(md)
	if err != nil {
		return md
	}
	return out
}
