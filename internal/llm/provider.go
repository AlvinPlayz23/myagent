// Package llm defines the Provider interface and an OpenAI-compatible adapter.
//
// Ported from pi packages/ai: the streaming contract (Provider.Stream) mirrors
// pi's StreamFunction. Failures are surfaced through the event stream, never by
// returning an error from Stream itself (see AssistantMessageEvent "error").
package llm

import (
	"context"

	"github.com/myagent/myagent/internal/types"
)

// Model describes a target model for a Provider request.
type Model struct {
	ID       string // model id sent as `model` in the request body
	Provider string // provider label (e.g. "openai", "ollama")
	BaseURL  string // OpenAI-compatible base URL
}

// Tool is the provider-facing tool definition (name/description/JSON schema).
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any // JSON Schema object
}

// Request is a single streaming request to a Provider.
type Request struct {
	SystemPrompt string
	Messages     []types.Message
	Tools        []Tool
	Temperature  *float64
	MaxTokens    *int
}

// StreamEvent is yielded on the channel returned by Provider.Stream. Exactly
// one terminal event (Type "done" or "error") is sent, after which the channel
// is closed.
type StreamEvent = types.AssistantMessageEvent

// Provider streams an assistant response for a Request.
//
// Contract (mirrors pi's StreamFunction): Stream must not return request/model
// runtime failures as a Go error return once streaming begins; instead it
// encodes them as a terminal "error" StreamEvent carrying an assistant Message
// with StopReason "error" or "aborted" and ErrorMessage set. A non-nil error
// return is reserved for programmer errors (e.g. nil model).
type Provider interface {
	Stream(ctx context.Context, model Model, req Request) (<-chan StreamEvent, error)
}
