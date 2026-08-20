package llm

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

// buildRequestBody converts a Request into the OpenAI chat-completions JSON body.
// Ported from pi buildParams (packages/ai/src/api/openai-completions.ts).
func buildRequestBody(model Model, req Request) ([]byte, error) {
	effort, err := NormalizeEffort(model, req.Effort)
	if err != nil {
		return nil, err
	}
	req.Effort = effort
	provider := reasoningProvider(model)
	cr := chatRequest{
		Model:         model.ID,
		Messages:      convertMessages(req.SystemPrompt, req.Messages, provider != reasoningProviderDefault),
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
		Temperature:   req.Temperature,
		MaxTokens:     req.MaxTokens,
	}
	// Reasoning models reject sampling controls such as temperature. Explicit
	// effort also marks reasoning for unknown custom endpoints.
	if req.Effort != "" || (model.ReasoningKnown && model.Reasoning) {
		cr.Temperature = nil
	}
	if req.Effort != "" {
		switch provider {
		case reasoningProviderOpenRouter:
			cr.Reasoning = &reasoningConfig{Effort: req.Effort}
		case reasoningProviderDeepSeek:
			cr.ReasoningEffort = deepSeekEffort(req.Effort)
			cr.Thinking = &thinkingConfig{Type: "enabled"}
		default:
			cr.ReasoningEffort = req.Effort
		}
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

func deepSeekEffort(effort Effort) Effort {
	// DeepSeek's OpenAI-format reasoning_effort accepts low/high/xhigh/max;
	// medium has no direct equivalent and is mapped to its default high tier.
	if effort == EffortMedium {
		return EffortHigh
	}
	return effort
}

type reasoningProviderKind int

const (
	reasoningProviderDefault reasoningProviderKind = iota
	reasoningProviderOpenRouter
	reasoningProviderDeepSeek
)

func reasoningProvider(model Model) reasoningProviderKind {
	switch model.ReasoningDialect {
	case ReasoningDialectOpenAI:
		return reasoningProviderDefault
	case ReasoningDialectOpenRouter:
		return reasoningProviderOpenRouter
	case ReasoningDialectDeepSeek:
		return reasoningProviderDeepSeek
	}
	switch strings.ToLower(strings.TrimSpace(model.Provider)) {
	case "openrouter":
		return reasoningProviderOpenRouter
	case "deepseek":
		return reasoningProviderDeepSeek
	}
	if parsed, err := url.Parse(model.BaseURL); err == nil {
		host := strings.ToLower(parsed.Hostname())
		switch {
		case host == "openrouter.ai" || strings.HasSuffix(host, ".openrouter.ai"):
			return reasoningProviderOpenRouter
		case host == "deepseek.com" || strings.HasSuffix(host, ".deepseek.com"):
			return reasoningProviderDeepSeek
		}
	}
	return reasoningProviderDefault
}

// convertMessages maps core Messages to OpenAI chat messages. The system prompt
// becomes a leading "system" message.
func convertMessages(systemPrompt string, messages []types.Message, replayReasoning ...bool) []chatMessage {
	includeReasoning := len(replayReasoning) > 0 && replayReasoning[0]
	var out []chatMessage
	if systemPrompt != "" {
		out = append(out, chatMessage{Role: "system", Content: systemPrompt})
	}
	for i := 0; i < len(messages); i++ {
		m := messages[i]
		switch m.Role {
		case types.RoleUser:
			out = append(out, chatMessage{Role: "user", Content: userContent(m.Content)})
		case types.RoleAssistant:
			cm := chatMessage{Role: "assistant"}
			if txt := textOf(m.Content); txt != "" {
				cm.Content = txt
			}
			for _, c := range m.Content {
				switch c.Type {
				case types.ContentThinking:
					if includeReasoning {
						cm.ReasoningContent += c.Thinking
					}
				case types.ContentToolCall:
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
			var images []chatContentPart
			for {
				text := textOf(m.Content)
				hasImage := false
				for _, block := range m.Content {
					if block.Type == types.ContentImage {
						hasImage = true
						images = append(images, chatContentPart{
							Type:     "image_url",
							ImageURL: &chatImageURL{URL: "data:" + block.MimeType + ";base64," + block.Data},
						})
					}
				}
				if text == "" {
					if hasImage {
						text = "(see attached image)"
					} else {
						text = "(no tool output)"
					}
				}
				out = append(out, chatMessage{
					Role:       "tool",
					ToolCallID: m.ToolCallID,
					Name:       m.ToolName,
					Content:    text,
				})
				if i+1 >= len(messages) || messages[i+1].Role != types.RoleToolResult {
					break
				}
				i++
				m = messages[i]
			}
			if len(images) > 0 {
				out = append(out, chatMessage{
					Role: "user",
					Content: append([]chatContentPart{{
						Type: "text",
						Text: "Attached image(s) from tool result:",
					}}, images...),
				})
			}
		}
	}
	return out
}

// userContent uses OpenAI's multipart shape when a user message contains an
// image. Text-only messages remain strings for broad compatible-provider support.
func userContent(blocks []types.ContentBlock) any {
	hasImage := false
	for _, block := range blocks {
		if block.Type == types.ContentImage {
			hasImage = true
			break
		}
	}
	if !hasImage {
		return textOf(blocks)
	}
	parts := make([]chatContentPart, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case types.ContentText:
			if block.Text != "" {
				parts = append(parts, chatContentPart{Type: "text", Text: block.Text})
			}
		case types.ContentImage:
			parts = append(parts, chatContentPart{
				Type:     "image_url",
				ImageURL: &chatImageURL{URL: "data:" + block.MimeType + ";base64," + block.Data},
			})
		}
	}
	return parts
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
