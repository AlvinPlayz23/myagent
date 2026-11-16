package tui

import (
	"go/parser"
	"go/token"
	"image/color"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestNoInitTimeTerminalQueries guards against reintroducing the startup hang
// that adaptiveColor in theme.go fixes: the lipgloss/v2/compat package runs
// lipgloss.HasDarkBackground(os.Stdin, os.Stdout) during package init, and on
// Windows that console read can block forever when the terminal never answers
// the OSC 11 query (the cancel reader behind its timeout cannot interrupt an
// in-flight read). The theme resolves light/dark at render time instead, from
// the background reported via tea.BackgroundColorMsg.
func TestNoInitTimeTerminalQueries(t *testing.T) {
	const banned = "charm.land/lipgloss/v2/compat"
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", file, err)
			continue
		}
		for _, imp := range parsed.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			if path == banned {
				t.Errorf("%s imports %s: it queries the terminal during package init and can hang startup; use the local adaptiveColor instead", file, banned)
			}
		}
	}
}

// TestAdaptiveColorFollowsReportedBackground pins the contract adaptiveColor
// relies on: dark is the fallback until the program reports the terminal
// background (the same fallback compat uses, and what the exact-ANSI color
// assertions in the diff tests assume), and the light/dark variants resolve
// per render once it does.
func TestAdaptiveColorFollowsReportedBackground(t *testing.T) {
	if !darkBackground.Load() {
		t.Fatal("dark background must be the default before any terminal report")
	}
	defer setDarkBackground(true) // restore the package default for other tests

	c := adaptiveColor{Light: lipgloss.Color("#FFFFFF"), Dark: lipgloss.Color("16")}
	if got, want := rgbaOf(c), rgbaOf(c.Dark); got != want {
		t.Fatalf("dark background: got %v, want %v", got, want)
	}
	setDarkBackground(false)
	if got, want := rgbaOf(c), rgbaOf(c.Light); got != want {
		t.Fatalf("light background: got %v, want %v", got, want)
	}
}

// rgbaOf collapses the four RGBA channels into one comparable value.
func rgbaOf(c color.Color) [4]uint32 {
	r, g, b, a := c.RGBA()
	return [4]uint32{r, g, b, a}
}

// frozenLegacyPalette is the pre-token palette, copied verbatim. It is the
// reference side of the parity gate: if a token mapping drifts, these literals
// stay put and the rendered-string comparison fails.
func frozenLegacyPalette() palette {
	return palette{
		primary:     adaptiveColor{Light: lipgloss.Color("#005F87"), Dark: lipgloss.Color("#5FB4D9")},
		secondary:   adaptiveColor{Light: lipgloss.Color("#287A8A"), Dark: lipgloss.Color("#649FA9")},
		textPrimary: adaptiveColor{Light: lipgloss.Color("#2A2A2A"), Dark: lipgloss.Color("#D0D0D0")},
		textBright:  adaptiveColor{Light: lipgloss.Color("#1A1A1A"), Dark: lipgloss.Color("#EEEEEE")},
		textDim:     adaptiveColor{Light: lipgloss.Color("#757575"), Dark: lipgloss.Color("#8A8A8A")},
		rule:        adaptiveColor{Light: lipgloss.Color("#BDBDBD"), Dark: lipgloss.Color("#585858")},
		userBlockBg: adaptiveColor{Light: lipgloss.Color("#E4E4E4"), Dark: lipgloss.Color("#303030")},
		rowHoverBg:  adaptiveColor{Light: lipgloss.Color("#D0D0D0"), Dark: lipgloss.Color("#444444")},
		selectionBg: adaptiveColor{Light: lipgloss.Color("#B2D7EE"), Dark: lipgloss.Color("#005F87")},
		pending:     adaptiveColor{Light: lipgloss.Color("#B26A00"), Dark: lipgloss.Color("#FFAF00")},
		success:     adaptiveColor{Light: lipgloss.Color("#2E7D32"), Dark: lipgloss.Color("35")},
		failure:     adaptiveColor{Light: lipgloss.Color("#C62828"), Dark: lipgloss.Color("203")},
		diffHunk:    adaptiveColor{Light: lipgloss.Color("#1565C0"), Dark: lipgloss.Color("75")},
		orbDim:      adaptiveColor{Light: lipgloss.Color("#4A7A94"), Dark: lipgloss.Color("24")},
		orbMedium:   adaptiveColor{Light: lipgloss.Color("#287A8A"), Dark: lipgloss.Color("31")},
		orbBright:   adaptiveColor{Light: lipgloss.Color("#005F87"), Dark: lipgloss.Color("39")},
	}
}

