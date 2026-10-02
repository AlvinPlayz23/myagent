// Package subagent provides a tool that delegates a self-contained task to a
// fresh agent loop. It lives outside internal/tools because it imports
// internal/agent, which itself imports internal/tools.
package subagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
		"conversation, so the prompt must be complete. The task runs in the background: this call returns " +
		"immediately with a task id, several subagents can run concurrently, and each one's full report " +
		"(prompt, trajectory, final answer) arrives automatically as a system-generated message when it " +
		"finishes. Do not poll or repeat the task. The subagent has the same tools as you except subagent itself."
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

// prepared is the validated, immutable input of one child run.
type prepared struct {
	cfg     agent.Config
	prompt  string
	timeout time.Duration
}

// prepare validates args and builds the child's config from the given parent
// config. It never starts a provider stream.
func (t *Tool) prepare(args map[string]any, cfg agent.Config) (*prepared, error) {
	prompt := str(args, "prompt")
	if prompt == "" {
		return nil, fmt.Errorf("subagent: missing required 'prompt' argument")
	}
	if cfg.Provider == nil {
		return nil, errors.New("subagent: no active model")
	}
	// Depth guard: the child can never see (or call) subagent.
	if cfg.Registry != nil {
		cfg.Registry = cfg.Registry.Without([]string{ToolName})
	}

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
	return &prepared{cfg: cfg, prompt: prompt, timeout: timeout}, nil
}

func (t *Tool) baseConfig() (agent.Config, error) {
	t.mu.RLock()
	baseFn := t.base
	t.mu.RUnlock()
	if baseFn == nil {
		return agent.Config{}, errors.New("subagent: not configured")
	}
	return baseFn(), nil
}

// Execute is the blocking, final-answer-only path. The agent loop prefers
// PrepareBackground; this remains for callers that run tools directly.
func (t *Tool) Execute(ctx context.Context, _ string, args map[string]any) (res *types.ToolResult, err error) {
	if str(args, "prompt") == "" {
		return nil, fmt.Errorf("subagent: missing required 'prompt' argument")
	}
	base, err := t.baseConfig()
	if err != nil {
		return nil, err
	}
	p, err := t.prepare(args, base)
	if err != nil {
		return nil, err
	}
	cfg, prompt, timeout := p.cfg, p.prompt, p.timeout
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	details := Details{Model: cfg.Model.ID, Effort: string(cfg.Effort)}
	msgs, runErr := runLoop(runCtx, cfg, prompt, &details, nil)
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

func newTaskID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("sub_%d", time.Now().UnixNano())
	}
	return "sub_" + hex.EncodeToString(b[:])
}

// AckDetails is attached to the immediate launch acknowledgement.
type AckDetails struct {
	TaskID string `json:"taskId"`
	Model  string `json:"model"`
	Effort string `json:"effort,omitempty"`
	Status string `json:"status"`
}

// PrepareBackground implements agent.BackgroundTool. It validates arguments
// and snapshots the parent config synchronously, starts nothing, and returns
// a task whose Run produces the system-generated completion message.
func (t *Tool) PrepareBackground(_ context.Context, callID string, args map[string]any, parent agent.Config) (*agent.BackgroundTask, error) {
	p, err := t.prepare(args, parent)
	if err != nil {
		return nil, err
	}
	id := newTaskID()
	p.cfg.Model.SessionID = id
	ack := types.TextResult(fmt.Sprintf(
		"Subagent task %s started in the background (model %s). It runs concurrently; its full "+
			"report will arrive automatically as a system-generated message when it finishes. "+
			"Continue with other work, or finish your turn if nothing else is needed.", id, p.cfg.Model.ID),
		AckDetails{TaskID: id, Model: p.cfg.Model.ID, Effort: string(p.cfg.Effort), Status: "running"})
	return &agent.BackgroundTask{
		ID:  id,
		Ack: ack,
		Run: func(ctx context.Context) (types.Message, error) {
			return completionMessage(t.runPrepared(ctx, p, id, callID)), nil
		},
		OnError: func(err error) types.Message {
			rep := t.newReport(p, id, callID)
			rep.Status, rep.Error = StatusFailed, err.Error()
			if errors.Is(err, context.Canceled) {
				rep.Status = StatusCancelled
			}
			rep.EndedAt = time.Now().UnixMilli()
			return completionMessage(rep)
		},
	}, nil
}

