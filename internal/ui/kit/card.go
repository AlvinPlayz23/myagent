package kit

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// Card is the OpenCode raised-card idiom: a left-only heavy border (┃ U+2503)
// over background.raised, horizontal padding 2, optional hover dim. It wraps
// one child and fills its whole assigned rect, so a card's surface reads as a
// block in the transcript rather than a floating label.
//
// OpenTUI counterpart: <box border={["left"]}> with a background. OpenCode
// counterpart: ui/border.ts + the user-message / block-tool cards in
// routes/session.
type Card struct {
	WidgetBase

	Content Widget
	// Border is the categorical agent accent, falling back to border.base.
	Border tui.Token
	// Fill is background.raised.base; hover dims it with Decrease.
	Fill tui.Token
	// Padding defaults to horizontal 2, vertical 0 (InsetsXY(2, 0)).
	Padding Insets
	// Hover dims the raised surface with Decrease(fill, 0.12). S1 exposes the
	// state; Phase 4 wires it to mouse motion.
	Hover bool

	inner Rect
	cache RenderCache
}

// NewCard builds a card around content using the default categorical accent,
// raised background, and the OpenCode horizontal padding of 2.
// OpenTUI: <box border={["left"]} backgroundColor={...}>.
func NewCard(content Widget) *Card {
	theme := DefaultTheme()
	return &Card{
		WidgetBase: NewWidgetBase(""),
		Content:    content,
		Border:     theme.Agent(0),
		Fill:       theme.Background.Raised.Base,
		Padding:    InsetsXY(2, 0),
	}
}

// SetHover updates the hover state idempotently.
// OpenCode: hover = decrease(backgroundColor).
func (c *Card) SetHover(hover bool) {
	if c.Hover == hover {
		return
	}
	c.Hover = hover
	c.MarkDirty()
}

// Layout assigns the card frame and the inner content rect (border column +
// horizontal padding).
// OpenTUI: yoga layout for a left-bordered box.
func (c *Card) Layout(r Rect) {
	c.SetBounds(r)
	if !c.Visible {
		return
	}
	left := 1 + c.Padding.Left
	c.inner = r.Apply(Insets{
		Top:    c.Padding.Top,
		Right:  c.Padding.Right,
		Bottom: c.Padding.Bottom,
		Left:   left,
	})
	if c.Content != nil && WidgetVisible(c.Content) {
		c.Content.Layout(c.inner)
	}
}

// Render paints the raised fill, the ┃ gutter, then the child.
// OpenTUI: box render; OpenCode: card idiom.
func (c *Card) Render(w, h int) string {
	if !c.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return c.cache.Render(w, h, subtreeRev(c), func() string {
		theme := DefaultTheme()
		fill := tokenOr(c.Fill, theme.Background.Raised.Base)
		if c.Hover {
			fill = Decrease(fill, 0.12)
		}
		border := tokenOr(c.Border, theme.Agent(0))

		cv := lipgloss.NewCanvas(w, h)
		fillRect(cv, 0, 0, w, h, fill)
		for y := 0; y < h; y++ {
			putGlyph(cv, 0, y, "┃", border.Color())
		}
		if c.Content != nil && WidgetVisible(c.Content) {
			drawWidget(cv, c.Content, c.inner, c.Bounds())
		}
		return padCanvas(cv, w, h)
	})
}

// Children exposes the single wrapped child.
func (c *Card) Children() []Widget {
	if c.Content == nil {
		return nil
	}
	return []Widget{c.Content}
}

// Key gives the card surface no text-input behavior.
func (c *Card) Key(tea.KeyPressMsg) bool { return false }

// Mouse is unconsumed in S1; hover wiring is a Phase 4 concern.
func (c *Card) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil: a card is a surface, not a focus stop.
func (c *Card) FocusTarget() *FocusTarget { return nil }
