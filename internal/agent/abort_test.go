package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

type abortTestTool struct {
	name    string
	started chan struct{}
	calls   int
	mu      sync.Mutex
}

func (t *abortTestTool) Name() string               { return t.name }
func (t *abortTestTool) Description() string        { return "test" }
func (t *abortTestTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (t *abortTestTool) callCount() int             { t.mu.Lock(); defer t.mu.Unlock(); return t.calls }
func (t *abortTestTool) Execute(ctx context.Context, _ string, _ map[string]any) (*types.ToolResult, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	if t.started != nil {
		close(t.started)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

type toolCallProvider struct {
	mu       sync.Mutex
	requests int
}

func (p *toolCallProvider) Stream(_ context.Context, _ llm.Model, _ llm.Request) (<-chan llm.StreamEvent, error) {
	p.mu.Lock()
	p.requests++
	p.mu.Unlock()
	out := make(chan llm.StreamEvent, 2)
	out <- llm.StreamEvent{Type: "start", Partial: &types.Message{Role: types.RoleAssistant}}
	out <- llm.StreamEvent{Type: "done", Message: &types.Message{
		Role: types.RoleAssistant, StopReason: types.StopToolUse,
		Content: []types.ContentBlock{
			{Type: types.ContentToolCall, ID: "1", Name: "first", Arguments: map[string]any{}},
			{Type: types.ContentToolCall, ID: "2", Name: "second", Arguments: map[string]any{}},
		},
	}}
	close(out)
	return out, nil
}

func (p *toolCallProvider) requestCount() int { p.mu.Lock(); defer p.mu.Unlock(); return p.requests }

type successfulTestTool struct{ name string }

func (t *successfulTestTool) Name() string               { return t.name }
func (t *successfulTestTool) Description() string        { return "test" }
func (t *successfulTestTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (t *successfulTestTool) Execute(context.Context, string, map[string]any) (*types.ToolResult, error) {
	return types.TextResult("done", nil), nil
}

func TestAbortDuringToolDoesNotRunLaterToolsOrRequestAgain(t *testing.T) {
	provider := &toolCallProvider{}
	first := &abortTestTool{name: "first", started: make(chan struct{})}
	second := &abortTestTool{name: "second"}
	loop := New(Config{
		Provider: provider,
		Model:    llm.Model{ID: "test"},
		Registry: tools.NewRegistry(first, second),
	}, nil, func(context.Context, types.AgentEvent) error { return nil })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := loop.Run(ctx, []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("go")}}})
		done <- err
	}()

	select {
	case <-first.started:
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("first tool did not start")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop after cancellation")
	}
	if got := first.callCount(); got != 1 {
		t.Fatalf("first tool calls = %d, want 1", got)
	}
	if got := second.callCount(); got != 0 {
		t.Fatalf("second tool calls = %d, want 0", got)
	}
	if got := provider.requestCount(); got != 1 {
		t.Fatalf("provider requests = %d, want 1", got)
	}

	// Preserve structurally valid context: each assistant tool call has a result,
	// but cancellation did not cause another model turn.
	msgs := loop.Messages()
	var results int
	for _, m := range msgs {
		if m.Role == types.RoleToolResult {
			results++
		}
	}
	if results != 2 {
		t.Fatalf("tool results in history = %d, want 2", results)
	}
}

func TestToolResultPersistenceFailureStopsBeforeHistoryMutation(t *testing.T) {
	provider := &toolCallProvider{}
	want := context.DeadlineExceeded
	loop := New(Config{
		Provider: provider,
		Model:    llm.Model{ID: "test"},
		Registry: tools.NewRegistry(&successfulTestTool{name: "first"}, &successfulTestTool{name: "second"}),
	}, nil, func(_ context.Context, ev types.AgentEvent) error {
		if ev.Type == types.EventMessageEnd && ev.Message != nil && ev.Message.Role == types.RoleToolResult {
			return want
		}
		return nil
	})

	_, err := loop.Run(context.Background(), []types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{types.TextBlock("go")},
	}})
	if err != want {
		t.Fatalf("Run error = %v, want %v", err, want)
	}
	if got := provider.requestCount(); got != 1 {
		t.Fatalf("provider requests = %d, want 1", got)
	}

	// The assistant tool-call message was successfully emitted before tool
	// execution, but the failed tool result was not admitted to loop history.
	for _, msg := range loop.Messages() {
		if msg.Role == types.RoleToolResult {
			t.Fatalf("history contains a tool result after its persistence failed: %#v", msg)
		}
	}
}
