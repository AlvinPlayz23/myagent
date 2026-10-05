package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/tools"
)

// ModeInstructionsHeading separates the base system prompt from a profile's
// instructions; plugin.Apply appends it and ChildSystemPrompt recovers it.
const ModeInstructionsHeading = "\n\nMode instructions:\n"

const (
	disabledPrefix     = "Disabled tools (do not use or offer these; use only the available tools above): "
	guidelinesMarker   = "\nGuidelines:\n"
	cwdMarker          = "\n\nCurrent working directory: "
	repoGuidanceMarker = "\n\nRepository instructions:\n"
)

// toolSnippets are the one-line "Available tools" descriptions. Adapted from pi
// promptSnippet values for the four core tools.
var toolSnippets = map[string]string{
	"read":  "Read file contents (supports offset/limit for large files)",
	"write": "Create or overwrite a file",
	"edit":  "Edit a file using exact text replacement",
	"bash":  "Execute shell commands (ls, rg, find, etc.). For code search use rg, not bare grep -r.",
}

// BuildSystemPrompt constructs the system prompt for a session. Adapted from pi
// buildSystemPrompt (packages/coding-agent/src/core/system-prompt.ts), trimmed
// to myagent's four core tools.
//
// The intro sentence and the "Available tools" list are both derived from reg,
// so they can never disagree: disabling a tool removes it from the list, the
// capability sentence, and the guidelines. disabled carries the deny-list
// names so the prompt can explicitly override repository guidance that names
// a now-disabled tool; pass nil when nothing is disabled.
func BuildSystemPrompt(reg *tools.Registry, cwd string, disabled ...string) string {
	promptCwd := strings.ReplaceAll(cwd, "\\", "/")

	var toolsList strings.Builder
	enabled := map[string]bool{}
	for _, t := range reg.All() {
		enabled[t.Name()] = true
		snippet := toolSnippets[t.Name()]
		if snippet == "" {
			snippet = t.Description()
		}
		toolsList.WriteString("- ")
		toolsList.WriteString(t.Name())
		toolsList.WriteString(": ")
		toolsList.WriteString(snippet)
		toolsList.WriteString("\n")
	}

	guidelines := buildGuidelines(enabled)

	var b strings.Builder
	b.WriteString("You are an expert coding assistant operating inside myagent, a coding agent harness. ")
	b.WriteString(capabilitySentence(reg))
	b.WriteString("\n\n")
	if len(reg.All()) == 0 {
		b.WriteString("Available tools: (none)\n")
	} else {
		b.WriteString("Available tools:\n")
		b.WriteString(strings.TrimRight(toolsList.String(), "\n"))
		b.WriteString("\n")
	}
	if line := disabledLine(disabled, enabled); line != "" {
		b.WriteString("\n")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(guidelinesMarker)
	b.WriteString(guidelines)
	b.WriteString(cwdMarker)
	b.WriteString(promptCwd)
	if guidance := loadRepositoryGuidance(cwd); guidance != "" {
		b.WriteString(repoGuidanceMarker)
		b.WriteString(guidance)
	}
	return b.String()
}

// capabilitySentence describes what the assistant can do, derived only from
// the enabled tools so the intro can never advertise a disabled capability.
// The mapping covers the four core tools; plugin ShellTools are described
// generically with their names so they appear without hardcoding.
func capabilitySentence(reg *tools.Registry) string {
	tools := reg.All()
	if len(tools) == 0 {
		return "You currently have no tools enabled, so answer directly from context without calling tools."
	}
	caps := []string{}
	seen := map[string]bool{}
	add := func(cap string) {
		if !seen[cap] {
			seen[cap] = true
			caps = append(caps, cap)
		}
	}
	var plugins []string
	for _, t := range tools {
		switch t.Name() {
		case "read":
			add("reading files")
		case "bash":
			add("executing shell commands")
		case "edit":
			add("editing code")
		case "write":
			add("writing new files")
		default:
			plugins = append(plugins, t.Name())
		}
	}
	if len(plugins) > 0 {
		add("running plugin tools (" + strings.Join(plugins, ", ") + ")")
	}
	if len(caps) == 1 {
		return "You help users by " + caps[0] + "."
	}
	return "You help users by " + strings.Join(caps[:len(caps)-1], ", ") + " and " + caps[len(caps)-1] + "."
}

// disabledLine renders an explicit override naming tools the model must not
// use, so repository guidance (AGENTS.md) that references a disabled tool
// cannot silently re-enable it in prose. Stale names that are also absent
// from the registry are dropped; when nothing remains, the line is empty.
func disabledLine(disabled []string, enabled map[string]bool) string {
	var names []string
	for _, n := range disabled {
		if enabled[n] {
			continue
		}
		dup := false
		for _, e := range names {
			if e == n {
				dup = true
				break
			}
		}
		if !dup {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return disabledPrefix + strings.Join(names, ", ")
}

// parseDisabledLine recovers the tool names from the disabled-tools line of a
// prompt built by BuildSystemPrompt. The line is the last one before the
// Guidelines section, so only that region is searched and repository guidance
// further down cannot spoof it.
func parseDisabledLine(prompt string) []string {
	head := prompt
	if i := strings.Index(prompt, guidelinesMarker); i >= 0 {
		head = prompt[:i]
	}
	head = strings.TrimRight(head, "\n")
	line := head[strings.LastIndex(head, "\n")+1:]
	if !strings.HasPrefix(line, disabledPrefix) {
		return nil
	}
	var names []string
	for _, n := range strings.Split(strings.TrimPrefix(line, disabledPrefix), ", ") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// parseModeInstructions returns the profile instructions plugin.Apply appended
// to prompt, or "" when there are none. The suffix sits after the repository
// guidance, so when the guidance for cwd is identifiable the heading must start
// exactly where it ends; this keeps a heading quoted inside AGENTS.md from
// matching. If the guidance cannot be matched (different cwd), the last
// occurrence of the heading is used as a best effort.
func parseModeInstructions(prompt, cwd string) string {
	start := 0
	if i := strings.Index(prompt, guidelinesMarker); i >= 0 {
		start = i
	}
	rest := prompt[start:]
	i := strings.Index(rest, cwdMarker)
	if i < 0 {
		return ""
	}
	rest = rest[i+len(cwdMarker):]
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[nl:]
	} else {
		return ""
	}
	if strings.HasPrefix(rest, ModeInstructionsHeading) {
		return rest[len(ModeInstructionsHeading):]
	}
	if !strings.HasPrefix(rest, repoGuidanceMarker) {
		return ""
	}
	if guidance := loadRepositoryGuidance(cwd); guidance != "" {
		if after, ok := strings.CutPrefix(rest, repoGuidanceMarker+guidance); ok {
			if instr, ok := strings.CutPrefix(after, ModeInstructionsHeading); ok {
				return instr
			}
			return ""
		}
	}
	if j := strings.LastIndex(rest, ModeInstructionsHeading); j >= 0 {
		return rest[j+len(ModeInstructionsHeading):]
	}
	return ""
}

// ChildSystemPrompt builds the system prompt for a delegated child agent that runs with childReg. It rebuilds the prompt from childReg and carries over, from parentPrompt, the disabled-tools notice and the profile mode instructions.
func ChildSystemPrompt(parentPrompt string, childReg *tools.Registry, cwd string) string {
	if childReg == nil {
		childReg = tools.NewRegistry()
	}
	prompt := BuildSystemPrompt(childReg, cwd, parseDisabledLine(parentPrompt)...)
	if instr := parseModeInstructions(parentPrompt, cwd); instr != "" {
		prompt += ModeInstructionsHeading + instr
	}
	return prompt
}

// buildGuidelines returns the prompt's operating guidelines, tailored to the
// tools the model can actually call. Bash-specific instructions are omitted
// when bash is disabled so the prompt never asks for a tool that is not
// available (e.g. after /tools or a restrictive profile).
func buildGuidelines(enabled map[string]bool) string {
	var g []string
	if enabled["bash"] {
		g = append(g,
			"- Use bash for file operations like ls, rg, find",
			"- For code search prefer `rg -n \"pattern\" -g \"*.go\" .` — it respects .gitignore and skips dependency/build/VCS dirs automatically. Example: `rg -ni \"abort|cancel\" -g \"*.go\" .`",
			"- Never run bare `grep -r <pattern> .`. If you must use grep, always exclude dependency/build/VCS dirs, e.g. `--exclude-dir=node_modules --exclude-dir=vendor --exclude-dir=.git --exclude-dir=out --exclude-dir=dist --exclude-dir=target -I`",
			"- Scope searches to a subdir + glob where possible, pipe to `head -n 50`, set `timeout` for large trees",
		)
	}
	if enabled["read"] {
		g = append(g, "- Use the read tool to inspect files; it supports offset/limit for large files")
	}
	if enabled["write"] || enabled["edit"] {
		g = append(g, "- Prefer edit for targeted changes and write only when creating or fully replacing a file")
	}
	g = append(g, "- Be concise in your responses", "- Show file paths clearly when working with files")
	return strings.Join(g, "\n")
}

// loadRepositoryGuidance collects AGENTS.md files from the filesystem root to
// cwd. Instructions in deeper directories appear later and are more specific.
// Missing, unreadable, and non-regular entries are ignored so guidance never
// prevents the agent from starting.
func loadRepositoryGuidance(cwd string) string {
	var paths []string
	for dir := filepath.Clean(cwd); ; {
		path := filepath.Join(dir, "AGENTS.md")
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			paths = append(paths, path)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	var b strings.Builder
	for i := len(paths) - 1; i >= 0; i-- {
		contents, err := os.ReadFile(paths[i])
		if err != nil || len(contents) == 0 {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("Instructions from ")
		b.WriteString(filepath.ToSlash(paths[i]))
		b.WriteString(":\n")
		b.Write(contents)
	}
	return strings.TrimSpace(b.String())
}

// HasRepositoryGuidance reports whether cwd has any usable AGENTS.md guidance.
// It is used by the interactive UI to show a startup status matching the
// instructions included in the system prompt.
func HasRepositoryGuidance(cwd string) bool {
	return loadRepositoryGuidance(cwd) != ""
}
