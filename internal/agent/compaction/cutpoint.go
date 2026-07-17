package compaction

import "github.com/myagent/myagent/internal/types"

// FindCutPoint returns the index of the first message to KEEP after
// compaction. Ported from pi findCutPoint, simplified for v1 (no split-turn
// handling).
//
// Algorithm: walk backward from the newest message, accumulating token
// estimates until the budget (keepRecentTokens) is reached. The cut must land
// on a valid boundary — a user or assistant message — never on a toolResult,
// which must stay with its preceding assistant toolCall. If the budget lands
// on a toolResult, walk backward to the preceding assistant message and keep
// the toolResult with it (i.e. cut earlier).
//
// Returns 0 when there is nothing to summarize (the budget covers the whole
// conversation or the conversation is too small to compact), which the caller
// treats as "no compaction".
func FindCutPoint(msgs []types.Message, keepRecentTokens int) int {
	if len(msgs) == 0 {
		return 0
	}
	// Accumulate tokens from the end until we hit the budget. The index at
	// which we cross the budget (if any) is the candidate cut (first kept).
	accumulated := 0
	candidate := 0 // default: keep everything, summarize nothing
	for i := len(msgs) - 1; i >= 0; i-- {
		accumulated += EstimateMessageTokens(msgs[i])
		if accumulated >= keepRecentTokens {
			candidate = i
			break
		}
	}
	if candidate == 0 {
		return 0
	}
	// Adjust the candidate to a valid cut boundary: never keep a toolResult
	// as the first kept message without its preceding assistant message.
	// Walk backward until we land on a user or assistant message.
	for candidate > 0 && !isValidCutBoundary(msgs[candidate]) {
		candidate--
	}
	// If the walk pushed us all the way back to 0, there's nothing safe to
	// summarize — keep everything.
	if candidate == 0 {
		return 0
	}
	return candidate
}

// isValidCutBoundary reports whether a message may be the first kept message
// after compaction. toolResult messages are never valid cut boundaries because
// they must remain paired with their preceding assistant toolCall.
func isValidCutBoundary(m types.Message) bool {
	return m.Role == types.RoleUser || m.Role == types.RoleAssistant
}
