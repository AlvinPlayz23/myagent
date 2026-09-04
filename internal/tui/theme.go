package tui

import (
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
)

// theme holds the semantic style roles for the UI. The palette is GrokNight
// (a neutral grayscale ramp with TokyoNight accents); roles are named for
// their meaning, not their color, so the palette can change (or degrade)
// without touching render code. Bubble Tea's renderer downsamples colors to
// the terminal's profile automatically; state is additionally encoded in
// glyphs and markers so monochrome terminals stay legible.
type theme struct {
	userPrefix  lipgloss.Style
	userText    lipgloss.Style
	queuedLabel lipgloss.Style

	assistantTxt lipgloss.Style
	toolPending  lipgloss.Style
	toolSuccess  lipgloss.Style
	toolError    lipgloss.Style
	toolTitle    lipgloss.Style
	toolCommand  lipgloss.Style

	diffMeta   lipgloss.Style
	diffHunk   lipgloss.Style
	diffAdd    lipgloss.Style
	diffRemove lipgloss.Style

	muted       lipgloss.Style
	accent      lipgloss.Style
	errorText   lipgloss.Style
	warning     lipgloss.Style
	border      lipgloss.Style
	borderOn    lipgloss.Style
	footer      lipgloss.Style
	footerRight lipgloss.Style
	spinner     lipgloss.Style

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
	// GrokNight: grayscale ramp anchored at #141414/#e1e1e1 with TokyoNight
	// Night accents. Hex values let lipgloss/Bubble Tea downsample to whatever
	// the terminal actually supports.
	const (
		textPrimary   = "#e1e1e1"
		textSecondary = "#c8c8c8"
		grayBright    = "#787878"
		gray          = "#6c6c6c"
		grayDim       = "#414141"
		blue          = "#7aa2f7"
		cyan          = "#7dcfff"
		green         = "#9ece6a"
		magenta       = "#bb9af7"
		orange        = "#ff9e64"
		red           = "#f7768e"
		yellow        = "#e0af68"
		borderDim     = "#323237"
		borderLit     = "#505058"
		insertBg      = "#063806"
		deleteBg      = "#420e14"
	)
	return &theme{
		userPrefix:  lipgloss.NewStyle().Foreground(lipgloss.Color(textSecondary)),
		userText:    lipgloss.NewStyle().Foreground(lipgloss.Color(textPrimary)),
		queuedLabel: lipgloss.NewStyle().Foreground(lipgloss.Color(cyan)),

		assistantTxt: lipgloss.NewStyle().Foreground(lipgloss.Color(textPrimary)),
		toolPending:  lipgloss.NewStyle().Foreground(lipgloss.Color(cyan)),
		toolSuccess:  lipgloss.NewStyle().Foreground(lipgloss.Color(green)),
		toolError:    lipgloss.NewStyle().Foreground(lipgloss.Color(red)),
		toolTitle:    lipgloss.NewStyle().Foreground(lipgloss.Color(grayBright)),
		toolCommand:  lipgloss.NewStyle().Foreground(lipgloss.Color(yellow)),

		diffMeta:   lipgloss.NewStyle().Foreground(lipgloss.Color(gray)),
		diffHunk:   lipgloss.NewStyle().Foreground(lipgloss.Color(blue)),
		diffAdd:    lipgloss.NewStyle().Foreground(lipgloss.Color(green)).Background(lipgloss.Color(insertBg)),
		diffRemove: lipgloss.NewStyle().Foreground(lipgloss.Color(red)).Background(lipgloss.Color(deleteBg)),

		muted:       lipgloss.NewStyle().Foreground(lipgloss.Color(gray)),
		accent:      lipgloss.NewStyle().Foreground(lipgloss.Color(blue)),
		errorText:   lipgloss.NewStyle().Foreground(lipgloss.Color(red)),
		warning:     lipgloss.NewStyle().Foreground(lipgloss.Color(yellow)),
		border:      lipgloss.NewStyle().Foreground(lipgloss.Color(borderDim)),
		borderOn:    lipgloss.NewStyle().Foreground(lipgloss.Color(borderLit)),
		footer:      lipgloss.NewStyle().Foreground(lipgloss.Color(gray)),
		footerRight: lipgloss.NewStyle().Foreground(lipgloss.Color(grayDim)),
		spinner:     lipgloss.NewStyle().Foreground(lipgloss.Color(cyan)),
		// Reverse video keeps selections visible even on terminals without
		// color support; the blue is TokyoNight's dim selection blue.
		selection:     lipgloss.NewStyle().Foreground(lipgloss.Color(textPrimary)).Background(lipgloss.Color("#3d59a1")).Reverse(true),
		cmdPickerSel:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(blue)),
		cmdPickerItem: lipgloss.NewStyle().Foreground(lipgloss.Color(textSecondary)),
		// Group headers are bold but stay neutral so the blue selection still
		// reads as the cursor rather than competing with them.
		pickerGroup:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(textSecondary)),
		composerRule: lipgloss.NewStyle().Foreground(lipgloss.Color(borderDim)),
		orbDim:       lipgloss.NewStyle().Foreground(lipgloss.Color(grayDim)),
		orbMedium:    lipgloss.NewStyle().Foreground(lipgloss.Color(gray)),
		// The brand color is TokyoNight violet, shared with assistant/thinking.
		orbBright: lipgloss.NewStyle().Foreground(lipgloss.Color(magenta)).Bold(true),
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
