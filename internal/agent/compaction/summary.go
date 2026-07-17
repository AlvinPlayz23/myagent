package compaction

import (
	"context"
	"fmt"
	"strings"

	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/types"
)

// GenerateSummary calls the provider/model to produce a structured summary of
// the given messages. Ported from pi generateSummary.
//
// The conversation is serialized to text (so the model cannot continue it) and
// sent as a single user message under SummarizationSystemPrompt, with NO
// tools. When previousSummary is non-empty, the UPDATE prompt is used and the
// previous summary is included in a <previous-summary> block for iterative
// merging.
//
// reserveTokens bounds the response size (maxTokens = 0.8 * reserveTokens),
// matching pi.
func GenerateSummary(ctx context.Context, provider llm.Provider, model llm.Model, msgs []types.Message, reserveTokens int, previousSummary string) (string, error) {
	if provider == nil {
		return "", fmt.Errorf("compaction: provider is nil")
	}
	if len(msgs) == 0 {
		return "No prior history.", nil
	}

	maxTokens := 0
	if reserveTokens > 0 {
		maxTokens = (reserveTokens * 8) / 10 // floor(0.8 * reserveTokens)
	}

	basePrompt := SummarizationPrompt
	if previousSummary != "" {
		basePrompt = UpdateSummarizationPrompt
	}

	var promptText strings.Builder
	promptText.WriteString("<conversation>\n")
	promptText.WriteString(SerializeConversation(msgs))
	promptText.WriteString("\n</conversation>\n\n")
	if previousSummary != "" {
		promptText.WriteString("<previous-summary>\n")
		promptText.WriteString(previousSummary)
		promptText.WriteString("\n</previous-summary>\n\n")
	}
	promptText.WriteString(basePrompt)

	summarizationMessages := []types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{types.TextBlock(promptText.String())},
	}}

	req := llm.Request{
		SystemPrompt: SummarizationSystemPrompt,
		Messages:     summarizationMessages,
		Tools:        nil, // summarization never uses tools
		MaxTokens:    &maxTokens,
	}

	events, err := provider.Stream(ctx, model, req)
	if err != nil {
		return "", fmt.Errorf("compaction: stream failed: %w", err)
	}

	var final *types.Message
	for ev := range events {
		switch ev.Type {
		case "done":
			final = ev.Message
		case "error":
			if ev.Error != nil {
				return "", fmt.Errorf("compaction: summarization failed: %s", ev.Error.ErrorMessage)
			}
			return "", fmt.Errorf("compaction: summarization failed")
		}
	}
	if final == nil {
		return "", fmt.Errorf("compaction: stream ended without a terminal event")
	}
	if final.StopReason == types.StopAborted {
		return "", fmt.Errorf("compaction: summarization aborted")
	}
	if final.StopReason == types.StopError {
		msg := final.ErrorMessage
		if msg == "" {
			msg = "unknown error"
		}
		return "", fmt.Errorf("compaction: summarization failed: %s", msg)
	}

	text := assistantText(*final)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("compaction: summarization produced no text")
	}
	return text, nil
}

// assistantText concatenates the text content blocks of an assistant message.
func assistantText(m types.Message) string {
	var parts []string
	for _, b := range m.Content {
		if b.Type == types.ContentText && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
