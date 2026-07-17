// Package agent implements the headless agent loop: it drives a Provider,
// executes tool calls, and emits AgentEvents. Ported from pi
// packages/agent/src/agent-loop.ts, keeping event names and ordering 1:1.
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/tools"
	"github.com/myagent/myagent/internal/types"
)

// EventSink receives every AgentEvent emitted by the loop, in order.
type EventSink func(context.Context, types.AgentEvent) error

// MessageQueue supplies steering (mid-turn) and follow-up (post-stop) messages.
// Implementations must be safe for the loop to poll between turns. A nil
// MessageQueue means no queued messages.
type MessageQueue interface {
	// Steering returns messages to inject before the next assistant response.
	Steering() []types.Message
	// FollowUp returns messages to process after the agent would otherwise stop.
	FollowUp() []types.Message
}

// Config bundles the dependencies a single agent run needs.
type Config struct {
	Provider     llm.Provider
	Model        llm.Model
	Registry     *tools.Registry
	SystemPrompt string
	Temperature  *float64
	MaxTokens    *int
	Queue        MessageQueue // optional
}

// Loop is a reusable agent driver over a fixed Config and conversation.
type Loop struct {
	cfg      Config
	messages []types.Message
	emit     EventSink
}

// New creates a Loop seeded with prior conversation messages (may be nil).
func New(cfg Config, history []types.Message, emit EventSink) *Loop {
	msgs := make([]types.Message, len(history))
	copy(msgs, history)
	return &Loop{cfg: cfg, messages: msgs, emit: emit}
}

// Messages returns the full conversation accumulated so far.
func (l *Loop) Messages() []types.Message { return l.messages }

// Run drives the agent to completion for the given prompts. It mirrors pi's
// runAgentLoop: emit agent_start/turn_start, the prompt messages, then the
// tool-call loop, finishing with agent_end. Returns the messages produced
// during this run (prompts + assistant + tool results).
func (l *Loop) Run(ctx context.Context, prompts []types.Message) ([]types.Message, error) {
	var produced []types.Message

	for i := range prompts {
		l.messages = append(l.messages, prompts[i])
		produced = append(produced, prompts[i])
	}

	if err := l.emit(ctx, types.AgentEvent{Type: types.EventAgentStart}); err != nil {
		return produced, err
	}
	if err := l.emit(ctx, types.AgentEvent{Type: types.EventTurnStart}); err != nil {
		return produced, err
	}
	for i := range prompts {
		p := prompts[i]
		if err := l.emit(ctx, types.AgentEvent{Type: types.EventMessageStart, Message: &p}); err != nil {
			return produced, err
		}
		if err := l.emit(ctx, types.AgentEvent{Type: types.EventMessageEnd, Message: &p}); err != nil {
			return produced, err
		}
	}

	if err := l.runLoop(ctx, &produced, true); err != nil {
		return produced, err
	}
	return produced, nil
}

