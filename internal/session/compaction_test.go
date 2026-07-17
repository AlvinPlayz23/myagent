package session

import (
	"os"
	"strings"
	"testing"

	"github.com/myagent/myagent/internal/agent/compaction"
	"github.com/myagent/myagent/internal/types"
)

// TestCompactionPersistAndReopen verifies that ApplyCompaction persists a
// compaction entry and Open reconstructs [summary] + [kept messages].
func TestCompactionPersistAndReopen(t *testing.T) {
	withTempDir(t)

	s, err := Create("/work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := s.Path()

	msgs := []types.Message{
		userMsg("first"),
		assistantMsg("second"),
		userMsg("third"),
		assistantMsg("fourth"),
		userMsg("fifth"),
	}
	for _, m := range msgs {
		if err := s.AppendMessage(m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	// Compact: keep the last 2 messages (index 3..4), summarize 0..2.
	summaryMsg := compaction.BuildSummaryMessage("## Goal\ntest goal", 1000)
	info := types.CompactionInfo{
		Summary:        "## Goal\ntest goal",
		FirstKeptIndex: 3,
		TokensBefore:   500,
		TokensAfter:    100,
	}
	if err := s.ApplyCompaction(info, summaryMsg); err != nil {
		t.Fatalf("ApplyCompaction: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()

	got := reopened.Messages()
	// Expect: [summary] + [fourth] + [fifth]
	if len(got) != 3 {
		t.Fatalf("message count after reopen: got %d want 3", len(got))
	}
	if !compaction.IsSummaryMessage(got[0]) {
		t.Errorf("message 0 should be a compaction summary, got role %s", got[0].Role)
	}
	if got[1].Content[0].Text != "fourth" {
		t.Errorf("message 1: got %q want %q", got[1].Content[0].Text, "fourth")
	}
	if got[2].Content[0].Text != "fifth" {
		t.Errorf("message 2: got %q want %q", got[2].Content[0].Text, "fifth")
	}
}

// TestCompactionAppendAfterReopen verifies that messages appended after a
// compaction are included when the session is reopened.
func TestCompactionAppendAfterReopen(t *testing.T) {
	withTempDir(t)

	s, err := Create("/work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := s.Path()

	for _, m := range []types.Message{
		userMsg("old1"),
		assistantMsg("old2"),
		userMsg("old3"),
		assistantMsg("old4"),
	} {
		_ = s.AppendMessage(m)
	}

	summaryMsg := compaction.BuildSummaryMessage("## Goal\nsummary", 1000)
	_ = s.ApplyCompaction(types.CompactionInfo{
		Summary:        "## Goal\nsummary",
		FirstKeptIndex: 2, // keep old3, old4
		TokensBefore:   400,
	}, summaryMsg)

	// Append a new message after compaction.
	_ = s.AppendMessage(userMsg("after-compaction"))
	_ = s.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()

	got := reopened.Messages()
	// Expect: [summary] + [old3] + [old4] + [after-compaction]
	if len(got) != 4 {
		t.Fatalf("message count: got %d want 4", len(got))
	}
	if !compaction.IsSummaryMessage(got[0]) {
		t.Error("message 0 should be summary")
	}
	if got[1].Content[0].Text != "old3" {
		t.Errorf("message 1: got %q want old3", got[1].Content[0].Text)
	}
	if got[2].Content[0].Text != "old4" {
		t.Errorf("message 2: got %q want old4", got[2].Content[0].Text)
	}
	if got[3].Content[0].Text != "after-compaction" {
		t.Errorf("message 3: got %q want after-compaction", got[3].Content[0].Text)
	}
}

// TestCompactionRepeated verifies that only the LATEST compaction entry is
// applied on reopen.
func TestCompactionRepeated(t *testing.T) {
	withTempDir(t)

	s, err := Create("/work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := s.Path()

	for _, m := range []types.Message{
		userMsg("m1"),
		assistantMsg("m2"),
		userMsg("m3"),
		assistantMsg("m4"),
		userMsg("m5"),
		assistantMsg("m6"),
	} {
		_ = s.AppendMessage(m)
	}

	// First compaction: keep m5, m6 (index 4).
	_ = s.ApplyCompaction(types.CompactionInfo{
		Summary:        "## Goal\nfirst summary",
		FirstKeptIndex: 4,
		TokensBefore:   600,
	}, compaction.BuildSummaryMessage("## Goal\nfirst summary", 1000))

	// After first compaction, in-memory: [summary1, m5, m6].
	// Append more messages.
	_ = s.AppendMessage(userMsg("m7"))
	_ = s.AppendMessage(assistantMsg("m8"))

	// Second compaction: keep m7, m8 (index 3 in the [summary1, m5, m6, m7, m8] list).
	_ = s.ApplyCompaction(types.CompactionInfo{
		Summary:        "## Goal\nsecond summary",
		FirstKeptIndex: 3, // in the [summary1, m5, m6, m7, m8] list
		TokensBefore:   500,
	}, compaction.BuildSummaryMessage("## Goal\nsecond summary", 2000))

	_ = s.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()

	got := reopened.Messages()
	// Only the LATEST compaction applies: [summary2] + [m7] + [m8].
	if len(got) != 3 {
		t.Fatalf("message count: got %d want 3 (summary2 + m7 + m8)", len(got))
	}
	if !compaction.IsSummaryMessage(got[0]) {
		t.Error("message 0 should be a compaction summary")
	}
	if !strings.Contains(got[0].Content[0].Text, "second summary") {
		t.Errorf("message 0 should contain 'second summary', got %q", got[0].Content[0].Text)
	}
	if got[1].Content[0].Text != "m7" {
		t.Errorf("message 1: got %q want m7", got[1].Content[0].Text)
	}
	if got[2].Content[0].Text != "m8" {
		t.Errorf("message 2: got %q want m8", got[2].Content[0].Text)
	}
}

// TestCompactionV3Compatibility verifies that a v3 session file (no compaction
// entries, version 3 header) is still read correctly.
func TestCompactionV3Compatibility(t *testing.T) {
	withTempDir(t)

	s, err := Create("/work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := s.Path()
	_ = s.AppendMessage(userMsg("v3msg1"))
	_ = s.AppendMessage(assistantMsg("v3msg2"))
	_ = s.Close()

	// Rewrite the header to version 3 to simulate an old file.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Replace "version":4 with "version":3 in the first line.
	lines := strings.SplitN(string(data), "\n", 2)
	lines[0] = strings.Replace(lines[0], `"version":4`, `"version":3`, 1)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open v3: %v", err)
	}
	defer reopened.Close()

	got := reopened.Messages()
	if len(got) != 2 {
		t.Fatalf("v3 message count: got %d want 2", len(got))
	}
	if got[0].Content[0].Text != "v3msg1" || got[1].Content[0].Text != "v3msg2" {
		t.Errorf("v3 messages out of order: %+v", got)
	}
}

// TestCompactionDetailsPersisted verifies that file-list details are persisted
// in the compaction entry and survive a round-trip.
func TestCompactionDetailsPersisted(t *testing.T) {
	withTempDir(t)

	s, err := Create("/work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	path := s.Path()
	_ = s.AppendMessage(userMsg("m1"))
	_ = s.AppendMessage(assistantMsg("m2"))
	_ = s.AppendMessage(userMsg("m3"))

	info := types.CompactionInfo{
		Summary:        "## Goal\ntest",
		FirstKeptIndex: 2,
		TokensBefore:   300,
		ReadFiles:      []string{"a.go", "b.go"},
		ModifiedFiles:  []string{"c.go"},
	}
	_ = s.ApplyCompaction(info, compaction.BuildSummaryMessage("## Goal\ntest", 1000))
	_ = s.Close()

	// Read the raw file and verify the compaction entry has details.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"readFiles"`) {
		t.Error("compaction entry should contain readFiles in details")
	}
	if !strings.Contains(string(data), "a.go") {
		t.Error("compaction entry should contain a.go in details")
	}
	if !strings.Contains(string(data), "c.go") {
		t.Error("compaction entry should contain c.go in details")
	}
}

// TestApplyCompactionWriteFailurePreservesInMemoryState verifies that a failed
// compaction append does not make the session's memory diverge from its JSONL
// history.
func TestApplyCompactionWriteFailurePreservesInMemoryState(t *testing.T) {
	withTempDir(t)

	s, err := Create("/work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer s.Close()
	for _, m := range []types.Message{userMsg("first"), assistantMsg("second"), userMsg("third")} {
		if err := s.AppendMessage(m); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	before := append([]types.Message(nil), s.Messages()...)
	if err := s.f.Close(); err != nil {
		t.Fatalf("close session file: %v", err)
	}
	if err := s.ApplyCompaction(types.CompactionInfo{
		Summary:        "## Goal\nsummary",
		FirstKeptIndex: 2,
		TokensBefore:   300,
	}, compaction.BuildSummaryMessage("## Goal\nsummary", 1000)); err == nil {
		t.Fatal("ApplyCompaction succeeded after the session file was closed")
	}

	after := s.Messages()
	if len(after) != len(before) {
		t.Fatalf("message count after failed compaction: got %d want %d", len(after), len(before))
	}
	for i := range before {
		if after[i].Role != before[i].Role || after[i].Content[0].Text != before[i].Content[0].Text {
			t.Errorf("message %d changed after failed compaction: got %+v want %+v", i, after[i], before[i])
		}
	}
}
