package kit

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// Pane is a full rounded-border box with an optional title in the top-right
// edge of the border. It wraps one child and paints a neat frame for the few
// places the OpenCode look uses real boxes (dialogs, panels, pickers).
//
// OpenTUI counterpart: <box border borderStyle="rounded" title="...">. OpenCode
// counterpart: ui/border.ts panel rendering and the dialog surface.
type Pane struct {
	WidgetBase

	Content Widget
	// Title is drawn over the top-right border, padded by one space.
	Title string
	// Border defaults to border.base; TitleFg defaults to text.muted.
	Border  tui.Token
	TitleFg tui.Token
	// Fill optionally paints the interior (zero means transparent).
	Fill   tui.Token
	Filled bool

	inner Rect
	cache RenderCache
}

// NewPane builds a rounded pane around content with the given title. An empty
// title renders a plain rounded box.
// OpenTUI: <box borderStyle="rounded" title="…">.
func NewPane(content Widget, title string) *Pane {
	theme := DefaultTheme()
	return &Pane{
		WidgetBase: NewWidgetBase(""),
		Content:    content,
		Title:      title,
		Border:     theme.Border.Base,
		TitleFg:    theme.Text.Muted,
	}
}

// SetTitle mutates the border label idempotently.
// OpenTUI: title prop setter.
func (p *Pane) SetTitle(title string) {
	if p.Title == title {
		return
	}
	p.Title = title
	p.MarkDirty()
}

// Layout assigns the pane frame and insets the child by the 1-cell border.
// OpenTUI: yoga layout for a bordered box.
func (p *Pane) Layout(r Rect) {
	p.SetBounds(r)
	if !p.Visible {
		return
	}
	p.inner = r.Apply(InsetsAll(1))
	if p.Content != nil && WidgetVisible(p.Content) {
		p.Content.Layout(p.inner)
	}
}

// Render paints the rounded frame, optional top-right title, and child.
// OpenTUI: box render; OpenCode: panel surface.
func (p *Pane) Render(w, h int) string {
	if !p.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return p.cache.Render(w, h, subtreeRev(p), func() string {
		theme := DefaultTheme()
		border := tokenOr(p.Border, theme.Border.Base)
		titleFg := tokenOr(p.TitleFg, theme.Text.Muted)
		fg := border.Color()

		cv := lipgloss.NewCanvas(w, h)
		if p.Filled {
			fillRect(cv, 1, 1, max(0, w-2), max(0, h-2), tokenOr(p.Fill, theme.Background.Raised.Base))
		}
		if w >= 2 && h >= 2 {
			for x := 1; x < w-1; x++ {
				putGlyph(cv, x, 0, "─", fg)
				putGlyph(cv, x, h-1, "─", fg)
			}
			for y := 1; y < h-1; y++ {
				putGlyph(cv, 0, y, "│", fg)
				putGlyph(cv, w-1, y, "│", fg)
			}
			putGlyph(cv, 0, 0, "╭", fg)
			putGlyph(cv, w-1, 0, "╮", fg)
			putGlyph(cv, 0, h-1, "╰", fg)
			putGlyph(cv, w-1, h-1, "╯", fg)
			p.drawTitle(cv, w, titleFg)
		}
		if p.Content != nil && WidgetVisible(p.Content) {
			drawWidget(cv, p.Content, p.inner, p.Bounds())
		}
		return padCanvas(cv, w, h)
	})
}

// drawTitle overlays " title " on the top border, right-aligned and padded by
// one cell from the corner.
// OpenTUI: titleAlignment="right".
func (p *Pane) drawTitle(cv *lipgloss.Canvas, w int, fg tui.Token) {
	if p.Title == "" || w < 4 {
		return
	}
	label := ansi.Truncate(" "+p.Title+" ", max(0, w-2), "…")
	lw := ansi.StringWidth(label)
	start := w - 1 - lw
	if start < 1 {
		return
	}
	drawText(cv, start, 0, lw, label, fg.Color())
}

// Children exposes the single wrapped child.
func (p *Pane) Children() []Widget {
	if p.Content == nil {
		return nil
	}
	return []Widget{p.Content}
}

// Key gives the pane surface no text-input behavior.
func (p *Pane) Key(tea.KeyPressMsg) bool { return false }

// Mouse is unconsumed; the wrapped child/registry handles clicks.
func (p *Pane) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil: a pane is not a focus stop.
func (p *Pane) FocusTarget() *FocusTarget { return nil }
