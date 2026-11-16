package tui

import (
	"image/color"
	"sync/atomic"

	"charm.land/lipgloss/v2"
)

// adaptiveColor resolves to Light or Dark at render time based on the terminal
// background reported by Bubble Tea. It deliberately replaces the AdaptiveColor
// type from lipgloss/v2/compat: importing that package runs
// lipgloss.HasDarkBackground(os.Stdin, os.Stdout) during package init, which on
// Windows blocks forever in ReadConsole when the console never answers the
// OSC 11 query (the cancel reader behind its timeout cannot interrupt an
// in-flight console read). The running program instead requests the background
// with tea.RequestBackgroundColor, and Bubble Tea's input loop feeds the reply
// back as tea.BackgroundColorMsg.
type adaptiveColor struct {
	Light color.Color
	Dark  color.Color
}

func (c adaptiveColor) RGBA() (uint32, uint32, uint32, uint32) {
	if darkBackground.Load() {
		return c.Dark.RGBA()
	}
	return c.Light.RGBA()
}

// darkBackground is the terminal background detected by the running program.
// It defaults to true — the same fallback compat uses when detection is
// unavailable — so renders before the first BackgroundColorMsg stay dark.
var darkBackground atomic.Bool

func init() { darkBackground.Store(true) }

// setDarkBackground records the terminal background reported by the program.
func setDarkBackground(dark bool) { darkBackground.Store(dark) }

// Semantic tokens are the single color source; legacy lipgloss styles and the
// Markdown compatibility palette derive from them. Built-in colors follow
// OpenCode's v2 default palette, with myagent's teal orb colors retained for
// its animated styles.

// palette is the set of adaptive colors the legacy markdown compatibility view
// is built from. New code should use Tokens; every field here is derived from
// one in paletteFromTokens. Every color has a light and a dark variant resolved
// at render time from the terminal's background, so the TUI reads well on both
// (kj's theme/styles.go model).
type palette struct {
	primary     adaptiveColor // brand accent: prompt markers, chips, selection
	secondary   adaptiveColor // teal: highlights, user-block border
	textPrimary adaptiveColor // main content foreground
	textBright  adaptiveColor // emphasized content foreground
	textDim     adaptiveColor // secondary info, footer, descriptions
	rule        adaptiveColor // hairlines: composer rules, box borders
	userBlockBg adaptiveColor // user message block background
	rowHoverBg  adaptiveColor // hovered (not selected) picker rows
	selectionBg adaptiveColor // text-selection background
	pending     adaptiveColor // amber: in-flight tool state, warnings
	success     adaptiveColor // green: completed tool state
	failure     adaptiveColor // red: errors, failed tool state
	diffHunk    adaptiveColor // blue: @@ hunk headers
	orbDim      adaptiveColor
	orbMedium   adaptiveColor
	orbBright   adaptiveColor
}

// newPalette is the legacy compatibility view consumed by markdown.go. New
// code should use Tokens instead; this function exists only because this parity
// release may not edit call sites.
func newPalette() palette {
	return paletteFromTokens(newTokens())
}

// paletteFromTokens maps the token set back to the flat palette markdown.go
// still reads. The mapping is intentionally mechanical: every field points at
// the same token the legacy theme style uses, so glamour and lipgloss can never
// drift apart.
func paletteFromTokens(t Tokens) palette {
	return palette{
		primary:     t.Text.Action.Primary.adaptive(StateBase),
		secondary:   t.Text.Action.Secondary.adaptive(StateBase),
		textPrimary: t.Text.Base.adaptive(StateBase),
		textBright:  t.Text.Strong.adaptive(StateBase),
		textDim:     t.Text.Muted.adaptive(StateBase),
		rule:        t.Border.Base.adaptive(StateBase),
		userBlockBg: t.Background.Raised.Base.adaptive(StateBase),
		rowHoverBg:  t.Background.Raised.High.adaptive(StateBase),
		selectionBg: t.Background.Selection.adaptive(StateBase),
		pending:     t.Text.Feedback.Warning.Base.adaptive(StateBase),
		success:     t.Text.Feedback.Success.Base.adaptive(StateBase),
		failure:     t.Text.Feedback.Error.Base.adaptive(StateBase),
		diffHunk:    t.Diff.Text.HunkHeader.adaptive(StateBase),
		orbDim:      t.Decor.Orb.Dim.adaptive(StateBase),
		orbMedium:   t.Decor.Orb.Medium.adaptive(StateBase),
		orbBright:   t.Decor.Orb.Bright.adaptive(StateBase),
	}
}

