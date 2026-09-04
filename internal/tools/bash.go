package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

// Execution bounds.
const (
	// bashWaitDelay caps how long Wait blocks on output pipes still held by
	// orphaned descendants after the shell dies; past it the pipes are
	// force-closed, so a timeout or abort always returns instead of hanging.
	bashWaitDelay = 5 * time.Second
	// bashMaxBufferedBytes caps in-memory output: once exceeded, older bytes
	// are discarded (and counted) so only the recent tail is retained.
	bashMaxBufferedBytes = 256 << 10
	// bashTrimSlack amortizes buffer compaction: trimming happens only once
	// the buffer overshoots the cap by this much.
	bashTrimSlack = 64 << 10
	// maxTimeoutSecs bounds the model-supplied timeout so a bogus value
	// cannot overflow the context-deadline arithmetic.
	maxTimeoutSecs = 24 * 60 * 60
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
		f, isNum := secs.(float64)
		if !isNum || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 || f > maxTimeoutSecs {
			return nil, fmt.Errorf("bash: timeout must be a positive number of seconds up to %d", maxTimeoutSecs)
		}
		timeoutSecs = f
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(f*float64(time.Second)))
		defer cancel()
	}

	shell, shellArgs := shellConfig()
	cmd := exec.CommandContext(runCtx, shell, append(shellArgs, command)...)
	cmd.Dir = t.Cwd
	cmd.WaitDelay = bashWaitDelay

	// Combine stdout and stderr into a single ordered stream with bounded memory.
	output := newBoundedTail(bashMaxBufferedBytes, bashTrimSlack)
	cmd.Stdout = output
	cmd.Stderr = output

	// Contain the whole child tree: a process group on Unix, a Job Object on
	// Windows. On cancellation cmd.Cancel tears down the tree, not just the
	// shell; WaitDelay guarantees Wait returns even when orphaned
	// grandchildren keep the output pipes open.
	prepareProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("bash: start %s: %w", shell, err)
	}
	pg, err := attachProcessGroup(cmd)
	if err != nil {
		// Tree containment unavailable (e.g. nested-job restrictions on
		// Windows): degrade to killing just the shell rather than failing.
		pg = &processGroup{kill: func() { _ = cmd.Process.Kill() }, release: func() {}}
	}
	cmd.Cancel = func() error { pg.kill(); return nil }

	waitErr := cmd.Wait()
	pg.release()

	// ErrWaitDelay means the process ended but orphaned descendants held the
	// pipes until WaitDelay force-closed them; the command itself completed.
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		waitErr = nil
	}

	combined, droppedBytes := output.snapshot()

	// Distinguish timeout vs. abort vs. exit code.
	if runCtx.Err() == context.DeadlineExceeded {
		timedOut = true
	}

	// Truncate (tail) and persist full output when truncated.
	tr := TruncateTail(combined, 0, 0)
	text := tr.Content
	if droppedBytes > 0 {
		text = fmt.Sprintf("[... first %s of output discarded]\n", FormatSize(int(droppedBytes))) + text
	}
	var details map[string]any
	var fullPath string
	if tr.Truncated {
		fullPath = writeFullOutput(combined)
		details = map[string]any{"truncation": tr}
		suffix := " (full output could not be saved)"
		if fullPath != "" {
			details["fullOutputPath"] = fullPath
			suffix = " Full output: " + fullPath
		}
		startLine := tr.TotalLines - tr.OutputLines + 1
		endLine := tr.TotalLines
		switch {
		case tr.LastLinePartial:
			text += fmt.Sprintf("\n\n[Showing last %s of line %d.%s]",
				FormatSize(tr.OutputBytes), startLine, suffix)
		case tr.TruncatedBy == "lines":
			text += fmt.Sprintf("\n\n[Showing lines %d-%d of %d.%s]",
				startLine, endLine, tr.TotalLines, suffix)
		default:
			text += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit).%s]",
				startLine, endLine, tr.TotalLines, FormatSize(DefaultMaxBytes), suffix)
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
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s", appendStatus(text, exitMessage(exitErr)))
		}
		return nil, fmt.Errorf("%s", appendStatus(text, waitErr.Error()))
	}

	if text == "" {
		text = "(no output)"
	}
	return types.TextResult(text, details), nil
}

