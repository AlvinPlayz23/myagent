package tui

import (
	_ "embed"
	"encoding/json"
	"image/color"
	"math"
	"strings"
	"sync/atomic"
	"unicode"

	"charm.land/lipgloss/v2"
)

// Palette colors are adapted from OpenCode's bundled MIT themes at revision
// 73de67f. The Vercel-inspired palette retains its source attribution in themes.json.
//
//go:embed themes.json
var themePaletteJSON []byte

type themeChoice struct {
	id          string
	label       string
	description string
}

var themeChoices = []themeChoice{
	{id: "default", label: "Default", description: "The built-in adaptive palette."},
	{id: "tokyonight", label: "Tokyo Night", description: "Cool blue accents over a midnight navy surface."},
	{id: "catppuccin", label: "Catppuccin Mocha", description: "Soft pastel accents over a warm dark surface."},
	{id: "kanagawa", label: "Kanagawa", description: "Muted, ink-inspired colors with warm highlights."},
	{id: "vesper", label: "Vesper", description: "Minimal charcoal with bright peach and mint accents."},
	{id: "osaka-jade", label: "Osaka Jade", description: "Deep green surfaces with vivid jade accents."},
	{id: "rosepine", label: "Rosé Pine", description: "Dusky plum surfaces with gentle rose and pine tones."},
	{id: "vercel", label: "Vercel", description: "Black surfaces with crisp blue and violet accents."},
	{id: "tokyo-mocha", label: "Tokyo Night × Catppuccin Mocha", description: "Tokyo Night surfaces with Catppuccin Mocha text, mauve accents, and pastel syntax."},
}

type themePaletteFile struct {
	Themes []themePalette `json:"themes"`
}

type themePalette struct {
	ID          string        `json:"id"`
	Label       string        `json:"label"`
	Description string        `json:"description"`
	Colors      paletteColors `json:"colors"`
}

type paletteColors struct {
	Primary           string `json:"primary"`
	Secondary         string `json:"secondary"`
	Accent            string `json:"accent"`
	Background        string `json:"background"`
	BackgroundPanel   string `json:"backgroundPanel"`
	BackgroundElement string `json:"backgroundElement"`
	Text              string `json:"text"`
	TextMuted         string `json:"textMuted"`
	Border            string `json:"border"`
	Success           string `json:"success"`
	Warning           string `json:"warning"`
	Error             string `json:"error"`
	SyntaxKeyword     string `json:"syntaxKeyword"`
	SyntaxString      string `json:"syntaxString"`
	SyntaxFunction    string `json:"syntaxFunction"`
	DiffAdded         string `json:"diffAdded"`
	DiffRemoved       string `json:"diffRemoved"`
}

type syntaxColors struct {
	keyword  color.Color
	string   color.Color
	function color.Color
}

type themeSelection struct {
	name     string
	tokens   Tokens
	syntax   syntaxColors
	revision uint64
}

var selectedTheme atomic.Pointer[themeSelection]

// normalizeThemeName accepts gallery IDs and common display-name spellings.
// Unknown names deliberately resolve to the built-in theme.
func normalizeThemeName(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	key = strings.ReplaceAll(key, "é", "e")
	var compact strings.Builder
	for _, r := range key {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			compact.WriteRune(r)
		}
	}
	switch compact.String() {
	case "", "default", "myagent":
		return "default"
	case "tokyonight":
		return "tokyonight"
	case "catppuccin", "catppuccinmocha":
		return "catppuccin"
	case "kanagawa":
		return "kanagawa"
	case "vesper":
		return "vesper"
	case "osakajade":
		return "osaka-jade"
	case "rosepine", "rosé pine":
		return "rosepine"
	case "vercel":
		return "vercel"
	case "tokyomocha", "tokyonightcatppuccinmocha":
		return "tokyo-mocha"
	default:
		return "default"
	}
}

