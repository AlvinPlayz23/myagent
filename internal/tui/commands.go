package tui

import (
	"fmt"
	"strings"
)

type commandKind int

const (
	commandNone commandKind = iota
	commandHelp
	commandClear
	commandNew
	commandCompact
	commandModel
	commandEffort
	commandProviders
	commandCustomize
	commandResume
	commandRename
	commandExport
	commandInit
	commandThinking
	commandQuit
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
	// hidden keeps an entry out of /help while still parsing and
	// appearing in the command picker suggestions.
	hidden bool
}

var commandItems = []commandItem{
	{name: "/help", usage: "/help", description: "Show available commands and keybindings", kind: commandHelp},
	{name: "/model", usage: "/model [provider/model-id]", description: "Choose a model and provider", kind: commandModel, requiresArg: true},
	{name: "/models", usage: "/models [provider/model-id]", description: "Alias of /model; live-discovered models included", kind: commandModel, requiresArg: true, hidden: true},
	{name: "/effort", usage: "/effort [level]", description: "Choose reasoning effort", kind: commandEffort, requiresArg: true},
	{name: "/providers", usage: "/providers", description: "Add compatible provider API keys", kind: commandProviders},
	{name: "/customize", usage: "/customize", description: "Customize the startup style and composer", kind: commandCustomize},
	{name: "/compact", usage: "/compact", description: "Summarize older conversation context now", kind: commandCompact},
	{name: "/clear", usage: "/clear", description: "Clear the visible transcript", kind: commandClear},
	{name: "/new", usage: "/new", description: "Start a new persisted conversation", kind: commandNew},
	{name: "/resume", usage: "/resume", description: "Resume a different persisted session", kind: commandResume},
	{name: "/rename", usage: "/rename <title>", description: "Rename the current session", kind: commandRename, requiresArg: true},
	{name: "/export", usage: "/export", description: "Export this session as Markdown or HTML", kind: commandExport},
	{name: "/init", usage: "/init", description: "Analyse this repo and write an AGENTS.md", kind: commandInit},
	{name: "/thinking", usage: "/thinking [on|off]", description: "Show or hide the model's thinking in the transcript", kind: commandThinking},
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
		optionalArg := item.kind == commandModel || item.kind == commandEffort || item.kind == commandThinking
		if (item.requiresArg && arg == "" && !optionalArg) || (!item.requiresArg && arg != "" && !optionalArg) {
			return slashCommand{}, fmt.Errorf("usage: %s", item.usage)
		}
		return slashCommand{kind: item.kind, arg: arg}, nil
	}
	return slashCommand{}, fmt.Errorf("unknown command: %s (try /help)", name)
}

// initPrompt drives /init. It is sent to the model as a normal user turn, so
// the agent explores the repository with its own tools and writes the file
// itself rather than the TUI emitting a fixed template.
const initPrompt = `Analyse this repository and create an AGENTS.md file at its root.

AGENTS.md is read automatically at the start of every future run in this
directory, so it should capture what an agent cannot cheaply rediscover each
time. Work in this order:

1. Explore before writing. Read the README, the build/package manifests, the
   CI configuration, and enough of the source tree to understand how the code
   is actually laid out. Prefer verifying a command over assuming it.
2. If an AGENTS.md already exists, treat it as the base: correct anything now
   wrong, fill in gaps, and preserve instructions that are still accurate. Do
   not silently discard existing guidance.
3. Also fold in any rules already written down for other agents — for example
   .cursorrules, .cursor/rules/, .github/copilot-instructions.md, or CLAUDE.md.

Cover, where they apply:

- The exact build, test, lint, and run commands, including how to run a single
  test, and any prerequisite setup.
- The architecture: the handful of things worth knowing up front that reading
  one file would not reveal.
- Project-specific conventions that differ from the language's defaults.
- Gotchas: platform quirks, required environment variables, slow or flaky
  steps, and anything that has a non-obvious workaround.

Keep it concise and factual — aim for something a new contributor could act on
immediately, not a restatement of the README. Omit sections that do not apply
rather than padding them. When you are done, tell me what you wrote and what
you were unsure about.`

var helpText = buildHelpText()

func buildHelpText() string {
	var b strings.Builder
	b.WriteString("Commands:\n")
	for _, item := range commandItems {
		if item.hidden {
			continue
		}
		fmt.Fprintf(&b, "  %-21s %s\n", item.usage, item.description)
	}
	b.WriteString("\nKeys: enter send/queue follow-up, ctrl+v paste, ctrl+enter newline, alt+enter steer, esc cancel, ctrl+o expand tools, ctrl+c quit")
	b.WriteString("\nImages: press ctrl+v for a clipboard image, or mention a png, jpeg, gif, or webp file with @path")
	return b.String()
}
