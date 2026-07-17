package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/myagent/myagent/internal/agent/compaction"
	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/types"
)

// fakeCompactionProvider is a test Provider that distinguishes summarization
// calls (SystemPrompt == SummarizationSystemPrompt, no tools) from regular
// calls, returning canned responses for each.
type fakeCompactionProvider struct {
	mu          sync.Mutex
	requests    []llm.Request
	summaryText string
	regularText string
}

func (p *fakeCompactionProvider) Stream(ctx context.Context, model llm.Model, req llm.Request) (<-chan llm.StreamEvent, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()

	out := make(chan llm.StreamEvent, 4)
	go func() {
		defer close(out)
		if req.SystemPrompt == compaction.SummarizationSystemPrompt {
			out <- llm.StreamEvent{Type: "start", Partial: &types.Message{Role: types.RoleAssistant}}
			out <- llm.StreamEvent{Type: "text_delta", Delta: p.summaryText}
			out <- llm.StreamEvent{Type: "done", Message: &types.Message{
				Role:       types.RoleAssistant,
				Content:    []types.ContentBlock{types.TextBlock(p.summaryText)},
				StopReason: types.StopStop,
			}}
		} else {
			out <- llm.StreamEvent{Type: "start", Partial: &types.Message{Role: types.RoleAssistant}}
			out <- llm.StreamEvent{Type: "text_delta", Delta: p.regularText}
			out <- llm.StreamEvent{Type: "done", Message: &types.Message{
				Role:       types.RoleAssistant,
				Content:    []types.ContentBlock{types.TextBlock(p.regularText)},
				StopReason: types.StopStop,
				Usage:      &types.Usage{TotalTokens: 50, Input: 30, Output: 20},
			}}
		}
	}()
	return out, nil
}

func (p *fakeCompactionProvider) request(i int) llm.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i < 0 || i >= len(p.requests) {
		return llm.Request{}
	}
	return p.requests[i]
}

func (p *fakeCompactionProvider) numRequests() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

// collectEvents is a test sink that accumulates all events.
func collectEvents(out *[]types.AgentEvent) EventSink {
	return func(_ context.Context, ev types.AgentEvent) error {
		*out = append(*out, ev)
		return nil
	}
}

func hasEvent(events []types.AgentEvent, typ types.AgentEventType) bool {
	for _, ev := range events {
		if ev.Type == typ {
			return true
		}
	}
	return false
}

// TestLoopAutoCompaction verifies that the loop triggers compaction when the
// context window is nearly full, replaces history with [summary] + [kept],
// and the subsequent regular call receives the compacted history.
func TestLoopAutoCompaction(t *testing.T) {
	// Build a history that exceeds the threshold.
	// ReserveTokens=255000 -> threshold = 256000-255000 = 1000 tokens = 4000 chars.
	// 20 messages * 500 chars = 10000 chars = 2500 tokens > 1000.
	var history []types.Message
	for i := 0; i < 10; i++ {
		history = append(history, types.Message{
			Role:    types.RoleUser,
			Content: []types.ContentBlock{types.TextBlock(strings.Repeat("x", 500))},
		})
		history = append(history, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{types.TextBlock(strings.Repeat("y", 500))},
		})
	}

	provider := &fakeCompactionProvider{
		summaryText: "## Goal\ncompacted summary",
		regularText: "done",
	}

	var events []types.AgentEvent
	cfg := Config{
		Provider: provider,
		Model:    llm.Model{ID: "test"},
		CompactionSettings: compaction.Settings{
			Enabled:          true,
			ReserveTokens:    255_000, // threshold = 1000 tokens
			KeepRecentTokens: 100,     // keep ~100 chars of recent context
		},
	}

	loop := New(cfg, history, collectEvents(&events))
	_, err := loop.Run(context.Background(), []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("continue")}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 1. Compaction events emitted.
	if !hasEvent(events, types.EventCompactionStart) {
		t.Error("compaction_start event not emitted")
	}
	if !hasEvent(events, types.EventCompactionEnd) {
		t.Error("compaction_end event not emitted")
	}

	// 2. At least 2 provider calls: summarization + regular.
	if provider.numRequests() < 2 {
		t.Fatalf("expected at least 2 provider calls, got %d", provider.numRequests())
	}

	// 3. The summarization call used the summarization system prompt and no tools.
	summaryReq := provider.request(0)
	if summaryReq.SystemPrompt != compaction.SummarizationSystemPrompt {
		t.Errorf("first call should use summarization system prompt, got %q", summaryReq.SystemPrompt)
	}
	if len(summaryReq.Tools) != 0 {
		t.Errorf("summarization call should have no tools, got %d", len(summaryReq.Tools))
	}

	// 4. The regular call received the compacted history (summary first).
	regularReq := provider.request(1)
	if len(regularReq.Messages) == 0 {
		t.Fatal("regular call has no messages")
	}
	if !compaction.IsSummaryMessage(regularReq.Messages[0]) {
		t.Errorf("regular call's first message should be the compaction summary, got role %s text %q",
			regularReq.Messages[0].Role, textOfMsg(regularReq.Messages[0]))
	}

	// 5. The loop's final messages are compacted (much fewer than original).
	finalMsgs := loop.Messages()
	if len(finalMsgs) >= len(history) {
		t.Errorf("compaction should reduce message count: got %d, original %d", len(finalMsgs), len(history))
	}
	if !compaction.IsSummaryMessage(finalMsgs[0]) {
		t.Error("loop's final messages should start with summary")
	}

	// 6. The last message should be the regular assistant response.
	lastMsg := finalMsgs[len(finalMsgs)-1]
	if lastMsg.Role != types.RoleAssistant || lastMsg.Content[0].Text != "done" {
		t.Errorf("last message should be the regular response 'done', got role %s text %q",
			lastMsg.Role, textOfMsg(lastMsg))
	}
}

