package tui

import (
	"sync"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/glamour"
)

// palette is the set of adaptive colors the theme is built from. Every color
// has a light and a dark variant resolved at render time from the terminal's
// background, so the TUI reads well on both (kj's theme/styles.go model).
type palette struct {
	primary     compat.AdaptiveColor // brand accent: prompt markers, chips, selection
	secondary   compat.AdaptiveColor // teal: highlights, user-block border
	textPrimary compat.AdaptiveColor // main content foreground
	textBright  compat.AdaptiveColor // emphasized content foreground
	textDim     compat.AdaptiveColor // secondary info, footer, descriptions
	rule        compat.AdaptiveColor // hairlines: composer rules, box borders
	userBlockBg compat.AdaptiveColor // user message block background
	rowHoverBg  compat.AdaptiveColor // hovered (not selected) picker rows
	selectionBg compat.AdaptiveColor // text-selection background
	pending     compat.AdaptiveColor // amber: in-flight tool state, warnings
	success     compat.AdaptiveColor // green: completed tool state
	failure     compat.AdaptiveColor // red: errors, failed tool state
	diffHunk    compat.AdaptiveColor // blue: @@ hunk headers
	orbDim      compat.AdaptiveColor
	orbMedium   compat.AdaptiveColor
	orbBright   compat.AdaptiveColor
}

func newPalette() palette {
	return palette{
		primary:     compat.AdaptiveColor{Light: lipgloss.Color("#005F87"), Dark: lipgloss.Color("#5FB4D9")},
		secondary:   compat.AdaptiveColor{Light: lipgloss.Color("#287A8A"), Dark: lipgloss.Color("#649FA9")},
		textPrimary: compat.AdaptiveColor{Light: lipgloss.Color("#2A2A2A"), Dark: lipgloss.Color("#D0D0D0")},
		textBright:  compat.AdaptiveColor{Light: lipgloss.Color("#1A1A1A"), Dark: lipgloss.Color("#EEEEEE")},
		textDim:     compat.AdaptiveColor{Light: lipgloss.Color("#757575"), Dark: lipgloss.Color("#8A8A8A")},
		rule:        compat.AdaptiveColor{Light: lipgloss.Color("#BDBDBD"), Dark: lipgloss.Color("#585858")},
		userBlockBg: compat.AdaptiveColor{Light: lipgloss.Color("#E4E4E4"), Dark: lipgloss.Color("#303030")},
		rowHoverBg:  compat.AdaptiveColor{Light: lipgloss.Color("#D0D0D0"), Dark: lipgloss.Color("#444444")},
		selectionBg: compat.AdaptiveColor{Light: lipgloss.Color("#B2D7EE"), Dark: lipgloss.Color("#005F87")},
		pending:     compat.AdaptiveColor{Light: lipgloss.Color("#B26A00"), Dark: lipgloss.Color("#FFAF00")},
		// Success/failure keep the exact ANSI-256 greens/reds the diff tests
		// assert on in dark mode; the light variants deepen them for contrast.
		success: compat.AdaptiveColor{Light: lipgloss.Color("#2E7D32"), Dark: lipgloss.Color("35")},
		failure: compat.AdaptiveColor{Light: lipgloss.Color("#C62828"), Dark: lipgloss.Color("203")},
		// Hunk headers keep the ANSI-256 blue in dark mode for consistency
		// with the diffAdd/diffRemove encoding.
		diffHunk: compat.AdaptiveColor{Light: lipgloss.Color("#1565C0"), Dark: lipgloss.Color("75")},
		orbDim:    compat.AdaptiveColor{Light: lipgloss.Color("#4A7A94"), Dark: lipgloss.Color("24")},
		orbMedium: compat.AdaptiveColor{Light: lipgloss.Color("#287A8A"), Dark: lipgloss.Color("31")},
		orbBright: compat.AdaptiveColor{Light: lipgloss.Color("#005F87"), Dark: lipgloss.Color("39")},
	}
}

