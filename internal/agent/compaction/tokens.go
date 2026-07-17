package compaction

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/myagent/myagent/internal/types"
)

// Estimated chars-per-token. Mirrors pi's chars/4 heuristic. Conservative
// (overestimates tokens) so compaction triggers slightly early rather than
// late.
const charsPerToken = 4

// estimatedImageChars is the rough character cost of one image content block.
// Ported from pi ESTIMATED_IMAGE_CHARS.
const estimatedImageChars = 4800

// EstimateMessageTokens returns a conservative token estimate for one message
// using the chars/4 heuristic across all content-block kinds. Ported from pi
// estimateTokens.
func EstimateMessageTokens(m types.Message) int {
	var chars int
	switch m.Role {
	case types.RoleUser:
		chars = textAndImageChars(m.Content)
	case types.RoleAssistant:
		for _, b := range m.Content {
			switch b.Type {
			case types.ContentText:
				chars += utf8.RuneCountInString(b.Text)
			case types.ContentThinking:
				chars += utf8.RuneCountInString(b.Thinking)
			case types.ContentToolCall:
				chars += utf8.RuneCountInString(b.Name)
				chars += safeJSONLen(b.Arguments)
			}
		}
	case types.RoleToolResult:
		chars = textAndImageChars(m.Content)
	default:
		return 0
	}
	return ceilDiv(chars, charsPerToken)
}

// ContextEstimate is the result of EstimateContextTokens. Ported from pi's
// ContextUsageEstimate.
type ContextEstimate struct {
	Tokens         int // total estimated context tokens
	UsageTokens    int // tokens reported by the most recent assistant usage
	TrailingTokens int // estimated tokens after the most recent assistant usage
	LastUsageIndex int // index of the message that provided usage, or -1 when none
}

// EstimateContextTokens estimates the live context size for a message list.
//
// Mirrors pi estimateContextTokens: when an assistant message with non-zero
// provider usage exists, use its reported totalTokens as the base and add
// char/4 estimates only for the messages that arrived after it. When no usable
// usage exists, estimate every message from scratch.
func EstimateContextTokens(msgs []types.Message) ContextEstimate {
	idx := lastAssistantUsageIndex(msgs)
	if idx < 0 {
		var est int
		for _, m := range msgs {
			est += EstimateMessageTokens(m)
		}
		return ContextEstimate{
			Tokens:         est,
			UsageTokens:    0,
			TrailingTokens: est,
			LastUsageIndex: -1,
		}
	}
	usageTokens := calculateContextTokens(*msgs[idx].Usage)
	var trailing int
	for i := idx + 1; i < len(msgs); i++ {
		trailing += EstimateMessageTokens(msgs[i])
	}
	return ContextEstimate{
		Tokens:         usageTokens + trailing,
		UsageTokens:    usageTokens,
		TrailingTokens: trailing,
		LastUsageIndex: idx,
	}
}

// calculateContextTokens mirrors pi calculateContextTokens: totalTokens if
// non-zero, else the sum of the component fields.
func calculateContextTokens(u types.Usage) int {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.Input + u.Output + u.CacheRead + u.CacheWrite
}

// lastAssistantUsageIndex returns the index of the last assistant message
// whose usage is usable for context accounting, or -1 when none exists.
// Mirrors pi getLastAssistantUsageInfo: skip aborted/error responses and
// zero-usage responses.
func lastAssistantUsageIndex(msgs []types.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role != types.RoleAssistant || m.Usage == nil {
			continue
		}
		if m.StopReason == types.StopAborted || m.StopReason == types.StopError {
			continue
		}
		if calculateContextTokens(*m.Usage) <= 0 {
			continue
		}
		return i
	}
	return -1
}

func textAndImageChars(blocks []types.ContentBlock) int {
	var chars int
	for _, b := range blocks {
		switch b.Type {
		case types.ContentText:
			chars += utf8.RuneCountInString(b.Text)
		case types.ContentImage:
			chars += estimatedImageChars
		}
	}
	return chars
}

func safeJSONLen(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return len("[unserializable]")
	}
	return utf8.RuneCountInString(string(b))
}

func ceilDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	return (a + b - 1) / b
}
