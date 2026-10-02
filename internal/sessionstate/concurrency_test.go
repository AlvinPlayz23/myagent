package sessionstate

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/filelock"
)

func TestConcurrentFileHandlesPreserveKeys(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	const writers = 24
	start := make(chan struct{})
	errors := make(chan error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			key := fmt.Sprintf("key-%d", i)
			errors <- NewFile().Set("session", key, []string{key}, "")
		}()
	}
	close(start)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for i := range writers {
		key := fmt.Sprintf("key-%d", i)
		got, ok := s.GetStringSlice("session", key)
		if !ok || !reflect.DeepEqual(got, []string{key}) {
			t.Fatalf("lost update for %s", key)
		}
	}
}

func TestSetErrorReleasesLockWithoutWriting(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	f := NewFile()
	if err := f.Set("session", "kept", []string{"original"}, ""); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(mustPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set("session", "bad", make(chan int), ""); err == nil {
		t.Fatal("Set accepted an unencodable value")
	}
	after, err := os.ReadFile(mustPath(t))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed Set changed the file")
	}
	if err := NewFile().Set("session", "next", []string{"value"}, ""); err != nil {
		t.Fatalf("lock not released after error: %v", err)
	}
}

func TestPruneLoadsAfterAcquiringLock(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	f := NewFile()
	if err := f.Set("dead", "key", []string{"old"}, ""); err != nil {
		t.Fatal(err)
	}
	held, err := filelock.Acquire(mustPath(t))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	type result struct {
		removed int
		err     error
	}
	started := make(chan struct{})
	done := make(chan result, 1)
	go func() {
		close(started)
		n, err := NewFile().Prune(map[string]bool{"keep": true})
		done <- result{n, err}
	}()
	<-started
	select {
	case <-done:
		t.Fatal("Prune did not wait for the writer lock")
	case <-time.After(30 * time.Millisecond):
	}
	// This test owns the lock: emulate another writer adding a live session
	// before the waiting Prune is allowed to load the latest state.
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("keep", "key", []string{"new"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || got.removed != 1 {
			t.Fatalf("Prune = %d, %v", got.removed, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Prune did not resume after lock release")
	}
	if got, ok := f.GetStringSlice("keep", "key"); !ok || !reflect.DeepEqual(got, []string{"new"}) {
		t.Fatal("Prune overwrote the writer's new session")
	}
}
