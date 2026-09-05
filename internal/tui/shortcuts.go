package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// shortcutHint is intentionally structured like Grok Build's HintItem: the
// renderer owns separators and truncation, while callers only describe intent.
type shortcutHint struct {
	key    string
	label  string
	pinned bool
}

func effectiveShortcutHints(hints []shortcutHint, maxVisible int, help *shortcutHint) []shortcutHint {
	if maxVisible <= 0 {
		maxVisible = len(hints)
	}
	pinned := 0
	for _, hint := range hints {
		if hint.pinned {
			pinned++
		}
	}
	budget := max(0, maxVisible-pinned)
	out := make([]shortcutHint, 0, min(len(hints), maxVisible)+1)
	used := 0
	for _, hint := range hints {
		if hint.pinned || used < budget {
			out = append(out, hint)
			if !hint.pinned {
				used++
			}
		}
	}
	if help != nil {
		out = append(out, *help)
	}
	return out
}

func renderShortcuts(hints []shortcutHint, width int, th *theme, compact bool) string {
	if width <= 0 {
		return ""
	}
	if compact {
		help := shortcutHint{key: "?", label: "shortcuts", pinned: true}
		hints = effectiveShortcutHints(hints, 4, &help)
	}
	parts := make([]string, 0, len(hints))
	for _, hint := range hints {
		parts = append(parts, th.shortcutKey.Render(hint.key)+th.shortcutLabel.Render(": "+hint.label))
	}
	return truncateColumns(strings.Join(parts, th.shortcutSeparator.Render("  │  ")), width)
}

// truncateColumns keeps ANSI-styled content inside a terminal width. The
// ellipsis replaces the final visible cell, never an arbitrary byte.
func truncateColumns(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.TruncateWc(s, width, "…")
}
