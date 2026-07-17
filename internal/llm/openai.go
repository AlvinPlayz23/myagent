package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/myagent/myagent/internal/types"
)

// OpenAIProvider is an OpenAI-compatible chat-completions streaming provider.
// Ported from pi packages/ai/src/api/openai-completions.ts. Works against
// OpenAI, Ollama, LM Studio, vLLM, etc. via the BaseURL override.
type OpenAIProvider struct {
	APIKey string
	Client *http.Client
}

// NewOpenAIProvider constructs a provider with a sane default HTTP client.
func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		APIKey: apiKey,
		Client: &http.Client{},
	}
}

// --- request body shapes ---

type chatRequest struct {
	Model         string          `json:"model"`
	Messages      []chatMessage   `json:"messages"`
	Stream        bool            `json:"stream"`
	StreamOptions *streamOptions  `json:"stream_options,omitempty"`
	Tools         []chatTool      `json:"tools,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	MaxTokens     *int            `json:"max_completion_tokens,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatToolFunc `json:"function"`
}

type chatToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatToolCallFunc `json:"function"`
}

type chatToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// --- streaming chunk shapes ---

type chatChunk struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chunkUsage  `json:"usage"`
}

type chatChoice struct {
	Delta        chatDelta   `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
	Usage        *chunkUsage `json:"usage"` // Moonshot fallback
}

type chatDelta struct {
	Content          string          `json:"content"`
	ReasoningContent string          `json:"reasoning_content"`
	Reasoning        string          `json:"reasoning"`
	ToolCalls        []deltaToolCall `json:"tool_calls"`
}

type deltaToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chunkUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
	PromptTokensDetails struct {
		CachedTokens     int `json:"cached_tokens"`
		CacheWriteTokens int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// Stream implements Provider. See the interface docs for the error contract.
func (p *OpenAIProvider) Stream(ctx context.Context, model Model, req Request) (<-chan StreamEvent, error) {
	if model.ID == "" {
		return nil, fmt.Errorf("llm: model id is empty")
	}
	out := make(chan StreamEvent, 64)

	go func() {
		defer close(out)
		p.run(ctx, model, req, out)
	}()

	return out, nil
}

func (p *OpenAIProvider) run(ctx context.Context, model Model, req Request, out chan<- StreamEvent) {
	// output is the accumulator that IS the final assistant message. Mirrors
	// pi's `output` object; Partial points at it on every event.
	output := &types.Message{
		Role:       types.RoleAssistant,
		Content:    []types.ContentBlock{},
		API:        "openai-completions",
		Provider:   model.Provider,
		Model:      model.ID,
		Usage:      &types.Usage{},
		StopReason: types.StopStop,
		Timestamp:  time.Now().UnixMilli(),
	}

	emitError := func(err error) {
		if ctx.Err() != nil {
			output.StopReason = types.StopAborted
		} else {
			output.StopReason = types.StopError
		}
		output.ErrorMessage = err.Error()
		out <- StreamEvent{Type: "error", Error: output}
	}

	body, err := buildRequestBody(model, req)
	if err != nil {
		emitError(err)
		return
	}

	url := strings.TrimRight(model.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		emitError(err)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := p.Client.Do(httpReq)
	if err != nil {
		emitError(err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(io.LimitReader(resp.Body, 4000))
		emitError(fmt.Errorf("%d: %s", resp.StatusCode, strings.TrimSpace(buf.String())))
		return
	}

	// start event
	out <- StreamEvent{Type: "start", Partial: cloneMessage(output)}

	acc := newAccumulator(output, out)
	hasFinishReason := false

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // skip malformed chunk, mirrors pi tolerance
		}

		if chunk.Usage != nil {
			*output.Usage = parseUsage(chunk.Usage)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if chunk.Usage == nil && choice.Usage != nil {
			*output.Usage = parseUsage(choice.Usage)
		}
		if choice.FinishReason != nil {
			output.StopReason, output.ErrorMessage = mapStopReason(*choice.FinishReason)
			hasFinishReason = true
		}

		d := choice.Delta
		if d.Content != "" {
			acc.appendText(d.Content)
		}
		if r := firstNonEmpty(d.ReasoningContent, d.Reasoning); r != "" {
			acc.appendThinking(r)
		}
		for _, tc := range d.ToolCalls {
			acc.applyToolCall(tc)
		}
	}
	if err := scanner.Err(); err != nil {
		emitError(err)
		return
	}

	acc.finish()

	// Guard checks in pi order.
	if ctx.Err() != nil {
		emitError(fmt.Errorf("Request was aborted"))
		return
	}
	if output.StopReason == types.StopAborted {
		emitError(fmt.Errorf("Request was aborted"))
		return
	}
	if output.StopReason == types.StopError {
		msg := output.ErrorMessage
		if msg == "" {
			msg = "Provider returned an error stop reason"
		}
		emitError(fmt.Errorf("%s", msg))
		return
	}
	if !hasFinishReason {
		emitError(fmt.Errorf("Stream ended without finish_reason"))
		return
	}

	out <- StreamEvent{Type: "done", Message: output}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