// frozenLegacyTheme is the pre-token newTheme, copied verbatim. It is the
// reference side of the parity gate and must not be "cleaned up" to use tokens.
func frozenLegacyTheme() *theme {
	p := frozenLegacyPalette()
	return &theme{
		userBlock: lipgloss.NewStyle().
			Background(p.userBlockBg).
			Foreground(p.textPrimary).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderLeftForeground(p.secondary).
			Padding(0, 1),
		queuedLabel:   lipgloss.NewStyle().Foreground(p.secondary),
		toolPending:   lipgloss.NewStyle().Foreground(p.pending),
		toolSuccess:   lipgloss.NewStyle().Foreground(p.success),
		toolError:     lipgloss.NewStyle().Foreground(p.failure),
		diffMeta:      lipgloss.NewStyle().Foreground(p.textDim),
		diffHunk:      lipgloss.NewStyle().Foreground(p.diffHunk),
		diffAdd:       lipgloss.NewStyle().Foreground(p.success),
		diffRemove:    lipgloss.NewStyle().Foreground(p.failure),
		muted:         lipgloss.NewStyle().Foreground(p.textDim),
		accent:        lipgloss.NewStyle().Foreground(p.primary),
		errorText:     lipgloss.NewStyle().Bold(true).Foreground(p.failure),
		footer:        lipgloss.NewStyle().Foreground(p.textDim),
		footerRight:   lipgloss.NewStyle().Foreground(p.textDim),
		spinner:       lipgloss.NewStyle().Foreground(p.primary),
		selection:     lipgloss.NewStyle().Foreground(p.textBright).Background(p.selectionBg),
		cmdPickerSel:  lipgloss.NewStyle().Bold(true).Foreground(p.primary),
		cmdPickerItem: lipgloss.NewStyle().Foreground(p.textPrimary),
		pickerGroup:   lipgloss.NewStyle().Bold(true).Foreground(p.textBright),
		composerRule:  lipgloss.NewStyle().Foreground(p.rule),
		composerBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.rule).
			Padding(0, 1),
		composerFocused: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(p.primary).
			Padding(0, 1),
		orbDim:      lipgloss.NewStyle().Foreground(p.orbDim),
		orbMedium:   lipgloss.NewStyle().Foreground(p.orbMedium),
		orbBright:   lipgloss.NewStyle().Foreground(p.orbBright).Bold(true),
		textPrimary: lipgloss.NewStyle().Foreground(p.textPrimary),
		accentUser:  lipgloss.NewStyle().Foreground(p.primary),
		chip: lipgloss.NewStyle().
			Background(p.secondary).
			Foreground(adaptiveColor{Light: lipgloss.Color("#FFFFFF"), Dark: lipgloss.Color("16")}).
			Bold(true).
			Padding(0, 1),
		ctxOK:       lipgloss.NewStyle().Foreground(p.secondary),
		ctxWarn:     lipgloss.NewStyle().Foreground(p.pending),
		ctxCritical: lipgloss.NewStyle().Foreground(p.failure),
		rowHover:    lipgloss.NewStyle().Foreground(p.textBright).Background(p.rowHoverBg),
	}
}

