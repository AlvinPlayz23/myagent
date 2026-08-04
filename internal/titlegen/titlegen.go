// Package titlegen generates concise session titles without involving the coding agent.
package titlegen

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

const systemPrompt = `Generate a concise title for a coding-agent session from the user's request. Return only the title: no quotes, markdown, explanation, or punctuation suffix. Use 3 to 7 words when possible.`

const maxTitleRunes = 80

// Generate makes a small, isolated no-tools request using only prompt. Its
// response is never added to the main agent's conversation history.
func Generate(ctx context.Context, provider llm.Provider, model llm.Model, prompt string) (string, error) {
	if provider == nil {
		return "", fmt.Errorf("title generation requires an LLM provider")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", fmt.Errorf("title generation requires a prompt")
	}
	maxTokens := 32
	temperature := 0.2
	events, err := provider.Stream(ctx, model, llm.Request{
		SystemPrompt: systemPrompt,
		Messages:     []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock(prompt)}}},
		Temperature:  &temperature,
		MaxTokens:    &maxTokens,
	})
	if err != nil {
		return "", err
	}
	for event := range events {
		switch event.Type {
		case "error":
			if event.Error != nil && event.Error.ErrorMessage != "" {
				return "", fmt.Errorf("title generation: %s", event.Error.ErrorMessage)
			}
			return "", fmt.Errorf("title generation failed")
		case "done":
			if event.Message == nil {
				return "", fmt.Errorf("title generation returned no message")
			}
			return clean(event.Message)
		}
	}
	return "", fmt.Errorf("title generation stream ended without a result")
}

func clean(message *types.Message) (string, error) {
	var parts []string
	for _, block := range message.Content {
		if block.Type == types.ContentText {
			parts = append(parts, block.Text)
		}
	}
	title := strings.Join(parts, " ")
	title = strings.Join(strings.Fields(title), " ")
	title = strings.Trim(title, " \t\r\n\"'`*_#:-–—")
	if title == "" {
		return "", fmt.Errorf("title generation returned an empty title")
	}
	if utf8.RuneCountInString(title) > maxTitleRunes {
		runes := []rune(title)
		title = strings.TrimSpace(string(runes[:maxTitleRunes]))
	}
	return title, nil
}
