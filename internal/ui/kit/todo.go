package kit

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// TodoStatus is the four-state todo grammar from §3.0.D.
type TodoStatus uint8

const (
	TodoPending TodoStatus = iota
	TodoActive
	TodoDone
	TodoCancelled
)

// TodoItem is one todo row.
type TodoItem struct {
	Text   string
	Status TodoStatus
}

// TodoBlockOpts configures a TodoBlock.
type TodoBlockOpts struct {
	ID string
	// Items is the ordered list; MaxRows caps the painted rows (0 = all).
	Items   []TodoItem
	MaxRows int
}

// TodoBlock is the compact single-row todo list: `[ ]` pending, `[•]`
// in-progress (warning), `[✓]` done, `[-]` cancelled (strikethrough faint).
// There is no box and no header, matching the OpenCode todo-item treatment.
//
// OpenTUI counterpart: a <text> list. OpenCode counterpart:
// component/todo-item.tsx.
type TodoBlock struct {
	WidgetBase

	Items   []TodoItem
	MaxRows int
	cache   RenderCache
}

// NewTodoBlock builds the list.
func NewTodoBlock(opts TodoBlockOpts) *TodoBlock {
	return &TodoBlock{
		WidgetBase: NewWidgetBase(opts.ID),
		Items:      append([]TodoItem(nil), opts.Items...),
		MaxRows:    opts.MaxRows,
	}
}

// SetItems replaces the todos idempotently.
func (t *TodoBlock) SetItems(items []TodoItem) {
	if sameTodos(t.Items, items) {
		return
	}
	t.Items = append(t.Items[:0], items...)
	t.MarkDirty()
}

// sameTodos compares todo slices.
func sameTodos(a, b []TodoItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Height is the number of painted rows, including the overflow row when
// MaxRows clips the list.
func (t *TodoBlock) Height() int {
	if len(t.Items) == 0 {
		return 0
	}
	if t.MaxRows > 0 && len(t.Items) > t.MaxRows {
		return t.MaxRows
	}
	return len(t.Items)
}

// Layout records the block rect.
func (t *TodoBlock) Layout(r Rect) { t.SetBounds(r) }

// Render paints the status rows. The active row keeps normal text; done and
// cancelled rows fade so the in-progress line is the visual focus.
func (t *TodoBlock) Render(w, h int) string {
	if !t.Visible || w <= 0 || h <= 0 || len(t.Items) == 0 {
		return ""
	}
	return t.cache.Render(w, h, subtreeRev(t), func() string {
		th := DefaultTheme()
		cv := lipgloss.NewCanvas(w, h)
		limit := len(t.Items)
		overflow := 0
		if t.MaxRows > 0 && limit > t.MaxRows {
			limit = t.MaxRows
			if limit > 1 {
				limit-- // last row becomes the overflow summary
			}
			overflow = len(t.Items) - limit
		}
		for y := 0; y < limit && y < h; y++ {
			item := t.Items[y]
			marker := "[ ]"
			fg := th.Text.Muted.Color()
			style := lipgloss.NewStyle().Foreground(fg)
			switch item.Status {
			case TodoActive:
				marker = "[•]"
				style = lipgloss.NewStyle().Foreground(th.Text.Feedback.Warning.Base.Color())
			case TodoDone:
				marker = "[✓]"
				style = lipgloss.NewStyle().Foreground(th.Text.Muted.Color()).
					Strikethrough(true).Faint(true)
			case TodoCancelled:
				marker = "[-]"
				style = lipgloss.NewStyle().Foreground(th.Text.Faint.Color()).
					Strikethrough(true).Faint(true)
			}
			drawStyled(cv, 0, y, 3, marker, style)
			drawStyled(cv, 4, y, max(0, w-4), item.Text, style)
		}
		if overflow > 0 && h > 0 {
			y := min(limit, h-1)
			drawStyled(cv, 0, y, w, fmt.Sprintf("… +%d more", overflow),
				lipgloss.NewStyle().Foreground(th.Text.Muted.Color()))
		}
		return padCanvas(cv, w, h)
	})
}

// Key is a no-op: the block is presentational.
func (t *TodoBlock) Key(tea.KeyPressMsg) bool { return false }

// Mouse is a no-op.
func (t *TodoBlock) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil.
func (t *TodoBlock) FocusTarget() *FocusTarget { return nil }
