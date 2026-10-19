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
