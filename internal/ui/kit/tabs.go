package kit

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TabItem is one session tab. Busy paints the spinner prefix, Unread the fading
// dot suffix; both are static no-motion-safe states in this widget.
type TabItem struct {
	ID     string
	Title  string
	Busy   bool
	Unread bool
}

// TabsOpts configures a Tabs rail.
type TabsOpts struct {
	ID string
	// Items is the ordered rail. Empty titles render as "(untitled)".
	Items []TabItem
	// Active is the selected tab ID. Empty selects the first item.
	Active string
	// NoMotion swaps the busy spinner for the "⋯" fallback.
	NoMotion bool
	// OnSelect runs on click or arrow navigation. Returning false keeps the
	// caller's active tab unchanged.
	OnSelect func(string) bool
}

// Tabs is the horizontal session rail: a 1-row strip of clickable tabs with an
// active selection surface, a busy glyph, and an unread dot. It clips at the
// assigned width and never wraps.
//
// OpenTUI counterpart: <tabselect> row. OpenCode counterpart:
// component/session-tabs.tsx (the top-bar variant).
type Tabs struct {
	WidgetBase

	Items    []TabItem
	Active   string
	NoMotion bool
	OnSelect func(string) bool

	rects []Rect
	hover int
	cache RenderCache
}

// NewTabs builds the rail with the first tab active.
func NewTabs(opts TabsOpts) *Tabs {
	t := &Tabs{
		WidgetBase: NewWidgetBase(opts.ID),
		Items:      append([]TabItem(nil), opts.Items...),
		Active:     opts.Active,
		NoMotion:   opts.NoMotion,
		OnSelect:   opts.OnSelect,
		hover:      -1,
	}
	if t.Active == "" && len(t.Items) > 0 {
		t.Active = t.Items[0].ID
	}
	return t
}

// SetItems replaces the rail and keeps the active id when it survives.
func (t *Tabs) SetItems(items []TabItem, active string) {
	if t.sameItems(items) && t.Active == active {
		return
	}
	t.Items = append(t.Items[:0], items...)
	if active != "" {
		t.Active = active
	} else if !t.has(t.Active) && len(t.Items) > 0 {
		t.Active = t.Items[0].ID
	}
	t.hover = -1
	t.MarkDirty()
}

// SetActive moves the active marker.
func (t *Tabs) SetActive(id string) {
	if t.Active == id {
		return
	}
	t.Active = id
	t.MarkDirty()
}

// ActiveID returns the current tab id.
func (t *Tabs) ActiveID() string { return t.Active }

// Len is the number of tabs.
func (t *Tabs) Len() int { return len(t.Items) }

// sameItems compares the rail by identity and paint-relevant flags.
func (t *Tabs) sameItems(items []TabItem) bool {
	if len(items) != len(t.Items) {
		return false
	}
	for i, item := range items {
		if item != t.Items[i] {
			return false
		}
	}
	return true
}

// has reports whether id is in the rail.
func (t *Tabs) has(id string) bool {
	for _, item := range t.Items {
		if item.ID == id {
			return true
		}
	}
	return false
}

// Layout records the rail width and resolves each tab's click rect.
func (t *Tabs) Layout(r Rect) {
	t.SetBounds(r)
	t.rects = t.rects[:0]
	if r.W <= 0 || r.H <= 0 {
		return
	}
	end := r.X + r.W
	x := r.X
	for i, item := range t.Items {
		w := min(t.tabWidth(item), end-x)
		t.rects = append(t.rects, Rect{X: x, Y: r.Y, W: w, H: 1})
		x += w
		if i == len(t.Items)-1 || x+1 >= end {
			break
		}
		x++ // separator column
	}
}

// tabWidth is the painted cells for one tab: pad + spinner + title + dot + pad.
func (t *Tabs) tabWidth(item TabItem) int {
	w := 2 + ansi.StringWidth(tabTitle(item.Title))
	if item.Busy {
		w += 2
	}
	if item.Unread {
		w += 2
	}
	return w
}

// tabTitle resolves the label.
func tabTitle(title string) string {
	if strings.TrimSpace(title) == "" {
		return "(untitled)"
	}
	return title
}

