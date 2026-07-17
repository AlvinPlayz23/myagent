package compaction

import (
	"context"
	"strings"
	"testing"

	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/types"
)

// --- helpers ---

func userMsg(text string) types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock(text)}}
}

func assistantTextMsg(text string) types.Message {
	return types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{types.TextBlock(text)}}
}

func assistantWithUsage(text string, total int) types.Message {
	return types.Message{
		Role:       types.RoleAssistant,
		Content:    []types.ContentBlock{types.TextBlock(text)},
		Usage:      &types.Usage{TotalTokens: total, Input: 100, Output: 50},
		StopReason: types.StopStop,
	}
}

func toolResultMsg(text string) types.Message {
	return types.Message{Role: types.RoleToolResult, Content: []types.ContentBlock{types.TextBlock(text)}}
}

func assistantWithToolCall(name string, args map[string]any) types.Message {
	return types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.ContentToolCall, ID: "tc1", Name: name, Arguments: args},
		},
		StopReason: types.StopToolUse,
	}
}

// --- ShouldCompact ---

func TestShouldCompactThreshold(t *testing.T) {
	s := DefaultSettings
	ctxWindow := ContextWindow
	// Trigger at contextWindow - reserveTokens + 1 = 230001.
	if !ShouldCompact(230_001, ctxWindow, s) {
		t.Errorf("ShouldCompact(230001) = false, want true (above 256k-26k threshold)")
	}
	// Do not trigger at the threshold itself.
	if ShouldCompact(230_000, ctxWindow, s) {
		t.Errorf("ShouldCompact(230000) = true, want false (strictly greater than threshold)")
	}
}

func TestShouldCompactDisabled(t *testing.T) {
	s := DefaultSettings
	s.Enabled = false
	if ShouldCompact(1_000_000, ContextWindow, s) {
		t.Errorf("ShouldCompact with disabled setting = true, want false")
	}
}

// --- EstimateMessageTokens ---

func TestEstimateMessageTokensCharsPer4(t *testing.T) {
	// 40 chars -> 10 tokens.
	m := userMsg(strings.Repeat("a", 40))
	if got := EstimateMessageTokens(m); got != 10 {
		t.Errorf("40-char user msg = %d tokens, want 10", got)
	}
}

func TestEstimateMessageTokensAssistantToolCall(t *testing.T) {
	m := assistantWithToolCall("read", map[string]any{"path": "foo.txt"})
	// name "read" (4) + json {"path":"foo.txt"} (20) = 24 chars -> 6 tokens.
	if got := EstimateMessageTokens(m); got != 6 {
		t.Errorf("assistant toolCall = %d tokens, want 6", got)
	}
}

func TestEstimateMessageTokensImage(t *testing.T) {
	m := types.Message{
		Role: types.RoleUser,
		Content: []types.ContentBlock{
			{Type: types.ContentImage, Data: "base64data"},
		},
	}
	// 4800 chars / 4 = 1200 tokens.
	if got := EstimateMessageTokens(m); got != 1200 {
		t.Errorf("image user msg = %d tokens, want 1200", got)
	}
}

// --- EstimateContextTokens ---

func TestEstimateContextTokensUsesProviderUsage(t *testing.T) {
	msgs := []types.Message{
		userMsg("hi"),                     // ~1 token
		assistantWithUsage("hello", 5000), // usage reports 5000 total
		userMsg("follow up"),              // ~3 tokens
	}
	est := EstimateContextTokens(msgs)
	if est.UsageTokens != 5000 {
		t.Errorf("UsageTokens = %d, want 5000", est.UsageTokens)
	}
	// "follow up" = 9 chars / 4 = 3 tokens (ceil).
	if est.TrailingTokens != 3 {
		t.Errorf("TrailingTokens = %d, want 3", est.TrailingTokens)
	}
	if est.Tokens != 5003 {
		t.Errorf("Tokens = %d, want 5003", est.Tokens)
	}
	if est.LastUsageIndex != 1 {
		t.Errorf("LastUsageIndex = %d, want 1", est.LastUsageIndex)
	}
}

func TestEstimateContextTokensNoUsageEstimatesAll(t *testing.T) {
	msgs := []types.Message{
		userMsg(strings.Repeat("a", 40)),          // 10 tokens
		assistantTextMsg(strings.Repeat("b", 80)), // 20 tokens
	}
	est := EstimateContextTokens(msgs)
	if est.LastUsageIndex != -1 {
		t.Errorf("LastUsageIndex = %d, want -1", est.LastUsageIndex)
	}
	if est.Tokens != 30 {
		t.Errorf("Tokens = %d, want 30", est.Tokens)
	}
}