// TestThemeParityWithFrozenLegacy is the zero-churn gate. Every legacy style
// field is rendered through both the token-backed theme and the frozen
// pre-token implementation, in both terminal modes, and the strings must match
// byte for byte.
func TestThemeParityWithFrozenLegacy(t *testing.T) {
	defer setDarkBackground(true)
	got, want := newTheme(), frozenLegacyTheme()

	fields := []struct {
		name string
		get  func(*theme) lipgloss.Style
	}{
		{"userBlock", func(th *theme) lipgloss.Style { return th.userBlock }},
		{"queuedLabel", func(th *theme) lipgloss.Style { return th.queuedLabel }},
		{"toolPending", func(th *theme) lipgloss.Style { return th.toolPending }},
		{"toolSuccess", func(th *theme) lipgloss.Style { return th.toolSuccess }},
		{"toolError", func(th *theme) lipgloss.Style { return th.toolError }},
		{"diffMeta", func(th *theme) lipgloss.Style { return th.diffMeta }},
		{"diffHunk", func(th *theme) lipgloss.Style { return th.diffHunk }},
		{"diffAdd", func(th *theme) lipgloss.Style { return th.diffAdd }},
		{"diffRemove", func(th *theme) lipgloss.Style { return th.diffRemove }},
		{"muted", func(th *theme) lipgloss.Style { return th.muted }},
		{"accent", func(th *theme) lipgloss.Style { return th.accent }},
		{"errorText", func(th *theme) lipgloss.Style { return th.errorText }},
		{"footer", func(th *theme) lipgloss.Style { return th.footer }},
		{"footerRight", func(th *theme) lipgloss.Style { return th.footerRight }},
		{"spinner", func(th *theme) lipgloss.Style { return th.spinner }},
		{"selection", func(th *theme) lipgloss.Style { return th.selection }},
		{"cmdPickerSel", func(th *theme) lipgloss.Style { return th.cmdPickerSel }},
		{"cmdPickerItem", func(th *theme) lipgloss.Style { return th.cmdPickerItem }},
		{"pickerGroup", func(th *theme) lipgloss.Style { return th.pickerGroup }},
		{"composerRule", func(th *theme) lipgloss.Style { return th.composerRule }},
		{"composerBox", func(th *theme) lipgloss.Style { return th.composerBox }},
		{"composerFocused", func(th *theme) lipgloss.Style { return th.composerFocused }},
		{"orbDim", func(th *theme) lipgloss.Style { return th.orbDim }},
		{"orbMedium", func(th *theme) lipgloss.Style { return th.orbMedium }},
		{"orbBright", func(th *theme) lipgloss.Style { return th.orbBright }},
		{"textPrimary", func(th *theme) lipgloss.Style { return th.textPrimary }},
		{"accentUser", func(th *theme) lipgloss.Style { return th.accentUser }},
		{"chip", func(th *theme) lipgloss.Style { return th.chip }},
		{"ctxOK", func(th *theme) lipgloss.Style { return th.ctxOK }},
		{"ctxWarn", func(th *theme) lipgloss.Style { return th.ctxWarn }},
		{"ctxCritical", func(th *theme) lipgloss.Style { return th.ctxCritical }},
		{"rowHover", func(th *theme) lipgloss.Style { return th.rowHover }},
	}

	for _, dark := range []bool{true, false} {
		setDarkBackground(dark)
		for _, f := range fields {
			gotStr := f.get(got).Render("sample")
			wantStr := f.get(want).Render("sample")
			if gotStr != wantStr {
				t.Errorf("%s (dark=%v):\n got %q\nwant %q", f.name, dark, gotStr, wantStr)
			}
		}
	}
}

// TestPaletteCompatibilityView pins the markdown.go compatibility view. glamour
// still reads newPalette(), so every field there must stay byte-identical to
// the frozen pre-token palette even though the theme styles use tokens directly.
func TestPaletteCompatibilityView(t *testing.T) {
	defer setDarkBackground(true)
	got, want := newPalette(), frozenLegacyPalette()

	fields := []struct {
		name string
		get  func(palette) adaptiveColor
	}{
		{"primary", func(p palette) adaptiveColor { return p.primary }},
		{"secondary", func(p palette) adaptiveColor { return p.secondary }},
		{"textPrimary", func(p palette) adaptiveColor { return p.textPrimary }},
		{"textBright", func(p palette) adaptiveColor { return p.textBright }},
		{"textDim", func(p palette) adaptiveColor { return p.textDim }},
		{"rule", func(p palette) adaptiveColor { return p.rule }},
		{"userBlockBg", func(p palette) adaptiveColor { return p.userBlockBg }},
		{"rowHoverBg", func(p palette) adaptiveColor { return p.rowHoverBg }},
		{"selectionBg", func(p palette) adaptiveColor { return p.selectionBg }},
		{"pending", func(p palette) adaptiveColor { return p.pending }},
		{"success", func(p palette) adaptiveColor { return p.success }},
		{"failure", func(p palette) adaptiveColor { return p.failure }},
		{"diffHunk", func(p palette) adaptiveColor { return p.diffHunk }},
		{"orbDim", func(p palette) adaptiveColor { return p.orbDim }},
		{"orbMedium", func(p palette) adaptiveColor { return p.orbMedium }},
		{"orbBright", func(p palette) adaptiveColor { return p.orbBright }},
	}

	for _, dark := range []bool{true, false} {
		setDarkBackground(dark)
		for _, f := range fields {
			if gotColor, wantColor := rgbaOf(f.get(got)), rgbaOf(f.get(want)); gotColor != wantColor {
				t.Errorf("%s (dark=%v): palette = %v, legacy = %v", f.name, dark, gotColor, wantColor)
			}
		}
	}
}

