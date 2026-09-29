package tui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/tools"
)

// toolsPicker is the multi-select panel behind /tools. It tracks which tools
// are currently disabled; toggling flips a name in the pending set, and esc
// applies it. It is intentionally independent of the live registry so the
// panel can render a stable list while the user edits.
type toolsPicker struct {
	active bool
	sel    int
	// names is the registered tool names in registry order, captured when the
	// picker opened. Reopening refreshes it so plugin tools added later appear.
	names []string
	// disabled is the pending deny set being edited (a copy of the model's
	// disabledTools). It is only published to the model when the user leaves
	// the panel.
	disabled map[string]bool
}

// open initializes the picker from the active registry and the current deny
// list. It positions the cursor on the first row.
func (p *toolsPicker) open(reg *tools.Registry, disabled []string) {
	p.active = true
	p.sel = 0
	p.names = p.names[:0]
	if reg != nil {
		p.names = append(p.names, reg.Names()...)
	}
	p.disabled = make(map[string]bool, len(disabled))
	for _, n := range disabled {
		p.disabled[n] = true
	}
}

func (p *toolsPicker) close() { p.active = false }

// move advances the cursor by delta rows, wrapping at either end.
func (p *toolsPicker) move(delta int) {
	n := len(p.names)
	if delta == 0 || n == 0 {
		return
	}
	p.sel = ((p.sel+delta)%n + n) % n
}

// toggleEnabled flips the tool at index i between enabled and disabled.
func (p *toolsPicker) toggleEnabled(i int) {
	if i < 0 || i >= len(p.names) {
		return
	}
	name := p.names[i]
	if p.disabled[name] {
		delete(p.disabled, name)
		return
	}
	p.disabled[name] = true
}

// toggle flips the row currently under the cursor.
func (p *toolsPicker) toggle() { p.toggleEnabled(p.sel) }

// isDisabled reports whether a tool is currently disabled in the pending set.
func (p *toolsPicker) isDisabled(name string) bool { return p.disabled[name] }

// height reports how many selectable rows the panel needs (plus the title).
func (p *toolsPicker) height() int { return len(p.names) + 1 }

// pending returns the pending deny list as a sorted slice. Sorting keeps the
// persisted config deterministic regardless of toggle order.
func (p *toolsPicker) pending() []string {
	if len(p.disabled) == 0 {
		return nil
	}
	out := make([]string, 0, len(p.disabled))
	for n := range p.disabled {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// enabledCount reports how many registered tools remain enabled.
func (p *toolsPicker) enabledCount() int { return len(p.names) - len(p.disabled) }

// disabledSummary renders the panel title with a count of active tools.
func (p *toolsPicker) disabledSummary() string {
	enabled := p.enabledCount()
	if enabled == len(p.names) {
		return "Tools — ↑/↓ move, space toggle, esc save"
	}
	return strings.Join([]string{
		"Tools",
		"↑/↓ move, space toggle, esc save",
	}, " — ") + " (" + strconv.Itoa(enabled) + "/" + strconv.Itoa(len(p.names)) + " enabled)"
}
