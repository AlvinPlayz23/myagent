package kit

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// probeWidget is a deterministic test leaf with a real focus target. It
// records key/mouse input so routing tests can prove first-responder order.
type probeWidget struct {
	WidgetBase

	keys         []string
	mouseEvents  int
	consumeKey   bool
	consumeMouse bool
	focused      bool
	blurred      bool
	target       *FocusTarget
}

func newProbe(id string) *probeWidget {
	p := &probeWidget{WidgetBase: NewWidgetBase(id)}
	p.target = &FocusTarget{
		ID:     id,
		Widget: p,
		OnFocus: func() {
			p.focused = true
			p.blurred = false
			p.MarkDirty()
		},
		OnBlur: func() {
			p.blurred = true
			p.focused = false
			p.MarkDirty()
		},
	}
	return p
}

func (p *probeWidget) Layout(r Rect) { p.SetBounds(r) }

func (p *probeWidget) Render(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	lines := make([]string, h)
	label := p.ID
	if n := ansiWidth(label); n > w {
		label = truncatePlain(label, w)
	}
	for i := range lines {
		if i == 0 {
			lines[i] = label + strings.Repeat(" ", max(0, w-ansiWidth(label)))
		} else {
			lines[i] = strings.Repeat(" ", w)
		}
	}
	return strings.Join(lines, "\n")
}

func (p *probeWidget) Key(ev tea.KeyPressMsg) bool {
	p.keys = append(p.keys, ev.String())
	return p.consumeKey
}

func (p *probeWidget) Mouse(tea.MouseMsg, Rect) bool {
	p.mouseEvents++
	return p.consumeMouse
}

func (p *probeWidget) FocusTarget() *FocusTarget { return p.target }

// ansiWidth/truncatePlain are tiny ASCII helpers for the probe; production
// widgets use ansi.StringWidth/ansi.Truncate.
func ansiWidth(s string) int { return len([]rune(s)) }

func truncatePlain(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w])
}