func (t *Tool) newReport(p *prepared, id, callID string) TaskReport {
	rep := TaskReport{
		Version:      ReportVersion,
		TaskID:       id,
		ToolCallID:   callID,
		Prompt:       p.prompt,
		SystemPrompt: p.cfg.SystemPrompt,
		Cwd:          t.cwd,
		Provider:     p.cfg.Model.Provider,
		Model:        p.cfg.Model.ID,
		Effort:       string(p.cfg.Effort),
		TimeoutMs:    p.timeout.Milliseconds(),
		StartedAt:    time.Now().UnixMilli(),
		Tools:        []ToolContext{},
		Trajectory:   []PublicMessage{},
	}
	if p.cfg.Registry != nil {
		for _, tl := range p.cfg.Registry.All() {
			rep.Tools = append(rep.Tools, ToolContext{Name: tl.Name(), Description: tl.Description(), Parameters: tl.Parameters()})
		}
	}
	return rep
}

// runPrepared executes the child and returns its full public report. Failures
// are reported (with any partial trajectory), not returned as errors.
func (t *Tool) runPrepared(ctx context.Context, p *prepared, id, callID string) TaskReport {
	rep := t.newReport(p, id, callID)
	runCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	details := Details{Model: p.cfg.Model.ID, Effort: string(p.cfg.Effort)}
	msgs, runErr := runLoop(runCtx, p.cfg, p.prompt, &details, func(ev types.AgentEvent) {
		switch ev.Type {
		case types.EventMessageEnd:
			if ev.Message != nil {
				rep.Trajectory = append(rep.Trajectory, publicMessage(*ev.Message))
			}
		case types.EventCompactionEnd:
			if ev.Compaction != nil {
				rep.Trajectory = append(rep.Trajectory, PublicMessage{
					Role: "compaction",
					Note: fmt.Sprintf("context compacted (%d -> %d tokens): %s", ev.Compaction.TokensBefore, ev.Compaction.TokensAfter, ev.Compaction.Summary),
				})
			}
		}
	})
	text := lastAssistantText(msgs)
	rep.Status = StatusCompleted
	switch {
	case ctx.Err() != nil:
		rep.Status, rep.Error = StatusCancelled, "cancelled"
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		rep.Status, rep.Error = StatusTimedOut, fmt.Sprintf("timed out after %s", p.timeout)
	case runErr != nil:
		rep.Status, rep.Error = StatusFailed, runErr.Error()
	default:
		if last := lastAssistant(msgs); last != nil {
			switch last.StopReason {
			case types.StopError:
				rep.Status, rep.Error = StatusFailed, orDefault(last.ErrorMessage, "model error")
			case types.StopAborted:
				rep.Status, rep.Error = StatusCancelled, "aborted"
			}
		}
	}
	if text == "" && rep.Status == StatusCompleted {
		text = "(subagent produced no output)"
	}
	rep.FinalResult = text
	rep.Turns, rep.ToolCallsN, rep.Usage = details.Turns, details.ToolCalls, details.Usage
	rep.EndedAt = time.Now().UnixMilli()
	return rep
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

// runLoop runs the child. onEvent, if non-nil, observes every event after the
// built-in accounting (used to record the public trajectory).
func runLoop(ctx context.Context, cfg agent.Config, prompt string, d *Details, onEvent func(types.AgentEvent)) (msgs []types.Message, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	sink := func(_ context.Context, ev types.AgentEvent) error {
		if onEvent != nil {
			defer onEvent(ev)
		}
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
