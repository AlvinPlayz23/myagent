package kit

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// TextOpts configures a Text/Label node.
type TextOpts struct {
	ID      string
	Content string
	// Fg is the semantic foreground token. Zero falls back to text.base at
	// render time, so a Text built before the theme is wired still renders.
	Fg   tui.Token
	Bold bool
	// Align is the horizontal alignment inside the assigned rect. AlignAuto/
	// AlignStretch/AlignStart = left, AlignCenter = center, AlignEnd = right.
	Align Align
}

// Text is a width-aware styled label. It wraps at the assigned width through
// lipgloss, pads to the assigned height, and caches by (w, h, rev).
//
// OpenTUI counterpart: <text> renderable. OpenCode counterpart: the ubiquitous
// <text> component with fg/bold props.
type Text struct {
	WidgetBase

	Content string
	Fg      tui.Token
	Bold    bool
	Align   Align

	cache RenderCache
}

// Label is the §3.2 name for a Text node.
type Label = Text

// NewText builds a text node. OpenTUI: new TextRenderable(...).
func NewText(opts TextOpts) *Text {
	return &Text{
		WidgetBase: NewWidgetBase(opts.ID),
		Content:    opts.Content,
		Fg:         opts.Fg,
		Bold:       opts.Bold,
		Align:      opts.Align,
	}
}

// SetContent mutates the label idempotently and dirties it.
// OpenTUI: text.content setter; OpenCode: reactive text prop.
func (t *Text) SetContent(s string) {
	if t.Content == s {
		return
	}
	t.Content = s
	t.MarkDirty()
}

// Layout records the assigned rect. Text has no children of its own.
// OpenTUI: text yoga node apply.
func (t *Text) Layout(r Rect) { t.SetBounds(r) }

// Render wraps to w, pads to h, and applies the token foreground.
// OpenTUI: text render with wrapMode; OpenCode: <text wrap>.
func (t *Text) Render(w, h int) string {
	if !t.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return t.cache.Render(w, h, t.Rev(), func() string {
		style := lipgloss.NewStyle().
			Foreground(tokenOr(t.Fg, DefaultTheme().Text.Base).Color()).
			Width(w).
			Height(h).
			MaxHeight(h).
			Align(lipglossPosition(t.Align))
		if t.Bold {
			style = style.Bold(true)
		}
		return fitBlock(style.Render(t.Content), w, h)
	})
}

// lipglossPosition maps the kit Align subset to lipgloss's horizontal one.
func lipglossPosition(a Align) lipgloss.Position {
	switch a {
	case AlignCenter:
		return lipgloss.Center
	case AlignEnd:
		return lipgloss.Right
	default:
		return lipgloss.Left
	}
}

// Key gives text no input behavior (labels are not editable).
// OpenTUI: text handleKeyPress no-op.
func (t *Text) Key(tea.KeyPressMsg) bool { return false }

// Mouse gives text no click behavior; a wrapping Card/List handles selection.
// OpenTUI: text handleMouse no-op.
func (t *Text) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil: plain text is not focusable.
// OpenTUI: focusable=false.
func (t *Text) FocusTarget() *FocusTarget { return nil }
