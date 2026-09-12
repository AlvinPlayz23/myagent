package agent

import (
	"context"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

// InterruptedText is the model-facing tool output used whenever the user
// aborts a tool call. It replaces raw Go errors ("context canceled",
// "Operation aborted", ...) so the next provider request sees a coherent
// explanation instead of junk, and strict providers don't reject the turn
// for incomplete tool output.
const InterruptedText = "User interrupted the tool execution before it completed."

// runWasAborted centralizes cancellation checks at agent lifecycle boundaries.
// The context remains the sole source of truth, so this adds no conversation
// state and cannot affect normal history or compaction behavior.
func runWasAborted(ctx context.Context) bool { return ctx.Err() != nil }

// isAbortText reports whether a persisted tool-result text looks like a
// cancellation artifact from the old code (raw ctx errors, "Operation
// aborted", bash's "Command aborted", stream "Request was aborted").
// Used to normalize legacy sessions in-memory on the next Run so the model
// sees InterruptedText instead.
func isAbortText(s string) bool {
	return strings.Contains(s, "context canceled") ||
		strings.Contains(s, "context cancelled") ||
		strings.Contains(s, "Operation aborted") ||
		strings.Contains(s, "Command aborted") ||
		strings.Contains(s, "Request was aborted")
}

// interruptedTextForError maps a tool error string observed under
// cancellation to the model-facing text. Bash (and plugin shell tools that
// delegate to it) embed partial output plus a "Command aborted" status —
// that partial output is preserved and only the status suffix is swapped for
// InterruptedText. Any other abort-like error collapses to InterruptedText.
func interruptedTextForError(errText string) string {
	if strings.Contains(errText, InterruptedText) {
		return errText
	}
	if strings.Contains(errText, "Command aborted") {
		return strings.ReplaceAll(errText, "Command aborted", InterruptedText)
	}
	return InterruptedText
}

func interruptedToolResult(call types.ContentBlock) types.Message {
	return types.Message{
		Role:       types.RoleToolResult,
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Content:    []types.ContentBlock{types.TextBlock(InterruptedText)},
		IsError:    true,
	}
}

func (l *Loop) normalizeAbortHistory() {
	for i := range l.messages {
		if l.messages[i].Role != types.RoleToolResult {
			continue
		}
		for j := range l.messages[i].Content {
			if l.messages[i].Content[j].Type == types.ContentText && isAbortText(l.messages[i].Content[j].Text) {
				l.messages[i].Content[j].Text = interruptedTextForError(l.messages[i].Content[j].Text)
			}
		}
	}
}

// repairDanglingMiddle repairs pairs that are followed by another message.
// Tail repairs are emitted separately because session persistence is append-only.
func (l *Loop) repairDanglingMiddle() {
	for i := 0; i < len(l.messages); i++ {
		calls := l.messages[i].ToolCalls()
		if l.messages[i].Role != types.RoleAssistant || len(calls) == 0 {
			continue
		}
		if i+1 >= len(l.messages) {
			continue
		}
		insert := l.missingResults(i, calls)
		if len(insert) > 0 {
			l.messages = append(l.messages[:i+1], append(insert, l.messages[i+1:]...)...)
			i += len(insert)
		}
	}
}

func (l *Loop) missingResults(index int, calls []types.ContentBlock) []types.Message {
	seen := map[string]bool{}
	for j := index + 1; j < len(l.messages) && l.messages[j].Role == types.RoleToolResult; j++ {
		seen[l.messages[j].ToolCallID] = true
	}
	var missing []types.Message
	for _, call := range calls {
		if !seen[call.ID] {
			missing = append(missing, interruptedToolResult(call))
		}
	}
	return missing
}

func (l *Loop) repairDanglingTail(ctx context.Context, produced *[]types.Message) error {
	if len(l.messages) == 0 {
		return nil
	}
	i := len(l.messages) - 1
	if l.messages[i].Role != types.RoleAssistant {
		return nil
	}
	insert := l.missingResults(i, l.messages[i].ToolCalls())
	for _, msg := range insert {
		m := msg
		if err := l.emit(ctx, types.AgentEvent{Type: types.EventMessageStart, Message: &m}); err != nil {
			return err
		}
		if err := l.emit(ctx, types.AgentEvent{Type: types.EventMessageEnd, Message: &m}); err != nil {
			return err
		}
		l.messages = append(l.messages, msg)
		*produced = append(*produced, msg)
	}
	return nil
}
