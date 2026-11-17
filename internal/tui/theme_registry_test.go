package tui

import (
	"charm.land/lipgloss/v2"
	"image/color"
	"strings"
	"sync"
	"testing"
)

func TestThemeChoicesAndNormalization(t *testing.T) {
	want := []string{"default", "tokyonight", "catppuccin", "kanagawa", "vesper", "osaka-jade", "rosepine", "vercel", "tokyo-mocha"}
	if len(themeChoices) != len(want) {
		t.Fatalf("themeChoices has %d items, want %d", len(themeChoices), len(want))
	}
	for i, id := range want {
		if themeChoices[i].id != id || themeChoices[i].label == "" || themeChoices[i].description == "" {
			t.Errorf("themeChoices[%d] = %#v, want populated choice %q", i, themeChoices[i], id)
		}
	}
	for input, expected := range map[string]string{
		"Tokyo Night": "tokyonight", " CATPPUCCIN_MOCHA ": "catppuccin",
		"Osaka Jade": "osaka-jade", "Rosé Pine": "rosepine", "Tokyo Night × Catppuccin Mocha": "tokyo-mocha",
		"not-a-theme": "default", "": "default",
	} {
		if got := normalizeThemeName(input); got != expected {
			t.Errorf("normalizeThemeName(%q) = %q, want %q", input, got, expected)
		}
	}
}

func TestNamedThemePalettes(t *testing.T) {
	t.Cleanup(func() { SetThemeName("default") })
	tests := []struct {
		name, background, panel, element string
	}{
		{"tokyonight", "#1A1B26", "#1E2030", "#222436"},
		{"catppuccin", "#1E1E2E", "#181825", "#11111B"},
		{"kanagawa", "#1F1F28", "#2A2A37", "#363646"},
		{"vesper", "#101010", "#101010", "#101010"},
		{"osaka-jade", "#111C18", "#1A2520", "#23372B"},
		{"rosepine", "#191724", "#1F1D2E", "#26233A"},
		{"vercel", "#000000", "#1A1A1A", "#292929"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			SetThemeName(tc.name)
			tokens := namedTokens(tc.name)
			palette := mustPalette(t, tc.name).Colors
			if got := strings.ToUpper(hexColor(tokens.Background.Base.Color())); got != tc.background {
				t.Errorf("background = %s, want %s", got, tc.background)
			}
			if got := strings.ToUpper(hexColor(tokens.Background.Raised.Base.Color())); got != tc.panel {
				t.Errorf("panel = %s, want %s", got, tc.panel)
			}
			if got := strings.ToUpper(hexColor(tokens.Background.Raised.High.Color())); got != tc.element {
				t.Errorf("element = %s, want %s", got, tc.element)
			}
			if got := len(tokens.Categorical); got != 3 {
				t.Errorf("categorical roles = %d, want 3", got)
			}
			roles := map[string][2]string{
				"primary":         {hexColor(tokens.Text.Action.Primary.Color()), palette.Primary},
				"secondary":       {hexColor(tokens.Text.Action.Secondary.Color()), palette.Secondary},
				"accent":          {hexColor(tokens.Categorical[2].Color()), palette.Accent},
				"background":      {hexColor(tokens.Background.Base.Color()), palette.Background},
				"panel":           {hexColor(tokens.Background.Raised.Base.Color()), palette.BackgroundPanel},
				"element":         {hexColor(tokens.Background.Raised.High.Color()), palette.BackgroundElement},
				"text":            {hexColor(tokens.Text.Base.Color()), palette.Text},
				"muted text":      {hexColor(tokens.Text.Muted.Color()), palette.TextMuted},
				"border":          {hexColor(tokens.Border.Base.Color()), palette.Border},
				"success":         {hexColor(tokens.Text.Feedback.Success.Base.Color()), palette.Success},
				"warning":         {hexColor(tokens.Text.Feedback.Warning.Base.Color()), palette.Warning},
				"error":           {hexColor(tokens.Text.Feedback.Error.Base.Color()), palette.Error},
				"syntax keyword":  {hexColor(ThemeSyntaxColor("syntaxKeyword")), palette.SyntaxKeyword},
				"syntax string":   {hexColor(ThemeSyntaxColor("syntaxString")), palette.SyntaxString},
				"syntax function": {hexColor(ThemeSyntaxColor("syntaxFunction")), palette.SyntaxFunction},
				"diff added":      {hexColor(tokens.Diff.Text.Added.Color()), palette.DiffAdded},
				"diff removed":    {hexColor(tokens.Diff.Text.Removed.Color()), palette.DiffRemoved},
			}
			for role, pair := range roles {
				if got, want := rgbaOf(lipgloss.Color(pair[0])), rgbaOf(lipgloss.Color(pair[1])); got != want {
					t.Errorf("%s = %v (%s), want %v (%s)", role, got, pair[0], want, pair[1])
				}
			}
		})
	}
}