// theme holds the lipgloss styles for the UI, derived from one adaptive
// palette so light and dark terminals share a single definition site.
type theme struct {
	userBlock       lipgloss.Style
	queuedLabel     lipgloss.Style
	toolPending     lipgloss.Style
	toolSuccess     lipgloss.Style
	toolError       lipgloss.Style
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
	composerBox     lipgloss.Style
	composerFocused lipgloss.Style
	orbDim          lipgloss.Style
	orbMedium       lipgloss.Style
	orbBright       lipgloss.Style
	textPrimary     lipgloss.Style
	accentUser      lipgloss.Style
	// chip is a filled badge (inverted secondary) for labels like "next" on
	// queued follow-ups; ctx* grade the context-window indicator.
	chip        lipgloss.Style
	ctxOK       lipgloss.Style
	ctxWarn     lipgloss.Style
	ctxCritical lipgloss.Style
	// rowHover backs hovered (not selected) picker/menu rows. The cursor
	// keeps bold + accent so hover never reads as selection (grok menu.rs).
	rowHover lipgloss.Style
}

func newTheme() *theme {
	p := newPalette()
	return &theme{
		userBlock: lipgloss.NewStyle().
			Background(p.userBlockBg).
			Foreground(p.textPrimary).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderLeftForeground(p.secondary).
			Padding(0, 1),
		queuedLabel: lipgloss.NewStyle().Foreground(p.secondary),
		// Pending is amber so it never reads as dim/muted text; success and
		// error keep their green/red meanings. Headers stay neutral — the
		// status icon/color carries the state, not the whole line.
		toolPending: lipgloss.NewStyle().Foreground(p.pending),
		toolSuccess: lipgloss.NewStyle().Foreground(p.success),
		toolError:   lipgloss.NewStyle().Foreground(p.failure),
		diffMeta:    lipgloss.NewStyle().Foreground(p.textDim),
		diffHunk:    lipgloss.NewStyle().Foreground(p.diffHunk),
		diffAdd:     lipgloss.NewStyle().Foreground(p.success),
		diffRemove:  lipgloss.NewStyle().Foreground(p.failure),
		muted:       lipgloss.NewStyle().Foreground(p.textDim),
		accent:      lipgloss.NewStyle().Foreground(p.primary),
		errorText:   lipgloss.NewStyle().Bold(true).Foreground(p.failure),
		footer:      lipgloss.NewStyle().Foreground(p.textDim),
		footerRight: lipgloss.NewStyle().Foreground(p.textDim),
		spinner:     lipgloss.NewStyle().Foreground(p.primary),
		selection:   lipgloss.NewStyle().Foreground(p.textBright).Background(p.selectionBg),
		cmdPickerSel:  lipgloss.NewStyle().Bold(true).Foreground(p.primary),
		cmdPickerItem: lipgloss.NewStyle().Foreground(p.textPrimary),
		// Group headers are bold but stay neutral so the blue selection still
		// reads as the cursor rather than competing with them.
		pickerGroup:  lipgloss.NewStyle().Bold(true).Foreground(p.textBright),
		composerRule: lipgloss.NewStyle().Foreground(p.rule),
		// The boxed composer is a single rounded surface: the border carries
		// the shape, so it stays muted and padding keeps text off the edges.
		composerBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.rule).
			Padding(0, 1),
		// The focused composer lights its border with the primary accent so
		// the active input target is visible at a glance (kj's mode rules).
		composerFocused: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.primary).
			Padding(0, 1),
		orbDim:       lipgloss.NewStyle().Foreground(p.orbDim),
		orbMedium:    lipgloss.NewStyle().Foreground(p.orbMedium),
		orbBright:    lipgloss.NewStyle().Foreground(p.orbBright).Bold(true),
		// Grok-parity aliases, now derived from the same adaptive palette.
		textPrimary: lipgloss.NewStyle().Foreground(p.textPrimary),
		accentUser:  lipgloss.NewStyle().Foreground(p.primary),
		// chip is a filled badge for inline labels (queued input, modes); the
		// inverted secondary keeps it readable without stealing the accent.
		chip: lipgloss.NewStyle().
			Background(p.secondary).
			Foreground(compat.AdaptiveColor{Light: lipgloss.Color("#FFFFFF"), Dark: lipgloss.Color("16")}).
			Bold(true).
			Padding(0, 1),
		// Context-window indicator tiers (kj's context_status.go thresholds):
		// calm teal normally, amber past the warn line, red when critical.
		ctxOK:       lipgloss.NewStyle().Foreground(p.secondary),
		ctxWarn:     lipgloss.NewStyle().Foreground(p.pending),
		ctxCritical: lipgloss.NewStyle().Foreground(p.failure),
		rowHover:    lipgloss.NewStyle().Foreground(p.textBright).Background(p.rowHoverBg),
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
