package titlegen

import (
	"context"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

type providerFunc func(context.Context, llm.Model, llm.Request) (<-chan llm.StreamEvent, error)

func (f providerFunc) Stream(ctx context.Context, model llm.Model, req llm.Request) (<-chan llm.StreamEvent, error) {
	return f(ctx, model, req)
}

func TestGenerateUsesIsolatedNoToolsRequest(t *testing.T) {
	p := providerFunc(func(_ context.Context, _ llm.Model, req llm.Request) (<-chan llm.StreamEvent, error) {
		if len(req.Tools) != 0 || len(req.Messages) != 1 || req.Messages[0].Role != types.RoleUser {
			t.Fatalf("unexpected request: %#v", req)
		}
		if req.MaxTokens == nil || *req.MaxTokens != 32 {
			t.Fatalf("MaxTokens = %v", req.MaxTokens)
		}
		ch := make(chan llm.StreamEvent, 1)
		ch <- llm.StreamEvent{Type: "done", Message: &types.Message{Content: []types.ContentBlock{types.TextBlock("  **Build login flow**  ")}}}
		close(ch)
		return ch, nil
	})
	title, err := Generate(context.Background(), p, llm.Model{ID: "small"}, "add login")
	if err != nil {
		t.Fatal(err)
	}
	if title != "Build login flow" {
		t.Fatalf("title = %q", title)
	}
}
