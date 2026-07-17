package llm

import (
	"encoding/json"

	"github.com/myagent/myagent/internal/types"
)

// buildRequestBody converts a Request into the OpenAI chat-completions JSON body.
// Ported from pi buildParams (packages/ai/src/api/openai-completions.ts).
func buildRequestBody(model Model, req Request) ([]byte, error) {
	cr := chatRequest{
		Model:         model.ID,
		Messages:      convertMessages(req.SystemPrompt, req.Messages),
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		Temperature:   req.Temperature,
		MaxTokens:     req.MaxTokens,
	}
	for _, t := range req.Tools {
		cr.Tools = append(cr.Tools, chatTool{
			Type: "function",
			Function: chatToolFunc{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return json.Marshal(cr)
}

// convertMessages maps core Messages to OpenAI chat messages. The system prompt
// becomes a leading "system" message.
func convertMessages(systemPrompt string, messages []types.Message) []chatMessage {
	var out []chatMessage
	if systemPrompt != "" {
		out = append(out, chatMessage{Role: "system", Content: systemPrompt})
	}
	for _, m := range messages {
		switch m.Role {
		case types.RoleUser:
			out = append(out, chatMessage{Role: "user", Content: textOf(m.Content)})
		case types.RoleAssistant:
			cm := chatMessage{Role: "assistant"}
			if txt := textOf(m.Content); txt != "" {
				cm.Content = txt
			}
			for _, c := range m.Content {
				if c.Type == types.ContentToolCall {
					args, _ := json.Marshal(c.Arguments)
					cm.ToolCalls = append(cm.ToolCalls, chatToolCall{
						ID:   c.ID,
						Type: "function",
						Function: chatToolCallFunc{
							Name:      c.Name,
							Arguments: string(args),
						},
					})
				}
			}
			out = append(out, cm)
		case types.RoleToolResult:
			out = append(out, chatMessage{
				Role:       "tool",
				ToolCallID: m.ToolCallID,
				Name:       m.ToolName,
				Content:    textOf(m.Content),
			})
		}
	}
	return out
}

// textOf concatenates text content blocks.
func textOf(blocks []types.ContentBlock) string {
	var sb []byte
	for _, b := range blocks {
		if b.Type == types.ContentText {
			if len(sb) > 0 {
				sb = append(sb, '\n')
			}
			sb = append(sb, b.Text...)
		}
	}
	return string(sb)
}

// parseUsage converts a raw usage payload into a types.Usage.
// Ported from pi parseChunkUsage.
func parseUsage(u *chunkUsage) types.Usage {
	cacheRead := u.PromptTokensDetails.CachedTokens
	if cacheRead == 0 {
		cacheRead = u.PromptCacheHitTokens
	}
	cacheWrite := u.PromptTokensDetails.CacheWriteTokens
	input := u.PromptTokens - cacheRead - cacheWrite
	if input < 0 {
		input = 0
	}
	output := u.CompletionTokens
	return types.Usage{
		Input:       input,
		Output:      output,
		CacheRead:   cacheRead,
		CacheWrite:  cacheWrite,
		Reasoning:   u.CompletionTokensDetails.ReasoningTokens,
		TotalTokens: input + output + cacheRead + cacheWrite,
	}
}

// mapStopReason maps an OpenAI finish_reason to a StopReason (and optional
// error message). Ported from pi mapStopReason.
func mapStopReason(reason string) (types.StopReason, string) {
	switch reason {
	case "", "stop", "end":
		return types.StopStop, ""
	case "length":
		return types.StopLength, ""
	case "function_call", "tool_calls":
		return types.StopToolUse, ""
	default:
		return types.StopError, "Provider finish_reason: " + reason
	}
}

// cloneMessage returns a deep-enough copy of a Message for use as an event
// Partial snapshot: the content slice is copied so later mutation of the
// accumulator does not race with a consumer inspecting the snapshot.
func cloneMessage(m *types.Message) *types.Message {
	cp := *m
	cp.Content = make([]types.ContentBlock, len(m.Content))
	copy(cp.Content, m.Content)
	if m.Usage != nil {
		u := *m.Usage
		cp.Usage = &u
	}
	return &cp
}
