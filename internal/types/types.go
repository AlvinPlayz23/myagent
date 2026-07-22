// Package types holds the UI-agnostic core types shared across the agent,
// llm, tools, and session packages.
//
// Ported from pi: packages/ai/src/types.ts (Message/Content/Usage/StopReason,
// AssistantMessageEvent) and packages/agent/src/types.ts (AgentEvent). Field
// names are kept aligned with pi so behavior maps 1:1.
package types

// Role identifies the author of a Message.
type Role string

const (
	RoleUser       Role = "user"
	RoleAssistant  Role = "assistant"
	RoleToolResult Role = "toolResult"
)

// ContentType discriminates a ContentBlock.
type ContentType string

const (
	ContentText     ContentType = "text"
	ContentThinking ContentType = "thinking"
	ContentImage    ContentType = "image"
	ContentToolCall ContentType = "toolCall"
)

// ContentBlock is a single piece of message content. Which fields are set
// depends on Type. Mirrors pi's TextContent | ThinkingContent | ImageContent |
// ToolCall union (packages/ai/src/types.ts).
type ContentBlock struct {
	Type ContentType `json:"type"`

	// text
	Text string `json:"text,omitempty"`

	// thinking
	Thinking          string `json:"thinking,omitempty"`
	ThinkingSignature string `json:"thinkingSignature,omitempty"`
	Redacted          bool   `json:"redacted,omitempty"`

	// image
	Data     string `json:"data,omitempty"`     // base64
	MimeType string `json:"mimeType,omitempty"` // e.g. image/png

	// toolCall
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// TextBlock is a convenience constructor for a text content block.
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: ContentText, Text: text}
}

// StopReason mirrors pi's StopReason (packages/ai/src/types.ts).
type StopReason string

const (
	StopStop    StopReason = "stop"
	StopLength  StopReason = "length"
	StopToolUse StopReason = "toolUse"
	StopError   StopReason = "error"
	StopAborted StopReason = "aborted"
)

// Usage captures token accounting for a single assistant response.
// Mirrors pi's Usage (packages/ai/src/types.ts).
type Usage struct {
	Input       int  `json:"input"`
	Output      int  `json:"output"`
	CacheRead   int  `json:"cacheRead"`
	CacheWrite  int  `json:"cacheWrite"`
	Reasoning   int  `json:"reasoning,omitempty"`
	TotalTokens int  `json:"totalTokens"`
	Cost        Cost `json:"cost"`
}

