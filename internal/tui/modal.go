package tui

import (
	"fmt"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/export"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/session"
	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
)

// modalKind identifies the active modal window.
type modalKind int

const (
	modalNone modalKind = iota
	modalCommands
	modalModels
	modalSessions
	modalEffort
	modalProviders
	modalProviderKey
	modalCustomize
	modalExportFormat
	modalExportName
	modalPalette
)

// effortPicker selects a reasoning effort level.
type effortPicker struct {
	items  []llmEffortItem
	sel    int
	active bool
	value  llm.Effort
}

type llmEffortItem struct {
	label  string
	effort llm.Effort
}

// newEffortPicker builds the picker over the registered effort levels.
func newEffortPicker(current llm.Effort) effortPicker {
	p := effortPicker{value: current}
	p.items = append(p.items, llmEffortItem{label: "Default", effort: ""})
	for _, level := range llm.EffortLevels() {
		p.items = append(p.items, llmEffortItem{label: strings.ToUpper(string(level)[:1]) + string(level)[1:], effort: level})
	}
	for i, item := range p.items {
		if item.effort == current {
			p.sel = i
		}
	}
	return p
}

func (p *effortPicker) open(current llm.Effort) {
	p.value = current
	p.sel = 0
	for i, item := range p.items {
		if item.effort == current {
			p.sel = i
		}
	}
	p.active = true
}

func (p *effortPicker) close() { p.active = false; p.sel = 0 }

func (p *effortPicker) move(delta int) {
	if !p.active || len(p.items) == 0 {
		return
	}
	p.sel = (p.sel + delta + len(p.items)) % len(p.items)
}

// exportPicker chooses the export format.
type exportPicker struct {
	items  []exportItem
	sel    int
	active bool
}

type exportItem struct {
	label  string
	format export.Format
}

// newExportPicker builds the format list (mirrors export.Format values).
func newExportPicker() exportPicker {
	p := exportPicker{}
	p.items = []exportItem{
		{label: "Markdown", format: export.Markdown},
		{label: "HTML", format: export.HTML},
	}
	return p
}

func (p *exportPicker) open() {
	p.sel = 0
	p.active = true
}

func (p *exportPicker) close() { p.active = false; p.sel = 0 }

func (p *exportPicker) move(delta int) {
	if !p.active || len(p.items) == 0 {
		return
	}
	p.sel = (p.sel + delta + len(p.items)) % len(p.items)
}

// sessionPicker lists persisted sessions; the active one is pinned on top.
type sessionPicker struct {
	items     []session.Info
	sel       int
	active    bool
	currentID string
}

// open pins the active session at the top of the picker. It remains selectable
// so Enter can explicitly mean "keep working in this session."
func (p *sessionPicker) open(items []session.Info, currentID string) {
	p.items = items
	p.currentID = currentID
	if currentID != "" {
		for i, info := range items {
			if info.ID == currentID {
				p.items[0], p.items[i] = p.items[i], p.items[0]
				break
			}
		}
	}
	p.sel = 0
	p.active = len(items) > 0
}

func (p *sessionPicker) close() {
	p.items = nil
	p.sel = 0
	p.active = false
	p.currentID = ""
}

func (p *sessionPicker) move(delta int) {
	if !p.active || len(p.items) == 0 {
		return
	}
	p.sel = (p.sel + delta + len(p.items)) % len(p.items)
}

func (p *sessionPicker) selected() (session.Info, bool) {
	if !p.active || p.sel < 0 || p.sel >= len(p.items) {
		return session.Info{}, false
	}
	return p.items[p.sel], true
}

// modal is the active modal window state.
type modal struct {
	kind modalKind
	// query collects typed text for the search-filtered pickers (models,
	// palette) and the export name / provider key inputs.
	input string
	// overwrite confirms an existing export target.
	overwrite bool
}

// maxModalRows bounds the visible list inside a modal.
const maxModalRows = 10

