package tui

import (
	"fmt"
	"strings"
)

type commandKind int

const (
	commandHelp commandKind = iota
	commandClear
	commandNew
	commandCompact
	commandModelID
)

type slashCommand struct {
	kind commandKind
	arg  string
}

type commandItem struct {
	name        string
	usage       string
	description string
	kind        commandKind
	requiresArg bool
}

var commandItems = []commandItem{
	{name: "/help", usage: "/help", description: "Show available commands and keybindings", kind: commandHelp},
	{name: "/model-id", usage: "/model-id <id>", description: "Use a model for subsequent turns", kind: commandModelID, requiresArg: true},
	{name: "/compact", usage: "/compact", description: "Summarize older conversation context now", kind: commandCompact},
	{name: "/clear", usage: "/clear", description: "Clear the visible transcript", kind: commandClear},
	{name: "/new", usage: "/new", description: "Start a new persisted conversation", kind: commandNew},
}

const commandPickerMaxVisible = 5

type commandPicker struct {
	items         []commandItem
	matched       []int
	sel           int
	prefix        string
	active        bool
	dismissedText string
}

func newCommandPicker() commandPicker {
	return commandPicker{items: commandItems}
}

// sync updates the picker from the textarea value. It is active only while
// editing a command name; arguments and multiline input are left to textarea.
func (p *commandPicker) sync(text string) {
	trimmed := strings.TrimLeft(text, " \t")
	if text == p.dismissedText {
		p.close()
		return
	}
	if strings.ContainsAny(trimmed, " \t\r\n") || !strings.HasPrefix(trimmed, "/") {
		p.dismissedText = ""
		p.close()
		return
	}
	p.dismissedText = ""

	p.prefix = strings.ToLower(trimmed)
	p.matched = p.matched[:0]
	for i, item := range p.items {
		if strings.HasPrefix(strings.ToLower(item.name), p.prefix) {
			p.matched = append(p.matched, i)
		}
	}
	if len(p.matched) == 0 {
		p.close()
		return
	}
	p.active = true
	if p.sel >= len(p.matched) {
		p.sel = len(p.matched) - 1
	}
}

func (p *commandPicker) close() {
	p.active = false
	p.matched = p.matched[:0]
	p.sel = 0
	p.prefix = ""
}

func (p *commandPicker) dismiss(text string) {
	p.dismissedText = text
	p.close()
}

func (p *commandPicker) move(delta int) {
	if !p.active || len(p.matched) == 0 {
		return
	}
	p.sel = (p.sel + delta + len(p.matched)) % len(p.matched)
}

func (p *commandPicker) selected() (commandItem, bool) {
	if !p.active || p.sel < 0 || p.sel >= len(p.matched) {
		return commandItem{}, false
	}
	return p.items[p.matched[p.sel]], true
}

func (p *commandPicker) height() int {
	if !p.active {
		return 0
	}
	return min(commandPickerMaxVisible, len(p.matched))
}

func (p *commandPicker) visibleRange(count int) (int, int) {
	count = min(count, p.height())
	start := p.sel - count + 1
	if start < 0 {
		start = 0
	}
	if maxStart := len(p.matched) - count; start > maxStart {
		start = maxStart
	}
	return start, start + count
}

// parseSlashCommand parses commands handled by the interactive UI. Local
// commands never become conversation messages or reach the model.
func parseSlashCommand(text string) (slashCommand, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return slashCommand{}, fmt.Errorf("not a slash command")
	}

	name := fields[0]
	arg := strings.TrimSpace(strings.TrimPrefix(text, name))
	for _, item := range commandItems {
		if item.name != name {
			continue
		}
		if (item.requiresArg && arg == "") || (!item.requiresArg && arg != "") {
			return slashCommand{}, fmt.Errorf("usage: %s", item.usage)
		}
		return slashCommand{kind: item.kind, arg: arg}, nil
	}
	return slashCommand{}, fmt.Errorf("unknown command: %s (try /help)", name)
}

var helpText = buildHelpText()

func buildHelpText() string {
	var b strings.Builder
	b.WriteString("Commands:\n")
	for _, item := range commandItems {
		fmt.Fprintf(&b, "  %-21s %s\n", item.usage, item.description)
	}
	b.WriteString("\nKeys: enter send/steer, alt+enter queue follow-up, esc cancel, ctrl+o expand tools, ctrl+c quit")
	return b.String()
}