// newTokens builds the OpenCode v2-inspired semantic palette. Every token dims
// toward background.base so hover and disabled shades stay consistent.
func newTokens() Tokens {
	bgBase := adaptiveColor{Light: lipgloss.Color("#FFFFFF"), Dark: lipgloss.Color("#0A0A0A")}
	textPrimary := adaptiveColor{Light: lipgloss.Color("#1A1A1A"), Dark: lipgloss.Color("#EEEEEE")}
	textBright := adaptiveColor{Light: lipgloss.Color("#000000"), Dark: lipgloss.Color("#FFFFFF")}
	textDim := adaptiveColor{Light: lipgloss.Color("#8A8A8A"), Dark: lipgloss.Color("#808080")}
	primary := textPrimary
	secondary := textDim
	rule := adaptiveColor{Light: lipgloss.Color("#B8B8B8"), Dark: lipgloss.Color("#484848")}
	userBlockBg := adaptiveColor{Light: lipgloss.Color("#FAFAFA"), Dark: lipgloss.Color("#141414")}
	rowHoverBg := adaptiveColor{Light: lipgloss.Color("#F5F5F5"), Dark: lipgloss.Color("#1E1E1E")}
	selectionBg := adaptiveColor{Light: lipgloss.Color("#3B7DD8"), Dark: lipgloss.Color("#FAB283")}
	pending := adaptiveColor{Light: lipgloss.Color("#D68C27"), Dark: lipgloss.Color("#F5A742")}
	success := adaptiveColor{Light: lipgloss.Color("#3D9A57"), Dark: lipgloss.Color("#7FD88F")}
	failure := adaptiveColor{Light: lipgloss.Color("#D1383D"), Dark: lipgloss.Color("#E06C75")}
	diffHunk := adaptiveColor{Light: lipgloss.Color("#7086B5"), Dark: lipgloss.Color("#828BB8")}
	categorical := adaptiveColor{Light: lipgloss.Color("#8C6FC5"), Dark: lipgloss.Color("#4C86D6")}
	orbDim := adaptiveColor{Light: lipgloss.Color("#4A7A94"), Dark: lipgloss.Color("24")}
	orbMedium := adaptiveColor{Light: lipgloss.Color("#287A8A"), Dark: lipgloss.Color("31")}
	orbBright := adaptiveColor{Light: lipgloss.Color("#005F87"), Dark: lipgloss.Color("39")}
	chipText := adaptiveColor{Light: lipgloss.Color("#FFFFFF"), Dark: lipgloss.Color("16")}
	selectionText := textBright
	markdownLink := selectionBg

	tok := func(c adaptiveColor) Token { return newToken(c, bgBase) }
	feedback := func(base adaptiveColor) FeedbackText {
		return FeedbackText{
			Base:  tok(base),
			Muted: newToken(blendAdaptive(base, bgBase, 0.35), bgBase),
		}
	}
	backgroundFeedback := func(base adaptiveColor) Token {
		return newToken(blendAdaptive(base, bgBase, 0.85), bgBase)
	}

	return Tokens{
		Text: TextTokens{
			Base:      tok(textPrimary),
			Muted:     tok(textDim),
			Faint:     newToken(blendAdaptive(textDim, bgBase, 0.35), bgBase),
			Strong:    tok(textBright),
			Selection: tok(selectionText),
			Inverse:   tok(chipText),
			Feedback: TextFeedbackTokens{
				Error:   feedback(failure),
				Warning: feedback(pending),
				Success: feedback(success),
				Info:    feedback(secondary),
			},
			Action: ActionTokens{
				Primary:     tok(primary).withState(StateFocused, chipText),
				Secondary:   tok(secondary).withState(StateFocused, chipText).withState(StateSelected, chipText),
				Destructive: tok(failure),
			},
			Formfield: tok(textPrimary).
				withState(StateFocused, primary).
				withState(StateSelected, primary).
				withState(StateDisabled, blendAdaptive(textPrimary, bgBase, disabledDecrease)),
		},
		Background: BackgroundTokens{
			Base: tok(bgBase),
			Raised: RaisedTokens{
				Base: tok(userBlockBg),
				High: tok(rowHoverBg),
				Max:  newToken(blendAdaptive(rowHoverBg, bgBase, 0.25), bgBase),
			},
			Selection: tok(selectionBg),
			Action: ActionTokens{
				Primary:     tok(primary),
				Secondary:   tok(secondary),
				Destructive: tok(failure),
			},
			Formfield: tok(userBlockBg).
				withState(StateFocused, rowHoverBg).
				withState(StateSelected, selectionBg).
				withState(StateDisabled, blendAdaptive(userBlockBg, bgBase, 0.40)),
			Feedback: BackgroundFeedbackTokens{
				Error:   backgroundFeedback(failure),
				Warning: backgroundFeedback(pending),
				Success: backgroundFeedback(success),
				Info:    backgroundFeedback(secondary),
			},
		},
		Border: BorderTokens{
			Base: tok(rule).
				withState(StateFocused, primary).
				withState(StateSelected, primary),
		},
		Scrollbar:   ScrollbarTokens{Base: tok(rule)},
		Categorical: []Token{tok(categorical)},
		Diff: DiffTokens{
			Text: DiffTextTokens{
				Added:      tok(success),
				Removed:    tok(failure),
				Context:    tok(textDim),
				HunkHeader: tok(diffHunk),
			},
		},
		Markdown: MarkdownTokens{Link: tok(markdownLink)},
		Decor: DecorTokens{
			Orb: OrbTokens{
				Dim:    tok(orbDim),
				Medium: tok(orbMedium),
				Bright: tok(orbBright),
			},
		},
	}
}