// renderModal paints the active modal centered over the screen. It returns
// the rect it occupied.
func (a *app) renderModal(scr *engine.Screen, w, h int) engine.Rect {
	if a.modalKind == modalNone {
		return engine.Rect{}
	}
	th := a.th
	rows := a.modalRows()
	width := a.modalWidth()
	height := len(rows) + 2
	x := (w - width) / 2
	y := (h - height) / 3
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}
	rect := engine.Rect{X: x, Y: y, W: width, H: height}

	border := engine.Style{}.WithFg(th.SelectionBord).WithBg(th.BGBase)

	// Frame.
	for c := 0; c < width; c++ {
		scr.SetCell(x+c, y, engine.Cell{Ch: '─', Width: 1, Style: border})
		scr.SetCell(x+c, y+height-1, engine.Cell{Ch: '─', Width: 1, Style: border})
	}
	for r := 1; r < height-1; r++ {
		scr.SetCell(x, y+r, engine.Cell{Ch: '│', Width: 1, Style: border})
		scr.SetCell(x+width-1, y+r, engine.Cell{Ch: '│', Width: 1, Style: border})
	}
	scr.SetCell(x, y, engine.Cell{Ch: '╭', Width: 1, Style: border})
	scr.SetCell(x+width-1, y, engine.Cell{Ch: '╮', Width: 1, Style: border})
	scr.SetCell(x, y+height-1, engine.Cell{Ch: '╰', Width: 1, Style: border})
	scr.SetCell(x+width-1, y+height-1, engine.Cell{Ch: '╯', Width: 1, Style: border})

	// Clear interior and paint rows.
	inner := engine.Rect{X: x + 1, Y: y + 1, W: width - 2, H: height - 2}
	for r := 0; r < inner.H; r++ {
		for c := 0; c < inner.W; c++ {
			scr.SetCell(inner.X+c, inner.Y+r, engine.Cell{Ch: ' ', Width: 1, Style: engine.Style{}.WithBg(th.BGBase)})
		}
	}
	for i, row := range rows {
		if i >= inner.H {
			break
		}
		st := row.style
		if row.selected {
			hover := engine.Style{}.WithBg(th.BGHighlight)
			for c := 0; c < inner.W; c++ {
				scr.SetCell(inner.X+c, inner.Y+i, engine.Cell{Ch: ' ', Width: 1, Style: hover})
			}
		}
		text := row.text
		if row.selected && i > 0 {
			text = "› " + text
		} else if i > 0 {
			text = "  " + text
		}
		spans := engine.TruncateSpans([]engine.Span{{Text: text, Style: st}}, inner.W)
		scr.Line(inner.X, inner.Y+i, spans)
	}
	return rect
}

// modalRow is one line of the modal interior.
type modalRow struct {
	text     string
	style    engine.Style
	selected bool
}

func (a *app) modalTitle() string {
	switch a.modalKind {
	case modalCommands:
		return "Commands"
	case modalPalette:
		return "Command palette — type to filter"
	case modalModels:
		return "Models — type to filter"
	case modalSessions:
		return "Resume a session"
	case modalEffort:
		return "Reasoning effort"
	case modalProviders:
		return "Providers: [x] configured, enter edits key"
	case modalProviderKey:
		return fmt.Sprintf("API key for %s (enter to save, esc to cancel)", a.keyFor.Name)
	case modalCustomize:
		return "Customize"
	case modalExportFormat:
		return "Export format"
	case modalExportName:
		return "Export file name (enter to save, esc to cancel)"
	}
	return ""
}

// modalWidth adapts to the modal kind.
func (a *app) modalWidth() int {
	switch a.modalKind {
	case modalProviderKey, modalExportName:
		return min(a.w-8, 56)
	}
	return min(a.w-8, 64)
}

// modalRows materializes the modal contents, including the title row.
func (a *app) modalRows() []modalRow {
	th := a.th
	base := engine.Style{}.WithFg(th.FGDark).WithBg(th.BGBase)
	selSt := engine.Style{}.WithFg(th.FG).WithBg(th.BGHighlight)
	rows := []modalRow{{text: a.modalTitle(), style: base}}

	row := func(text string, selected bool) {
		st := base
		if selected {
			st = selSt
		}
		rows = append(rows, modalRow{text: text, style: st, selected: selected})
	}

	switch a.modalKind {
	case modalCommands, modalPalette:
		for i, idx := range a.picker.matched {
			item := a.picker.items[idx]
			row(item.name+" — "+item.description, i == a.picker.sel)
		}
	case modalModels:
		count := min(maxModalRows, len(a.models.matched))
		start := clamp(a.models.sel-count+1, 0, max(0, len(a.models.matched)-count))
		for i := start; i < start+count; i++ {
			m := a.models.items[a.models.matched[i]]
			row(m.Ref(), i == a.models.sel)
		}
	case modalSessions:
		for i, info := range a.sessions.items {
			label := info.Title
			if label == "" {
				label = info.Preview
			}
			if info.ID == a.sessions.currentID {
				label += "  (current)"
			}
			row(label, i == a.sessions.sel)
		}
	case modalEffort:
		for i, item := range a.effort.items {
			row(item.label, i == a.effort.sel)
		}
	case modalProviders:
		for i, p := range a.providers.items {
			label := p.Name
			if a.providerIsCustom != nil && a.providerIsCustom(p.ID) {
				label += "  managed as custom"
			} else if a.providerConfigured != nil && a.providerConfigured(p.ID) {
				label += "  [x]"
			}
			row(label, i == a.providers.sel)
		}
	case modalProviderKey:
		masked := strings.Repeat("•", len([]rune(a.modalInput)))
		row(masked+"█", false)
	case modalCustomize:
		for i, r := range customizeRows {
			if r.header {
				rows = append(rows, modalRow{text: r.label, style: engine.Style{}.WithFg(th.FG).WithBg(th.BGBase).Bold()})
				continue
			}
			label := r.label
			if a.customizeIsCurrent(r) {
				label += "  ✓"
			}
			row(label+"  "+r.description, i == a.customizeSel)
		}
	case modalExportFormat:
		for i, item := range a.exportPick.items {
			row(item.label, i == a.exportPick.sel)
		}
	case modalExportName:
		row(a.modalInput+"█", false)
	}
	return rows
}
