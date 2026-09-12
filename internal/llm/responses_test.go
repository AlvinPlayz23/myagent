package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func streamResponsesFromSSE(t *testing.T, modelID, payload string, check func(t *testing.T, r *http.Request, body string)) []StreamEvent {
	t.Helper()
	var gotBody string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		buf := new(strings.Builder)
		_, _ = fmt.Fprint(buf, "")
		_ = buf
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		} else {
			// Chunked / unknown length: read up to 64k.
			tmp := make([]byte, 64*1024)
			n, _ := r.Body.Read(tmp)
			body = tmp[:n]
		}
		gotBody = string(body)
		if check != nil {
			// Defer path/body assertions to caller after collect.
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	provider := NewOpenAIProvider("")
	stream, err := provider.Stream(context.Background(), Model{ID: modelID, BaseURL: srv.URL}, Request{})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, stream)
	if !strings.HasSuffix(gotPath, "/responses") {
		t.Fatalf("request path = %q, want suffix /responses", gotPath)
	}
	if check != nil {
		check(t, nil, gotBody)
	}
	return events
}

func TestMuseSparkRoutesToResponses(t *testing.T) {
	events := streamResponsesFromSSE(t, "muse-spark-1.3-contributor-free",
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n",
		nil)
	terminal := terminalEvent(t, events)
	if terminal.Type != "done" || terminal.Message == nil {
		t.Fatalf("terminal = %#v, want done", terminal)
	}
	if terminal.Message.API != "openai-responses" {
		t.Fatalf("API = %q, want openai-responses", terminal.Message.API)
	}
	if len(terminal.Message.Content) != 1 || terminal.Message.Content[0].Text != "hi" {
		t.Fatalf("content = %#v, want hi", terminal.Message.Content)
	}
}

func TestResponsesToolCallStreams(t *testing.T) {
	events := streamResponsesFromSSE(t, "muse-spark-1.3",
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"read\"}}\n\n"+
			"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"delta\":\"{}\"}}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n",
		nil)
	terminal := terminalEvent(t, events)
	if terminal.Type != "done" || terminal.Message == nil {
		t.Fatalf("terminal = %#v, want done", terminal)
	}
	if terminal.Message.StopReason != types.StopToolUse {
		t.Fatalf("stop = %q, want toolUse", terminal.Message.StopReason)
	}
	calls := terminal.Message.ToolCalls()
	if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Name != "read" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestResponsesHTTPErrorCarriesHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"type":"error","error":{"type":"MissingSessionID","message":"OpenCode's free tier can only be used in OpenCode"}}`)
	}))
	defer srv.Close()

	provider := NewOpenAIProvider("")
	stream, err := provider.Stream(context.Background(), Model{ID: "muse-spark-1.3-contributor-free", BaseURL: srv.URL}, Request{})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, stream)
	terminal := terminalEvent(t, events)
	if terminal.Type != "error" || terminal.Error == nil {
		t.Fatalf("terminal = %#v, want error", terminal)
	}
	if !strings.Contains(terminal.Error.ErrorMessage, "requires an OpenCode client session") {
		t.Fatalf("error = %q, want OpenCode client session hint", terminal.Error.ErrorMessage)
	}
	if terminal.Retryable {
		t.Fatalf("retryable = true, want false for 400")
	}
}

func TestResponsesArgsOnlyInArgumentsDone(t *testing.T) {
	// Gateway buffers arguments into the done event with no prior deltas.
	events := streamResponsesFromSSE(t, "muse-spark-1.3",
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"bash\"}}\n\n"+
			"data: {\"type\":\"response.function_call_arguments.done\",\"output_index\":0,\"arguments\":\"{\\\"command\\\":\\\"ls\\\"}\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n",
		nil)
	terminal := terminalEvent(t, events)
	if terminal.Type != "done" || terminal.Message == nil {
		t.Fatalf("terminal = %#v, want done", terminal)
	}
	calls := terminal.Message.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want one", terminal.Message.Content)
	}
	if got := fmt.Sprint(calls[0].Arguments["command"]); got != "ls" {
		t.Fatalf("command = %q (%#v), want ls", got, calls[0].Arguments)
	}
}

func TestResponsesArgsAsObjectInItemDone(t *testing.T) {
	// Some gateways send arguments as a decoded object instead of a string.
	events := streamResponsesFromSSE(t, "muse-spark-1.3",
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"bash\"}}\n\n"+
			"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"bash\",\"arguments\":{\"command\":\"pwd\"}}}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n",
		nil)
	terminal := terminalEvent(t, events)
	if terminal.Type != "done" || terminal.Message == nil {
		t.Fatalf("terminal = %#v, want done", terminal)
	}
	calls := terminal.Message.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want one", terminal.Message.Content)
	}
	if got := fmt.Sprint(calls[0].Arguments["command"]); got != "pwd" {
		t.Fatalf("command = %q (%#v), want pwd", got, calls[0].Arguments)
	}
}

func TestResponsesDeltasPlusDoneDoNotDuplicate(t *testing.T) {
	// Normal OpenAI shape: deltas stream, then done repeats the full args.
	events := streamResponsesFromSSE(t, "muse-spark-1.3",
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"bash\"}}\n\n"+
			"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"delta\":\"{\\\"command\\\"\"}\n\n"+
			"data: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"delta\":\":\\\"ls\\\"}\"}\n\n"+
			"data: {\"type\":\"response.function_call_arguments.done\",\"output_index\":0,\"arguments\":\"{\\\"command\\\":\\\"ls\\\"}\"}\n\n"+
			"data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n",
		nil)
	terminal := terminalEvent(t, events)
	if terminal.Type != "done" || terminal.Message == nil {
		t.Fatalf("terminal = %#v, want done", terminal)
	}
	calls := terminal.Message.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want one", terminal.Message.Content)
	}
	if got := fmt.Sprint(calls[0].Arguments["command"]); got != "ls" {
		t.Fatalf("command = %q (%#v), want exactly ls", got, calls[0].Arguments)
	}
}