// DefaultTokens returns the built-in semantic token set. It is the entry point
// for the S1 kit and S4 chrome phases, which should never reference the legacy
// palette or raw lipgloss colors directly.
func DefaultTokens() Tokens { return newTokens() }

// theme holds the lipgloss styles for the UI, derived from one semantic token
// set so light and dark terminals share a single definition site.
type theme struct {
	tokens Tokens

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

// TokenSet returns the semantic token set backing this theme.
func (t *theme) TokenSet() Tokens { return t.tokens }

// newTheme builds every legacy style from the token set. The field names are
// the pre-token API; each assignment below is a thin alias, not a second
// palette definition.
func newTheme() *theme {
	t := newTokens()
	return &theme{
		tokens: t,
		userBlock: lipgloss.NewStyle().
			Background(t.Background.Raised.Base.Color()).
			Foreground(t.Text.Base.Color()).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderLeftForeground(t.Agent(0).Color()).
			Padding(0, 1),
		queuedLabel: lipgloss.NewStyle().Foreground(t.Text.Action.Secondary.Color()),
		// Pending is amber so it never reads as dim/muted text; success and
		// error keep their green/red meanings. Headers stay neutral — the
		// status icon/color carries the state, not the whole line.
		toolPending: lipgloss.NewStyle().Foreground(t.Text.Feedback.Warning.Base.Color()),
		toolSuccess: lipgloss.NewStyle().Foreground(t.Text.Feedback.Success.Base.Color()),
		toolError:   lipgloss.NewStyle().Foreground(t.Text.Feedback.Error.Base.Color()),
		diffMeta:    lipgloss.NewStyle().Foreground(t.Diff.Text.Context.Color()),
		diffHunk:    lipgloss.NewStyle().Foreground(t.Diff.Text.HunkHeader.Color()),
		diffAdd:     lipgloss.NewStyle().Foreground(t.Diff.Text.Added.Color()),
		diffRemove:  lipgloss.NewStyle().Foreground(t.Diff.Text.Removed.Color()),
		muted:       lipgloss.NewStyle().Foreground(t.Text.Muted.Color()),
		accent:      lipgloss.NewStyle().Foreground(t.Text.Action.Primary.Color()),
		errorText:   lipgloss.NewStyle().Bold(true).Foreground(t.Text.Feedback.Error.Base.Color()),
		footer:      lipgloss.NewStyle().Foreground(t.Text.Muted.Color()),
		footerRight: lipgloss.NewStyle().Foreground(t.Text.Muted.Color()),
		spinner:     lipgloss.NewStyle().Foreground(t.Text.Action.Primary.Color()),
		selection: lipgloss.NewStyle().
			Foreground(t.Text.Selection.Color()).
			Background(t.Background.Selection.Color()),
		cmdPickerSel:  lipgloss.NewStyle().Bold(true).Foreground(t.Text.Action.Primary.Color()),
		cmdPickerItem: lipgloss.NewStyle().Foreground(t.Text.Base.Color()),
		// Group headers are bold but stay neutral so the blue selection still
		// reads as the cursor rather than competing with them.
		pickerGroup:  lipgloss.NewStyle().Bold(true).Foreground(t.Text.Strong.Color()),
		composerRule: lipgloss.NewStyle().Foreground(t.Border.Base.Color()),
		// The boxed composer is a single rounded surface: the border carries
		// the shape, so it stays muted and padding keeps text off the edges.
		composerBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Border.Base.Color()).
			Padding(0, 1),
		// The focused composer lights its border with the primary accent so
		// the active input target is visible at a glance (kj's mode rules).
		composerFocused: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(t.Border.Base.Focused()).
			Padding(0, 1),
		orbDim:    lipgloss.NewStyle().Foreground(t.Decor.Orb.Dim.Color()),
		orbMedium: lipgloss.NewStyle().Foreground(t.Decor.Orb.Medium.Color()),
		orbBright: lipgloss.NewStyle().Foreground(t.Decor.Orb.Bright.Color()).Bold(true),
		// Grok-parity aliases, now derived from the same semantic tokens.
		textPrimary: lipgloss.NewStyle().Foreground(t.Text.Base.Color()),
		accentUser:  lipgloss.NewStyle().Foreground(t.Text.Action.Primary.Color()),
		// chip is a filled badge for inline labels (queued input, modes); the
		// inverted secondary keeps it readable without stealing the accent.
		chip: lipgloss.NewStyle().
			Background(t.Background.Action.Secondary.Color()).
			Foreground(t.Text.Inverse.Color()).
			Bold(true).
			Padding(0, 1),
		// Context-window indicator tiers (kj's context_status.go thresholds):
		// calm teal normally, amber past the warn line, red when critical.
		// text.status.info was rejected in the S1 sweep; the info borrow is
		// superseded by the S4 context widget.
		ctxOK:       lipgloss.NewStyle().Foreground(t.Text.Feedback.Info.Base.Color()),
		ctxWarn:     lipgloss.NewStyle().Foreground(t.Text.Feedback.Warning.Base.Color()),
		ctxCritical: lipgloss.NewStyle().Foreground(t.Text.Feedback.Error.Base.Color()),
		rowHover:    lipgloss.NewStyle().Foreground(t.Text.Strong.Color()).Background(t.Background.Raised.High.Color()),
	}
}