// boundedTail accumulates combined command output while retaining at most
// roughly max bytes (plus one trim slack window) of the most recent output;
// older bytes are counted and discarded so a chatty or runaway command cannot
// grow memory without bound.
type boundedTail struct {
	mu      sync.Mutex
	buf     []byte
	max     int
	slack   int
	written int64
	dropped int64
}

func newBoundedTail(max, slack int) *boundedTail {
	return &boundedTail{buf: make([]byte, 0, 4096), max: max, slack: slack}
}

func (b *boundedTail) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.written += int64(len(p))
	b.buf = append(b.buf, p...)
	if excess := len(b.buf) - b.max; excess > b.slack {
		b.dropped += int64(excess)
		copy(b.buf, b.buf[excess:])
		b.buf = b.buf[:len(b.buf)-excess]
	}
	return len(p), nil
}

// snapshot returns the retained tail and the number of discarded head bytes.
func (b *boundedTail) snapshot() (string, int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf), b.dropped
}

// shellConfig returns the shell and its command-string flag for the current OS.
//
// On Windows we must NOT use exec.LookPath("bash"): that resolves to
// C:\Windows\System32\bash.exe, the WSL launcher stub, which runs commands
// inside a Linux environment (failing entirely when WSL isn't installed, and
// operating on the wrong filesystem even when it is). Instead we resolve, in
// priority order:
//
//  1. the MYAGENT_SHELL env var (used verbatim), for full user control;
//  2. a real Git Bash / MSYS2 bash, explicitly excluding the System32 and
//     WindowsApps WSL stubs, so bash-style commands keep working;
//  3. cmd.exe via %ComSpec% as a guaranteed native fallback.
func shellConfig() (string, []string) {
	if shell := strings.TrimSpace(os.Getenv("MYAGENT_SHELL")); shell != "" {
		return shell, shellArgsFor(shell)
	}
	if runtime.GOOS == "windows" {
		if bash := findWindowsBash(); bash != "" {
			return bash, []string{"-c"}
		}
		comspec := os.Getenv("ComSpec")
		if comspec == "" {
			comspec = "cmd.exe"
		}
		return comspec, []string{"/C"}
	}
	return "/bin/sh", []string{"-c"}
}

// shellArgsFor picks the command-string flag for a shell path: "/C" for
// cmd.exe, "-Command" for PowerShell, and "-c" for everything else (bash/sh).
// Windows-style values must parse identically on every platform, so both "/"
// and "\" are treated as separators.
func shellArgsFor(shell string) []string {
	s := strings.ToLower(shell)
	s = strings.ReplaceAll(s, "\\", "/")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	base := strings.TrimSuffix(s, ".exe")
	switch base {
	case "cmd":
		return []string{"/C"}
	case "powershell", "pwsh":
		return []string{"-Command"}
	default:
		return []string{"-c"}
	}
}

// isWSLStub reports whether a bash path is actually the Windows WSL launcher
// stub (System32\bash.exe) or a WindowsApps alias rather than a real bash.
// Backslashes are normalized so the check works on every platform.
func isWSLStub(path string) bool {
	lower := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	return strings.Contains(lower, "/system32/") || strings.Contains(lower, "/windowsapps/")
}

// findWindowsBash locates a real Git Bash / MSYS2 bash on Windows, explicitly
// skipping the System32/WindowsApps WSL stub. Returns "" if none is found.
func findWindowsBash() string {
	// 1. Common Git-for-Windows / MSYS2 install locations.
	var candidates []string
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"} {
		if root := os.Getenv(env); root != "" {
			candidates = append(candidates,
				filepath.Join(root, "Git", "bin", "bash.exe"),
				filepath.Join(root, "Git", "usr", "bin", "bash.exe"),
			)
		}
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		candidates = append(candidates,
			filepath.Join(local, "Programs", "Git", "bin", "bash.exe"),
		)
	}
	for _, c := range candidates {
		if !isWSLStub(c) && fileExists(c) {
			return c
		}
	}

	// 2. Walk PATH, skipping the System32/WindowsApps stubs.
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, "bash.exe")
		if !isWSLStub(candidate) && fileExists(candidate) {
			return candidate
		}
	}
	return ""
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// writeFullOutput persists the complete command output to a temp file and
// returns the path, or "" on failure (cleaning up any partial file).
func writeFullOutput(content string) string {
	f, err := os.CreateTemp("", "myagent-bash-*.txt")
	if err != nil {
		return ""
	}
	name := f.Name()
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return ""
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return ""
	}
	return name
}
