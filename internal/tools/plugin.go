package tools

import (
	"path/filepath"
	"strings"
)

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
