package tools

import (
	"bytes"
	"context"
	"math"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBoundedTailKeepsRecentOutputAndCountsDrops(t *testing.T) {
	b := newBoundedTail(1024, 256)
	const chunks = 100
	for i := 0; i < chunks; i++ {
		if _, err := b.Write(bytes.Repeat([]byte{byte('a' + i%26)}, 100)); err != nil {
			t.Fatal(err)
		}
	}
	s, dropped := b.snapshot()
	if dropped <= 0 {
		t.Fatalf("dropped = %d, want head bytes to be discarded", dropped)
	}
	if len(s) > 1024+256 {
		t.Errorf("retained %d bytes, want at most max+slack", len(s))
	}
	wantSuffix := string(bytes.Repeat([]byte{byte('a' + (chunks-1)%26)}, 100))
	if !strings.HasSuffix(s, wantSuffix) {
		t.Errorf("tail does not retain the most recent write")
	}
	if b.written != chunks*100 {
		t.Errorf("written = %d, want %d", b.written, chunks*100)
	}
}

// TestBashTimeoutReturnsDespiteOrphanedChild reproduces the hang: the command
// exits leaving a background child holding the output pipes. Before the fix,
// Wait blocked until the child exited (~20s) even though the timeout fired at
// 2s; now the tree is killed and WaitDelay caps any residual pipe wait.
func TestBashTimeoutReturnsDespiteOrphanedChild(t *testing.T) {
	if testing.Short() {
		t.Skip("timing-dependent test")
	}
	shell, _ := shellConfig()
	base := strings.ToLower(filepath.Base(shell))
	orphanCmd := "sleep 20 & echo spawned"
	if runtime.GOOS == "windows" && !strings.HasPrefix(base, "bash") {
		orphanCmd = `start /b cmd /c "timeout /t 20 /nobreak"`
	}

	tool := &BashTool{Cwd: t.TempDir()}
	start := time.Now()
	_, err := tool.Execute(context.Background(), "call-1", map[string]any{
		"command": orphanCmd,
		"timeout": float64(2),
	})
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout error", err)
	}
	if elapsed > 15*time.Second {
		t.Fatalf("Execute took %s: orphaned descendant kept the pipes open", elapsed)
	}
}

func TestBashEchoSucceeds(t *testing.T) {
	tool := &BashTool{Cwd: t.TempDir()}
	res, err := tool.Execute(context.Background(), "call-1", map[string]any{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if text := res.Content[0].Text; !strings.Contains(text, "hello") {
		t.Errorf("text = %q, want it to contain hello", text)
	}
}

func TestBashRejectsInvalidTimeout(t *testing.T) {
	tool := &BashTool{Cwd: t.TempDir()}
	for _, tv := range []any{float64(-1), float64(0), math.Inf(1), "soon", 1e9} {
		if _, err := tool.Execute(context.Background(), "call-1", map[string]any{
			"command": "echo hi", "timeout": tv,
		}); err == nil || !strings.Contains(err.Error(), "timeout") {
			t.Errorf("timeout %v: err = %v, want invalid-timeout error", tv, err)
		}
	}
}
