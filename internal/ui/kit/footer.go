package kit

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ComposerFooterOpts configures the composer status row.
type ComposerFooterOpts struct {
	ID string
	// Animations toggles the one-cell spinner; false holds the ▪ fallback.
	Animations bool
}

// ComposerFooter is the 1-row composer chrome below the textarea: a one-cell
// spinner and cwd on the left, a file-context chip on the right. It is the
// OpenCode footer anatomy (§3.0.D) without reaching into the textarea.
//
// OpenTUI counterpart: a space-between flex row. OpenCode counterpart: the
// composer footer (left spinner/cwd, right file-context chip).
type ComposerFooter struct {
	WidgetBase

	Busy        bool
	Cwd         string
	File        string
	FilePending bool
	Hint        string

	spinner *OneCellSpinner
	cache   RenderCache
}

// NewComposerFooter builds the row with the SEED one-cell spinner.
func NewComposerFooter(opts ComposerFooterOpts) *ComposerFooter {
	f := &ComposerFooter{
		WidgetBase: NewWidgetBase(opts.ID),
		spinner:    NewOneCellSpinner(SeedMotion()),
	}
	f.spinner.SetAnimations(opts.Animations)
	return f
}

// SetState replaces every footer field idempotently.
func (f *ComposerFooter) SetState(busy bool, cwd, file string, pending bool, hint string) {
	if f.Busy == busy && f.Cwd == cwd && f.File == file && f.FilePending == pending && f.Hint == hint {
		return
	}
	f.Busy, f.Cwd, f.File, f.FilePending, f.Hint = busy, cwd, file, pending, hint
	f.MarkDirty()
}

// SetAnimations toggles the spinner motion.
func (f *ComposerFooter) SetAnimations(on bool) {
	if f.spinner != nil {
		f.spinner.SetAnimations(on)
		f.MarkDirty()
	}
}

// Advance drives the spinner clock from the app tick. A painted change bumps
// the footer revision so its render cache notices the child-only mutation.
func (f *ComposerFooter) Advance(d time.Duration) bool {
	if f.spinner == nil || !f.Busy {
		return false
	}
	changed := f.spinner.Advance(d)
	if changed {
		f.MarkDirty()
	}
	return changed
}

// Any reports whether the row has anything to paint. The shell hides the row
// (and reclaims its cell) when this is false.
func (f *ComposerFooter) Any() bool {
	return f.Busy || strings.TrimSpace(f.Cwd) != "" || f.File != "" || f.Hint != ""
}

// Layout records the assigned row.
func (f *ComposerFooter) Layout(r Rect) { f.SetBounds(r) }

// Render paints the footer row exactly w by h.
func (f *ComposerFooter) Render(w, h int) string {
	if !f.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return f.cache.Render(w, h, subtreeRev(f), func() string {
		th := DefaultTheme()
		cv := lipgloss.NewCanvas(w, h)
		x := 0
		if f.Busy && f.spinner != nil {
			fg := tokenOr(f.spinner.Fg, th.Text.Action.Primary).Color()
			if level := f.spinner.Level(); level > 0 && level < 1 {
				fg = blendToward(fg, th.Background.Base.Color(), 1-level)
			}
			putGlyph(cv, 0, 0, f.spinner.Glyph(), fg)
			x = 2
		}
		right := ""
		if f.File != "" {
			right = " " + f.File + " "
		} else if f.Hint != "" {
			right = f.Hint
		}
		rightW := ansi.StringWidth(right)
		if rightW > w {
			right = ansi.Truncate(strings.TrimSpace(right), w, "…")
			rightW = ansi.StringWidth(right)
		}
		leftW := max(0, w-x)
		if rightW > 0 {
			rightX := w - rightW
			leftW = max(0, rightX-x-1)
		}
		if f.Cwd != "" && leftW > 0 {
			drawStyled(cv, x, 0, leftW, ansi.Truncate(f.Cwd, leftW, "…"),
				lipgloss.NewStyle().Foreground(th.Text.Muted.Color()))
		}
		if rightW > 0 {
			rx := w - rightW
			fg := th.Text.Muted
			if f.File != "" {
				fg = th.Text.Action.Primary
				if f.FilePending {
					fg = th.Text.Feedback.Warning.Base
				}
				fillCells(cv, rx, 0, rightW, th.Background.Raised.High.Color())
			}
			drawStyled(cv, rx, 0, rightW, right, lipgloss.NewStyle().Foreground(fg.Color()))
		}
		return padCanvas(cv, w, h)
	})
}

// Key is a no-op: footer affordances are click targets owned by the shell.
func (f *ComposerFooter) Key(tea.KeyPressMsg) bool { return false }

// Mouse is a no-op; the shell maps a click to composer focus.
func (f *ComposerFooter) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil.
func (f *ComposerFooter) FocusTarget() *FocusTarget { return nil }
