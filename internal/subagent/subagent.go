// Package subagent provides a tool that delegates a self-contained task to a
// fresh agent loop. It lives outside internal/tools because it imports
// internal/agent, which itself imports internal/tools.
package subagent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

const (
	ToolName       = "subagent"
	DefaultTimeout = 10 * time.Minute
	maxResultBytes = 50_000
)

// ResolveFunc maps (provider, model) names to a live provider and model.
type ResolveFunc func(provider, model string) (llm.Provider, llm.Model, error)

// Details is attached to the tool result for UIs and logs.
type Details struct {
	Model     string       `json:"model"`
	Effort    string       `json:"effort,omitempty"`
	Turns     int          `json:"turns"`
	ToolCalls int          `json:"toolCalls"`
	Usage     *types.Usage `json:"usage,omitempty"`
}

// Tool implements tools.Tool. It holds no per-call mutable state, so parallel
// calls are safe.
type Tool struct {
	cwd     string
	resolve ResolveFunc
	timeout time.Duration

	mu   sync.RWMutex
	base func() agent.Config
}

// Option configures a Tool.
type Option func(*Tool)

func WithResolve(r ResolveFunc) Option     { return func(t *Tool) { t.resolve = r } }
func WithCwd(cwd string) Option            { return func(t *Tool) { t.cwd = cwd } }
func WithTimeout(d time.Duration) Option   { return func(t *Tool) { t.timeout = d } }
func WithBase(f func() agent.Config) Option { return func(t *Tool) { t.base = f } }

// New builds the tool. base returns the parent's live agent.Config; it may be
// nil here and supplied later with SetBase (the registry is built before the
// frontend owns its config).
func New(opts ...Option) *Tool {
	t := &Tool{timeout: DefaultTimeout}
	for _, o := range opts {
		o(t)
	}
	return t
}

// SetBase installs the parent-config accessor.
func (t *Tool) SetBase(base func() agent.Config) {
	t.mu.Lock()
	t.base = base
	t.mu.Unlock()
}

func (t *Tool) Name() string { return ToolName }

func (t *Tool) Description() string {
	return "Delegate a self-contained task to a fresh agent with its own context. It has no memory of this " +
		"conversation, so the prompt must be complete. Returns only the subagent's final answer. " +
		"The subagent has the same tools as you except subagent itself."
}

func (t *Tool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{"type": "string", "description": "Complete task description for the subagent. Include all needed context; say which tools to prefer if that matters."},
			"model":  map[string]any{"type": "string", "description": "Leave unset. Only set if the user explicitly asked for a specific model (provider/model or model id)."},
			"effort": map[string]any{"type": "string", "description": "Leave unset. Only set if the user explicitly asked for a specific effort."},
			"timeout": map[string]any{"type": "number", "description": "Optional timeout in seconds (capped at 600)."},
		},
		"required": []string{"prompt"},
	}
}

func str(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return strings.TrimSpace(s)
}

