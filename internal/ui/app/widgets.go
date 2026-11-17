package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/AlvinPlayz23/myagent/internal/ui/kit"
)

// composerWidget wraps the legacy textarea render. It owns the scroll-key
// interception: only PageUp/PageDown/Ctrl+G and an empty-prompt Home/End bubble
// to the ScrollBox; everything else is the legacy onKey path.
type composerWidget struct {
	kit.WidgetBase

	shell   *Shell
	host    tui.KitHost
	content string
	target  *kit.FocusTarget
}

func newComposer(shell *Shell, host tui.KitHost) *composerWidget {
	c := &composerWidget{
		WidgetBase: kit.NewWidgetBase("kit.composer"),
		shell:      shell,
		host:       host,
	}
	c.target = &kit.FocusTarget{
		ID:     c.ID,
		Widget: c,
		OnFocus: func() {
			c.MarkDirty()
		},
		OnBlur: func() {
			c.MarkDirty()
		},
	}
	return c
}

func (c *composerWidget) setContent(s string) {
	if c.content == s {
		return
	}
	c.content = s
	c.MarkDirty()
}

func (c *composerWidget) Layout(r kit.Rect) { c.SetBounds(r) }

func (c *composerWidget) Render(w, h int) string { return fitBlock(c.content, w, h) }

func (c *composerWidget) Key(ev tea.KeyPressMsg) bool {
	switch ev.Keystroke() {
	case "pgup", "pgdown", "ctrl+g":
		return false
	case "home", "end":
		if c.host.KitComposerEmpty() {
			return false
		}
	}
	c.shell.emit(c.host.KitKey(ev))
	c.shell.markBlocksDirty()
	return true
}

func (c *composerWidget) Mouse(ev tea.MouseMsg, _ kit.Rect) bool {
	if ev == nil {
		return false
	}
	if _, wheel := kit.WheelDelta(ev); wheel {
		return false
	}
	if _, ok := ev.(tea.MouseClickMsg); ok {
		c.shell.root.Focus.Focus(c.ID)
		return true
	}
	return false
}

func (c *composerWidget) FocusTarget() *kit.FocusTarget { return c.target }

// blockWidget is one transcript run. It delegates paint to the existing
// transcript block cache and only applies the selection overlay on top.
type blockWidget struct {
	kit.WidgetBase

	shell    *Shell
	host     tui.KitHost
	run      tui.KitBlock
	startRow int
}

func newBlock(shell *Shell, host tui.KitHost, run tui.KitBlock, idx int) *blockWidget {
	return &blockWidget{
		WidgetBase: kit.NewWidgetBase("kit.block." + itoa(idx)),
		shell:      shell,
		host:       host,
		run:        run,
	}
}

func (b *blockWidget) setContentStart(row int) { b.startRow = row }

func (b *blockWidget) Layout(r kit.Rect) { b.SetBounds(r) }

func (b *blockWidget) MeasureHeight(width int) int {
	return max(1, lineCount(b.run.Render(width)))
}

func (b *blockWidget) Render(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	content := b.host.KitOverlay(b.run.Render(w), b.startRow)
	return fitBlock(content, w, h)
}

func (b *blockWidget) Key(tea.KeyPressMsg) bool { return false }

func (b *blockWidget) Mouse(ev tea.MouseMsg, r kit.Rect) bool {
	if ev == nil {
		return false
	}
	if _, wheel := kit.WheelDelta(ev); wheel {
		return false
	}
	m := ev.Mouse()
	row := b.startRow + m.Y - r.Y
	col := m.X - r.X
	if row < b.startRow || col < 0 || m.Y < r.Y || m.Y >= r.Y+r.H {
		return false
	}
	switch msg := ev.(type) {
	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return false
		}
		b.shell.emit(b.host.KitMouseClick(row, col, msg.Mod))
	case tea.MouseMotionMsg:
		if msg.Button != tea.MouseLeft {
			return false
		}
		b.shell.emit(b.host.KitMouseMotion(row, col, msg.Button))
	case tea.MouseReleaseMsg:
		b.shell.emit(b.host.KitMouseRelease(row, col))
	default:
		return false
	}
	b.shell.markBlocksDirty()
	return true
}

func (b *blockWidget) FocusTarget() *kit.FocusTarget { return nil }

// welcomeWidget caches the current startup frame; the shell refreshes it when
// the host advances its welcome revision.
type welcomeWidget struct {
	kit.WidgetBase

	shell   *Shell
	host    tui.KitHost
	content string
}

func newWelcome(shell *Shell, host tui.KitHost) *welcomeWidget {
	return &welcomeWidget{
		WidgetBase: kit.NewWidgetBase("kit.welcome"),
		shell:      shell,
		host:       host,
	}
}

func (w *welcomeWidget) setContent(s string) {
	if w.content == s {
		return
	}
	w.content = s
	w.MarkDirty()
}

func (w *welcomeWidget) Layout(r kit.Rect) { w.SetBounds(r) }

func (w *welcomeWidget) MeasureHeight(int) int { return lineCount(w.content) }

func (w *welcomeWidget) Render(width, h int) string { return fitBlock(w.content, width, h) }

func (w *welcomeWidget) Key(tea.KeyPressMsg) bool { return false }

func (w *welcomeWidget) Mouse(tea.MouseMsg, kit.Rect) bool { return false }

func (w *welcomeWidget) FocusTarget() *kit.FocusTarget { return nil }

// spacer is the one-row blank separator matching transcript.render.
type spacer struct {
	kit.WidgetBase
}

func newSpacer() *spacer { return &spacer{WidgetBase: kit.NewWidgetBase("")} }

func (s *spacer) Layout(r kit.Rect) { s.SetBounds(r) }

func (s *spacer) Render(w, h int) string { return fitBlock("", w, h) }

func (s *spacer) Key(tea.KeyPressMsg) bool { return false }

func (s *spacer) Mouse(tea.MouseMsg, kit.Rect) bool { return false }

func (s *spacer) FocusTarget() *kit.FocusTarget { return nil }

// itoa keeps IDs allocation-light and deterministic for hit-registry tests.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