// SetThemeName atomically selects a gallery theme. Selecting the current theme
// is a no-op; returning to default restores the adaptive built-in palette.
func SetThemeName(name string) {
	name = normalizeThemeName(name)
	var tokens Tokens
	var syntax syntaxColors
	if name != "default" {
		palette, ok := findThemePalette(name)
		if !ok {
			name = "default"
		} else {
			tokens = tokensForPalette(palette.Colors)
			syntax = syntaxForPalette(palette.Colors)
		}
	}

	for {
		old := selectedTheme.Load()
		oldName := "default"
		var revision uint64
		if old != nil {
			oldName = old.name
			revision = old.revision
		}
		if oldName == name {
			return
		}
		next := &themeSelection{name: name, tokens: tokens, syntax: syntax, revision: revision + 1}
		if selectedTheme.CompareAndSwap(old, next) {
			return
		}
	}
}

// CurrentThemeName reports the selected gallery ID, or "default" before any
// named theme has been selected.
func CurrentThemeName() string {
	selection := selectedTheme.Load()
	if selection == nil {
		return "default"
	}
	return selection.name
}

// CurrentThemeTokens returns the selected semantic palette. Default tokens are
// rebuilt on demand so terminal background detection stays adaptive.
func CurrentThemeTokens() Tokens {
	selection := selectedTheme.Load()
	if selection == nil || selection.name == "default" {
		return newTokens()
	}
	tokens := selection.tokens
	tokens.Categorical = append([]Token(nil), tokens.Categorical...)
	return tokens
}

// ThemeRevision increases only when the selected theme ID changes.
func ThemeRevision() uint64 {
	selection := selectedTheme.Load()
	if selection == nil {
		return 0
	}
	return selection.revision
}

// namedTokens builds a named dark palette without changing the active theme.
// Named palettes intentionally use the same color in light and dark modes.
func namedTokens(name string) Tokens {
	name = normalizeThemeName(name)
	if name == "default" {
		return newTokens()
	}
	palette, ok := findThemePalette(name)
	if !ok {
		return newTokens()
	}
	return tokensForPalette(palette.Colors)
}

// ThemeSyntaxColor returns the requested syntax role from the active palette.
// Unknown roles fall back to the built-in categorical accent.
func ThemeSyntaxColor(role string) color.Color {
	selection := selectedTheme.Load()
	if selection != nil && selection.name != "default" {
		switch strings.ToLower(strings.TrimSpace(role)) {
		case "syntaxkeyword", "keyword":
			return selection.syntax.keyword
		case "syntaxstring", "string":
			return selection.syntax.string
		case "syntaxfunction", "function":
			return selection.syntax.function
		default:
			return newTokens().Categorical[0].Color()
		}
	}
	defaults := newTokens()
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "syntaxstring", "string":
		return defaults.Diff.Text.Added.Color()
	case "syntaxfunction", "function":
		return defaults.Markdown.Link.Color()
	default:
		return defaults.Categorical[0].Color()
	}
}

func findThemePalette(name string) (themePalette, bool) {
	var palettes themePaletteFile
	if err := json.Unmarshal(themePaletteJSON, &palettes); err != nil {
		panic("invalid embedded theme palettes: " + err.Error())
	}
	for _, palette := range palettes.Themes {
		if palette.ID == name {
			return palette, true
		}
	}
	return themePalette{}, false
}

func fixedColor(value string) adaptiveColor {
	c := lipgloss.Color(value)
	return adaptiveColor{Light: c, Dark: c}
}