func (t *Tool) Execute(ctx context.Context, _ string, args map[string]any) (res *types.ToolResult, err error) {
	prompt := str(args, "prompt")
	if prompt == "" {
		return nil, fmt.Errorf("subagent: missing required 'prompt' argument")
	}
	t.mu.RLock()
	baseFn := t.base
	t.mu.RUnlock()
	if baseFn == nil {
		return nil, errors.New("subagent: not configured")
	}
	cfg := baseFn()
	if cfg.Provider == nil {
		return nil, errors.New("subagent: no active model")
	}
	// Depth guard: the child can never see (or call) subagent.
	cfg.Registry = cfg.Registry.Without([]string{ToolName})

	if m := str(args, "model"); m != "" {
		if t.resolve == nil {
			return nil, errors.New("subagent: model override is not supported here")
		}
		p, model, rerr := t.resolveModel(m)
		if rerr != nil {
			return nil, fmt.Errorf("subagent: %w", rerr)
		}
		cfg.Provider, cfg.Model = p, model
		// A different model may not support the inherited effort.
		if eff, nerr := llm.NormalizeEffort(model, cfg.Effort); nerr == nil {
			cfg.Effort = eff
		} else {
			cfg.Effort = ""
		}
	}
	if e := str(args, "effort"); e != "" {
		eff, perr := llm.ParseEffort(e)
		if perr != nil {
			return nil, fmt.Errorf("subagent: %w", perr)
		}
		if eff, perr = llm.NormalizeEffort(cfg.Model, eff); perr != nil {
			return nil, fmt.Errorf("subagent: %w", perr)
		}
		cfg.Effort = eff
	}
	cfg.SystemPrompt = agent.BuildSystemPrompt(cfg.Registry, t.cwd)
	cfg.Queue = nil

	timeout := t.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if secs, ok := args["timeout"].(float64); ok && secs > 0 && time.Duration(secs*float64(time.Second)) < timeout {
		timeout = time.Duration(secs * float64(time.Second))
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	details := Details{Model: cfg.Model.ID, Effort: string(cfg.Effort)}
	msgs, runErr := runLoop(runCtx, cfg, prompt, &details)
	text := lastAssistantText(msgs)

	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		return nil, fmt.Errorf("subagent: timed out after %s%s", timeout, partial(text))
	case runErr != nil:
		return nil, fmt.Errorf("subagent: %w", runErr)
	}
	if last := lastAssistant(msgs); last != nil {
		switch last.StopReason {
		case types.StopError:
			return nil, fmt.Errorf("subagent: %s%s", orDefault(last.ErrorMessage, "model error"), partial(text))
		case types.StopAborted:
			return nil, fmt.Errorf("subagent: aborted%s", partial(text))
		}
	}
	if text == "" {
		text = "(subagent produced no output)"
	}
	if len(text) > maxResultBytes {
		text = text[:maxResultBytes] + "\n[output truncated]"
	}
	return types.TextResult(text, details), nil
}

// resolveModel accepts "provider/model" or a bare model id.
func (t *Tool) resolveModel(ref string) (llm.Provider, llm.Model, error) {
	if i := strings.Index(ref, "/"); i > 0 {
		if p, m, err := t.resolve(ref[:i], ref[i+1:]); err == nil {
			return p, m, nil
		}
	}
	return t.resolve("", ref)
}

func runLoop(ctx context.Context, cfg agent.Config, prompt string, d *Details) (msgs []types.Message, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	sink := func(_ context.Context, ev types.AgentEvent) error {
		switch ev.Type {
		case types.EventTurnStart:
			d.Turns++
		case types.EventToolExecutionStart:
			d.ToolCalls++
		case types.EventMessageEnd:
			if ev.Message != nil && ev.Message.Role == types.RoleAssistant && ev.Message.Usage != nil {
				if d.Usage == nil {
					d.Usage = &types.Usage{}
				}
				u := ev.Message.Usage
				d.Usage.Input += u.Input
				d.Usage.Output += u.Output
				d.Usage.CacheRead += u.CacheRead
				d.Usage.CacheWrite += u.CacheWrite
				d.Usage.TotalTokens += u.TotalTokens
				d.Usage.Cost.Total += u.Cost.Total
			}
		}
		return nil
	}
	loop := agent.New(cfg, nil, sink)
	return loop.Run(ctx, []types.Message{{
		Role:      types.RoleUser,
		Content:   []types.ContentBlock{types.TextBlock(prompt)},
		Timestamp: time.Now().UnixMilli(),
	}})
}

func lastAssistant(msgs []types.Message) *types.Message {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleAssistant {
			return &msgs[i]
		}
	}
	return nil
}

func lastAssistantText(msgs []types.Message) string {
	m := lastAssistant(msgs)
	if m == nil {
		return ""
	}
	var parts []string
	for _, c := range m.Content {
		if c.Type == types.ContentText && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func partial(text string) string {
	if text == "" {
		return ""
	}
	return "\npartial output:\n" + text
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
