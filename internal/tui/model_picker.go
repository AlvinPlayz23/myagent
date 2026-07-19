package tui

import (
	"strings"

	modelcatalog "github.com/myagent/myagent/internal/models"
)

type modelPicker struct {
	items   []modelcatalog.Model
	matched []int
	query   string
	sel     int
	active  bool
}

type providerPicker struct {
	items  []modelcatalog.Provider
	sel    int
	active bool
}

func (p *providerPicker) open(items []modelcatalog.Provider) {
	p.items = append(p.items[:0], items...)
	p.sel = 0
	p.active = true
}

func (p *providerPicker) close() {
	p.active = false
	p.items = p.items[:0]
	p.sel = 0
}

func (p *providerPicker) move(delta int) {
	if len(p.items) == 0 {
		return
	}
	p.sel = (p.sel + delta + len(p.items)) % len(p.items)
}

func (p *providerPicker) selected() (modelcatalog.Provider, bool) {
	if p.sel < 0 || p.sel >= len(p.items) {
		return modelcatalog.Provider{}, false
	}
	return p.items[p.sel], true
}

func (p *providerPicker) height() int {
	if !p.active {
		return 0
	}
	return min(10, len(p.items)+1)
}

func (p *modelPicker) open(items []modelcatalog.Model, query string) {
	p.items = append(p.items[:0], items...)
	p.query = query
	p.sel = 0
	p.active = true
	p.filter()
}

func (p *modelPicker) close() {
	p.active = false
	p.matched = p.matched[:0]
	p.query = ""
	p.sel = 0
}

func (p *modelPicker) filter() {
	p.matched = p.matched[:0]
	query := strings.ToLower(strings.TrimSpace(p.query))
	for i, item := range p.items {
		search := strings.ToLower(item.Ref() + " " + item.ProviderName + " " + item.Name)
		if query == "" || strings.Contains(search, query) {
			p.matched = append(p.matched, i)
		}
	}
	if p.sel >= len(p.matched) {
		p.sel = max(0, len(p.matched)-1)
	}
}

func (p *modelPicker) move(delta int) {
	if len(p.matched) == 0 {
		return
	}
	p.sel = (p.sel + delta + len(p.matched)) % len(p.matched)
}

func (p *modelPicker) selected() (modelcatalog.Model, bool) {
	if p.sel < 0 || p.sel >= len(p.matched) {
		return modelcatalog.Model{}, false
	}
	return p.items[p.matched[p.sel]], true
}

func (p *modelPicker) height() int {
	if !p.active {
		return 0
	}
	return min(8, len(p.matched)+1)
}
