//go:build unix

// Package ptytest is a deterministic PTY integration harness for the myagent
// TUI. It builds the myagent binary once per test run, launches it in
// interactive mode under a real pseudo-terminal with an isolated MYAGENT_DIR
// pointing at a fake OpenAI-compatible streaming server, and exposes
// send/read helpers whose assertions run against a plain-text projection of
// the terminal screen. Streaming scenarios use server-side gates rather than
// wall-clock sleeps, so mid-stream interactions are deterministic.
package ptytest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

const (
	DefaultWidth  = 100
	DefaultHeight = 30

	// pollInterval is the polling granularity of WaitFor.
	pollInterval = 10 * time.Millisecond

	// exitGrace bounds how long Close waits for the child to die after a kill.
	exitGrace = 5 * time.Second
)

// Harness is one myagent TUI process running under a PTY, plus the state
// needed to drive it: a screen projection, the raw output log, the child's
// stderr, its exit status, and its isolated MYAGENT_DIR.
type Harness struct {
	t      *testing.T
	env    *Env
	ptmx   *os.File
	cmd    *exec.Cmd
	stderr *syncBuffer

	mu     sync.Mutex
	screen *Screen
	raw    bytes.Buffer

	exited    chan struct{}
	outputEOF chan struct{}
	exitErr   error

	closeOnce sync.Once
}

// Launch builds the myagent binary (cached per test binary) and starts it at
// the default PTY size.
func Launch(t *testing.T, srv *Server) *Harness {
	return LaunchSized(t, srv, DefaultWidth, DefaultHeight)
}

// LaunchSized is Launch with an explicit initial PTY size. A non-positive
// dimension falls back to the default.
func LaunchSized(t *testing.T, srv *Server, width, height int) *Harness {
	t.Helper()
	if width <= 0 {
		width = DefaultWidth
	}
	if height <= 0 {
		height = DefaultHeight
	}
	env := NewEnv(t, srv)
	binary := buildBinary(t)

	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("open pty: %v", err)
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)}); err != nil {
		_ = ptmx.Close()
		_ = tty.Close()
		t.Fatalf("set initial pty size: %v", err)
	}

	h := &Harness{
		t:         t,
		env:       env,
		ptmx:      ptmx,
		stderr:    &syncBuffer{},
		screen:    NewScreen(width, height),
		exited:    make(chan struct{}),
		outputEOF: make(chan struct{}),
	}
	cmd := exec.Command(binary)
	cmd.Dir = env.Cwd
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = h.stderr
	cmd.Env = childEnv(env)
	// Session leader with the pty as controlling terminal, so SIGWINCH from
	// resizes (and only its own input) reaches the child.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		_ = tty.Close()
		t.Fatalf("start myagent: %v", err)
	}
	h.cmd = cmd
	_ = tty.Close() // the child keeps its own descriptor

	go func() {
		err := cmd.Wait()
		h.mu.Lock()
		h.exitErr = err
		h.mu.Unlock()
		close(h.exited)
	}()
	go h.readLoop()
	t.Cleanup(h.Close)
	return h
}

// childEnv builds the child environment, stripping every variable myagent
// uses to override the temp config, then adding the isolated ones.
func childEnv(env *Env) []string {
	skip := map[string]bool{
		"MYAGENT_DIR":          true,
		"MYAGENT_MODEL":        true,
		"MYAGENT_SESSIONS_DIR": true,
		"OPENAI_API_KEY":       true,
		"OPENAI_BASE_URL":      true,
		"HOME":                 true,
	}
	var out []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if skip[key] {
			continue
		}
		out = append(out, kv)
	}
	return append(out,
		"MYAGENT_DIR="+env.Dir,
		"HOME="+env.Dir,
		"TERM=xterm-256color",
	)
}

func (h *Harness) readLoop() {
	defer close(h.outputEOF)
	buf := make([]byte, 4096)
	for {
		n, err := h.ptmx.Read(buf)
		if n > 0 {
			h.mu.Lock()
			_, _ = h.raw.Write(buf[:n])
			h.screen.Feed(buf[:n])
			h.mu.Unlock()
		}
		if err != nil {
			return // EIO once the child exits and the slave is fully closed
		}
	}
}

// Send writes raw keys to the PTY (e.g. "hello\r", "\x03" for ctrl+c,
// "\x1b" for esc, "\x1b[5~" for PageUp).
func (h *Harness) Send(keys string) {
	h.t.Helper()
	if _, err := h.ptmx.WriteString(keys); err != nil {
		h.t.Logf("send %q: %v", keys, err)
	}
}

