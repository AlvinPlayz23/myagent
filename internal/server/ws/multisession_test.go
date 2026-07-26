package ws

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// blockingProvider holds each request after its stream has started. It lets
// these tests prove that distinct server sessions are in flight together,
// rather than merely completing one after another.
type blockingProvider struct {
	mu       sync.Mutex
	requests []llm.Request
	block    chan struct{}
}

func (p *blockingProvider) Stream(ctx context.Context, model llm.Model, req llm.Request) (<-chan llm.StreamEvent, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()

	out := make(chan llm.StreamEvent, 4)
	go func() {
		defer close(out)
		out <- llm.StreamEvent{Type: "start", Partial: &types.Message{Role: types.RoleAssistant}}
		select {
		case <-p.block:
		case <-ctx.Done():
			out <- llm.StreamEvent{Type: "error", Error: &types.Message{
				Role: types.RoleAssistant, StopReason: types.StopAborted, ErrorMessage: "aborted",
			}}
			return
		}
		out <- llm.StreamEvent{Type: "text_delta", Delta: "parallel reply"}
		out <- llm.StreamEvent{Type: "done", Message: &types.Message{
			Role:       types.RoleAssistant,
			Content:    []types.ContentBlock{types.TextBlock("parallel reply")},
			StopReason: types.StopStop,
		}}
	}()
	return out, nil
}

func (p *blockingProvider) requestCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func waitForRequests(t *testing.T, p *blockingProvider, want int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for p.requestCount() < want {
		select {
		case <-deadline:
			t.Fatalf("provider received %d requests, want %d", p.requestCount(), want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestMultipleSessionsRunConcurrently verifies the full WebSocket path: one
// client creates several sessions, starts all their turns, and receives a
// separate completion and persisted history for every session.
func TestMultipleSessionsRunConcurrently(t *testing.T) {
	provider := &blockingProvider{block: make(chan struct{})}
	url, _ := testServer(t, provider)
	c := dial(t, url)

	const sessionCount = 3
	ids := make([]string, 0, sessionCount)
	for i := 0; i < sessionCount; i++ {
		var created struct {
			SessionID string `json:"sessionId"`
		}
		c.result(c.call("session.create", nil), &created)
		if created.SessionID == "" {
			t.Fatal("session.create returned an empty id")
		}
		ids = append(ids, created.SessionID)
	}

	// Each prompt returns immediately. The provider cannot finish any of them
	// until we release the shared block below.
	for i, id := range ids {
		c.result(c.call("session.prompt", map[string]any{
			"sessionId": id,
			"message":   "prompt for session " + string(rune('A'+i)),
		}), &struct{}{})
	}
	waitForRequests(t, provider, sessionCount)
	close(provider.block)

	done := make(map[string]bool, sessionCount)
	deadline := time.After(10 * time.Second)
	for len(done) < sessionCount {
		select {
		case n := <-c.notifs:
			if n.Method != "session.done" {
				continue
			}
			var p struct {
				SessionID string `json:"sessionId"`
				Error     string `json:"error"`
			}
			if err := json.Unmarshal(n.Params, &p); err != nil {
				t.Fatal(err)
			}
			if p.Error != "" {
				t.Fatalf("session %s completed with error: %s", p.SessionID, p.Error)
			}
			done[p.SessionID] = true
		case <-deadline:
			t.Fatalf("received completions for %d/%d sessions", len(done), sessionCount)
		}
	}

	for i, id := range ids {
		var result struct {
			Messages []types.Message `json:"messages"`
		}
		c.result(c.call("session.messages", map[string]any{"sessionId": id}), &result)
		if len(result.Messages) != 2 {
			t.Fatalf("session %d has %d messages, want its own user/assistant pair", i, len(result.Messages))
		}
		if got, want := result.Messages[0].Content[0].Text, "prompt for session "+string(rune('A'+i)); got != want {
			t.Errorf("session %d user message = %q, want %q", i, got, want)
		}
	}
}
