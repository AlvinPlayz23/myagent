package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The markdown cleanup pass exists to remove glamour's terminal noise: literal
// heading markers, full-width styled padding, and list continuation lines that
// snap back to the left margin. These tests pin exactly that.

func TestMarkdownCleanup(t *testing.T) {
	out := newMDRenderer().render("## Plan\n\nA short paragraph.\n", 60)
	plain := ansi.Strip(out)
	if strings.Contains(plain, "##") {
		t.Errorf("heading marker survived: %q", plain)
	}
	for _, line := range strings.Split(plain, "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("trailing padding: %q", line)
		}
	}
}

func TestMarkdownHangsListContinuations(t *testing.T) {
	md := "1. Step 1: Add `loadDir(glob)` alongside existing `GlobalPath/ProjectPath`; " +
		"load `~/.myagent/plugins.json` and `~/.myagent/plugins/*.json`.\n" +
		"   - nested note that is also long enough to wrap onto another line.\n" +
		"2. Second item.\n"
	lines := strings.Split(ansi.Strip(newMDRenderer().render(md, 60)), "\n")

	want := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lead := ansi.StringWidth(line) - ansi.StringWidth(strings.TrimLeft(line, " "))
		switch {
		case strings.HasPrefix(trimmed, "1. "):
			want = lead + 3
		case strings.HasPrefix(trimmed, "• "):
			want = lead + 2
		case strings.HasPrefix(trimmed, "2. "):
			return // reached the sibling item; the wrappable items were checked
		default:
			if want >= 0 && lead != want {
				t.Fatalf("continuation lead = %d, want %d: %q", lead, want, line)
			}
		}
	}
	t.Fatalf("no ordered list rendered:\n%s", strings.Join(lines, "\n"))
}

func TestMarkdownStaysInsideWidth(t *testing.T) {
	md := "1. A list item with `inline code` and a long run of text that wraps several times, " +
		"plus https://example.com/a/really/long/url/path to finish.\n"
	for _, width := range []int{40, 60, 80} {
		for _, line := range strings.Split(ansi.Strip(newMDRenderer().render(md, width)), "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: %d cells: %q", width, w, line)
			}
		}
	}
}

func TestTrimTrailingPaddingPreservesReset(t *testing.T) {
	got := trimTrailingPadding("\x1b[38;5;252mtext\x1b[0m   \x1b[38;5;252m \x1b[0m")
	if strings.HasSuffix(got, " ") || !strings.HasSuffix(got, "\x1b[0m") || !strings.Contains(got, "text") {
		t.Fatalf("trimTrailingPadding = %q", got)
	}
}
