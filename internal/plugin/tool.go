package plugin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// ShellTool implements tools.Tool for a ToolDef. Template rendering happens
// at Execute time (per-session cwd/sessionID); execution semantics (bounded
// output, truncation to 50KB/2000 lines, full-output temp file, error
// contract) are delegated to tools.BashTool so they cannot drift from bash.
type ShellTool struct {
	Def       ToolDef
	Cwd       string
	SessionID string
	Deny      []string // compiled deny handled by wrapper; raw regexes here are checked via DenyCheck
	denyFn    func(command string) error
}

// ShellQuoteForCwd quotes cwd for the resolved shell.
func ShellQuoteForCwd(cwd string) string {
	return tools.QuoteArg(cwd)
}

// NewShellTool builds a ShellTool bound to a session cwd/id.
func NewShellTool(def ToolDef, cwd, sessionID string) *ShellTool {
	timeout := def.TimeoutMs
	if timeout == 0 {
		timeout = defaultTimeoutMs
	}
	return &ShellTool{Def: def, Cwd: cwd, SessionID: sessionID}
}

// SetDeny installs a pre-exec deny check (profile bashDeny). A non-nil error
// from fn aborts execution and is returned as the Execute error.
func (t *ShellTool) SetDeny(fn func(command string) error) {
	t.denyFn = fn
}

func (t *ShellTool) Name() string { return t.Def.Name }

func (t *ShellTool) Description() string { return t.Def.Description }

func (t *ShellTool) Parameters() map[string]any { return t.Def.Parameters }

// Render builds the shell command for args without executing (exported for
// tests and logging). Every substituted value — declared args as well as
// {{.cwd}} and {{.sessionId}} — is shell-quoted, so templates must not add
// their own quotes around placeholders.
func (t *ShellTool) Render(args map[string]any) (string, error) {
	data := map[string]any{
		"cwd":       tools.QuoteArg(t.Cwd),
		"sessionId": tools.QuoteArg(t.SessionID),
	}
	// Declare every parameter so missingkey=error fires only for truly
	// undeclared fields: absent args render as "".
	props := map[string]bool{}
	if p, ok := t.Def.Parameters["properties"].(map[string]any); ok {
		for k := range p {
			props[k] = true
		}
	}
	formatValue := func(v any) string {
		switch n := v.(type) {
		case string:
			return tools.QuoteArg(n)
		case bool:
			if n {
				return tools.QuoteArg("true")
			}
			return tools.QuoteArg("false")
		case float64:
			if n == float64(int64(n)) {
				return tools.QuoteArg(fmt.Sprintf("%d", int64(n)))
			}
			return tools.QuoteArg(fmt.Sprintf("%v", n))
		case nil:
			return ""
		default:
			return tools.QuoteArg(fmt.Sprintf("%v", v))
		}
	}
	if len(props) == 0 {
		// No declared properties: accept whatever args the model sent.
		for k, v := range args {
			data[k] = formatValue(v)
		}
	} else {
		for k := range props {
			data[k] = formatValue(args[k])
		}
	}
	return render(normalizeArgsVar(t.Def.Command), data)
}

func (t *ShellTool) Execute(ctx context.Context, _ string, args map[string]any) (*types.ToolResult, error) {
	rendered, err := t.Render(args)
	if err != nil {
		return nil, fmt.Errorf("%s: template error: %v", t.Def.Name, err)
	}
	if t.denyFn != nil {
		if err := t.denyFn(rendered); err != nil {
			return nil, err
		}
	}
	timeout := t.Def.TimeoutMs
	if timeout == 0 {
		timeout = defaultTimeoutMs
	}
	// Delegate to BashTool: same shell, same truncation, same error
	// contract. Timeout is converted ms → seconds (fractional allowed).
	inner := &tools.BashTool{Cwd: t.Cwd}
	secs := float64(timeout) / 1000.0
	bargs := map[string]any{"command": rendered, "timeout": secs}
	res, err := inner.Execute(ctx, "", bargs)
	if err != nil {
		// Prefix with the plugin tool name so transcripts stay readable,
		// preserving BashTool's text+status body.
		msg := strings.TrimSpace(err.Error())
		if msg == "" {
			msg = "command failed"
		}
		return nil, fmt.Errorf("%s: %s", t.Def.Name, msg)
	}
	return res, nil
}

// ShellTools builds one ShellTool per def, bound to cwd/sessionID.
func ShellTools(defs []ToolDef, cwd, sessionID string) []tools.Tool {
	out := make([]tools.Tool, 0, len(defs))
	for _, d := range defs {
		out = append(out, NewShellTool(d, cwd, sessionID))
	}
	return out
}

// FilteredRegistry rebuilds a registry containing only allowlisted tools
// (base order preserved). Unknown names are dropped (caller warns).
func FilteredRegistry(base *tools.Registry, allow []string) *tools.Registry {
	if base == nil {
		return tools.NewRegistry()
	}
	if len(allow) == 0 {
		return base
	}
	keep := map[string]bool{}
	for _, n := range allow {
		keep[n] = true
	}
	var out []tools.Tool
	for _, t := range base.All() {
		if keep[t.Name()] {
			out = append(out, t)
		}
	}
	return tools.NewRegistry(out...)
}

// ApplyTimeout converts a ToolDef/CommandDef timeoutMs to time.Duration.
func ApplyTimeout(ms int) time.Duration {
	if ms <= 0 {
		ms = defaultTimeoutMs
	}
	return time.Duration(ms) * time.Millisecond
}
