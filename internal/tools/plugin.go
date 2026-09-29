package tools

import (
	"path/filepath"
	"strings"
)

// Without returns a new registry with the named tools removed, preserving
// order. Unknown names are ignored. An empty name list returns r unchanged so
// callers can treat "nothing disabled" as a no-op.
func (r *Registry) Without(names []string) *Registry {
	if len(names) == 0 {
		return r
	}
	remove := make(map[string]bool, len(names))
	for _, n := range names {
		remove[n] = true
	}
	out := make([]Tool, 0, len(r.order))
	for _, t := range r.All() {
		if remove[t.Name()] {
			continue
		}
		out = append(out, t)
	}
	return NewRegistry(out...)
}

// Add appends a tool to the registry, replacing any existing tool with the
// same name (order is preserved for replacements).
func (r *Registry) Add(t Tool) {
	name := t.Name()
	if _, ok := r.byName[name]; !ok {
		r.order = append(r.order, name)
	}
	r.byName[name] = t
}

// ShellConfig returns the shell and its command-string flag for the current
// OS. Exported wrapper around shellConfig for the plugin loader so
// shell-quoting matches the shell that will actually execute the command.
func ShellConfig() (string, []string) {
	return shellConfig()
}

// QuoteArg shell-quotes one argument value for the resolved shell.
func QuoteArg(s string) string {
	shell, _ := shellConfig()
	base := strings.ToLower(filepath.Base(shell))
	base = strings.TrimSuffix(base, ".exe")
	switch base {
	case "cmd":
		return quoteCmd(s)
	case "powershell", "pwsh":
		return quotePowerShell(s)
	default:
		return quoteSh(s)
	}
}

func quoteSh(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '_' || r == '@' || r == '%' || r == '+' || r == '=' ||
			r == ':' || r == ',' || r == '.' || r == '/' || r == '-') {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func quoteCmd(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"&<>|^%") {
		return s
	}
	// Escape embedded double quotes for cmd.exe.
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func quotePowerShell(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t'\"`$") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
