package sessionstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestSetGetRoundTrip(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	f := NewFile()

	if err := f.Set("sess1", "disabledTools", []string{"bash", "write"}, "/tmp/sess1.jsonl"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := f.GetStringSlice("sess1", "disabledTools")
	if !ok {
		t.Fatal("GetStringSlice reported missing after Set")
	}
	if want := []string{"bash", "write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("value = %v, want %v", got, want)
	}

	// A second session's write must not clobber the first entry.
	if err := f.Set("sess2", "disabledTools", []string{"edit"}, ""); err != nil {
		t.Fatalf("Set second: %v", err)
	}
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(s.Sessions))
	}
	if got := s.Sessions["sess1"].Path; got != "/tmp/sess1.jsonl" {
		t.Fatalf("recorded path = %q, want /tmp/sess1.jsonl", got)
	}
	if s.Sessions["sess2"].UpdatedAt == "" {
		t.Error("UpdatedAt not stamped")
	}
}

func TestSaveWrites0600AndAtomic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MYAGENT_DIR", dir)
	f := NewFile()
	if err := f.Set("s", "k", []string{"a"}, ""); err != nil {
		t.Fatal(err)
	}
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("path %q not under MYAGENT_DIR %q", path, dir)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows has no POSIX permission bits; Chmod there does not produce 0600.
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("perm = %o, want 600", perm)
		}
	}
	// No temp files left behind by the atomic rename.
	matches, _ := filepath.Glob(filepath.Join(dir, ".session-state-*.json.tmp"))
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestLoadMissingAndCorruptFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MYAGENT_DIR", dir)

	// Missing file: empty store, no error.
	s, err := Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if len(s.Sessions) != 0 {
		t.Fatalf("sessions = %d, want 0", len(s.Sessions))
	}

	// Corrupt file: tolerated, still empty.
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err = Load()
	if err != nil {
		t.Fatalf("Load on corrupt file = %v, want tolerance", err)
	}
	if len(s.Sessions) != 0 {
		t.Fatalf("corrupt file produced %d sessions, want 0", len(s.Sessions))
	}
}

func TestDeleteDropsEmptySession(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	s := newStore()
	if err := s.Set("s1", "a", []string{"x"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("s1", "b", []string{"y"}, ""); err != nil {
		t.Fatal(err)
	}
	s.Delete("s1", "a")
	if _, ok := s.Sessions["s1"]; !ok {
		t.Fatal("session dropped while it still had a value")
	}
	s.Delete("s1", "b")
	if _, ok := s.Sessions["s1"]; ok {
		t.Fatal("empty session entry not dropped after last Delete")
	}
}

func TestPruneRemovesDeadSessions(t *testing.T) {
	s := newStore()
	for _, id := range []string{"live1", "dead1", "dead2"} {
		if err := s.Set(id, "k", []string{"v"}, ""); err != nil {
			t.Fatal(err)
		}
	}
	removed := s.Prune(map[string]bool{"live1": true})
	if removed != 2 {
		t.Fatalf("Prune removed %d, want 2", removed)
	}
	if _, ok := s.Sessions["live1"]; !ok {
		t.Error("live session was pruned")
	}
	if len(s.Sessions) != 1 {
		t.Fatalf("remaining sessions = %d, want 1", len(s.Sessions))
	}
}

// TestFilePersistsAcrossHandles proves the store is genuinely on disk: a second
// handle (as a later process would open) sees what the first wrote. This is the
// end-to-end shape of session-state.json for a /tools session save.
func TestFilePersistsAcrossHandles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MYAGENT_DIR", dir)

	writer := NewFile()
	if err := writer.Set("sess42", "disabledTools", []string{"bash", "write"}, filepath.Join(dir, "sessions", "sess42.jsonl")); err != nil {
		t.Fatal(err)
	}

	// A fresh handle, as a new process would create.
	got, ok := NewFile().GetStringSlice("sess42", "disabledTools")
	if !ok {
		t.Fatal("value did not survive across handles")
	}
	if want := []string{"bash", "write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("value = %v, want %v", got, want)
	}

	data, err := os.ReadFile(filepath.Join(dir, "session-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Show the on-disk shape so a regression in the layout is obvious.
	t.Logf("session-state.json:\n%s", data)
	if !strings.Contains(string(data), `"sess42"`) {
		t.Error("session id missing from the file")
	}
	if !strings.Contains(string(data), `"path"`) {
		t.Error("session path missing from the file")
	}
}

func TestGetStringSliceRejectsWrongType(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	f := NewFile()
	if err := f.Set("s", "k", 42, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.GetStringSlice("s", "k"); ok {
		t.Fatal("GetStringSlice accepted a non-slice payload")
	}
	if _, ok := f.GetStringSlice("s", "absent"); ok {
		t.Fatal("GetStringSlice reported an absent key as present")
	}
}

func TestSetRequiresIDAndKey(t *testing.T) {
	s := newStore()
	if err := s.Set("", "k", []string{"v"}, ""); err == nil {
		t.Error("empty id accepted")
	}
	if err := s.Set("s", "", []string{"v"}, ""); err == nil {
		t.Error("empty key accepted")
	}
}

func TestStoreJSONShape(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	f := NewFile()
	if err := f.Set("abc", "disabledTools", []string{"bash"}, "/p/abc.jsonl"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(mustPath(t))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["version"]; !ok {
		t.Error("version missing from file")
	}
	sessions, _ := raw["sessions"].(map[string]any)
	entry, _ := sessions["abc"].(map[string]any)
	values, _ := entry["values"].(map[string]any)
	if _, ok := values["disabledTools"]; !ok {
		t.Fatalf("values.disabledTools missing; got %v", entry)
	}
	// Value-only schema: no speculative args envelope.
	if _, ok := values["args"]; ok {
		t.Error("unexpected args envelope in value-only schema")
	}
}

func mustPath(t *testing.T) string {
	t.Helper()
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	return p
}