// Cost holds the USD cost breakdown for a response.
type Cost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// Message is the union of user/assistant/toolResult messages.
// Mirrors pi's Message (packages/ai/src/types.ts). A single struct carries all
// role-specific fields; only those relevant to Role are populated.
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`

	// assistant-only
	API          string     `json:"api,omitempty"`
	Provider     string     `json:"provider,omitempty"`
	Model        string     `json:"model,omitempty"`
	Usage        *Usage     `json:"usage,omitempty"`
	StopReason   StopReason `json:"stopReason,omitempty"`
	ErrorMessage string     `json:"errorMessage,omitempty"`

	// toolResult-only
	ToolCallID string `json:"toolCallId,omitempty"`
	ToolName   string `json:"toolName,omitempty"`
	IsError    bool   `json:"isError,omitempty"`
	Details    any    `json:"details,omitempty"`

	Timestamp int64 `json:"timestamp"` // Unix millis
}

// ToolCalls returns the toolCall content blocks in this (assistant) message.
func (m Message) ToolCalls() []ContentBlock {
	var calls []ContentBlock
	for _, c := range m.Content {
		if c.Type == ContentToolCall {
			calls = append(calls, c)
		}
	}
	return calls
}

// AssistantMessageEvent is a low-level streaming event from a Provider.
// Mirrors pi's AssistantMessageEvent (packages/ai/src/types.ts). The Partial
// field carries the in-progress assistant Message on delta events.
type AssistantMessageEvent struct {
	Type         string   `json:"type"` // start|text_start|text_delta|...|done|error|retry
	ContentIndex int      `json:"contentIndex,omitempty"`
	Delta        string   `json:"delta,omitempty"`
	Partial      *Message `json:"partial,omitempty"`
	Message      *Message `json:"message,omitempty"` // done
	Error        *Message `json:"error,omitempty"`   // error
	// Retryable marks an "error" event whose failure is transient (network or a
	// retryable HTTP status), so a retry wrapper may re-issue the request.
	Retryable bool `json:"retryable,omitempty"`
	// Attempt / MaxAttempts describe a "retry" event: the upcoming attempt
	// number and the configured attempt ceiling.
	Attempt     int `json:"attempt,omitempty"`
	MaxAttempts int `json:"maxAttempts,omitempty"`
}

// AgentEventType enumerates the agent lifecycle events. Names match pi's
// AgentEvent 1:1 (packages/agent/src/types.ts).
type AgentEventType string

const (
	EventAgentStart          AgentEventType = "agent_start"
	EventAgentEnd            AgentEventType = "agent_end"
	EventTurnStart           AgentEventType = "turn_start"
	EventTurnEnd             AgentEventType = "turn_end"
	EventMessageStart        AgentEventType = "message_start"
	EventMessageUpdate       AgentEventType = "message_update"
	EventMessageEnd          AgentEventType = "message_end"
	EventToolExecutionStart  AgentEventType = "tool_execution_start"
	EventToolExecutionUpdate AgentEventType = "tool_execution_update"
	EventToolExecutionEnd    AgentEventType = "tool_execution_end"
	EventCompactionStart     AgentEventType = "compaction_start"
	EventCompactionEnd       AgentEventType = "compaction_end"
	EventRetry               AgentEventType = "retry"
)

// AgentEvent is a single event emitted by the agent for UIs to render.
// Mirrors pi's AgentEvent union (packages/agent/src/types.ts). Only the fields
// relevant to Type are populated.
type AgentEvent struct {
	Type AgentEventType `json:"type"`

	// message_* (Message is the message this event concerns)
	Message *Message `json:"message,omitempty"`
	// message_update carries the underlying provider stream event
	AssistantMessageEvent *AssistantMessageEvent `json:"assistantMessageEvent,omitempty"`

	// turn_end
	ToolResults []Message `json:"toolResults,omitempty"`
	// agent_end
	Messages []Message `json:"messages,omitempty"`

	// tool_execution_*
	ToolCallID    string         `json:"toolCallId,omitempty"`
	ToolName      string         `json:"toolName,omitempty"`
	Args          map[string]any `json:"args,omitempty"`
	Result        *ToolResult    `json:"result,omitempty"`
	PartialResult *ToolResult    `json:"partialResult,omitempty"`
	IsError       bool           `json:"isError,omitempty"`

	// compaction_start / compaction_end
	Compaction *CompactionInfo `json:"compaction,omitempty"`

	// retry (Attempt is the upcoming attempt; MaxAttempts is the ceiling)
	Attempt     int `json:"attempt,omitempty"`
	MaxAttempts int `json:"maxAttempts,omitempty"`
}

// CompactionInfo carries the result of an auto-compaction. On compaction_end,
// the AgentEvent's Message field carries the synthesized summary-as-user-message
// that replaces the compacted history. FirstKeptIndex is the index (in the
// pre-compaction message list) where the verbatim-kept region begins.
type CompactionInfo struct {
	Summary        string   `json:"summary"`
	FirstKeptIndex int      `json:"firstKeptIndex"`
	TokensBefore   int      `json:"tokensBefore"`
	TokensAfter    int      `json:"tokensAfter"`
	ReadFiles      []string `json:"readFiles,omitempty"`
	ModifiedFiles  []string `json:"modifiedFiles,omitempty"`
}

// ToolResult is the value produced by a tool execution.
// Mirrors pi's AgentToolResult (packages/agent/src/types.ts).
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	Details any            `json:"details,omitempty"`
	// Terminate hints the loop to stop after the current tool batch.
	Terminate bool `json:"terminate,omitempty"`
}

// TextResult builds a ToolResult with a single text content block.
func TextResult(text string, details any) *ToolResult {
	return &ToolResult{Content: []ContentBlock{TextBlock(text)}, Details: details}
}
