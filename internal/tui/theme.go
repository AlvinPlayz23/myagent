package tui

import (
	"sync"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
)

type themePalette struct {
	bg, bgDark, bgStorm, highlight                  string
	fg, fgSecondary, muted, gutter, grayBright      string
	blue, cyan, green, magenta, orange, purple, red string
	yellow, teal                                    string
	promptBorder, promptBorderActive                string
}

// GrokNight is the default reference palette: grayscale surfaces with
// TokyoNight accents. Keep values semantic so one role change updates every
// component instead of scattering terminal color indexes through the TUI.
var grokNightPalette = themePalette{
	bg: "#0a0a0a", bgDark: "#0c0c0c", bgStorm: "#141414", highlight: "#242424",
	fg: "#e1e1e1", fgSecondary: "#c8c8c8", muted: "#6c6c6c", gutter: "#414141", grayBright: "#787878",
	blue: "#7aa2f7", cyan: "#7dcfff", green: "#9ece6a", magenta: "#bb9af7", orange: "#ff9e64", purple: "#9d7cd8", red: "#f7768e",
	yellow: "#e0af68", teal: "#1abc9c",
	promptBorder: "#323237", promptBorderActive: "#505058",
}

// Keep the alternate reference palette available for future user-selectable
// themes while GrokNight remains the product default.
var tokyoNightPalette = themePalette{
	bg: "#1a1b26", bgDark: "#16161e", bgStorm: "#24283b", highlight: "#292e42",
	fg: "#c0caf5", fgSecondary: "#a9b1d6", muted: "#565f89", gutter: "#3b4261", grayBright: "#737aa2",
	blue: "#7aa2f7", cyan: "#7dcfff", green: "#9ece6a", magenta: "#bb9af7", orange: "#ff9e64", purple: "#9d7cd8", red: "#f7768e",
	yellow: "#e0af68", teal: "#1abc9c",
	promptBorder: "#3c4b78", promptBorderActive: "#4b5c8c",
}

// theme holds the Lip Gloss styles for both the responsive layout and the
// existing transcript/picker renderers.
type theme struct {
	palette             themePalette
	background          lipgloss.Style
	topBar              lipgloss.Style
	panel               lipgloss.Style
	timeline            lipgloss.Style
	shortcutKey         lipgloss.Style
	shortcutLabel       lipgloss.Style
	shortcutSeparator   lipgloss.Style
	modalBorder         lipgloss.Style
	modalTitle          lipgloss.Style
	promptBase          lipgloss.Style
	promptText          lipgloss.Style
	promptTextBlurred   lipgloss.Style
	promptPlaceholder   lipgloss.Style
	promptPrompt        lipgloss.Style
	promptPromptBlurred lipgloss.Style
	promptCursor        lipgloss.Style
	promptBorder        lipgloss.Style
	promptBorderActive  lipgloss.Style
	promptRail          lipgloss.Style
	promptRailActive    lipgloss.Style
	promptInfo          lipgloss.Style
	promptInfoMuted     lipgloss.Style
	promptInfoSeparator lipgloss.Style

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
	footer        lipgloss.Style
	footerRight   lipgloss.Style
	spinner       lipgloss.Style
	selection     lipgloss.Style
	cmdPickerSel  lipgloss.Style
	cmdPickerItem lipgloss.Style
	pickerGroup   lipgloss.Style
	composerRule  lipgloss.Style
	composerBox   lipgloss.Style
	orbDim        lipgloss.Style
	orbMedium     lipgloss.Style
	orbBright     lipgloss.Style

	// Legacy aliases keep the welcome and picker renderers on the same palette.
	textPrimary lipgloss.Style
	grayBright  lipgloss.Style
	grayDim     lipgloss.Style
	accentUser  lipgloss.Style
	bgBase      lipgloss.Style
	bgHighlight lipgloss.Style
	modalHint   lipgloss.Style
	rowDesc     lipgloss.Style
	rowDescSel  lipgloss.Style
	scrollTrack lipgloss.Style
	scrollThumb lipgloss.Style
	rowHover    lipgloss.Style
}

