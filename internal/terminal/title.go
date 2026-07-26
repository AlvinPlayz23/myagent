// Package terminal contains small helpers for controlling an interactive terminal.
package terminal

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

const maxTitleRunes = 80

// SetTitle updates the terminal window title using OSC 0. Windows Terminal and
// most modern terminal emulators support this sequence.
func SetTitle(title string) {
	WriteTitle(os.Stdout, title)
}

// WriteTitle writes an OSC 0 title sequence to w. It is exported primarily so
// the terminal escape sequence can be tested without changing the real title.
func WriteTitle(w io.Writer, title string) {
	_, _ = fmt.Fprintf(w, "\x1b]0;%s\x07", CleanTitle(title))
}

// CleanTitle makes a single-line, bounded title that cannot inject terminal
// control sequences.
func CleanTitle(title string) string {
	var b strings.Builder
	for _, r := range title {
		if unicode.IsControl(r) || r == '\x1b' {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	words := strings.Fields(b.String())
	title = strings.Join(words, " ")
	if runes := []rune(title); len(runes) > maxTitleRunes {
		return string(runes[:maxTitleRunes-1]) + "…"
	}
	return title
}