func TestEstimateContextTokensSkipsAbortedErrorUsage(t *testing.T) {
	aborted := assistantWithUsage("oops", 9999)
	aborted.StopReason = types.StopAborted
	msgs := []types.Message{
		userMsg("hi"),
		aborted,
		assistantWithUsage("ok", 4000),
		userMsg("next"),
	}
	est := EstimateContextTokens(msgs)
	if est.LastUsageIndex != 2 {
		t.Errorf("LastUsageIndex = %d, want 2 (skip aborted at 1)", est.LastUsageIndex)
	}
	if est.UsageTokens != 4000 {
		t.Errorf("UsageTokens = %d, want 4000", est.UsageTokens)
	}
}

// --- SerializeConversation ---

func TestSerializeConversationShape(t *testing.T) {
	msgs := []types.Message{
		userMsg("hello"),
		assistantTextMsg("hi there"),
		assistantWithToolCall("read", map[string]any{"path": "foo.txt"}),
		toolResultMsg("file contents here"),
	}
	out := SerializeConversation(msgs)
	wantContains := []string{
		"[User]: hello",
		"[Assistant]: hi there",
		"[Assistant tool calls]: read(path=\"foo.txt\")",
		"[Tool result]: file contents here",
	}
	for _, w := range wantContains {
		if !strings.Contains(out, w) {
			t.Errorf("SerializeConversation missing %q\nGot:\n%s", w, out)
		}
	}
}

func TestSerializeConversationTruncatesToolResults(t *testing.T) {
	long := strings.Repeat("x", ToolResultMaxChars*2)
	msgs := []types.Message{toolResultMsg(long)}
	out := SerializeConversation(msgs)
	if strings.Contains(out, long) {
		t.Error("tool result not truncated")
	}
	if !strings.Contains(out, "more characters truncated]") {
		t.Errorf("truncation marker missing\nGot:\n%s", out)
	}
}

// --- FindCutPoint ---

func TestFindCutPointKeepsRecentBudget(t *testing.T) {
	// 10 messages of 1000 tokens each, keepRecent=3000 -> keep last 3,
	// summarize first 7. Cut at index 7 (first kept).
	var msgs []types.Message
	for i := 0; i < 10; i++ {
		msgs = append(msgs, userMsg(strings.Repeat("a", 4000))) // 1000 tokens
	}
	cut := FindCutPoint(msgs, 3000)
	if cut != 7 {
		t.Errorf("cut = %d, want 7 (keep 3 of 10 with budget 3000)", cut)
	}
}

func TestFindCutPointNeverCutsAtToolResult(t *testing.T) {
	// Layout: user, assistant(toolCall), toolResult, user, assistant(toolCall), toolResult
	// Each ~1000 tokens, keepRecent=1500 -> budget lands mid-conversation.
	// The cut must not be a toolResult; it should walk back to a user/assistant.
	msgs := []types.Message{
		userMsg(strings.Repeat("a", 4000)),
		assistantWithToolCall("read", map[string]any{"path": "x"}),
		toolResultMsg(strings.Repeat("r", 4000)),
		userMsg(strings.Repeat("a", 4000)),
		assistantWithToolCall("read", map[string]any{"path": "y"}),
		toolResultMsg(strings.Repeat("r", 4000)),
	}
	cut := FindCutPoint(msgs, 1500)
	if cut == 0 {
		t.Fatalf("cut = 0, expected a non-zero cut")
	}
	if msgs[cut].Role == types.RoleToolResult {
		t.Errorf("cut at index %d is a toolResult — must keep with its assistant toolCall", cut)
	}
}

func TestFindCutPointTooSmallReturnsZero(t *testing.T) {
	msgs := []types.Message{userMsg("hi"), assistantTextMsg("hello")}
	if cut := FindCutPoint(msgs, 100_000); cut != 0 {
		t.Errorf("cut = %d, want 0 (conversation fits in budget)", cut)
	}
}

func TestFindCutPointEmpty(t *testing.T) {
	if cut := FindCutPoint(nil, 1000); cut != 0 {
		t.Errorf("cut = %d, want 0 for empty input", cut)
	}
}

// --- FileOps ---