func newTheme() *theme {
	p := grokNightPalette
	color := lipgloss.Color
	return &theme{
		palette:             p,
		background:          lipgloss.NewStyle().Background(color(p.bg)).Foreground(color(p.fg)),
		topBar:              lipgloss.NewStyle().Foreground(color(p.muted)),
		panel:               lipgloss.NewStyle().Background(color(p.bgDark)),
		timeline:            lipgloss.NewStyle().Foreground(color(p.gutter)),
		shortcutKey:         lipgloss.NewStyle().Foreground(color(p.cyan)),
		shortcutLabel:       lipgloss.NewStyle().Foreground(color(p.muted)),
		shortcutSeparator:   lipgloss.NewStyle().Foreground(color(p.gutter)),
		modalBorder:         lipgloss.NewStyle().Foreground(color(p.grayBright)),
		modalTitle:          lipgloss.NewStyle().Bold(true).Foreground(color(p.cyan)),
		promptBase:          lipgloss.NewStyle().Background(color(p.bgStorm)),
		promptText:          lipgloss.NewStyle().Foreground(color(p.fg)),
		promptTextBlurred:   lipgloss.NewStyle().Foreground(color(p.grayBright)).Background(color(p.bgStorm)),
		promptPlaceholder:   lipgloss.NewStyle().Foreground(color(p.muted)),
		promptPrompt:        lipgloss.NewStyle().Foreground(color(p.magenta)).Bold(true),
		promptPromptBlurred: lipgloss.NewStyle().Foreground(color(p.muted)).Background(color(p.bgStorm)),
		promptCursor:        lipgloss.NewStyle().Foreground(color(p.cyan)).Background(color(p.cyan)),
		promptBorder:        lipgloss.NewStyle().Foreground(color(p.promptBorder)).Background(color(p.bgStorm)),
		promptBorderActive:  lipgloss.NewStyle().Foreground(color(p.promptBorderActive)).Background(color(p.bgStorm)),
		promptRail:          lipgloss.NewStyle().Foreground(color(p.gutter)).Background(color(p.bgStorm)),
		promptRailActive:    lipgloss.NewStyle().Foreground(color(p.fg)).Background(color(p.bgStorm)),
		promptInfo:          lipgloss.NewStyle().Foreground(color(p.grayBright)).Background(color(p.bgStorm)),
		promptInfoMuted:     lipgloss.NewStyle().Foreground(color(p.muted)).Background(color(p.bgStorm)),
		promptInfoSeparator: lipgloss.NewStyle().Foreground(color(p.gutter)).Background(color(p.bgStorm)),

		userBlock:     lipgloss.NewStyle().Background(color(p.highlight)).Foreground(color(p.fg)).Padding(0, 1),
		queuedLabel:   lipgloss.NewStyle().Foreground(color(p.cyan)),
		assistantTxt:  lipgloss.NewStyle().Foreground(color(p.fg)),
		toolPending:   lipgloss.NewStyle().Foreground(color(p.yellow)),
		toolSuccess:   lipgloss.NewStyle().Foreground(color(p.green)),
		toolError:     lipgloss.NewStyle().Foreground(color(p.red)),
		toolTitle:     lipgloss.NewStyle().Bold(true).Foreground(color(p.cyan)),
		diffMeta:      lipgloss.NewStyle().Foreground(color(p.muted)),
		diffHunk:      lipgloss.NewStyle().Foreground(color(p.blue)),
		diffAdd:       lipgloss.NewStyle().Foreground(color(p.green)),
		diffRemove:    lipgloss.NewStyle().Foreground(color(p.red)),
		muted:         lipgloss.NewStyle().Foreground(color(p.muted)),
		accent:        lipgloss.NewStyle().Foreground(color(p.blue)),
		errorText:     lipgloss.NewStyle().Foreground(color(p.red)),
		footer:        lipgloss.NewStyle().Foreground(color(p.muted)),
		footerRight:   lipgloss.NewStyle().Foreground(color(p.grayBright)),
		spinner:       lipgloss.NewStyle().Foreground(color(p.magenta)),
		selection:     lipgloss.NewStyle().Foreground(color(p.fg)).Background(color(p.blue)),
		cmdPickerSel:  lipgloss.NewStyle().Bold(true).Foreground(color(p.cyan)),
		cmdPickerItem: lipgloss.NewStyle().Foreground(color(p.muted)),
		pickerGroup:   lipgloss.NewStyle().Bold(true).Foreground(color(p.fgSecondary)),
		composerRule:  lipgloss.NewStyle().Foreground(color(p.gutter)),
		composerBox:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(color(p.promptBorder)).Padding(0, 1),
		orbDim:        lipgloss.NewStyle().Foreground(color(p.purple)),
		orbMedium:     lipgloss.NewStyle().Foreground(color(p.magenta)),
		orbBright:     lipgloss.NewStyle().Foreground(color(p.cyan)).Bold(true),

		textPrimary: lipgloss.NewStyle().Foreground(color(p.fg)),
		grayBright:  lipgloss.NewStyle().Foreground(color(p.grayBright)),
		grayDim:     lipgloss.NewStyle().Foreground(color(p.muted)),
		accentUser:  lipgloss.NewStyle().Foreground(color(p.cyan)),
		bgBase:      lipgloss.NewStyle().Background(color(p.bg)),
		bgHighlight: lipgloss.NewStyle().Background(color(p.highlight)),
		modalHint:   lipgloss.NewStyle().Foreground(color(p.muted)),
		rowDesc:     lipgloss.NewStyle().Foreground(color(p.muted)),
		rowDescSel:  lipgloss.NewStyle().Foreground(color(p.fgSecondary)),
		scrollTrack: lipgloss.NewStyle().Foreground(color(p.gutter)),
		scrollThumb: lipgloss.NewStyle().Foreground(color(p.cyan)),
		rowHover:    lipgloss.NewStyle().Foreground(color(p.fg)).Background(color(p.highlight)),
	}
}

