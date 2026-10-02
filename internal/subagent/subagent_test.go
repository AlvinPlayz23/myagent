package subagent

import (
	"context"
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

type replyProvider struct{ text string }

func (p replyProvider) Stream(context.Context, llm.Model, llm.Request) (<-chan llm.StreamEvent, error) {
	msg := types.Message{
		Role:       types.RoleAssistant,
		StopReason: types.StopStop,
		Content: []types.ContentBlock{
			{Type: types.ContentThinking, Thinking: "secret chain of thought"},
			types.TextBlock(p.text),
		},
	}
	out := make(chan llm.StreamEvent, 1)
	out <- llm.StreamEvent{Type: "done", Message: &msg}
	close(out)
	return out, nil
}

func parentConfig(text string) agent.Config {
	return agent.Config{
		Provider: replyProvider{text},
		Model:    llm.Model{ID: "m", Provider: "p"},
		Registry: tools.NewRegistry(New()),
	}
}

func TestPrepareBackgroundRejectsBlankPrompt(t *testing.T) {
	if _, err := New().PrepareBackground(context.Background(), "c", map[string]any{"prompt": "  "}, parentConfig("x")); err == nil {
		t.Fatal("expected error for blank prompt")
	}
}

func TestPrepareBackgroundBuildsFullReport(t *testing.T) {
	long := strings.Repeat("é", maxResultBytes) // untruncated, multibyte
	task, err := New(WithCwd("/work")).PrepareBackground(context.Background(), "call-1",
		map[string]any{"prompt": "  do it  "}, parentConfig(long))
	if err != nil {
		t.Fatal(err)
	}
	if task.ID == "" || task.Ack == nil || task.Ack.Terminate {
		t.Fatalf("bad task: %+v", task)
	}
	msg, err := task.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg.Role != types.RoleUser || msg.Source != types.SourceSubagentCompletion {
		t.Fatalf("role/source = %s/%s", msg.Role, msg.Source)
	}
	rep, ok := msg.Details.(TaskReport)
	if !ok {
		t.Fatalf("details = %T", msg.Details)
	}
	if rep.Status != StatusCompleted || rep.TaskID != task.ID || rep.ToolCallID != "call-1" {
		t.Fatalf("report = %+v", rep)
	}
	if rep.Prompt != "do it" {
		t.Fatalf("prompt = %q", rep.Prompt)
	}
	if rep.FinalResult != long {
		t.Fatalf("final result truncated: %d bytes", len(rep.FinalResult))
	}
	for _, tl := range rep.Tools {
		if tl.Name == ToolName {
			t.Fatal("child must not see the subagent tool")
		}
	}
	text := msg.Content[0].Text
	if !strings.HasPrefix(text, completionPreamble) || strings.Contains(text, "secret chain of thought") {
		t.Fatal("preamble missing or thinking leaked into the report")
	}
	if len(rep.Trajectory) < 2 || rep.Trajectory[0].Role != "user" {
		t.Fatalf("trajectory = %+v", rep.Trajectory)
	}
}

func TestOnErrorReportsCancellation(t *testing.T) {
	task, err := New().PrepareBackground(context.Background(), "c", map[string]any{"prompt": "x"}, parentConfig("x"))
	if err != nil {
		t.Fatal(err)
	}
	rep := task.OnError(context.Canceled).Details.(TaskReport)
	if rep.Status != StatusCancelled || rep.Prompt != "x" {
		t.Fatalf("report = %+v", rep)
	}
}
