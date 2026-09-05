package tui

import "runtime"

// promptGlyph follows the reference fallback for consoles that do not render
// the heavier Unicode arrow reliably.
func promptGlyph() string {
	if runtime.GOOS == "windows" {
		return ">"
	}
	return "❯"
}

const accentBarGlyph = "┃"

func timelineChevronUpGlyph() string {
	if runtime.GOOS == "windows" {
		return "▲"
	}
	return "▴"
}

func timelineChevronDownGlyph() string {
	if runtime.GOOS == "windows" {
		return "▼"
	}
	return "▾"
}

func timelineTickGlyph(active bool) string {
	if active {
		if runtime.GOOS == "windows" {
			return "══"
		}
		return "━━"
	}
	return " ─"
}