// ScreenText returns the current plain-text projection of the screen.
func (h *Harness) ScreenText() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.screen.Text()
}

// RawOutput returns everything the child wrote to the PTY, escape sequences
// included.
func (h *Harness) RawOutput() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.raw.String()
}

// Stderr returns the child's captured stderr.
func (h *Harness) Stderr() string { return h.stderr.String() }

// EnvDir returns the isolated MYAGENT_DIR the child runs with.
func (h *Harness) EnvDir() string { return h.env.Dir }

// SessionText returns the contents of the newest persisted session file
// under the child's MYAGENT_DIR. Session writes are flushed synchronously
// before their events reach the UI, so this is durable, race-free evidence
// of what a run actually produced (roles, stop reasons, streamed content).
func (h *Harness) SessionText() string {
	matches, err := filepath.Glob(filepath.Join(h.env.Dir, "sessions", "*.jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool {
		mi, _ := os.Stat(matches[i])
		mj, _ := os.Stat(matches[j])
		return mi.ModTime().After(mj.ModTime())
	})
	data, err := os.ReadFile(matches[0])
	if err != nil {
		return ""
	}
	return string(data)
}

// Resize changes the PTY size and resizes the screen projection.
func (h *Harness) Resize(width, height int) {
	h.t.Helper()
	if err := pty.Setsize(h.ptmx, &pty.Winsize{Rows: uint16(height), Cols: uint16(width)}); err != nil {
		h.t.Fatalf("resize pty: %v", err)
	}
	h.mu.Lock()
	h.screen.Resize(width, height)
	h.mu.Unlock()
}

// Signal delivers sig to the child process.
func (h *Harness) Signal(sig syscall.Signal) {
	h.t.Helper()
	if err := h.cmd.Process.Signal(sig); err != nil {
		h.t.Logf("signal %v: %v", sig, err)
	}
}

// Exited reports whether the child has exited.
func (h *Harness) Exited() bool {
	select {
	case <-h.exited:
		return true
	default:
		return false
	}
}

// RequireAlive fails the test if the child has already exited.
func (h *Harness) RequireAlive() {
	h.t.Helper()
	if h.Exited() {
		h.t.Fatalf("myagent exited unexpectedly; stderr:\n%s", h.Stderr())
	}
}

// WaitExit waits for the child to exit and returns cmd.Wait's error (nil for
// a clean, status-0 exit), or an error if the timeout expires first.
func (h *Harness) WaitExit(timeout time.Duration) error {
	h.t.Helper()
	select {
	case <-h.exited:
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.exitErr
	case <-time.After(timeout):
		return fmt.Errorf("myagent did not exit within %s", timeout)
	}
}

// WaitFor polls the screen projection every pollInterval until cond passes
// or timeout expires, returning the passing snapshot. It fails fast once the
// child has exited and its output is drained, without burning the timeout.
func (h *Harness) WaitFor(what string, timeout time.Duration, cond func(string) bool) string {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		text := h.ScreenText()
		if cond(text) {
			return text
		}
		if h.exitedAndDrained() {
			h.failWait(what)
		}
		if time.Now().After(deadline) {
			h.failWait(what)
		}
		time.Sleep(pollInterval)
	}
}

func (h *Harness) exitedAndDrained() bool {
	select {
	case <-h.exited:
	default:
		return false
	}
	select {
	case <-h.outputEOF:
		return true
	default:
		return false
	}
}

// failWait reports a failed wait with the diagnostics that make the failure
// debuggable: the projected screen, the child stderr, exit state, and the
// raw output dumped to a file.
func (h *Harness) failWait(what string) {
	h.t.Helper()
	screen := h.ScreenText()
	if len(screen) > 4000 {
		screen = "…\n" + screen[len(screen)-4000:]
	}
	stderr := h.Stderr()
	if len(stderr) > 2000 {
		stderr = "…\n" + stderr[len(stderr)-2000:]
	}
	exit := "still running"
	if err := func() error {
		select {
		case <-h.exited:
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.exitErr == nil {
				return fmt.Errorf("exited cleanly")
			}
			return h.exitErr
		default:
			return nil
		}
	}(); err != nil {
		exit = err.Error()
	}
	rawPath := filepath.Join(os.TempDir(), fmt.Sprintf("myagent-ptytest-raw-%d.log", time.Now().UnixNano()))
	if err := os.WriteFile(rawPath, []byte(h.RawOutput()), 0o644); err != nil {
		rawPath = fmt.Sprintf("(raw dump failed: %v)", err)
	}
	h.t.Fatalf("timed out waiting for %s\nexit: %s\nstderr:\n%s\nscreen:\n%s\nraw output: %s",
		what, exit, stderr, screen, rawPath)
	return // unreachable; keeps the compiler happy for callers using the result
}

