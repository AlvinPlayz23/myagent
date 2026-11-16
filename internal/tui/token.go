package tui

import (
	"fmt"
	"image/color"
	"math"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"
)

// State mirrors OpenCode's ActionState
// (opencode/packages/theme/src/tui/schema.ts). The order matters: Token keeps
// its explicit overrides in an array indexed by State, and StateBase is always
// the undecorated value.
type State uint8

const (
	StateBase State = iota
	StateHovered
	StateFocused
	StatePressed
	StateSelected
	StateDisabled
)

// stateCount is the number of State values, used to size Token's override
// table. Keep it in sync with the constants above.
const stateCount = int(StateDisabled) + 1

// Default dimming degrees for states without an explicit theme override.
// OpenCode themes may set $hovered/$pressed/$disabled explicitly; when they do
// not, myagent derives them with Decrease instead of carrying a second
// hand-picked constant per role. Focused and Selected have no universal
// derivation (focus usually means "accent", selection usually means
// "inverted"), so they fall back to the base token unless a theme supplies an
// explicit value — the same fallback OpenCode's statefulColor uses.
const (
	hoverDecrease    = 0.12
	pressedDecrease  = 0.20
	disabledDecrease = 0.55
)

// Token is one semantic color role. It is the only color currency the theme
// exposes to widgets: callers ask for a role (theme.Text.Muted) and then for a
// state (.Hovered(), .Disabled()), never for a raw hue.
//
// Token resolves light/dark lazily through adaptiveColor, so a token built
// before the terminal reports its background still renders correctly after
// tea.BackgroundColorMsg arrives.
type Token struct {
	base  adaptiveColor
	dimTo adaptiveColor
	// overrides holds explicit per-state colors. A false set entry means the
	// state is derived from base/dimTo on demand.
	overrides [stateCount]adaptiveColor
	set       [stateCount]bool
}

// newToken builds a base token that dims toward the background surface. Every
// token in a Tokens set is created with the same dimTo (background.base), so
// hover/disabled shades are consistent across roles.
func newToken(base, dimTo adaptiveColor) Token {
	return Token{base: base, dimTo: dimTo}
}

// withState attaches an explicit state color, overriding the derived blend.
// It returns a copy so token literals stay values.
func (t Token) withState(s State, c adaptiveColor) Token {
	if s == StateBase || int(s) >= stateCount {
		return t
	}
	t.overrides[s] = c
	t.set[s] = true
	return t
}

// Color returns the base color. It is the one method widgets need when they
// build a lipgloss style; the raw palette is never handed out.
func (t Token) Color() color.Color { return t.adaptive(StateBase) }

// Base is an alias for Color, matching OpenCode's `base` token name.
func (t Token) Base() color.Color { return t.Color() }

// Hovered returns the hovered variant: an explicit theme override when one
// exists, otherwise Decrease(base, hoverDecrease).
func (t Token) Hovered() color.Color { return t.adaptive(StateHovered) }

// Focused returns the focused variant. OpenCode falls back to base when a
// theme does not define $focused; myagent does the same because "focus" has no
// universal luminance direction (borders brighten, fields keep their text).
func (t Token) Focused() color.Color { return t.adaptive(StateFocused) }

// Pressed returns the pressed variant: an explicit override, otherwise
// Decrease(base, pressedDecrease).
func (t Token) Pressed() color.Color { return t.adaptive(StatePressed) }

// Selected returns the selected variant. Like Focused it falls back to base
// unless the theme defines $selected; selected rows normally also carry a
// background token, so the foreground often stays unchanged.
func (t Token) Selected() color.Color { return t.adaptive(StateSelected) }

// Disabled returns the disabled variant: an explicit override, otherwise
// Decrease(base, disabledDecrease) — a ghost shade toward the surface.
func (t Token) Disabled() color.Color { return t.adaptive(StateDisabled) }

// State resolves any state in one call. It is the Go analogue of OpenCode's
// StatefulColor.state(states) helper.
func (t Token) State(s State) color.Color { return t.adaptive(s) }