func TestExtractFileOps(t *testing.T) {
	msgs := []types.Message{
		assistantWithToolCall("read", map[string]any{"path": "a.go"}),
		assistantWithToolCall("write", map[string]any{"path": "b.go"}),
		assistantWithToolCall("edit", map[string]any{"path": "c.go"}),
		assistantWithToolCall("bash", map[string]any{"command": "ls"}), // no path
	}
	ops := NewFileOps()
	for _, m := range msgs {
		ExtractFileOpsFromMessage(m, ops)
	}
	if _, ok := ops.Read["a.go"]; !ok {
		t.Error("a.go not in Read")
	}
	if _, ok := ops.Written["b.go"]; !ok {
		t.Error("b.go not in Written")
	}
	if _, ok := ops.Edited["c.go"]; !ok {
		t.Error("c.go not in Edited")
	}
}

func TestComputeFileListsReadOnlyVsModified(t *testing.T) {
	ops := NewFileOps()
	ops.Read["only_read.go"] = struct{}{}
	ops.Read["also_edited.go"] = struct{}{}
	ops.Edited["also_edited.go"] = struct{}{}
	ops.Written["new_file.go"] = struct{}{}

	lists := ComputeFileLists(ops)
	if !sliceEqual(lists.ReadFiles, []string{"only_read.go"}) {
		t.Errorf("ReadFiles = %v, want [only_read.go]", lists.ReadFiles)
	}
	want := []string{"also_edited.go", "new_file.go"}
	if !sliceEqual(lists.ModifiedFiles, want) {
		t.Errorf("ModifiedFiles = %v, want %v", lists.ModifiedFiles, want)
	}
}

func TestFormatFileOperations(t *testing.T) {
	lists := FileLists{
		ReadFiles:     []string{"a.go"},
		ModifiedFiles: []string{"b.go"},
	}
	out := FormatFileOperations(lists)
	if !strings.Contains(out, "<read-files>\na.go\n</read-files>") {
		t.Errorf("missing read-files block:\n%s", out)
	}
	if !strings.Contains(out, "<modified-files>\nb.go\n</modified-files>") {
		t.Errorf("missing modified-files block:\n%s", out)
	}
	if FormatFileOperations(FileLists{}) != "" {
		t.Error("empty file lists should format to empty string")
	}
}

// --- PrepareCompaction + Compact (with fake provider) ---

// fakeProvider implements llm.Provider, returning a canned summary.
type fakeProvider struct {
	summaryText string
	lastReq     llm.Request
}

func (p *fakeProvider) Stream(ctx context.Context, model llm.Model, req llm.Request) (<-chan llm.StreamEvent, error) {
	p.lastReq = req
	out := make(chan llm.StreamEvent, 4)
	go func() {
		defer close(out)
		out <- llm.StreamEvent{Type: "start", Partial: &types.Message{Role: types.RoleAssistant}}
		out <- llm.StreamEvent{Type: "text_delta", Delta: p.summaryText}
		out <- llm.StreamEvent{
			Type: "done",
			Message: &types.Message{
				Role:       types.RoleAssistant,
				Content:    []types.ContentBlock{types.TextBlock(p.summaryText)},
				StopReason: types.StopStop,
			},
		}
	}()
	return out, nil
}

func TestPrepareCompactionSplitsMessages(t *testing.T) {
	// 10 user messages of 1000 tokens each, keepRecent=3000 -> summarize 7, keep 3.
	var msgs []types.Message
	for i := 0; i < 10; i++ {
		msgs = append(msgs, userMsg(strings.Repeat("a", 4000)))
	}
	prep, ok := PrepareCompaction(msgs, Settings{Enabled: true, ReserveTokens: 1000, KeepRecentTokens: 3000})
	if !ok {
		t.Fatalf("PrepareCompaction returned ok=false")
	}
	if len(prep.MessagesToSummarize) != 7 {
		t.Errorf("MessagesToSummarize = %d, want 7", len(prep.MessagesToSummarize))
	}
	if len(prep.KeptMessages) != 3 {
		t.Errorf("KeptMessages = %d, want 3", len(prep.KeptMessages))
	}
	if prep.PreviousSummary != "" {
		t.Errorf("PreviousSummary = %q, want empty", prep.PreviousSummary)
	}
}

func TestPrepareCompactionNothingToCompact(t *testing.T) {
	// Single message: nothing to summarize.
	msgs := []types.Message{userMsg("hi")}
	if _, ok := PrepareCompaction(msgs, DefaultSettings); ok {
		t.Error("PrepareCompaction on single message should return ok=false")
	}
}

