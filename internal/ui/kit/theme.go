package kit

import "github.com/AlvinPlayz23/myagent/internal/tui"

// Theme aliases the semantic token set so kit widgets and their consumers can
// talk about one type without importing internal/tui everywhere.
//
//	fill := kit.DefaultTheme().Background.Raised.Base
//	label := kit.NewText(kit.TextOpts{Fg: kit.DefaultTheme().Text.Muted})
type Theme = tui.Tokens

// DefaultTheme returns the built-in token set. Kit code never constructs a
// raw color: every visible hue comes from a Token on this value.
// OpenCode counterpart: useTheme() / ResolvedThemeTokens.
func DefaultTheme() Theme { return tui.DefaultTokens() }

// Decrease dims a token toward the background surface. It is the kit's public
// alias for OpenCode's decrease() helper (the S1 Card hover rule).
func Decrease(t tui.Token, level float64) tui.Token { return tui.Decrease(t, level) }

// tokenOr falls back when a widget was built without a theme token. Token is
// comparable (its adaptive colors hold comparable dynamic values), so the zero
// value is a safe sentinel and never reaches a color call.
func tokenOr(t tui.Token, fallback tui.Token) tui.Token {
	if t == (tui.Token{}) {
		return fallback
	}
	return t
}