// adaptive is the unexported core of the state accessors. It returns an
// adaptiveColor (not a resolved color) so state shades keep light/dark
// resolution and never freeze to the background mode active at construction.
func (t Token) adaptive(s State) adaptiveColor {
	if s == StateBase {
		return t.base
	}
	if int(s) < stateCount && t.set[s] {
		return t.overrides[s]
	}
	switch s {
	case StateHovered:
		return blendAdaptive(t.base, t.dimTo, hoverDecrease)
	case StatePressed:
		return blendAdaptive(t.base, t.dimTo, pressedDecrease)
	case StateDisabled:
		return blendAdaptive(t.base, t.dimTo, disabledDecrease)
	default:
		return t.base
	}
}

// Decrease dims a token toward its background surface. level is the fraction
// of background mixed in: 0 keeps the token, 1 returns background.base, values
// between produce the hover/ghost shades used by OpenCode's decrease() helper
// (opencode/packages/theme/src/tui/resolve.ts). OpenCode shifts a 100–900 hue
// scale; myagent has no hue scales yet, so the equivalent operation is an
// sRGB blend toward the surface. The result stays adaptive.
//
// Derived tokens deliberately drop explicit state overrides: a dimmed token's
// states should derive from the dimmed base, not from the old role's states.
func Decrease(t Token, level float64) Token {
	level = clampLevel(level)
	if level == 0 {
		return t
	}
	return Token{
		base:  blendAdaptive(t.base, t.dimTo, level),
		dimTo: t.dimTo,
	}
}

// Tint drifts a token toward an accent token. It is the color-math primitive
// behind the composer's agent-accent rule (§3.0.D): the composer border starts
// at a neutral border token and is tinted toward the active agent's
// categorical color. amount 0 keeps target, 1 returns accent; the result stays
// adaptive and keeps target's background for later state derivation.
//
// OpenCode expresses the same idea by compositing a tinted accent over a
// surface; it has no single tint() helper in packages/theme.
func Tint(target, accent Token, amount float64) Token {
	amount = clampLevel(amount)
	if amount == 0 {
		return target
	}
	return Token{
		base:  blendAdaptive(target.base, accent.base, amount),
		dimTo: target.dimTo,
	}
}

// blendAdaptive applies the same blend to both light and dark variants. This
// is what keeps Decrease/Tint safe before the terminal background is known.
func blendAdaptive(a, b adaptiveColor, level float64) adaptiveColor {
	return adaptiveColor{
		Light: blendColor(a.Light, b.Light, level),
		Dark:  blendColor(a.Dark, b.Dark, level),
	}
}

// blendColor mixes a toward b in sRGB. Endpoints return the original color
// value unchanged so a 1:1 mapping never round-trips through a hex string.
func blendColor(a, b color.Color, level float64) color.Color {
	level = clampLevel(level)
	if level == 0 {
		return a
	}
	if level == 1 {
		return b
	}
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	ca, ok := colorful.MakeColor(a)
	if !ok {
		return b
	}
	cb, ok := colorful.MakeColor(b)
	if !ok {
		return a
	}
	r, g, bl := ca.BlendRgb(cb, level).RGB255()
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", r, g, bl))
}

// clampLevel normalises a blend amount to [0,1]. NaN is treated as 0 so a bad
// caller can never poison a color with NaN channels.
func clampLevel(level float64) float64 {
	if math.IsNaN(level) || level < 0 {
		return 0
	}
	if level > 1 {
		return 1
	}
	return level
}

// Tokens is myagent's semantic token set. It mirrors OpenCode's
// ResolvedThemeTokens (opencode/packages/theme/src/tui/types.ts) for the groups
// GOTUI.md §3.0.C requires, plus the extension groups documented in theme.go.
//
// Widgets should take a Tokens value (or a *theme) and address roles by name:
//
//	theme.Text.Base            // primary content foreground
//	theme.Text.Muted           // secondary metadata
//	theme.Background.Raised.Base // the ┃ card surface
//	theme.Border.Base          // neutral hairline / box border
//	theme.Categorical[i]       // per-agent accent
type Tokens struct {
	Text        TextTokens
	Background  BackgroundTokens
	Border      BorderTokens
	Scrollbar   ScrollbarTokens
	Categorical []Token
	// Diff is a subset of OpenCode's diff.* palette: only the text roles
	// myagent emits today. Background/highlight/line-number tokens land with
	// the S7 diff card (GOTUI.md §3.0.D).
	Diff DiffTokens
	// Markdown carries the document-render roles. Only Link is adopted so far;
	// markdown.go still reaches it through the legacy palette view.
	Markdown MarkdownTokens
	// Decor carries non-content decorative roles. Orb is a myagent extension;
	// see the mapping table in theme.go.
	Decor DecorTokens
}