// TestLegacyPaletteToTokenMapping is the explicit legacy→token matrix. Every
// legacy color the old palette exposed must resolve to the same RGBA as the
// token that now owns it, in both terminal modes.
func TestLegacyPaletteToTokenMapping(t *testing.T) {
	defer setDarkBackground(true)
	p := frozenLegacyPalette()
	toks := newTokens()

	cases := []struct {
		name  string
		old   color.Color
		token Token
		state State
	}{
		{"primary", p.primary, toks.Text.Action.Primary, StateBase},
		{"secondary", p.secondary, toks.Text.Action.Secondary, StateBase},
		{"textPrimary", p.textPrimary, toks.Text.Base, StateBase},
		{"textBright", p.textBright, toks.Text.Strong, StateBase},
		{"textDim", p.textDim, toks.Text.Muted, StateBase},
		{"rule", p.rule, toks.Border.Base, StateBase},
		{"userBlockBg", p.userBlockBg, toks.Background.Raised.Base, StateBase},
		{"rowHoverBg", p.rowHoverBg, toks.Background.Raised.High, StateBase},
		{"selectionText", p.textBright, toks.Text.Selection, StateBase},
		{"selectionBg", p.selectionBg, toks.Background.Selection, StateBase},
		{"pending", p.pending, toks.Text.Feedback.Warning.Base, StateBase},
		{"success", p.success, toks.Text.Feedback.Success.Base, StateBase},
		{"failure", p.failure, toks.Text.Feedback.Error.Base, StateBase},
		{"diffHunk", p.diffHunk, toks.Diff.Text.HunkHeader, StateBase},
		{"markdownLink", p.diffHunk, toks.Markdown.Link, StateBase},
		{"orbDim", p.orbDim, toks.Decor.Orb.Dim, StateBase},
		{"orbMedium", p.orbMedium, toks.Decor.Orb.Medium, StateBase},
		{"orbBright", p.orbBright, toks.Decor.Orb.Bright, StateBase},
		{
			"chip text",
			adaptiveColor{Light: lipgloss.Color("#FFFFFF"), Dark: lipgloss.Color("16")},
			toks.Text.Inverse,
			StateBase,
		},
	}

	for _, dark := range []bool{true, false} {
		setDarkBackground(dark)
		for _, c := range cases {
			if got, want := rgbaOf(c.token.State(c.state)), rgbaOf(c.old); got != want {
				t.Errorf("%s (dark=%v): token = %v, legacy = %v", c.name, dark, got, want)
			}
		}
	}
}

// TestTokenStateResolution pins the state accessor table: explicit overrides
// win, and StateBase always returns the untouched base token.
func TestTokenStateResolution(t *testing.T) {
	base := adaptiveColor{Light: lipgloss.Color("#112233"), Dark: lipgloss.Color("#445566")}
	dim := adaptiveColor{Light: lipgloss.Color("#FFFFFF"), Dark: lipgloss.Color("#000000")}
	explicit := adaptiveColor{Light: lipgloss.Color("#AA0000"), Dark: lipgloss.Color("#00AA00")}

	tok := newToken(base, dim).
		withState(StateHovered, explicit).
		withState(StateFocused, explicit).
		withState(StatePressed, explicit).
		withState(StateSelected, explicit).
		withState(StateDisabled, explicit)

	cases := []struct {
		name string
		got  color.Color
		want color.Color
	}{
		{"base", tok.Color(), base},
		{"base alias", tok.Base(), base},
		{"hovered", tok.Hovered(), explicit},
		{"focused", tok.Focused(), explicit},
		{"pressed", tok.Pressed(), explicit},
		{"selected", tok.Selected(), explicit},
		{"disabled", tok.Disabled(), explicit},
		{"state base", tok.State(StateBase), base},
		{"state hovered", tok.State(StateHovered), explicit},
		{"state focused", tok.State(StateFocused), explicit},
		{"state pressed", tok.State(StatePressed), explicit},
		{"state selected", tok.State(StateSelected), explicit},
		{"state disabled", tok.State(StateDisabled), explicit},
	}
	for _, c := range cases {
		if got, want := rgbaOf(c.got), rgbaOf(c.want); got != want {
			t.Errorf("%s: got %v, want %v", c.name, got, want)
		}
	}
}