func TestPrepareCompactionDetectsPreviousSummary(t *testing.T) {
	// Build a conversation that already contains a compaction summary, then
	// append more messages.
	prev := BuildSummaryMessage("## Goal\nprevious goal", 1000)
	msgs := []types.Message{
		prev,
		userMsg("continue"),
		assistantTextMsg("working"),
	}
	prep, ok := PrepareCompaction(msgs, Settings{Enabled: true, ReserveTokens: 1000, KeepRecentTokens: 1})
	if !ok {
		t.Fatalf("PrepareCompaction returned ok=false")
	}
	if prep.PreviousSummary != "## Goal\nprevious goal" {
		t.Errorf("PreviousSummary = %q, want previous goal", prep.PreviousSummary)
	}
}

func TestCompactEndToEndWithFakeProvider(t *testing.T) {
	// Build messages with file ops so we exercise the file-list formatting.
	msgs := []types.Message{
		userMsg("build a feature"),
		assistantWithToolCall("read", map[string]any{"path": "main.go"}),
		toolResultMsg("contents"),
		assistantWithToolCall("edit", map[string]any{"path": "main.go"}),
		toolResultMsg("ok"),
		userMsg("looks good"), // last user msg kept
	}
	provider := &fakeProvider{summaryText: "## Goal\nbuild a feature"}
	prep, ok := PrepareCompaction(msgs, Settings{Enabled: true, ReserveTokens: 1000, KeepRecentTokens: 1})
	if !ok {
		t.Fatalf("PrepareCompaction ok=false")
	}
	res, err := Compact(context.Background(), provider, llm.Model{ID: "test"}, prep)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if res.Summary == "" {
		t.Error("Summary is empty")
	}
	if !strings.Contains(res.Summary, "## Goal") {
		t.Errorf("Summary missing ## Goal:\n%s", res.Summary)
	}
	// File lists should be appended: main.go was read+edited -> modified.
	if !strings.Contains(res.Summary, "<modified-files>") {
		t.Errorf("Summary missing modified-files block:\n%s", res.Summary)
	}
	if !strings.Contains(res.Summary, "main.go") {
		t.Errorf("Summary missing main.go:\n%s", res.Summary)
	}
	// The summarization call should use the SUMMARIZATION system prompt and no tools.
	if provider.lastReq.SystemPrompt != SummarizationSystemPrompt {
		t.Errorf("summarization system prompt not set")
	}
	if len(provider.lastReq.Tools) != 0 {
		t.Errorf("summarization call should not include tools, got %d", len(provider.lastReq.Tools))
	}
}

func TestCompactUsesUpdatePromptWhenPreviousSummary(t *testing.T) {
	provider := &fakeProvider{summaryText: "## Goal\nupdated"}
	prev := BuildSummaryMessage("## Goal\nold", 1000)
	msgs := []types.Message{
		prev,
		userMsg("more work"),
		assistantTextMsg("did stuff"),
	}
	prep, ok := PrepareCompaction(msgs, Settings{Enabled: true, ReserveTokens: 1000, KeepRecentTokens: 1})
	if !ok {
		t.Fatalf("PrepareCompaction ok=false")
	}
	if _, err := Compact(context.Background(), provider, llm.Model{ID: "test"}, prep); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	// The user message sent to the provider should contain the UPDATE prompt
	// and the previous summary block.
	userContent := textOf(provider.lastReq.Messages)
	if !strings.Contains(userContent, UpdateSummarizationPrompt) {
		t.Error("UPDATE prompt not used when previousSummary is set")
	}
	if !strings.Contains(userContent, "<previous-summary>") {
		t.Error("<previous-summary> block not included")
	}
}

func TestBuildSummaryMessageWrapsPrefixSuffix(t *testing.T) {
	m := BuildSummaryMessage("hello", 1234)
	if m.Role != types.RoleUser {
		t.Errorf("Role = %s, want user", m.Role)
	}
	if m.Timestamp != 1234 {
		t.Errorf("Timestamp = %d, want 1234", m.Timestamp)
	}
	if len(m.Content) != 1 || m.Content[0].Type != types.ContentText {
		t.Fatalf("expected single text content block")
	}
	text := m.Content[0].Text
	if !strings.HasPrefix(text, CompactionSummaryPrefix) {
		t.Error("summary not wrapped with prefix")
	}
	if !strings.HasSuffix(text, CompactionSummarySuffix) {
		t.Error("summary not wrapped with suffix")
	}
	if !strings.Contains(text, "hello") {
		t.Error("summary text not present in wrapped message")
	}
}

// --- helpers ---

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func textOf(msgs []types.Message) string {
	var parts []string
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type == types.ContentText {
				parts = append(parts, c.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}
