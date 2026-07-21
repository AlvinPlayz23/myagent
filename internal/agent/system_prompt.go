package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/myagent/myagent/internal/tools"
)

// toolSnippets are the one-line "Available tools" descriptions. Adapted from pi
// promptSnippet values for the four core tools.
var toolSnippets = map[string]string{
	"read":  "Read file contents (supports offset/limit for large files)",
	"write": "Create or overwrite a file",
	"edit":  "Edit a file using exact text replacement",
	"bash":  "Execute bash commands (ls, grep, find, etc.)",
}

// BuildSystemPrompt constructs the system prompt for a session. Adapted from pi
// buildSystemPrompt (packages/coding-agent/src/core/system-prompt.ts), trimmed
// to myagent's four core tools.
func BuildSystemPrompt(reg *tools.Registry, cwd string) string {
	promptCwd := strings.ReplaceAll(cwd, "\\", "/")

	var toolsList strings.Builder
	for _, t := range reg.All() {
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

	guidelines := strings.Join([]string{
		"- Use bash for file operations like ls, rg, find",
		"- Be concise in your responses",
		"- Show file paths clearly when working with files",
	}, "\n")

	var b strings.Builder
	b.WriteString("You are an expert coding assistant operating inside myagent, a coding agent harness. ")
	b.WriteString("You help users by reading files, executing commands, editing code, and writing new files.\n\n")
	b.WriteString("Available tools:\n")
	b.WriteString(strings.TrimRight(toolsList.String(), "\n"))
	b.WriteString("\n\nGuidelines:\n")
	b.WriteString(guidelines)
	b.WriteString("\n\nCurrent working directory: ")
	b.WriteString(promptCwd)
	if guidance := loadRepositoryGuidance(cwd); guidance != "" {
		b.WriteString("\n\nRepository instructions:\n")
		b.WriteString(guidance)
	}
	return b.String()
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
