package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

type panicTool struct{}

func (panicTool) Name() string               { return "panic" }
func (panicTool) Description() string        { return "always panics" }
func (panicTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (panicTool) Execute(context.Context, string, map[string]any) (*types.ToolResult, error) {
	panic("boom")
}

// TestRunToolRecoversFromPanic ensures a panicking tool degrades to an error
// tool-result instead of crashing the process.
func TestRunToolRecoversFromPanic(t *testing.T) {
	l := New(Config{Registry: tools.NewRegistry(panicTool{})}, nil, func(context.Context, types.AgentEvent) error {
		return nil
	})
	result, isError := l.runTool(context.Background(), types.ContentBlock{
		Type: types.ContentToolCall, ID: "t1", Name: "panic",
	})
	if !isError {
		t.Fatal("runTool reported isError = false for a panicking tool")
	}
	if text := result.Content[0].Text; !strings.Contains(text, "panicked: boom") {
		t.Errorf("result text = %q, want it to mention the panic", text)
	}
}