func configureTextareaTheme(input *textarea.Model, th *theme) {
	styles := input.Styles()
	styles.Focused.Base = th.promptBase
	styles.Focused.Text = th.promptText
	styles.Focused.Placeholder = th.promptPlaceholder
	styles.Focused.Prompt = th.promptPrompt
	styles.Focused.EndOfBuffer = th.promptBase
	styles.Focused.CursorLine = lipgloss.NewStyle()
	styles.Blurred.Base = th.promptBase
	styles.Blurred.Text = th.promptTextBlurred
	styles.Blurred.Placeholder = th.promptPlaceholder
	styles.Blurred.Prompt = th.promptPromptBlurred
	styles.Blurred.EndOfBuffer = th.promptBase
	styles.Blurred.CursorLine = lipgloss.NewStyle()
	styles.Cursor.Color = lipgloss.Color(th.palette.cyan)
	input.SetStyles(styles)
}

// myagentDarkStyle is a minimal dark glamour style tuned to the TUI palette.
var myagentDarkStyle = []byte(`{
  "document": {"block_prefix": "\n", "block_suffix": "\n", "color": "252", "margin": 2},
  "block_quote": {"indent": 1, "indent_token": "│ "},
  "paragraph": {},
  "list": {"level_indent": 2},
  "heading": {"block_suffix": "\n", "color": "252", "bold": true},
  "h1": {"prefix": "# ", "color": "252", "bold": true},
  "h2": {"prefix": "## ", "color": "252", "bold": true},
  "h3": {"prefix": "### ", "color": "250"},
  "h4": {"prefix": "#### ", "color": "250"},
  "h5": {"prefix": "##### ", "color": "250"},
  "h6": {"prefix": "###### ", "color": "250"},
  "text": {},
  "strikethrough": {"crossed_out": true},
  "emph": {"italic": true},
  "strong": {"bold": true},
  "hr": {"color": "240", "format": "\n────────\n"},
  "item": {"block_prefix": "• "},
  "enumeration": {"block_prefix": ". "},
  "task": {"ticked": "[✓] ", "unticked": "[ ] "},
  "link": {"color": "39", "underline": true},
  "link_text": {"color": "39", "bold": true},
  "image": {"color": "212", "underline": true},
  "image_text": {"color": "243", "format": "Image: {{.text}}"},
  "code": {"prefix": " ", "suffix": " ", "color": "86", "background_color": "235"},
  "code_block": {"color": "252", "margin": 2},
  "table": {},
  "definition_list": {},
  "definition_term": {},
  "definition_description": {"block_prefix": "\n▪ "},
  "html_block": {},
  "html_span": {}
}`)

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
			glamour.WithStylesFromJSONBytes(myagentDarkStyle),
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
