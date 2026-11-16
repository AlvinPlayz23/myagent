package kit

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// JumpOpts configures the "Jump to latest" status row.
type JumpOpts struct {
	ID string
	// Label overrides the affordance text; empty uses "Jump to latest ↓".
	Label string
	// OnJump runs on click. The owner resumes sticky follow itself.
	OnJump func()
}

// Jump is the 1-row status affordance shown while transcript follow is
// suspended: a muted, clickable "Jump to latest ↓" aligned to the right edge
// (§3.0.A's status line below the ScrollBox). It paints nothing while inactive
// so the shell can reclaim the row.
//
// OpenTUI counterpart: a <text> status row. OpenCode counterpart: the
// clickable "Jump to latest ↓" status line.
type Jump struct {
	WidgetBase

	Label  string
	OnJump func()

	hover bool
	cache RenderCache
}

// NewJump builds the affordance in its active state.
func NewJump(opts JumpOpts) *Jump {
	label := opts.Label
	if label == "" {
		label = "Jump to latest ↓"
	}
	j := &Jump{
		WidgetBase: NewWidgetBase(opts.ID),
		Label:      label,
		OnJump:     opts.OnJump,
	}
	j.SetActive(false)
	return j
}

// SetActive toggles the row; inactive paints and hits nothing.
func (j *Jump) SetActive(on bool) {
	if !j.Visible && !on {
		return
	}
	if j.Visible == on {
		return
	}
	j.SetVisible(on)
	j.hover = false
}

// Active reports whether the affordance is showing.
func (j *Jump) Active() bool { return j.Visible }

// Layout records the row.
func (j *Jump) Layout(r Rect) { j.SetBounds(r) }

// Render paints the right-aligned label; hover brightens it to text.strong.
func (j *Jump) Render(w, h int) string {
	if !j.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return j.cache.Render(w, h, j.Rev(), func() string {
		th := DefaultTheme()
		cv := lipgloss.NewCanvas(w, 1)
		label := ansi.Truncate(j.Label, max(0, w-2), "…")
		fg := th.Text.Muted
		if j.hover {
			fg = th.Text.Strong
		}
		drawStyled(cv, max(0, w-2-ansi.StringWidth(label)), 0, ansi.StringWidth(label), label,
			lipgloss.NewStyle().Foreground(fg.Color()))
		return padCanvas(cv, w, 1)
	})
}

// Mouse resumes follow on click and tracks hover on motion.
func (j *Jump) Mouse(ev tea.MouseMsg, r Rect) bool {
	if ev == nil || !j.Visible {
		return false
	}
	m := ev.Mouse()
	switch ev.(type) {
	case tea.MouseMotionMsg:
		hover := r.Contains(m.X, m.Y)
		if j.hover != hover {
			j.hover = hover
			j.MarkDirty()
		}
		return hover
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft || !r.Contains(m.X, m.Y) {
			return false
		}
		if j.OnJump != nil {
			j.OnJump()
		}
		return true
	}
	return false
}

// Key is a no-op: Enter is an owner-level binding so the composer keeps it
// whenever it holds text.
func (j *Jump) Key(tea.KeyPressMsg) bool { return false }

// FocusTarget is nil: the row is a pointer affordance.
func (j *Jump) FocusTarget() *FocusTarget { return nil }