// TestTokenStateDefaults pins the derived-state table: hover/pressed/disabled
// dim by their documented degrees, while focused/selected fall back to base
// until a theme supplies an explicit override (OpenCode's fallback behavior).
func TestTokenStateDefaults(t *testing.T) {
	base := adaptiveColor{Light: lipgloss.Color("#112233"), Dark: lipgloss.Color("#445566")}
	dim := adaptiveColor{Light: lipgloss.Color("#FFFFFF"), Dark: lipgloss.Color("#000000")}
	tok := newToken(base, dim)

	if got, want := rgbaOf(tok.Focused()), rgbaOf(base); got != want {
		t.Errorf("focused fallback: got %v, want base %v", got, want)
	}
	if got, want := rgbaOf(tok.Selected()), rgbaOf(base); got != want {
		t.Errorf("selected fallback: got %v, want base %v", got, want)
	}

	derived := []struct {
		name   string
		got    color.Color
		degree float64
	}{
		{"hovered", tok.Hovered(), hoverDecrease},
		{"pressed", tok.Pressed(), pressedDecrease},
		{"disabled", tok.Disabled(), disabledDecrease},
	}
	for _, d := range derived {
		if got, want := rgbaOf(d.got), rgbaOf(Decrease(tok, d.degree).Color()); got != want {
			t.Errorf("%s: got %v, want Decrease(%v) = %v", d.name, got, d.degree, want)
		}
		if rgbaOf(d.got) == rgbaOf(base) {
			t.Errorf("%s did not dim toward the background", d.name)
		}
	}
}

// TestDecreaseTintBlendMath pins the color math at the endpoints, midpoint and
// clamps. Black/white keep the expected values exact and easy to audit.
func TestDecreaseTintBlendMath(t *testing.T) {
	black := adaptiveColor{Light: lipgloss.Color("#000000"), Dark: lipgloss.Color("#000000")}
	white := adaptiveColor{Light: lipgloss.Color("#FFFFFF"), Dark: lipgloss.Color("#FFFFFF")}
	gray := lipgloss.Color("#808080")
	blackToken := newToken(black, white)
	whiteToken := newToken(white, black)

	cases := []struct {
		name string
		got  color.Color
		want color.Color
	}{
		{"decrease 0", Decrease(blackToken, 0).Color(), black},
		{"decrease 0.5", Decrease(blackToken, 0.5).Color(), gray},
		{"decrease 1", Decrease(blackToken, 1).Color(), white},
		{"decrease clamp low", Decrease(blackToken, -2).Color(), black},
		{"decrease clamp high", Decrease(blackToken, 2).Color(), white},
		{"tint 0", Tint(blackToken, whiteToken, 0).Color(), black},
		{"tint 0.5", Tint(blackToken, whiteToken, 0.5).Color(), gray},
		{"tint 1", Tint(blackToken, whiteToken, 1).Color(), white},
		{"tint clamp low", Tint(blackToken, whiteToken, -1).Color(), black},
		{"tint clamp high", Tint(blackToken, whiteToken, 2).Color(), white},
	}
	for _, c := range cases {
		if got, want := rgbaOf(c.got), rgbaOf(c.want); got != want {
			t.Errorf("%s: got %v, want %v", c.name, got, want)
		}
	}
}

// TestDecreaseKeepsAdaptiveResolution guards the startup-hang fix: derived
// shades must not freeze to the background mode active when they were built.
func TestDecreaseKeepsAdaptiveResolution(t *testing.T) {
	defer setDarkBackground(true)
	base := adaptiveColor{Light: lipgloss.Color("#000000"), Dark: lipgloss.Color("#FFFFFF")}
	dim := adaptiveColor{Light: lipgloss.Color("#FFFFFF"), Dark: lipgloss.Color("#000000")}
	derived := Decrease(newToken(base, dim), 0.25)

	setDarkBackground(true)
	if got, want := rgbaOf(derived.Color()), rgbaOf(lipgloss.Color("#BFBFBF")); got != want {
		t.Errorf("dark: got %v, want #BFBFBF", got)
	}
	setDarkBackground(false)
	if got, want := rgbaOf(derived.Color()), rgbaOf(lipgloss.Color("#404040")); got != want {
		t.Errorf("light: got %v, want #404040", got)
	}
}

// TestAgentCategorical pins the §3.0.B fallback rule: categorical[i] when the
// palette is populated, border.base otherwise, with stable wrapping for
// out-of-range agent indices.
func TestAgentCategorical(t *testing.T) {
	toks := newTokens()
	want := rgbaOf(toks.Categorical[0].Color())
	for _, i := range []int{0, 1, 7, -3} {
		if got := rgbaOf(toks.Agent(i).Color()); got != want {
			t.Errorf("Agent(%d) = %v, want %v", i, got, want)
		}
	}

	fallbackColor := adaptiveColor{Light: lipgloss.Color("#123456"), Dark: lipgloss.Color("#654321")}
	fallback := Tokens{Border: BorderTokens{Base: newToken(fallbackColor, fallbackColor)}}
	if got, want := rgbaOf(fallback.Agent(0).Color()), rgbaOf(fallbackColor); got != want {
		t.Errorf("empty categorical fallback = %v, want border.base %v", got, want)
	}
}