// RequireContains waits until substr is visible on the screen.
func (h *Harness) RequireContains(substr string, timeout time.Duration) {
	h.t.Helper()
	h.WaitFor(fmt.Sprintf("screen contains %q", substr), timeout, func(s string) bool {
		return strings.Contains(s, substr)
	})
}

// RequireGone waits until substr is no longer visible on the screen.
func (h *Harness) RequireGone(substr string, timeout time.Duration) {
	h.t.Helper()
	h.WaitFor(fmt.Sprintf("screen no longer contains %q", substr), timeout, func(s string) bool {
		return !strings.Contains(s, substr)
	})
}

// RequireNoPanic fails if the child's captured stderr contains a Go panic.
func (h *Harness) RequireNoPanic() {
	h.t.Helper()
	stderr := h.Stderr()
	for _, marker := range []string{"panic:", "fatal error:"} {
		if strings.Contains(stderr, marker) {
			h.t.Fatalf("child stderr contains %q:\n%s", marker, stderr)
		}
	}
}

// WaitIdle waits until no turn is running (the "esc to cancel" status is
// gone) and the composer placeholder is visible again.
func (h *Harness) WaitIdle(timeout time.Duration) {
	h.t.Helper()
	h.RequireGone("esc to cancel", timeout)
	h.RequireContains("Send a message", timeout)
}

// QuitClean waits for the app to go idle, sends ctrl+c, and requires a clean
// exit: status 0, no panic in stderr.
func (h *Harness) QuitClean() {
	h.t.Helper()
	h.WaitIdle(45 * time.Second)
	h.Send("\x03")
	if err := h.WaitExit(45 * time.Second); err != nil {
		h.t.Fatalf("clean exit after ctrl+c: %v", err)
	}
	h.RequireNoPanic()
}

// Close kills the child if it is still running and releases the PTY. Safe to
// call multiple times.
func (h *Harness) Close() {
	h.closeOnce.Do(func() {
		if h.cmd.Process != nil {
			// Kill the whole session: the leader plus anything it spawned.
			_ = syscall.Kill(-h.cmd.Process.Pid, syscall.SIGKILL)
			_ = h.cmd.Process.Kill()
		}
		select {
		case <-h.exited:
		case <-time.After(exitGrace):
		}
		_ = h.ptmx.Close()
		select {
		case <-h.outputEOF:
		case <-time.After(exitGrace):
		}
	})
}

// syncBuffer is a goroutine-safe bytes.Buffer for capturing child stderr.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// buildBinary compiles the repo's myagent binary once per test binary run
// into a shared temp path. The worktree is shared with other agents editing
// internal/tui concurrently, so a broken intermediate state is expected:
// failures are retried for a bounded budget, and only a successful build is
// cached so later tests can retry after the tree settles.
var (
	binMu     sync.Mutex
	binPath   string
	binCached bool
)

// buildBudget bounds how long one buildBinary call retries a failing go build
// (the tree may be transiently broken by concurrent edits).
const buildBudget = 60 * time.Second

func buildBinary(t *testing.T) string {
	t.Helper()
	binMu.Lock()
	defer binMu.Unlock()
	if binCached {
		return binPath
	}
	if binPath == "" {
		dir, err := os.MkdirTemp("", "myagent-ptytest-bin-")
		if err != nil {
			t.Fatalf("temp dir for binary: %v", err)
		}
		binPath = filepath.Join(dir, "myagent")
	}
	root := repoRoot(t)
	deadline := time.Now().Add(buildBudget)
	var lastErr error
	for {
		cmd := exec.Command("go", "build", "-o", binPath, ".")
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			binCached = true
			return binPath
		}
		lastErr = fmt.Errorf("go build: %v\n%s", err, out)
		if time.Now().After(deadline) {
			break
		}
		t.Logf("go build failed, retrying (the worktree is edited concurrently):\n%v", lastErr)
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("build myagent binary: %v", lastErr)
	return ""
}

// repoRoot walks up from the test's working directory to the go.mod root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root above %s", dir)
		}
		dir = parent
	}
}
