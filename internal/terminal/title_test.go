package terminal

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteTitle(t *testing.T) {
	var output bytes.Buffer
	WriteTitle(&output, "myagent - first prompt")
	if got, want := output.String(), "\x1b]0;myagent - first prompt\x07"; got != want {
		t.Fatalf("title sequence = %q, want %q", got, want)
	}
}

func TestCleanTitleNormalizesAndSanitizes(t *testing.T) {
	got := CleanTitle(" first\n prompt\x1b]2;injected\x07 ")
	if want := "first prompt ]2;injected"; got != want {
		t.Fatalf("CleanTitle() = %q, want %q", got, want)
	}
}

func TestCleanTitleTruncatesRunes(t *testing.T) {
	got := CleanTitle(strings.Repeat("界", 81))
	if want := strings.Repeat("界", 79) + "…"; got != want {
		t.Fatalf("CleanTitle() = %q, want %q", got, want)
	}
}
