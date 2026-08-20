package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func streamFromSSE(t *testing.T, payload string) []StreamEvent {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	provider := NewOpenAIProvider("")
	stream, err := provider.Stream(context.Background(), Model{ID: "model", BaseURL: srv.URL}, Request{})
	if err != nil {
		t.Fatal(err)
	}
	return collect(t, stream)
}

func terminalEvent(t *testing.T, events []StreamEvent) StreamEvent {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("stream emitted no events")
	}
	return events[len(events)-1]
}

func TestStreamAcceptsDoneAfterTextWithoutFinishReason(t *testing.T) {
	events := streamFromSSE(t, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n"+
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n"+
		"data: [DONE]\n\n")

	terminal := terminalEvent(t, events)
	if terminal.Type != "done" || terminal.Message == nil {
		t.Fatalf("terminal event = %#v, want done", terminal)
	}
	if terminal.Message.StopReason != types.StopStop {
		t.Fatalf("stop reason = %q, want stop", terminal.Message.StopReason)
	}
	if len(terminal.Message.Content) != 1 || terminal.Message.Content[0].Text != "hi" {
		t.Fatalf("content = %#v, want hi", terminal.Message.Content)
	}
}

func TestStreamAcceptsCleanEOFAfterTextWithoutFinishReason(t *testing.T) {
	events := streamFromSSE(t, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":null}]}\n\n")

	terminal := terminalEvent(t, events)
	if terminal.Type != "done" || terminal.Message == nil || terminal.Message.StopReason != types.StopStop {
		t.Fatalf("terminal event = %#v, want inferred stop", terminal)
	}
}

func TestStreamAcceptsDoneAfterToolCallWithoutFinishReason(t *testing.T) {
	events := streamFromSSE(t, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]},\"finish_reason\":null}]}\n\n"+
		"data: [DONE]\n\n")

	terminal := terminalEvent(t, events)
	if terminal.Type != "done" || terminal.Message == nil {
		t.Fatalf("terminal event = %#v, want done", terminal)
	}
	if terminal.Message.StopReason != types.StopToolUse {
		t.Fatalf("stop reason = %q, want toolUse", terminal.Message.StopReason)
	}
	calls := terminal.Message.ToolCalls()
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Name != "read" {
		t.Fatalf("tool calls = %#v", calls)
	}
}

func TestStreamRejectsUsageOnlyDoneWithoutFinishReason(t *testing.T) {
	events := streamFromSSE(t, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1}}\n\n"+
		"data: [DONE]\n\n")

	terminal := terminalEvent(t, events)
	if terminal.Type != "error" || terminal.Error == nil || terminal.Error.ErrorMessage != "Stream ended without finish_reason" {
		t.Fatalf("terminal event = %#v, want missing finish_reason error", terminal)
	}
}

func TestStreamStillUsesExplicitFinishReason(t *testing.T) {
	events := streamFromSSE(t, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"length\"}]}\n\n"+
		"data: [DONE]\n\n")

	terminal := terminalEvent(t, events)
	if terminal.Type != "done" || terminal.Message == nil || terminal.Message.StopReason != types.StopLength {
		t.Fatalf("terminal event = %#v, want explicit length stop", terminal)
	}
}
