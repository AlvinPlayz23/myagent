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
	userBlock       lipgloss.Style
	queuedLabel     lipgloss.Style
	assistantTxt    lipgloss.Style
	toolPending     lipgloss.Style
	toolSuccess     lipgloss.Style
	toolError       lipgloss.Style
	toolTitle       lipgloss.Style
	diffMeta        lipgloss.Style
	diffHunk        lipgloss.Style
	diffAdd         lipgloss.Style
	diffRemove      lipgloss.Style
	muted           lipgloss.Style
	accent          lipgloss.Style
	errorText       lipgloss.Style
	footer          lipgloss.Style
	footerRight     lipgloss.Style
	spinner         lipgloss.Style
	selection       lipgloss.Style
	cmdPickerSel    lipgloss.Style
	cmdPickerItem   lipgloss.Style
	pickerGroup     lipgloss.Style
	composerRule    lipgloss.Style
	orbDim          lipgloss.Style
	orbMedium       lipgloss.Style
	orbBright       lipgloss.Style
}

func newTheme() *theme {
	return &theme{
		userBlock: lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("252")).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderLeftForeground(lipgloss.Color("39")).
			Padding(0, 1),
		queuedLabel:  lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		assistantTxt: lipgloss.NewStyle(),
		// Pending is amber so it never reads as dim/muted text; success and
		// error keep their green/red meanings. Headers stay neutral — the
		// status icon/color carries the state, not the whole line.
		toolPending: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		toolSuccess: lipgloss.NewStyle().Foreground(lipgloss.Color("35")),
		toolError:   lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		toolTitle:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")),
		diffMeta:    lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		diffHunk:    lipgloss.NewStyle().Foreground(lipgloss.Color("75")),
		diffAdd:     lipgloss.NewStyle().Foreground(lipgloss.Color("35")),
		diffRemove:  lipgloss.NewStyle().Foreground(lipgloss.Color("203")),
		muted:       lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		accent:      lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		errorText:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203")),
		footer:      lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		footerRight: lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		spinner:     lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
		selection:   lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("25")),
		cmdPickerSel:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
		cmdPickerItem: lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
		// Group headers are bold but stay neutral so the blue selection still
		// reads as the cursor rather than competing with them.
		pickerGroup:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")),
		composerRule: lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		orbDim:       lipgloss.NewStyle().Foreground(lipgloss.Color("24")),
		orbMedium:    lipgloss.NewStyle().Foreground(lipgloss.Color("31")),
		orbBright:    lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true),
	}
}

// myagentDarkStyle is a minimal dark glamour style tuned to the TUI palette:
// neutral 252 body, quiet headings, subtle 235 code background, 39 links.
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