// Render paints the rail. The active tab uses the selection surface; busy and
// unread states use feedback/accent tokens rather than raw colors.
// OpenCode: active = bg-pulse; busy = spinner frames; unread = fading dot.
func (t *Tabs) Render(w, h int) string {
	if !t.Visible || w <= 0 || h <= 0 || len(t.Items) == 0 {
		return ""
	}
	return t.cache.Render(w, h, subtreeRev(t), func() string {
		th := DefaultTheme()
		cv := lipgloss.NewCanvas(w, 1)
		for i, item := range t.Items {
			if i >= len(t.rects) {
				break
			}
			r := t.rects[i]
			x := r.X - t.Bounds().X
			if x < 0 || x >= w {
				continue
			}
			active := item.ID == t.Active
			if active {
				fillCells(cv, x, 0, min(r.W, w-x), th.Background.Selection.Color())
			} else if i == t.hover {
				fillCells(cv, x, 0, min(r.W, w-x), th.Background.Raised.High.Color())
			}
			fg := th.Text.Muted
			if active {
				fg = th.Text.Selection
			}
			style := lipgloss.NewStyle().Foreground(fg.Color())
			if active {
				style = style.Bold(true)
			}
			frameW := 2
			if item.Busy {
				frameW += 2
			}
			if item.Unread {
				frameW += 2
			}
			if r.W < frameW+1 {
				title := ansi.Truncate(tabTitle(item.Title), r.W, "…")
				drawStyled(cv, x, 0, r.W, title, style)
				continue
			}
			cursor := x + 1
			if item.Busy {
				glyph := BrailleFrames[0]
				if t.NoMotion {
					glyph = SpinnerStillGlyph
				}
				drawStyled(cv, cursor, 0, 2, glyph, lipgloss.NewStyle().Foreground(th.Text.Feedback.Warning.Base.Color()))
				cursor += 2
			}
			titleW := r.W - frameW
			title := ansi.Truncate(tabTitle(item.Title), titleW, "…")
			drawStyled(cv, cursor, 0, titleW, title, style)
			cursor += ansi.StringWidth(title)
			if item.Unread {
				drawStyled(cv, cursor+1, 0, 1, "●", lipgloss.NewStyle().Foreground(th.Text.Feedback.Info.Base.Color()))
			}
			if i+1 < len(t.rects) && x+r.W < w {
				drawStyled(cv, x+r.W, 0, 1, "│", lipgloss.NewStyle().Foreground(th.Border.Base.Color()))
			}
		}
		return padCanvas(cv, w, 1)
	})
}

// Pointer syncs hover from an app-level motion event, including the leave case
// Root cannot route (the pointer moved onto another widget).
func (t *Tabs) Pointer(x, y int) bool {
	idx := -1
	for i, r := range t.rects {
		if r.Contains(x, y) {
			idx = i
			break
		}
	}
	if t.hover == idx {
		return false
	}
	t.hover = idx
	t.MarkDirty()
	return true
}

// Mouse selects a tab on click and tracks hover on motion.
func (t *Tabs) Mouse(ev tea.MouseMsg, r Rect) bool {
	if ev == nil || !t.Visible {
		return false
	}
	m := ev.Mouse()
	t.Pointer(m.X, m.Y)
	switch ev.(type) {
	case tea.MouseMotionMsg:
		return true
	case tea.MouseClickMsg:
		for i, tr := range t.rects {
			if !tr.Contains(m.X, m.Y) || i >= len(t.Items) {
				continue
			}
			item := t.Items[i]
			if t.OnSelect != nil && t.OnSelect(item.ID) {
				t.SetActive(item.ID)
			}
			return true
		}
	}
	return false
}

// Key moves the rail with left/right and selects the new tab.
func (t *Tabs) Key(ev tea.KeyPressMsg) bool {
	if len(t.Items) == 0 {
		return false
	}
	delta := 0
	switch ev.String() {
	case "right":
		delta = 1
	case "left":
		delta = -1
	default:
		return false
	}
	idx := 0
	for i, item := range t.Items {
		if item.ID == t.Active {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(t.Items)) % len(t.Items)
	next := t.Items[idx].ID
	if t.OnSelect != nil && !t.OnSelect(next) {
		return true
	}
	t.SetActive(next)
	return true
}

// FocusTarget is nil: the rail is pointer/keyboard-driven but not a tab stop.
func (t *Tabs) FocusTarget() *FocusTarget { return nil }