// TextTokens mirrors OpenCode's text.{base,muted,faint,action,formfield,feedback}.
// Strong is the brighter-than-base emphasis role. Selection/Inverse were adopted
// in the S1 token sweep so selection and filled badges stop borrowing action
// roles: text.selection.base and text.inverse respectively.
type TextTokens struct {
	Base  Token
	Muted Token
	// provisional: no consumer yet.
	Faint     Token
	Strong    Token
	Selection Token
	Inverse   Token
	Feedback  TextFeedbackTokens
	Action    ActionTokens
	// provisional: text.formfield lands with the S1 kit input widgets.
	Formfield Token
}

// TextFeedbackTokens mirrors text.feedback.{error,warning,success,info}. Each
// kind has a base and a muted shade, matching OpenCode's TextFeedbackDefinition.
type TextFeedbackTokens struct {
	Error   FeedbackText
	Warning FeedbackText
	Success FeedbackText
	Info    FeedbackText
}

// FeedbackText is one feedback kind's text pair. Muted is provisional until a
// chrome surface consumes it.
type FeedbackText struct {
	Base Token
	// provisional: no consumer yet.
	Muted Token
}

// ActionTokens mirrors OpenCode's action.{primary,secondary,destructive}.
// Destructive is provisional: no current surface uses it.
type ActionTokens struct {
	Primary   Token
	Secondary Token
	// provisional: no consumer yet.
	Destructive Token
}

// BackgroundTokens mirrors
// OpenCode's background.{base,raised,action,formfield,feedback}. Selection is
// the adopted selection surface, kept separate from action because selection
// is not an action.
type BackgroundTokens struct {
	Base      Token
	Raised    RaisedTokens
	Selection Token
	// provisional: no consumer yet.
	Action ActionTokens
	// provisional: background.formfield lands with the S1 kit input widgets.
	Formfield Token
	// provisional: no consumer yet.
	Feedback BackgroundFeedbackTokens
}

// RaisedTokens mirrors background.raised.{base,high,max}: the stacked card
// surfaces (user card, hovered row, dialog surface). Max is provisional.
type RaisedTokens struct {
	Base Token
	High Token
	// provisional: no consumer yet.
	Max Token
}

// BackgroundFeedbackTokens mirrors background.feedback.{error,warning,success,info}.
// The whole group is provisional until a notice surface consumes it.
type BackgroundFeedbackTokens struct {
	// provisional: all four roles are unconsumed.
	Error   Token
	Warning Token
	Success Token
	Info    Token
}

// BorderTokens mirrors border.base.
type BorderTokens struct {
	Base Token
}

// ScrollbarTokens mirrors scrollbar.base.
// provisional: the ScrollBox lands in a later slice.
type ScrollbarTokens struct {
	// provisional: no consumer yet.
	Base Token
}

// DiffTokens mirrors the text subset of OpenCode's diff.* group.
type DiffTokens struct {
	Text DiffTextTokens
}

// DiffTextTokens mirrors diff.text.{added,removed,context,hunkHeader}.
// hunkHeader is a formal role (adopted in the S1 sweep), not an extension.
type DiffTextTokens struct {
	Added      Token
	Removed    Token
	Context    Token
	HunkHeader Token
}

// MarkdownTokens mirrors the markdown.* palette. Link is the first adopted
// role; the rest land with the markdown chrome phase.
type MarkdownTokens struct {
	Link Token
}

// DecorTokens is a myagent-only decorative group (no OpenCode counterpart).
type DecorTokens struct {
	// Orb is the welcome-screen orb gradient, decor.orb.{dim,medium,bright}.
	Orb OrbTokens
}

// OrbTokens is the three-step welcome-orb gradient under decor.orb.
type OrbTokens struct {
	Dim    Token
	Medium Token
	Bright Token
}

// Agent returns the categorical accent for an agent index, following the
// §3.0.B rule: use the agent's categorical color when one exists, otherwise
// fall back to border.base. Out-of-range indices wrap so callers can pass a
// stable hash without bounds checks.
func (t Tokens) Agent(i int) Token {
	if len(t.Categorical) == 0 {
		return t.Border.Base
	}
	if i < 0 {
		i = 0
	}
	return t.Categorical[i%len(t.Categorical)]
}
