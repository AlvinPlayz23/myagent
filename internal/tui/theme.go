package tui

import (
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
)

// theme holds the semantic style roles for the UI. Roles are named for their
// meaning, not their color, so the palette can change (or degrade) without
// touching render code. Bubble Tea's renderer downsamples colors to the
// terminal's profile automatically; state is additionally encoded in glyphs
// and markers so monochrome terminals stay legible.
type theme struct {
	userBlock     lipgloss.Style
	queuedLabel   lipgloss.Style
	assistantTxt  lipgloss.Style
	toolPending   lipgloss.Style
	toolSuccess   lipgloss.Style
	toolError     lipgloss.Style
	toolTitle     lipgloss.Style
	diffMeta      lipgloss.Style
	diffHunk      lipgloss.Style
	diffAdd       lipgloss.Style
	diffRemove    lipgloss.Style
	muted         lipgloss.Style
	accent        lipgloss.Style
	errorText     lipgloss.Style
	warning       lipgloss.Style
	border        lipgloss.Style
	footer        lipgloss.Style
	footerRight   lipgloss.Style
	spinner       lipgloss.Style
	selection     lipgloss.Style
	cmdPickerSel  lipgloss.Style
	cmdPickerItem lipgloss.Style
	pickerGroup   lipgloss.Style
	composerRule  lipgloss.Style
	orbDim        lipgloss.Style
	orbMedium     lipgloss.Style
	orbBright     lipgloss.Style
}

func newTheme() *theme {
	return &theme{
		userBlock:    lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("255")).Padding(0, 1),
		queuedLabel:  lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		assistantTxt: lipgloss.NewStyle(),
		toolPending:  lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		toolSuccess:  lipgloss.NewStyle().Foreground(lipgloss.Color("35")),
		toolError:    lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		toolTitle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		diffMeta:     lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		diffHunk:     lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		diffAdd:      lipgloss.NewStyle().Foreground(lipgloss.Color("35")),
		diffRemove:   lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		muted:        lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		accent:       lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		errorText:    lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		warning:      lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		border:       lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		footer:       lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		footerRight:  lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		spinner:      lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		// Reverse video keeps selections visible even on terminals without
		// color support.
		selection:     lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("25")).Reverse(true),
		cmdPickerSel:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		cmdPickerItem: lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		// Group headers are bold but stay neutral so the blue selection still
		// reads as the cursor rather than competing with them.
		pickerGroup:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")),
		composerRule: lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		orbDim:       lipgloss.NewStyle().Foreground(lipgloss.Color("24")),
		orbMedium:    lipgloss.NewStyle().Foreground(lipgloss.Color("31")),
		orbBright:    lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true),
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