// runLoop is the shared inner driver. Ported from pi runLoop.
func (l *Loop) runLoop(ctx context.Context, produced *[]types.Message, firstTurn bool) error {
	pending := l.steering()

	for {
		hasMoreToolCalls := true

		for hasMoreToolCalls || len(pending) > 0 {
			if !firstTurn {
				if err := l.emit(ctx, types.AgentEvent{Type: types.EventTurnStart}); err != nil {
					return err
				}
			} else {
				firstTurn = false
			}

			// Inject pending (steering/follow-up) messages before responding.
			if len(pending) > 0 {
				for i := range pending {
					m := pending[i]
					if err := l.emit(ctx, types.AgentEvent{Type: types.EventMessageStart, Message: &m}); err != nil {
						return err
					}
					if err := l.emit(ctx, types.AgentEvent{Type: types.EventMessageEnd, Message: &m}); err != nil {
						return err
					}
					l.messages = append(l.messages, m)
					*produced = append(*produced, m)
				}
				pending = nil
			}

			message, err := l.streamAssistant(ctx)
			if err != nil {
				return err
			}
			l.messages = append(l.messages, message)
			*produced = append(*produced, message)

			if message.StopReason == types.StopError || message.StopReason == types.StopAborted {
				msg := message
				if err := l.emit(ctx, types.AgentEvent{Type: types.EventTurnEnd, Message: &msg}); err != nil {
					return err
				}
				return l.emit(ctx, types.AgentEvent{Type: types.EventAgentEnd, Messages: l.messages})
			}

			toolCalls := message.ToolCalls()
			var toolResults []types.Message
			hasMoreToolCalls = false
			if len(toolCalls) > 0 {
				var terminate bool
				if message.StopReason == types.StopLength {
					toolResults, terminate = l.failTruncatedToolCalls(ctx, toolCalls)
				} else {
					toolResults, terminate = l.executeToolCalls(ctx, toolCalls)
				}
				hasMoreToolCalls = !terminate
				for i := range toolResults {
					l.messages = append(l.messages, toolResults[i])
					*produced = append(*produced, toolResults[i])
				}
			}

			msg := message
			if err := l.emit(ctx, types.AgentEvent{Type: types.EventTurnEnd, Message: &msg, ToolResults: toolResults}); err != nil {
				return err
			}

			pending = l.steering()
		}

		// Agent would stop. Check for follow-up messages.
		if followUp := l.followUp(); len(followUp) > 0 {
			pending = followUp
			continue
		}
		break
	}

	return l.emit(ctx, types.AgentEvent{Type: types.EventAgentEnd, Messages: l.messages})
}

// streamAssistant runs a single provider stream, emitting message_* events and
// returning the final assistant Message. Ported from pi streamAssistantResponse.
func (l *Loop) streamAssistant(ctx context.Context) (types.Message, error) {
	req := llm.Request{
		SystemPrompt: l.cfg.SystemPrompt,
		Messages:     l.messages,
		Tools:        l.providerTools(),
		Temperature:  l.cfg.Temperature,
		MaxTokens:    l.cfg.MaxTokens,
	}

	events, err := l.cfg.Provider.Stream(ctx, l.cfg.Model, req)
	if err != nil {
		return types.Message{}, err
	}

	var final *types.Message
	started := false
	for ev := range events {
		switch ev.Type {
		case "start":
			started = true
			if ev.Partial != nil {
				if err := l.emit(ctx, types.AgentEvent{Type: types.EventMessageStart, Message: ev.Partial}); err != nil {
					return types.Message{}, err
				}
			}
		case "done":
			final = ev.Message
		case "error":
			final = ev.Error
		default:
			// Streaming deltas: forward as message_update carrying the raw event.
			evCopy := ev
			if err := l.emit(ctx, types.AgentEvent{
				Type:                  types.EventMessageUpdate,
				AssistantMessageEvent: &evCopy,
				Message:               ev.Partial,
			}); err != nil {
				return types.Message{}, err
			}
		}
	}

	if final == nil {
		// Provider closed the channel without a terminal event: synthesize.
		final = &types.Message{
			Role:         types.RoleAssistant,
			StopReason:   types.StopError,
			ErrorMessage: "stream ended without a terminal event",
			Timestamp:    time.Now().UnixMilli(),
		}
	}
	if !started {
		if err := l.emit(ctx, types.AgentEvent{Type: types.EventMessageStart, Message: final}); err != nil {
			return types.Message{}, err
		}
	}
	if err := l.emit(ctx, types.AgentEvent{Type: types.EventMessageEnd, Message: final}); err != nil {
		return types.Message{}, err
	}
	return *final, nil
}

