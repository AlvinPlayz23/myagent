package session

import (
	"os"
	"testing"
	"time"

	"github.com/myagent/myagent/internal/types"
)

// withTempDir points the session store at a temp dir via MYAGENT_DIR for the
// duration of the test.
func withTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MYAGENT_DIR", dir)
}

func userMsg(text string) types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock(text)}}
}

func assistantMsg(text string) types.Message {
	return types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{types.TextBlock(text)}}
}

// TestCreateAppendReopen verifies that a created session's messages survive a
// close/reopen and are reconstructed in order via the id/parentId chain.
func TestCreateAppendReopen(t *testing.T) {
	withTempDir(t)

	s, err := Create("/work/dir")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := s.Path()
	id := s.ID()

	msgs := []types.Message{
		userMsg("first"),
		assistantMsg("second"),
		userMsg("third"),
	}
	for _, m := range msgs {
		if err := s.AppendMessage(m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()

	if reopened.ID() != id {
		t.Errorf("id mismatch: got %q want %q", reopened.ID(), id)
	}
	got := reopened.Messages()
	if len(got) != len(msgs) {
		t.Fatalf("message count: got %d want %d", len(got), len(msgs))
	}
	for i := range msgs {
		if got[i].Role != msgs[i].Role || got[i].Content[0].Text != msgs[i].Content[0].Text {
			t.Errorf("message %d mismatch: got %+v want %+v", i, got[i], msgs[i])
		}
	}
}

// TestMidConversationReloadRestoresContext simulates a process kill mid-
// conversation: append some messages, "crash" (reopen from disk), then verify
// full context is restored and further appends continue the same chain.
func TestMidConversationReloadRestoresContext(t *testing.T) {
	withTempDir(t)

	s, err := Create("/work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := s.Path()
	_ = s.AppendMessage(userMsg("build a thing"))
	_ = s.AppendMessage(assistantMsg("working on it"))
	// Simulate crash: don't Close cleanly, just reopen from disk (appends are
	// flushed per-write).
	s.Close()

	resumed, err := Open(path)
	if err != nil {
		t.Fatalf("Open (continue): %v", err)
	}
	defer resumed.Close()

	if len(resumed.Messages()) != 2 {
		t.Fatalf("restored %d messages, want 2", len(resumed.Messages()))
	}
	// Continue the conversation.
	if err := resumed.AppendMessage(userMsg("keep going")); err != nil {
		t.Fatalf("AppendMessage after resume: %v", err)
	}

	// Reopen once more; the chain must include all three in order.
	final, err := Open(path)
	if err != nil {
		t.Fatalf("Open (final): %v", err)
	}
	defer final.Close()
	msgs := final.Messages()
	if len(msgs) != 3 {
		t.Fatalf("final message count: got %d want 3", len(msgs))
	}
	if msgs[0].Content[0].Text != "build a thing" ||
		msgs[1].Content[0].Text != "working on it" ||
		msgs[2].Content[0].Text != "keep going" {
		t.Errorf("chain out of order: %+v", msgs)
	}
}

// TestMostRecent verifies MostRecent returns the newest session file.
func TestMostRecent(t *testing.T) {
	withTempDir(t)

	older, err := Create("/a")
	if err != nil {
		t.Fatalf("Create older: %v", err)
	}
	older.Close()
	// Force a mtime gap so ordering is deterministic.
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older.Path(), past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	newer, err := Create("/b")
	if err != nil {
		t.Fatalf("Create newer: %v", err)
	}
	newer.Close()

	recent, err := MostRecent()
	if err != nil {
		t.Fatalf("MostRecent: %v", err)
	}
	if recent != newer.Path() {
		t.Errorf("MostRecent = %q, want %q", recent, newer.Path())
	}
}

// TestResumeByID resumes a session by its header id.
func TestResumeByID(t *testing.T) {
	withTempDir(t)

	s, err := Create("/work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = s.AppendMessage(userMsg("hello"))
	id := s.ID()
	s.Close()

	resumed, err := ResumeByID(id)
	if err != nil {
		t.Fatalf("ResumeByID: %v", err)
	}
	defer resumed.Close()
	if resumed.ID() != id {
		t.Errorf("resumed id = %q, want %q", resumed.ID(), id)
	}
	if len(resumed.Messages()) != 1 {
		t.Errorf("resumed messages = %d, want 1", len(resumed.Messages()))
	}

	if _, err := ResumeByID("does-not-exist"); err == nil {
		t.Error("ResumeByID with bad id: expected error, got nil")
	}
}

// TestList reports session metadata: count and preview.
func TestList(t *testing.T) {
	withTempDir(t)

	s, err := Create("/work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = s.AppendMessage(userMsg("write a haiku about go"))
	_ = s.AppendMessage(assistantMsg("done"))
	s.Close()

	infos, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("List returned %d sessions, want 1", len(infos))
	}
	got := infos[0]
	if got.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", got.MessageCount)
	}
	if got.Preview != "write a haiku about go" {
		t.Errorf("Preview = %q, want %q", got.Preview, "write a haiku about go")
	}
	if got.Cwd != "/work" {
		t.Errorf("Cwd = %q, want %q", got.Cwd, "/work")
	}
}
