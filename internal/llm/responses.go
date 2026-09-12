package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

// --- Responses API request shapes (OpenAI Responses API, used by Zen for Muse Spark) ---

type responsesRequest struct {
	Model           string         `json:"model"`
	Input           []any          `json:"input"`
	Stream          bool           `json:"stream"`
	Store           bool           `json:"store"`
	Reasoning       *respReasoning `json:"reasoning,omitempty"`
	Include         []string       `json:"include,omitempty"`
	Tools           []respTool     `json:"tools,omitempty"`
	Temperature     *float64       `json:"temperature,omitempty"`
	MaxOutputTokens *int           `json:"max_output_tokens,omitempty"`
}

type respReasoning struct {
	Effort  Effort `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type respTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// buildResponsesBody converts a Request into the Responses API JSON body.
// Effort is normalized then clamped for Contributor-tier Muse Spark
// (max is Standard-tier only).
func buildResponsesBody(model Model, req Request) ([]byte, error) {
	effort, err := NormalizeEffort(model, req.Effort)
	if err != nil {
		return nil, err
	}
	effort = ClampContributorEffort(model.ID, effort)
	req.Effort = effort

	rr := responsesRequest{
		Model:  model.ID,
		Input:  convertResponsesInput(req.SystemPrompt, req.Messages),
		Stream: true,
		Store:  false,
	}
	if req.Temperature != nil && effort == "" {
		rr.Temperature = req.Temperature
	}
	if req.MaxTokens != nil {
		rr.MaxOutputTokens = req.MaxTokens
	}
	if effort != "" {
		rr.Reasoning = &respReasoning{Effort: effort, Summary: "auto"}
		rr.Include = []string{"reasoning.encrypted_content"}
	}
	for _, t := range req.Tools {
		rr.Tools = append(rr.Tools, respTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}
	return json.Marshal(rr)
}

// convertResponsesInput maps core messages to Responses API input items.
// Text-only user messages use input_text parts; images use input_image with
// data URIs; tool results become function_call_output items.
func convertResponsesInput(systemPrompt string, messages []types.Message) []any {
	var out []any
	if systemPrompt != "" {
		out = append(out, map[string]any{"role": "system", "content": systemPrompt})
	}
	for _, m := range messages {
		switch m.Role {
		case types.RoleUser:
			out = append(out, userResponsesItem(m))
		case types.RoleAssistant:
			if txt := textOf(m.Content); txt != "" {
				out = append(out, map[string]any{
					"role":    "assistant",
					"content": []any{map[string]any{"type": "output_text", "text": txt}},
				})
			}
			for _, c := range m.Content {
				if c.Type != types.ContentToolCall {
					continue
				}
				args, _ := json.Marshal(c.Arguments)
				out = append(out, map[string]any{
					"type":      "function_call",
					"call_id":   c.ID,
					"name":      c.Name,
					"arguments": string(args),
				})
			}
		case types.RoleToolResult:
			text := textOf(m.Content)
			if text == "" {
				text = "(no tool output)"
			}
			callID := m.ToolCallID
			if callID == "" {
				callID = m.ToolName
			}
			out = append(out, map[string]any{
				"type":    "function_call_output",
				"call_id": callID,
				"output":  text,
			})
		}
	}
	if out == nil {
		out = []any{}
	}
	return out
}

func userResponsesItem(m types.Message) any {
	var parts []any
	for _, b := range m.Content {
		switch b.Type {
		case types.ContentText:
			if b.Text != "" {
				parts = append(parts, map[string]any{"type": "input_text", "text": b.Text})
			}
		case types.ContentImage:
			parts = append(parts, map[string]any{
				"type":      "input_image",
				"detail":    "auto",
				"image_url": "data:" + b.MimeType + ";base64," + b.Data,
			})
		}
	}
	if len(parts) == 0 {
		parts = []any{map[string]any{"type": "input_text", "text": ""}}
	}
	return map[string]any{"role": "user", "content": parts}
}

// runResponses streams a Responses API request (POST {baseURL}/responses).
// Event shapes follow the OpenAI Responses SSE protocol:
// response.output_text.delta, response.function_call_arguments.delta,
// response.output_item.added/done, response.completed/failed, error.
func (p *OpenAIProvider) runResponses(ctx context.Context, model Model, req Request, out chan<- StreamEvent) {
	output := &types.Message{
		Role:       types.RoleAssistant,
		Content:    []types.ContentBlock{},
		API:        "openai-responses",
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      &types.Usage{},
		StopReason: types.StopStop,
		Timestamp:  time.Now().UnixMilli(),
	}
	emitError := func(err error, retryable bool) {
		if ctx.Err() != nil {
			output.StopReason = types.StopAborted
		} else {
			output.StopReason = types.StopError
		}
		output.ErrorMessage = err.Error()
		out <- StreamEvent{Type: "error", Error: output, Retryable: retryable}
	}

	body, err := buildResponsesBody(model, req)
	if err != nil {
		emitError(err, false)
		return
	}

	url := strings.TrimRight(model.BaseURL, "/") + "/responses"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		emitError(err, false)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	setZenHeaders(httpReq, model)

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		emitError(err, true)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(io.LimitReader(resp.Body, 4000))
		raw := strings.TrimSpace(buf.String())
		hint := ClassifyZenError(resp.StatusCode, raw, model.ID, "/responses")
		emitError(fmt.Errorf("%d: %s%s", resp.StatusCode, raw, hint), isRetryableStatus(resp.StatusCode) && !IsZenFreeTierLimit(resp.StatusCode, raw, model.ID, model.BaseURL))
		return
	}

	acc := newAccumulator(output, out)
	started := false
	sawDone := false
	hasOutput := false
	// output_index -> call id/name/args for in-flight function calls.
	callIDs := map[int]string{}
	callNames := map[int]string{}
	argBufs := map[int]string{}

	emitStart := func() {
		if !started {
			started = true
			out <- StreamEvent{Type: "start", Partial: cloneMessage(output)}
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawDone = true
			break
		}
		var evt map[string]any
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			continue
		}
		typ, _ := evt["type"].(string)
		switch typ {
		case "response.output_text.delta":
			delta, _ := evt["delta"].(string)
			if delta != "" {
				emitStart()
				hasOutput = true
				acc.appendText(delta)
			}
		case "response.reasoning_summary_text.delta",
			"response.reasoning_text.delta":
			delta, _ := evt["delta"].(string)
			if delta != "" {
				emitStart()
				hasOutput = true
				acc.appendThinking(delta)
			}
		case "response.output_item.added":
			item, _ := evt["item"].(map[string]any)
			if item == nil {
				continue
			}
			if item["type"] == "function_call" {
				idx := intField(evt, "output_index")
				callID, _ := item["call_id"].(string)
				if callID == "" {
					callID, _ = item["id"].(string)
				}
				name, _ := item["name"].(string)
				if callID != "" {
					callIDs[idx] = callID
				}
				if name != "" {
					callNames[idx] = name
				}
				// Some gateways announce the call with its arguments already
				// attached; seed the buffer so later deltas accumulate onto it.
				if seed := responsesArgsString(item["arguments"]); seed != "" {
					argBufs[idx] = seed
				}
				// Emit start immediately so tool loops track the call.
				if callID != "" || name != "" {
					emitStart()
					hasOutput = true
					acc.applyToolCall(deltaToolCall{Index: idx, ID: callID, Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: name, Arguments: argBufs[idx]}})
				}
			}
		case "response.function_call_arguments.delta":
			delta := responsesArgsString(evt["delta"])
			if delta == "" {
				continue
			}
			idx := intField(evt, "output_index")
			argBufs[idx] += delta
			emitStart()
			hasOutput = true
			acc.applyToolCall(deltaToolCall{Index: idx, ID: callIDs[idx], Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: callNames[idx], Arguments: delta}})
		case "response.function_call_arguments.done":
			// Authoritative final arguments (mirrors pi): replace the
			// accumulated buffer so calls whose args were never streamed as
			// deltas still arrive complete.
			final := responsesArgsString(evt["arguments"])
			idx := intField(evt, "output_index")
			if callID := callIDs[idx]; callID == "" {
				if id, ok := evt["item_id"].(string); ok {
					callIDs[idx] = id
				}
			}
			argBufs[idx] = final
			if final == "" && callIDs[idx] == "" && callNames[idx] == "" {
				continue
			}
			emitStart()
			hasOutput = true
			acc.setToolCallArgs(idx, callIDs[idx], callNames[idx], final)
		case "response.output_item.done":
			item, _ := evt["item"].(map[string]any)
			if item == nil || item["type"] != "function_call" {
				continue
			}
			idx := intField(evt, "output_index")
			callID, _ := item["call_id"].(string)
			if callID == "" {
				callID = callIDs[idx]
			} else {
				callIDs[idx] = callID
			}
			name, _ := item["name"].(string)
			if name == "" {
				name = callNames[idx]
			} else {
				callNames[idx] = name
			}
			args := responsesArgsString(item["arguments"])
			if args == "" {
				args = argBufs[idx]
			} else {
				argBufs[idx] = args
			}
			if callID != "" || name != "" || args != "" {
				emitStart()
				hasOutput = true
				acc.setToolCallArgs(idx, callID, name, args)
			}
		case "response.completed":
			sawDone = true
			if r, ok := evt["response"].(map[string]any); ok {
				applyResponsesUsage(output, r["usage"])
				if status, _ := r["status"].(string); status == "failed" || status == "incomplete" {
					emitError(fmt.Errorf("Provider returned response status: %s", status), false)
					return
				}
			}
		case "response.failed", "response.incomplete":
			msg := typ
			if r, ok := evt["response"].(map[string]any); ok {
				if s, _ := r["status"].(string); s != "" {
					msg = "Provider returned response status: " + s
				}
			}
			emitError(fmt.Errorf("%s", msg), false)
			return
		case "error":
			msg := "Provider returned an error"
			if e, ok := evt["error"].(map[string]any); ok {
				if s, _ := e["message"].(string); s != "" {
					msg = s
				}
			} else if s, _ := evt["message"].(string); s != "" {
				msg = s
			}
			emitError(fmt.Errorf("%s", msg), false)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		emitError(err, !started)
		return
	}

	acc.finish()

	if ctx.Err() != nil {
		emitError(fmt.Errorf("Request was aborted"), false)
		return
	}
	if output.StopReason == types.StopAborted {
		emitError(fmt.Errorf("Request was aborted"), false)
		return
	}
	if output.StopReason == types.StopError {
		msg := output.ErrorMessage
		if msg == "" {
			msg = "Provider returned an error stop reason"
		}
		emitError(fmt.Errorf("%s", msg), false)
		return
	}
	if !hasOutput || (!sawDone && !started) {
		emitError(fmt.Errorf("Stream ended without finish_reason"), !started)
		return
	}
	if len(output.ToolCalls()) > 0 {
		output.StopReason = types.StopToolUse
	} else {
		output.StopReason = types.StopStop
	}
	out <- StreamEvent{Type: "done", Message: output}
}

func intField(evt map[string]any, key string) int {
	switch v := evt[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// responsesArgsString normalizes Responses API arguments payloads, which
// arrive as a JSON string from OpenAI-compatible servers but as a decoded
// object from some gateways. A bare type assertion would silently drop the
// object form and leave the tool call with empty arguments.
func responsesArgsString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if b, err := json.Marshal(t); err == nil {
			return string(b)
		}
	}
	return ""
}

// applyResponsesUsage maps Responses API usage ({input_tokens,output_tokens,
// output_tokens_details:{reasoning_tokens}}) onto types.Usage.
func applyResponsesUsage(output *types.Message, raw any) {
	m, ok := raw.(map[string]any)
	if !ok {
		return
	}
	num := func(key string) int {
		switch v := m[key].(type) {
		case float64:
			return int(v)
		case int:
			return v
		case int64:
			return int(v)
		}
		return 0
	}
	input := num("input_tokens")
	outputTokens := num("output_tokens")
	reasoning := 0
	if det, ok := m["output_tokens_details"].(map[string]any); ok {
		switch v := det["reasoning_tokens"].(type) {
		case float64:
			reasoning = int(v)
		case int:
			reasoning = v
		case int64:
			reasoning = int(v)
		}
	}
	if output.Usage == nil {
		output.Usage = &types.Usage{}
	}
	output.Usage.Input = input
	output.Usage.Output = outputTokens
	output.Usage.Reasoning = reasoning
	output.Usage.TotalTokens = input + outputTokens
}
