package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSelectedRenderedTextUsesInclusiveCellRange(t *testing.T) {
	content := "first line\nsecond line"
	selection := textSelection{
		anchor:  textPoint{row: 0, col: 6},
		current: textPoint{row: 1, col: 5},
		dragged: true,
	}

	if got, want := selectedRenderedText(content, selection), "line\nsecond"; got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}
}

func TestSelectedRenderedTextNormalizesReverseDrag(t *testing.T) {
	selection := textSelection{
		anchor:  textPoint{row: 1, col: 5},
		current: textPoint{row: 0, col: 6},
		dragged: true,
	}

	if got, want := selectedRenderedText("first line\nsecond line", selection), "line\nsecond"; got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}
}

func TestSelectedRenderedTextStripsANSI(t *testing.T) {
	content := "\x1b[31mcolored\x1b[0m text"
	selection := textSelection{
		anchor:  textPoint{row: 0, col: 0},
		current: textPoint{row: 0, col: 6},
		dragged: true,
	}

	if got, want := selectedRenderedText(content, selection), "colored"; got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}
}

func TestRenderTextSelectionPreservesText(t *testing.T) {
	content := "alpha\nbeta"
	selection := &textSelection{
		anchor:  textPoint{row: 0, col: 1},
		current: textPoint{row: 1, col: 1},
		dragged: true,
	}

	rendered := renderTextSelection(content, selection, newTheme().selection)
	if got := ansi.Strip(rendered); got != content {
		t.Fatalf("highlight changed rendered text to %q", got)
	}
	if !strings.Contains(rendered, "\x1b[") {
		t.Fatalf("highlight did not add ANSI styling: %q", rendered)
	}
}
