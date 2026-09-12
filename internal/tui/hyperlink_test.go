package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMakeURLsClickableWrapsBareURL(t *testing.T) {
	in := "see https://example.com for details"
	out := makeURLsClickable(in)
	if !strings.Contains(out, "\033]8;;https://example.com\033\\") {
		t.Fatalf("missing OSC 8 open in %q", out)
	}
	if got := ansi.Strip(out); got != ansi.Strip(in) && !strings.Contains(got, "https://example.com") {
		t.Fatalf("visible text changed: %q", got)
	}
	// Visible text must be byte-identical apart from the added underline wraps.
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "see https://example.com for details") {
		t.Fatalf("visible text = %q, want original words", plain)
	}
}

func TestMakeURLsClickableTrimsTrailingPeriod(t *testing.T) {
	out := makeURLsClickable("visit https://example.com.")
	if strings.Contains(out, "\033]8;;https://example.com.\033\\") {
		t.Fatalf("trailing period included in href: %q", out)
	}
	if !strings.Contains(out, "\033]8;;https://example.com\033\\") {
		t.Fatalf("href missing in %q", out)
	}
}

func TestMakeURLsClickableSkipsANSIEscapes(t *testing.T) {
	in := "\033[32mgreen\033[0m https://example.com"
	out := makeURLsClickable(in)
	if !strings.Contains(out, "\033[32mgreen\033[0m") {
		t.Fatalf("ANSI escape disturbed in %q", out)
	}
	if !strings.Contains(out, "\033]8;;https://example.com\033\\") {
		t.Fatalf("URL not linkified in %q", out)
	}
}

func TestMakeURLsClickableNoDoubleUnderline(t *testing.T) {
	in := "\033[4mhttps://example.com\033[24m"
	out := makeURLsClickable(in)
	if strings.Count(out, "\033[4m") != 1 {
		t.Fatalf("already-underlined URL re-underlined: %q", out)
	}
}
