// Package llm defines the Provider interface and an OpenAI-compatible adapter.
//
// Ported from pi packages/ai: the streaming contract (Provider.Stream) mirrors
// pi's StreamFunction. Failures are surfaced through the event stream, never by
// returning an error from Stream itself (see AssistantMessageEvent "error").
package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

// Effort controls how much reasoning work a model should perform. The empty
// value leaves the provider's default unchanged.
type Effort string

const (
	EffortOff     Effort = "off"
	EffortMinimal Effort = "minimal"
	EffortNone    Effort = "none"
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
	EffortMax     Effort = "max"
)

// ParseEffort parses a user-supplied effort value. Empty input means the
// setting is unspecified and should be omitted from provider requests.
func ParseEffort(value string) (Effort, error) {
	effort := Effort(strings.ToLower(strings.TrimSpace(value)))
	switch effort {
	case "", EffortOff, EffortMinimal, EffortNone, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return effort, nil
	default:
		return "", fmt.Errorf("invalid effort %q: must be one of off, minimal, low, medium, high, xhigh, max", value)
	}
}

// NormalizeEffort applies known model capabilities. Unknown models remain
// permissive because custom endpoints do not have trustworthy catalog data.
func NormalizeEffort(model Model, effort Effort) (Effort, error) {
	if effort == EffortNone {
		effort = EffortOff
	}
	if effort == "" || !model.ReasoningKnown {
		return effort, nil
	}
	if !model.Reasoning {
		if effort != EffortOff {
			return "", fmt.Errorf("model %q does not support reasoning effort", model.ID)
		}
		return effort, nil
	}
	if len(model.SupportedEfforts) == 0 {
		return effort, nil
	}
	for _, supported := range model.SupportedEfforts {
		if effort == supported {
			return effort, nil
		}
	}
	return clampEffort(effort, model.SupportedEfforts), nil
}

func clampEffort(requested Effort, supported []Effort) Effort {
	order := []Effort{EffortOff, EffortMinimal, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	index := func(value Effort) int {
		for i, item := range order {
			if item == value {
				return i
			}
		}
		return 0
	}
	requestedIndex := index(requested)
	for i := requestedIndex; i < len(order); i++ {
		for _, item := range supported {
			if item == order[i] {
				return item
			}
		}
	}
	for i := requestedIndex - 1; i >= 0; i-- {
		for _, item := range supported {
			if item == order[i] {
				return item
			}
		}
	}
	return supported[0]
}

// Model describes a target model for a Provider request.
type Model struct {
	ID               string // model id sent as `model` in the request body
	Provider         string // provider label (e.g. "openai", "ollama")
	BaseURL          string // OpenAI-compatible base URL
	ReasoningDialect ReasoningDialect
	ReasoningKnown   bool
	Reasoning        bool
	SupportedEfforts []Effort
	ProviderOrigin   string
}

// ReasoningDialect selects the provider-specific request shape for effort.
// Auto detects known providers by name and endpoint, then falls back to OpenAI.
type ReasoningDialect string

const (
	ReasoningDialectAuto       ReasoningDialect = ""
	ReasoningDialectOpenAI     ReasoningDialect = "openai"
	ReasoningDialectOpenRouter ReasoningDialect = "openrouter"
	ReasoningDialectDeepSeek   ReasoningDialect = "deepseek"
)

// ParseReasoningDialect validates a configured reasoning request dialect.
func ParseReasoningDialect(value string) (ReasoningDialect, error) {
	dialect := ReasoningDialect(strings.ToLower(strings.TrimSpace(value)))
	if dialect == "auto" {
		return ReasoningDialectAuto, nil
	}
	switch dialect {
	case ReasoningDialectAuto, ReasoningDialectOpenAI, ReasoningDialectOpenRouter, ReasoningDialectDeepSeek:
		return dialect, nil
	default:
		return "", fmt.Errorf("invalid reasoning dialect %q: must be one of auto, openai, openrouter, deepseek", value)
	}
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
	Effort       Effort
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
