package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSelectedTextUsesInclusiveCellRange(t *testing.T) {
	rows := rowsFromLines("first line", "second line")
	selection := textSelection{
		anchor:  textPoint{row: 0, col: 6},
		current: textPoint{row: 1, col: 5},
		dragged: true,
	}

	if got, want := copyRowsText(rows, selection), "line\nsecond"; got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}
}

func TestSelectedTextNormalizesReverseDrag(t *testing.T) {
	rows := rowsFromLines("first line", "second line")
	selection := textSelection{
		anchor:  textPoint{row: 1, col: 5},
		current: textPoint{row: 0, col: 6},
		dragged: true,
	}

	if got, want := copyRowsText(rows, selection), "line\nsecond"; got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}
}

func TestSelectedTextStripsANSI(t *testing.T) {
	rows := rowsFromLines("\x1b[31mcolored\x1b[0m text")
	selection := textSelection{
		anchor:  textPoint{row: 0, col: 0},
		current: textPoint{row: 0, col: 6},
		dragged: true,
	}

	if got, want := copyRowsText(rows, selection), "colored"; got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}
}

func TestSelectionOverlayPreservesText(t *testing.T) {
	rows := rowsFromLines("alpha", "beta")
	selection := &textSelection{
		anchor:  textPoint{row: 0, col: 1},
		current: textPoint{row: 1, col: 1},
		dragged: true,
	}

	rendered := strings.Join(renderRowsSelection(rows, selection, newTheme().selection), "\n")
	if got := ansi.Strip(rendered); got != "alpha\nbeta" {
		t.Fatalf("highlight changed rendered text to %q", got)
	}
	if !strings.Contains(rendered, "\x1b[") {
		t.Fatalf("highlight did not add ANSI styling: %q", rendered)
	}
}

func TestSelectionExcludesGutterChrome(t *testing.T) {
	// A diff-style row: the +/- prefix is gutter chrome, the text is content.
	row := layoutRow{
		kind:       rowDiff,
		blockID:    1,
		spans:      []layoutSpan{{text: "+added line", style: lipgloss.NewStyle()}},
		gutterCols: 1,
	}
	selection := textSelection{
		anchor:  textPoint{row: 0, col: 0},
		current: textPoint{row: 0, col: row.width() - 1},
		dragged: true,
	}

	if got, want := copyRowsText([]layoutRow{row}, selection), "added line"; got != want {
		t.Fatalf("selected text = %q, want %q without the gutter prefix", got, want)
	}

	overlay := renderRowSelection(row, 0, row.width(), newTheme().selection)
	if got := ansi.Strip(overlay); got != "+added line" {
		t.Fatalf("overlay changed visible text: %q", got)
	}
}

func TestSelectionJoinsWrappedRows(t *testing.T) {
	long := "the quick brown fox jumps over the lazy dog"
	base := layoutRow{
		kind:    rowAssistant,
		blockID: 1,
		spans:   []layoutSpan{{text: long, raw: true}},
	}
	rows := wrapRow(base, 20)
	if len(rows) < 2 || !rows[1].wrapped {
		t.Fatalf("wrapRow produced %d rows, want a continuation", len(rows))
	}

	selection := textSelection{
		anchor:  textPoint{row: 0, col: 0},
		current: textPoint{row: len(rows) - 1, col: rows[len(rows)-1].width() - 1},
		dragged: true,
	}
	if got := copyRowsText(rows, selection); got != long {
		t.Fatalf("wrapped selection = %q, want %q joined without newlines", got, long)
	}
}

func TestUserRowSelectionSkipsPadding(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.addUser("hello world")
	rows := tr.layout(40)
	if len(rows) != 1 {
		t.Fatalf("user block laid out %d rows, want 1", len(rows))
	}
	// The rendered line is padded to the full width; selection columns must
	// still address the text cells, and copying must skip the padding.
	if got := rows[0].width(); got > 40 {
		t.Fatalf("user row width = %d, want <= 40", got)
	}
	selection := textSelection{
		anchor:  textPoint{row: 0, col: 2},
		current: textPoint{row: 0, col: 6},
		dragged: true,
	}
	if got, want := copyRowsText(rows, selection), "hello"; got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}
}
