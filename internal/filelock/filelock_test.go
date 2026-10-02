package filelock

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireTimeoutAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	held, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	started := time.Now()
	if _, err := acquire(path, 30*time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("contended acquire = %v, want timeout", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("acquisition exceeded its bounded timeout")
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	if err := held.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := os.Stat(path + ".lock"); err != nil {
		t.Fatalf("persistent lock sidecar: %v", err)
	}
	for range 3 {
		next, err := acquire(path, 30*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		if err := next.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAcquireOpenError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "state.json")
	if _, err := Acquire(path); err == nil || errors.Is(err, ErrTimeout) {
		t.Fatalf("acquire in missing directory = %v, want open error", err)
	}
}

func TestAcquireCrossProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	held, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	runHelper := func(mode string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestLockHelper$")
		cmd.Env = append(os.Environ(), "MYAGENT_FILELOCK_TEST_PATH="+path, "MYAGENT_FILELOCK_TEST_MODE="+mode)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("lock helper: %v\n%s", err, output)
		}
	}
	runHelper("timeout")
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	// The child exits while holding the lock, without an explicit Close.
	runHelper("exit")
	next, err := acquire(path, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire after child exit: %v", err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLockHelper(t *testing.T) {
	path := os.Getenv("MYAGENT_FILELOCK_TEST_PATH")
	if path == "" {
		return
	}
	lock, err := acquire(path, 50*time.Millisecond)
	if os.Getenv("MYAGENT_FILELOCK_TEST_MODE") == "timeout" {
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("child acquire = %v, want timeout", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	_ = lock
	os.Exit(0)
}
