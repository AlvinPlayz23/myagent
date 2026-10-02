package tui

import "sort"

// toolScope is where a /tools deny list is persisted.
type toolScope int

const (
	// scopeSession writes to session-state.json, applying to this session only.
	scopeSession toolScope = iota
	// scopeGlobal writes to config.json, applying to every session.
	scopeGlobal
	// scopeExit discards the staged deny list and closes the entire /tools
	// dialog without writing to either scope.
	scopeExit
)

type scopeChoice struct {
	scope       toolScope
	label       string
	description string
}

// scopeChoices are the two options offered after the /tools toggles are staged.
// Session scope is listed first so it is the default selection.
var scopeChoices = []scopeChoice{
	{scopeSession, "This session only", "stored for this conversation"},
	{scopeGlobal, "Global", "saved to config.json for every session"},
	{scopeExit, "Exit without saving", "discard changes and close"},
}

// scopePicker is the chained panel shown after the /tools multi-select closes:
// it asks where the staged deny list should be saved. It mirrors the
// exportPicker chained flow (list -> follow-up choice).
type scopePicker struct {
	active bool
	sel    int
	// pending is the staged deny list carried over from the tools picker. It is
	// held here so backing out to the toggles loses nothing.
	pending []string
	// count is how many tools the staged list disables, for the header.
	count int
}

// open stages pending and shows the scope panel, defaulting to session scope.
func (p *scopePicker) open(pending []string) {
	p.active = true
	p.sel = 0
	p.pending = append([]string(nil), pending...)
	p.count = len(pending)
}

func (p *scopePicker) close() {
	p.active = false
	p.pending = nil
	p.count = 0
}

// move advances the cursor by delta rows, wrapping at either end.
func (p *scopePicker) move(delta int) {
	n := len(scopeChoices)
	if delta == 0 || n == 0 {
		return
	}
	p.sel = ((p.sel+delta)%n + n) % n
}

// choice returns the currently highlighted scope.
func (p *scopePicker) choice() scopeChoice {
	if p.sel < 0 || p.sel >= len(scopeChoices) {
		return scopeChoices[0]
	}
	return scopeChoices[p.sel]
}

// height reports the panel height: the title plus one row per choice.
func (p *scopePicker) height() int { return len(scopeChoices) + 1 }

// summary renders the panel title, naming how many tools are being saved.
func (p *scopePicker) summary() string {
	if p.count == 0 {
		return "Save — enable every tool? choose a scope (q exits without saving)"
	}
	return "Save " + itoa(p.count) + " disabled tool" + plural(p.count) + " — ↑/↓ select, enter saves, esc back, q exit"
}

// itoa is a tiny local helper so this file stays dependency-free.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// plural returns "s" when n is not 1.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// unionTools merges the global and session deny lists. A tool disabled in
// either scope stays disabled: intersecting would let a session silently
// re-enable something the user disabled globally, defeating the deny list.
func unionTools(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, name := range list {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	// Sorted so the effective list and any rendering of it stay stable.
	sort.Strings(out)
	return out
}
