package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/myagent/myagent/internal/types"
)

// BashTool executes a shell command in the working directory. Ported from pi
// bash.ts: streams stdout+stderr, supports an optional per-call timeout (in
// seconds, no default), tail-truncates the combined output, and writes the full
// output to a temp file when truncated.
type BashTool struct {
	Cwd string
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Description() string {
	// Verbatim from pi bash.ts createBashToolDefinition.
	return fmt.Sprintf(
		"Execute a bash command in the current working directory. Returns stdout and stderr. "+
			"Output is truncated to last %d lines or %dKB (whichever is hit first). If truncated, "+
			"full output is saved to a temp file. Optionally provide a timeout in seconds.",
		DefaultMaxLines, DefaultMaxBytes/1024,
	)
}

func (t *BashTool) Parameters() map[string]any {
	// Ported verbatim from pi bash.ts bashSchema.
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string", "description": "Bash command to execute"},
			"timeout": map[string]any{"type": "number", "description": "Timeout in seconds (optional, no default timeout)"},
		},
		"required": []string{"command"},
	}
}

func (t *BashTool) Execute(ctx context.Context, _ string, args map[string]any) (*types.ToolResult, error) {
	command, ok := argString(args, "command")
	if !ok || command == "" {
		return nil, fmt.Errorf("bash: missing required 'command' argument")
	}

	// Optional timeout (seconds). Applied as a derived context.
	runCtx := ctx
	var cancel context.CancelFunc
	timedOut := false
	var timeoutSecs float64
	if secs, ok := args["timeout"]; ok {
		if f, ok := secs.(float64); ok && f > 0 {
			timeoutSecs = f
			runCtx, cancel = context.WithTimeout(ctx, time.Duration(f*float64(time.Second)))
			defer cancel()
		}
	}

	shell, shellArgs := shellConfig()
	cmd := exec.CommandContext(runCtx, shell, append(shellArgs, command)...)
	cmd.Dir = t.Cwd

	// Combine stdout and stderr into a single ordered stream, guarded by a mutex.
	var mu sync.Mutex
	var buf strings.Builder
	writer := &lockedWriter{mu: &mu, sb: &buf}
	cmd.Stdout = writer
	cmd.Stderr = writer

	err := cmd.Run()

	mu.Lock()
	combined := buf.String()
	mu.Unlock()

	// Distinguish timeout vs. abort vs. exit code.
	if runCtx.Err() == context.DeadlineExceeded {
		timedOut = true
	}

	// Truncate (tail) and persist full output when truncated.
	tr := TruncateTail(combined, 0, 0)
	text := tr.Content
	var details map[string]any
	var fullPath string
	if tr.Truncated {
		fullPath = writeFullOutput(combined)
		details = map[string]any{"truncation": tr, "fullOutputPath": fullPath}
		startLine := tr.TotalLines - tr.OutputLines + 1
		endLine := tr.TotalLines
		switch {
		case tr.LastLinePartial:
			text += fmt.Sprintf("\n\n[Showing last %s of line %d. Full output: %s]",
				FormatSize(tr.OutputBytes), endLine, fullPath)
		case tr.TruncatedBy == "lines":
			text += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Full output: %s]",
				startLine, endLine, tr.TotalLines, fullPath)
		default:
			text += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Full output: %s]",
				startLine, endLine, tr.TotalLines, FormatSize(DefaultMaxBytes), fullPath)
		}
	}

	appendStatus := func(base, status string) string {
		if base != "" {
			return base + "\n\n" + status
		}
		return status
	}

	// Aborted (parent context cancelled, not our timeout).
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s", appendStatus(text, "Command aborted"))
	}
	if timedOut {
		return nil, fmt.Errorf("%s", appendStatus(text, fmt.Sprintf("Command timed out after %g seconds", timeoutSecs)))
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s", appendStatus(text, fmt.Sprintf("Command exited with code %d", exitErr.ExitCode())))
		}
		return nil, fmt.Errorf("%s", appendStatus(text, err.Error()))
	}

	if text == "" {
		text = "(no output)"
	}
	return types.TextResult(text, details), nil
}

// lockedWriter serializes concurrent stdout/stderr writes.
type lockedWriter struct {
	mu *sync.Mutex
	sb *strings.Builder
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sb.Write(p)
}

// shellConfig returns the shell and its command-string flag for the current OS.
func shellConfig() (string, []string) {
	if runtime.GOOS == "windows" {
		if bash, err := exec.LookPath("bash"); err == nil {
			return bash, []string{"-c"}
		}
		return "cmd", []string{"/C"}
	}
	return "/bin/sh", []string{"-c"}
}

// writeFullOutput persists the complete command output to a temp file and
// returns the path, or "" on failure.
func writeFullOutput(content string) string {
	f, err := os.CreateTemp("", "myagent-bash-*.txt")
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return ""
	}
	return f.Name()
}