// TestLoopNoCompactionBelowThreshold verifies that compaction does NOT trigger
// when the context is below the threshold.
func TestLoopNoCompactionBelowThreshold(t *testing.T) {
	// Small history, well below the threshold.
	history := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("hi")}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{types.TextBlock("hello")}},
	}

	provider := &fakeCompactionProvider{
		summaryText: "should not be used",
		regularText: "response",
	}

	var events []types.AgentEvent
	cfg := Config{
		Provider: provider,
		Model:    llm.Model{ID: "test"},
		CompactionSettings: compaction.Settings{
			Enabled:          true,
			ReserveTokens:    255_000,
			KeepRecentTokens: 100,
		},
	}

	loop := New(cfg, history, collectEvents(&events))
	_, err := loop.Run(context.Background(), []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("more")}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if hasEvent(events, types.EventCompactionStart) {
		t.Error("compaction should not trigger below threshold")
	}
	if hasEvent(events, types.EventCompactionEnd) {
		t.Error("compaction should not trigger below threshold")
	}
	// Only one provider call (the regular response).
	if provider.numRequests() != 1 {
		t.Errorf("expected 1 provider call, got %d", provider.numRequests())
	}
}

// TestLoopCompactionDisabled verifies that compaction is skipped when disabled.
func TestLoopCompactionDisabled(t *testing.T) {
	// Large history that would trigger compaction if enabled.
	var history []types.Message
	for i := 0; i < 10; i++ {
		history = append(history, types.Message{
			Role:    types.RoleUser,
			Content: []types.ContentBlock{types.TextBlock(strings.Repeat("x", 500))},
		})
		history = append(history, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{types.TextBlock(strings.Repeat("y", 500))},
		})
	}

	provider := &fakeCompactionProvider{
		summaryText: "should not be used",
		regularText: "response",
	}

	var events []types.AgentEvent
	cfg := Config{
		Provider: provider,
		Model:    llm.Model{ID: "test"},
		CompactionSettings: compaction.Settings{
			Enabled: false,
		},
	}

	loop := New(cfg, history, collectEvents(&events))
	_, err := loop.Run(context.Background(), []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("continue")}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if hasEvent(events, types.EventCompactionStart) {
		t.Error("compaction should not trigger when disabled")
	}
	if provider.numRequests() != 1 {
		t.Errorf("expected 1 provider call (no summarization), got %d", provider.numRequests())
	}
}

// TestLoopCompactionPreservesToolResultGroup verifies that compaction does not
// cut between an assistant tool-call and its tool-result.
func TestLoopCompactionPreservesToolResultGroup(t *testing.T) {
	// Build a history where the last few messages are: assistant(toolCall) +
	// toolResult. The cut point must walk back past the toolResult to the
	// assistant, keeping the pair intact.
	var history []types.Message
	for i := 0; i < 8; i++ {
		history = append(history, types.Message{
			Role:    types.RoleUser,
			Content: []types.ContentBlock{types.TextBlock(strings.Repeat("a", 500))},
		})
		history = append(history, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{types.TextBlock(strings.Repeat("b", 500))},
		})
	}
	// End with an assistant tool-call + tool-result pair.
	history = append(history, types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.ContentToolCall, ID: "tc1", Name: "read", Arguments: map[string]any{"path": "f.go"}},
		},
		StopReason: types.StopToolUse,
	})
	history = append(history, types.Message{
		Role:       types.RoleToolResult,
		ToolCallID: "tc1",
		Content:    []types.ContentBlock{types.TextBlock("file contents")},
	})

	provider := &fakeCompactionProvider{
		summaryText: "## Goal\nsummary",
		regularText: "ok",
	}

	var events []types.AgentEvent
	cfg := Config{
		Provider: provider,
		Model:    llm.Model{ID: "test"},
		CompactionSettings: compaction.Settings{
			Enabled:          true,
			ReserveTokens:    255_000, // threshold = 1000 tokens
			KeepRecentTokens: 200,     // keep ~200 chars
		},
	}

	loop := New(cfg, history, collectEvents(&events))
	_, err := loop.Run(context.Background(), []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("continue")}},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !hasEvent(events, types.EventCompactionEnd) {
		t.Fatal("compaction_end not emitted")
	}

	// The compacted history must not start with a toolResult — the
	// assistant toolCall + toolResult pair must stay together.
	regularReq := provider.request(1)
	if len(regularReq.Messages) < 2 {
		t.Fatalf("compacted history too short: %d messages", len(regularReq.Messages))
	}
	// First message should be the summary.
	if !compaction.IsSummaryMessage(regularReq.Messages[0]) {
		t.Error("first message should be compaction summary")
	}
	// No message in the kept region should be a toolResult without its
	// preceding assistant toolCall.
	for i, m := range regularReq.Messages[1:] {
		if m.Role == types.RoleToolResult {
			if i == 0 {
				t.Error("toolResult is the first kept message — cut broke the tool-call/result pair")
			}
			prev := regularReq.Messages[i] // i is 0-based from [1:], so regularReq.Messages[i] is the prev
			if prev.Role != types.RoleAssistant {
				t.Errorf("toolResult at position %d is not preceded by an assistant message (got %s)",
					i+1, prev.Role)
			}
		}
	}
}

func textOfMsg(m types.Message) string {
	var parts []string
	for _, c := range m.Content {
		if c.Type == types.ContentText {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}