// executeToolCalls runs each tool call sequentially in call order, emitting
// tool_execution_* events and building toolResult messages. Ported from pi
// executeToolCallsSequential.
func (l *Loop) executeToolCalls(ctx context.Context, toolCalls []types.ContentBlock) ([]types.Message, bool) {
	var results []types.Message
	allTerminate := len(toolCalls) > 0
	for _, tc := range toolCalls {
		_ = l.emit(ctx, types.AgentEvent{
			Type:       types.EventToolExecutionStart,
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Args:       tc.Arguments,
		})

		result, isError := l.runTool(ctx, tc)

		_ = l.emit(ctx, types.AgentEvent{
			Type:       types.EventToolExecutionEnd,
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Result:     result,
			IsError:    isError,
		})

		msg := l.toolResultMessage(tc, result, isError)
		_ = l.emit(ctx, types.AgentEvent{Type: types.EventMessageStart, Message: &msg})
		_ = l.emit(ctx, types.AgentEvent{Type: types.EventMessageEnd, Message: &msg})
		results = append(results, msg)

		if !result.Terminate {
			allTerminate = false
		}
		if ctx.Err() != nil {
			break
		}
	}
	return results, allTerminate
}

// runTool validates and executes a single tool call, converting a returned
// error into an error ToolResult. Ported from pi prepareToolCall +
// executePreparedToolCall.
func (l *Loop) runTool(ctx context.Context, tc types.ContentBlock) (*types.ToolResult, bool) {
	tool := l.cfg.Registry.Get(tc.Name)
	if tool == nil {
		return types.TextResult(fmt.Sprintf("Tool %s not found", tc.Name), nil), true
	}
	if ctx.Err() != nil {
		return types.TextResult("Operation aborted", nil), true
	}
	result, err := tool.Execute(ctx, tc.ID, tc.Arguments)
	if err != nil {
		return types.TextResult(err.Error(), nil), true
	}
	if result == nil {
		result = types.TextResult("", nil)
	}
	return result, false
}

// failTruncatedToolCalls rejects every tool call from a length-truncated
// message. Ported from pi failToolCallsFromTruncatedMessage.
func (l *Loop) failTruncatedToolCalls(ctx context.Context, toolCalls []types.ContentBlock) ([]types.Message, bool) {
	var results []types.Message
	for _, tc := range toolCalls {
		_ = l.emit(ctx, types.AgentEvent{
			Type:       types.EventToolExecutionStart,
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Args:       tc.Arguments,
		})
		result := types.TextResult(fmt.Sprintf(
			"Tool call %q was not executed: the response hit the output token limit, so its arguments "+
				"may be truncated. Re-issue the tool call with complete arguments.", tc.Name), nil)
		_ = l.emit(ctx, types.AgentEvent{
			Type:       types.EventToolExecutionEnd,
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
			Result:     result,
			IsError:    true,
		})
		msg := l.toolResultMessage(tc, result, true)
		_ = l.emit(ctx, types.AgentEvent{Type: types.EventMessageStart, Message: &msg})
		_ = l.emit(ctx, types.AgentEvent{Type: types.EventMessageEnd, Message: &msg})
		results = append(results, msg)
	}
	return results, false
}

// toolResultMessage builds a toolResult Message. Ported from pi
// createToolResultMessage.
func (l *Loop) toolResultMessage(tc types.ContentBlock, result *types.ToolResult, isError bool) types.Message {
	content := result.Content
	if content == nil {
		content = []types.ContentBlock{}
	}
	return types.Message{
		Role:       types.RoleToolResult,
		ToolCallID: tc.ID,
		ToolName:   tc.Name,
		Content:    content,
		Details:    result.Details,
		IsError:    isError,
		Timestamp:  time.Now().UnixMilli(),
	}
}

// providerTools converts the registry into provider-facing tool definitions.
func (l *Loop) providerTools() []llm.Tool {
	if l.cfg.Registry == nil {
		return nil
	}
	var out []llm.Tool
	for _, t := range l.cfg.Registry.All() {
		out = append(out, llm.Tool{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return out
}

func (l *Loop) steering() []types.Message {
	if l.cfg.Queue == nil {
		return nil
	}
	return l.cfg.Queue.Steering()
}

func (l *Loop) followUp() []types.Message {
	if l.cfg.Queue == nil {
		return nil
	}
	return l.cfg.Queue.FollowUp()
}
