package compaction

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/myagent/myagent/internal/types"
)

// ToolResultMaxChars bounds a tool result's serialized text. Ported from pi
// TOOL_RESULT_MAX_CHARS. Keeps summarization requests within reasonable token
// budgets since tool results (especially from read and bash) are typically the
// largest contributors to context size.
const ToolResultMaxChars = 2000

// SerializeConversation flattens messages to plain text for the summarization
// prompt, so the model cannot treat it as a conversation to continue. Ported
// from pi serializeConversation.
//
// Output shape:
//
//	[User]: message text
//	[Assistant thinking]: thinking content
//	[Assistant]: response text
//	[Assistant tool calls]: read(path=...); bash(command=...)
//	[Tool result]: output text (truncated to ToolResultMaxChars)
func SerializeConversation(msgs []types.Message) string {
	var parts []string
	for _, m := range msgs {
		switch m.Role {
		case types.RoleUser:
			if s := userText(m.Content); s != "" {
				parts = append(parts, "[User]: "+s)
			}
		case types.RoleAssistant:
			var textParts, thinkingParts, toolCalls []string
			for _, b := range m.Content {
				switch b.Type {
				case types.ContentText:
					if b.Text != "" {
						textParts = append(textParts, b.Text)
					}
				case types.ContentThinking:
					if b.Thinking != "" {
						thinkingParts = append(thinkingParts, b.Thinking)
					}
				case types.ContentToolCall:
					toolCalls = append(toolCalls, formatToolCall(b.Name, b.Arguments))
				}
			}
			if len(thinkingParts) > 0 {
				parts = append(parts, "[Assistant thinking]: "+strings.Join(thinkingParts, "\n"))
			}
			if len(textParts) > 0 {
				parts = append(parts, "[Assistant]: "+strings.Join(textParts, "\n"))
			}
			if len(toolCalls) > 0 {
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(toolCalls, "; "))
			}
		case types.RoleToolResult:
			if s := userText(m.Content); s != "" {
				parts = append(parts, "[Tool result]: "+truncateForSummary(s, ToolResultMaxChars))
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

// formatToolCall renders a tool call as name(k=v, k=v). Mirrors pi's
// `${block.name}(${argsStr})`.
func formatToolCall(name string, args map[string]any) string {
	type kv struct {
		k string
		v any
	}
	// Stable order by key so output is deterministic for tests.
	var pairs []kv
	for k, v := range args {
		pairs = append(pairs, kv{k: k, v: v})
	}
	// Sort by key.
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j-1].k > pairs[j].k; j-- {
			pairs[j-1], pairs[j] = pairs[j], pairs[j-1]
		}
	}
	var sb strings.Builder
	sb.WriteString(name)
	sb.WriteByte('(')
	for i, p := range pairs {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(p.k)
		sb.WriteByte('=')
		sb.WriteString(safeJSONString(p.v))
	}
	sb.WriteByte(')')
	return sb.String()
}

func userText(blocks []types.ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == types.ContentText && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "")
}

// truncateForSummary truncates text to maxChars and appends a marker. Ported
// from pi truncateForSummary.
func truncateForSummary(text string, maxChars int) string {
	if utf8.RuneCountInString(text) <= maxChars {
		return text
	}
	runes := []rune(text)
	truncatedChars := len(runes) - maxChars
	return string(runes[:maxChars]) + "\n\n[... " + itoa(truncatedChars) + " more characters truncated]"
}

func safeJSONString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[unserializable]"
	}
	return string(b)
}

// itoa is a small allocation-free int→string for the truncation marker.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