func TestTokyoMochaCombinesTokyoSurfacesAndCatppuccinRoles(t *testing.T) {
	t.Cleanup(func() { SetThemeName("default") })
	SetThemeName("tokyo-mocha")
	tokens := namedTokens("tokyo-mocha")
	checks := map[string]string{
		"background":       hexColor(tokens.Background.Base.Color()),
		"panel":            hexColor(tokens.Background.Raised.Base.Color()),
		"text":             hexColor(tokens.Text.Base.Color()),
		"primary accent":   hexColor(tokens.Text.Action.Primary.Color()),
		"secondary accent": hexColor(tokens.Text.Action.Secondary.Color()),
		"syntax keyword":   hexColor(ThemeSyntaxColor("syntaxKeyword")),
		"syntax string":    hexColor(ThemeSyntaxColor("syntaxString")),
		"syntax function":  hexColor(ThemeSyntaxColor("syntaxFunction")),
	}
	want := map[string]string{
		"background": "#1A1B26", "panel": "#1E2030", "text": "#CDD6F4",
		"primary accent": "#CBA6F7", "secondary accent": "#82AAFF", "syntax keyword": "#CBA6F7",
		"syntax string": "#A6E3A1", "syntax function": "#89B4FA",
	}
	for role, value := range checks {
		if got := strings.ToUpper(value); got != want[role] {
			t.Errorf("%s = %s, want %s", role, got, want[role])
		}
	}
}

func TestThemeRevisionAndDefaultReset(t *testing.T) {
	t.Cleanup(func() { SetThemeName("default") })
	SetThemeName("tokyonight")
	if got := CurrentThemeName(); got != "tokyonight" {
		t.Fatalf("CurrentThemeName() = %q", got)
	}
	revision := ThemeRevision()
	SetThemeName(" Tokyo Night ")
	if got := ThemeRevision(); got != revision {
		t.Fatalf("same theme incremented revision: %d -> %d", revision, got)
	}
	SetThemeName("catppuccin")
	if got := ThemeRevision(); got != revision+1 {
		t.Fatalf("changed theme revision = %d, want %d", got, revision+1)
	}
	SetThemeName("default")
	if got := CurrentThemeName(); got != "default" {
		t.Fatalf("reset name = %q, want default", got)
	}
	if got := ThemeRevision(); got != revision+2 {
		t.Fatalf("reset revision = %d, want %d", got, revision+2)
	}
	SetThemeName("unknown")
	if got := ThemeRevision(); got != revision+2 {
		t.Fatalf("unknown/default no-op changed revision: %d", got)
	}
}

func TestNamedThemesStayDarkAndFocusedTextContrasts(t *testing.T) {
	t.Cleanup(func() { setDarkBackground(true); SetThemeName("default") })
	for _, choice := range themeChoices[1:] {
		SetThemeName(choice.id)
		var first [3]string
		for i, dark := range []bool{true, false} {
			setDarkBackground(dark)
			tokens := CurrentThemeTokens()
			colors := [3]string{
				hexColor(tokens.Background.Base.Color()),
				hexColor(tokens.Background.Raised.Base.Color()),
				hexColor(tokens.Text.Base.Color()),
			}
			if i == 0 {
				first = colors
			} else if colors != first {
				t.Errorf("%s changed with terminal mode: dark=%v light=%v", choice.id, first, colors)
			}
			for _, pair := range [][2]color.Color{
				{tokens.Text.Action.Primary.Focused(), tokens.Background.Action.Primary.Focused()},
				{tokens.Text.Selection.Color(), tokens.Background.Selection.Color()},
				{tokens.Text.Inverse.Color(), tokens.Background.Action.Secondary.Color()},
				{tokens.Text.Action.Secondary.Focused(), tokens.Background.Action.Secondary.Focused()},
				{tokens.Text.Action.Destructive.Focused(), tokens.Background.Action.Destructive.Focused()},
			} {
				if ratio := contrastRatio(pair[0], pair[1]); ratio < 4.5 {
					t.Errorf("%s focused action contrast = %.2f in dark=%v, want >= 4.5", choice.id, ratio, dark)
				}
			}
		}
	}
}

func TestThemeRegistryConcurrentAccess(t *testing.T) {
	t.Cleanup(func() { SetThemeName("default") })
	names := []string{"tokyonight", "catppuccin", "kanagawa", "tokyo-mocha", "default"}
	var wg sync.WaitGroup
	for worker := 0; worker < 6; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				if worker%2 == 0 {
					SetThemeName(names[(worker+i)%len(names)])
				}
				_ = CurrentThemeName()
				_ = CurrentThemeTokens()
				_ = ThemeSyntaxColor("syntaxKeyword")
				_ = ThemeRevision()
			}
		}(worker)
	}
	wg.Wait()
}

func mustPalette(t *testing.T, name string) themePalette {
	t.Helper()
	palette, ok := findThemePalette(name)
	if !ok {
		t.Fatalf("palette %q not found", name)
	}
	return palette
}