func tokensForPalette(p paletteColors) Tokens {
	bg := fixedColor(p.Background)
	panel := fixedColor(p.BackgroundPanel)
	element := fixedColor(p.BackgroundElement)
	primary := fixedColor(p.Primary)
	secondary := fixedColor(p.Secondary)
	accent := fixedColor(p.Accent)
	text := fixedColor(p.Text)
	muted := fixedColor(p.TextMuted)
	border := fixedColor(p.Border)
	success := fixedColor(p.Success)
	warning := fixedColor(p.Warning)
	failure := fixedColor(p.Error)
	selectionText := bestContrast(primary.Light, text.Light, bg.Light)

	tok := func(c adaptiveColor) Token { return newToken(c, bg) }
	feedback := func(c adaptiveColor) FeedbackText {
		return FeedbackText{Base: tok(c), Muted: newToken(blendAdaptive(c, bg, 0.35), bg)}
	}
	backgroundFeedback := func(c adaptiveColor) Token {
		return newToken(blendAdaptive(c, bg, 0.85), bg)
	}

	return Tokens{
		Text: TextTokens{
			Base:      tok(text),
			Muted:     tok(muted),
			Faint:     newToken(blendAdaptive(muted, bg, 0.35), bg),
			Strong:    tok(text),
			Selection: tok(selectionText),
			Inverse:   tok(bestContrast(secondary.Light, text.Light, bg.Light)),
			Feedback: TextFeedbackTokens{
				Error: feedback(failure), Warning: feedback(warning),
				Success: feedback(success), Info: feedback(secondary),
			},
			Action: ActionTokens{
				Primary:     tok(primary).withState(StateFocused, bestContrast(primary.Light, text.Light, bg.Light)),
				Secondary:   tok(secondary).withState(StateFocused, bestContrast(secondary.Light, text.Light, bg.Light)).withState(StateSelected, bestContrast(secondary.Light, text.Light, bg.Light)),
				Destructive: tok(failure).withState(StateFocused, bestContrast(failure.Light, text.Light, bg.Light)),
			},
			Formfield: tok(text).
				withState(StateFocused, primary).
				withState(StateSelected, primary).
				withState(StateDisabled, blendAdaptive(text, bg, disabledDecrease)),
		},
		Background: BackgroundTokens{
			Base: tok(bg),
			Raised: RaisedTokens{
				Base: tok(panel), High: tok(element),
				Max: newToken(blendAdaptive(element, bg, 0.25), bg),
			},
			Selection: tok(primary),
			Action:    ActionTokens{Primary: tok(primary), Secondary: tok(secondary), Destructive: tok(failure)},
			Formfield: tok(panel).
				withState(StateFocused, element).
				withState(StateSelected, primary).
				withState(StateDisabled, blendAdaptive(panel, bg, 0.40)),
			Feedback: BackgroundFeedbackTokens{
				Error: backgroundFeedback(failure), Warning: backgroundFeedback(warning),
				Success: backgroundFeedback(success), Info: backgroundFeedback(secondary),
			},
		},
		Border:      BorderTokens{Base: tok(border).withState(StateFocused, primary).withState(StateSelected, primary)},
		Scrollbar:   ScrollbarTokens{Base: tok(border)},
		Categorical: []Token{tok(primary), tok(secondary), tok(accent)},
		Diff: DiffTokens{Text: DiffTextTokens{
			Added: tok(fixedColor(p.DiffAdded)), Removed: tok(fixedColor(p.DiffRemoved)),
			Context: tok(muted), HunkHeader: tok(secondary),
		}},
		Markdown: MarkdownTokens{Link: tok(primary)},
		Decor:    DecorTokens{Orb: OrbTokens{Dim: tok(secondary), Medium: tok(primary), Bright: tok(accent)}},
	}
}

func syntaxForPalette(p paletteColors) syntaxColors {
	return syntaxColors{
		keyword:  fixedColor(p.SyntaxKeyword).Light,
		string:   fixedColor(p.SyntaxString).Light,
		function: fixedColor(p.SyntaxFunction).Light,
	}
}

func bestContrast(background, first, second color.Color) adaptiveColor {
	candidates := []color.Color{first, second, lipgloss.Color("#FFFFFF"), lipgloss.Color("#000000")}
	best := candidates[0]
	bestRatio := contrastRatio(background, best)
	for _, candidate := range candidates[1:] {
		if ratio := contrastRatio(background, candidate); ratio > bestRatio {
			best, bestRatio = candidate, ratio
		}
	}
	return adaptiveColor{Light: best, Dark: best}
}

func contrastRatio(a, b color.Color) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func relativeLuminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	channel := func(v uint32) float64 {
		x := float64(v) / 65535
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}
