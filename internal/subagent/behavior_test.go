package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

type funcProvider func(ctx context.Context, req llm.Request) *types.Message

func (f funcProvider) Stream(ctx context.Context, _ llm.Model, req llm.Request) (<-chan llm.StreamEvent, error) {
	out := make(chan llm.StreamEvent, 1)
	go func() {
		defer close(out)
		if msg := f(ctx, req); msg != nil {
			out <- llm.StreamEvent{Type: "done", Message: msg}
		}
	}()
	return out, nil
}

// blockProvider emits nothing until ctx is done.
var blockProvider = funcProvider(func(ctx context.Context, _ llm.Request) *types.Message {
	<-ctx.Done()
	return nil
})

var errProvider = funcProvider(func(context.Context, llm.Request) *types.Message {
	return &types.Message{Role: types.RoleAssistant, StopReason: types.StopError, ErrorMessage: "boom",
		Content: []types.ContentBlock{types.TextBlock("half done")}}
})

type stubTool struct{ name string }

func (s stubTool) Name() string               { return s.name }
func (s stubTool) Description() string        { return "stub " + s.name }
func (s stubTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (s stubTool) Execute(context.Context, string, map[string]any) (*types.ToolResult, error) {
	return types.TextResult("tool output", nil), nil
}

func cfgWith(p llm.Provider, reg *tools.Registry) agent.Config {
	return agent.Config{Provider: p, Model: llm.Model{ID: "m", Provider: "p"}, Registry: reg, SystemPrompt: "parent"}
}

func runBG(t *testing.T, tool *Tool, args map[string]any, cfg agent.Config) TaskReport {
	t.Helper()
	task, err := tool.PrepareBackground(context.Background(), "c", args, cfg)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := task.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return msg.Details.(TaskReport)
}

func TestTruncateUTF8(t *testing.T) {
	if got := truncateUTF8("héllo", 100); got != "héllo" {
		t.Fatalf("short string changed: %q", got)
	}
	for max := 0; max < 8; max++ {
		got := truncateUTF8("日本語日本語", max)
		if !utf8.ValidString(got) || !strings.Contains(got, "[truncated:") {
			t.Fatalf("max=%d: %q", max, got)
		}
	}
	if got := truncateUTF8("日本語", 4); !strings.HasPrefix(got, "日\n[truncated") {
		t.Fatalf("got %q", got)
	}
}

func TestPrepareTimeout(t *testing.T) {
	tool := New(WithTimeout(time.Minute))
	cases := []struct {
		name string
		arg  any
		want time.Duration
	}{
		{"unset", nil, time.Minute},
		{"smaller", 5.0, 5 * time.Second},
		{"int", 7, 7 * time.Second},
		{"above cap", 1e6, time.Minute},
		{"huge", 1e30, time.Minute},
		{"inf", math.Inf(1), time.Minute},
		{"nan", math.NaN(), time.Minute},
		{"negative", -3.0, time.Minute},
		{"zero", 0.0, time.Minute},
		{"tiny", 1e-12, time.Millisecond},
	}
	for _, c := range cases {
		args := map[string]any{"prompt": "x"}
		if c.arg != nil {
			args["timeout"] = c.arg
		}
		p, err := tool.prepare(args, cfgWith(replyProvider{"x"}, nil))
		if err != nil {
			t.Fatal(err)
		}
		if p.timeout != c.want {
			t.Errorf("%s: timeout = %s, want %s", c.name, p.timeout, c.want)
		}
	}
}

func TestHugeTimeoutDoesNotInstantlyTimeOut(t *testing.T) {
	rep := runBG(t, New(), map[string]any{"prompt": "x", "timeout": 1e30}, parentConfig("fine"))
	if rep.Status != StatusCompleted {
		t.Fatalf("status = %s (%s)", rep.Status, rep.Error)
	}
}

func TestStatuses(t *testing.T) {
	t.Run("timed out", func(t *testing.T) {
		rep := runBG(t, New(WithTimeout(50*time.Millisecond)), map[string]any{"prompt": "x"}, cfgWith(blockProvider, nil))
		if rep.Status != StatusTimedOut || !strings.Contains(rep.Error, "timed out") {
			t.Fatalf("report = %s/%s", rep.Status, rep.Error)
		}
	})
	t.Run("failed with partial", func(t *testing.T) {
		rep := runBG(t, New(), map[string]any{"prompt": "x"}, cfgWith(errProvider, nil))
		if rep.Status != StatusFailed || rep.Error != "boom" || rep.FinalResult != "half done" {
			t.Fatalf("report = %+v", rep)
		}
		text := completionText(rep)
		if !strings.Contains(text, "Error: boom") || !strings.Contains(text, "Partial output:\nhalf done") {
			t.Fatalf("text = %s", text)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		p, err := New().prepare(map[string]any{"prompt": "x"}, cfgWith(blockProvider, nil))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(30*time.Millisecond, cancel)
		rep := New().runPrepared(ctx, p, "id", "c")
		if rep.Status != StatusCancelled {
			t.Fatalf("status = %s", rep.Status)
		}
	})
}

func TestOnErrorMapsStatus(t *testing.T) {
	task, err := New().PrepareBackground(context.Background(), "c", map[string]any{"prompt": "x"}, parentConfig("x"))
	if err != nil {
		t.Fatal(err)
	}
	for err, want := range map[error]string{
		context.DeadlineExceeded: StatusTimedOut,
		context.Canceled:         StatusCancelled,
		errors.New("boom"):       StatusFailed,
		errors.Join(errors.New("x"), context.DeadlineExceeded): StatusTimedOut,
	} {
		if got := task.OnError(err).Details.(TaskReport).Status; got != want {
			t.Errorf("%v -> %s, want %s", err, got, want)
		}
	}
}

func TestModelOverride(t *testing.T) {
	failing := New(WithResolve(func(provider, model string) (llm.Provider, llm.Model, error) {
		return nil, llm.Model{}, errors.New("nope:" + provider + "|" + model)
	}))
	_, err := failing.prepare(map[string]any{"prompt": "x", "model": "acme/big"}, parentConfig("x"))
	if err == nil || !strings.Contains(err.Error(), "nope:acme|big") || !strings.Contains(err.Error(), "nope:|acme/big") {
		t.Fatalf("both failures should surface, got %v", err)
	}

	fallback := New(WithResolve(func(provider, model string) (llm.Provider, llm.Model, error) {
		if provider != "" {
			return nil, llm.Model{}, errors.New("no provider")
		}
		return replyProvider{"x"}, llm.Model{ID: model, ReasoningKnown: true, Reasoning: true, SupportedEfforts: []llm.Effort{llm.Effort("low")}}, nil
	}))
	cfg := parentConfig("x")
	cfg.Effort = llm.Effort("high")
	p, err := fallback.prepare(map[string]any{"prompt": "x", "model": "acme/big"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.cfg.Model.ID != "acme/big" || p.cfg.Effort != llm.Effort("low") {
		t.Fatalf("model=%s effort=%q", p.cfg.Model.ID, p.cfg.Effort)
	}

	noReason := New(WithResolve(func(_, model string) (llm.Provider, llm.Model, error) {
		return replyProvider{"x"}, llm.Model{ID: model, ReasoningKnown: true}, nil
	}))
	p, err = noReason.prepare(map[string]any{"prompt": "x", "model": "plain"}, cfg)
	if err != nil || p.cfg.Effort != "" {
		t.Fatalf("effort should be cleared: %v %q", err, p.cfg.Effort)
	}

	if _, err := New().prepare(map[string]any{"prompt": "x", "model": "m2"}, cfg); err == nil {
		t.Fatal("override without resolver must fail")
	}
}

func TestExecute(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tool := New(WithBase(func() agent.Config { return parentConfig("answer") }))
		res, err := tool.Execute(context.Background(), "c", map[string]any{"prompt": "x"})
		if err != nil {
			t.Fatal(err)
		}
		d, ok := res.Details.(Details)
		if !ok || d.Model != "m" || d.Turns != 1 {
			t.Fatalf("details = %#v", res.Details)
		}
		if got := res.Content[0].Text; got != "answer" {
			t.Fatalf("text = %q", got)
		}
	})
	t.Run("failure keeps partial output", func(t *testing.T) {
		tool := New(WithBase(func() agent.Config { return cfgWith(errProvider, nil) }))
		_, err := tool.Execute(context.Background(), "c", map[string]any{"prompt": "x"})
		if err == nil || !strings.Contains(err.Error(), "boom") || !strings.Contains(err.Error(), "half done") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		tool := New(WithTimeout(40*time.Millisecond), WithBase(func() agent.Config { return cfgWith(blockProvider, nil) }))
		_, err := tool.Execute(context.Background(), "c", map[string]any{"prompt": "x"})
		if err == nil || !strings.Contains(err.Error(), "timed out") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("caller cancellation", func(t *testing.T) {
		tool := New(WithBase(func() agent.Config { return cfgWith(blockProvider, nil) }))
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(30*time.Millisecond, cancel)
		if _, err := tool.Execute(ctx, "c", map[string]any{"prompt": "x"}); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("validation and truncation", func(t *testing.T) {
		tool := New(WithBase(func() agent.Config { return parentConfig(strings.Repeat("é", maxResultBytes)) }))
		if _, err := tool.Execute(context.Background(), "c", map[string]any{"prompt": " "}); err == nil {
			t.Fatal("blank prompt must fail")
		}
		res, err := tool.Execute(context.Background(), "c", map[string]any{"prompt": "x"})
		if err != nil || !utf8.ValidString(res.Content[0].Text) || !strings.Contains(res.Content[0].Text, "[truncated:") {
			t.Fatalf("err=%v", err)
		}
		if _, err := New().Execute(context.Background(), "c", map[string]any{"prompt": "x"}); err == nil {
			t.Fatal("unconfigured tool must fail")
		}
	})
}

func TestDepthGuard(t *testing.T) {
	seen := make(chan llm.Request, 2)
	p := funcProvider(func(_ context.Context, req llm.Request) *types.Message {
		seen <- req
		return &types.Message{Role: types.RoleAssistant, StopReason: types.StopStop, Content: []types.ContentBlock{types.TextBlock("ok")}}
	})
	reg := tools.NewRegistry(New(), stubTool{"bash"})
	tool := New(WithBase(func() agent.Config { return cfgWith(p, reg) }))
	rep := runBG(t, tool, map[string]any{"prompt": "x"}, cfgWith(p, reg))
	for _, tl := range rep.Tools {
		if tl.Name == ToolName {
			t.Fatal("background child sees subagent")
		}
	}
	if _, err := tool.Execute(context.Background(), "c", map[string]any{"prompt": "x"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		req := <-seen
		for _, tl := range req.Tools {
			if tl.Name == ToolName {
				t.Fatalf("request %d exposes subagent", i)
			}
		}
	}
	if reg.Get(ToolName) == nil {
		t.Fatal("parent registry must be untouched")
	}
}

func TestChildSystemPromptKeepsModeInstructions(t *testing.T) {
	var got string
	p := funcProvider(func(_ context.Context, req llm.Request) *types.Message {
		got = req.SystemPrompt
		return &types.Message{Role: types.RoleAssistant, StopReason: types.StopStop, Content: []types.ContentBlock{types.TextBlock("ok")}}
	})
	cfg := cfgWith(p, tools.NewRegistry(New(), stubTool{"bash"}))
	cfg.SystemPrompt = agent.BuildSystemPrompt(cfg.Registry, "") + "\n\nMode instructions:\nfoo"
	runBG(t, New(), map[string]any{"prompt": "x"}, cfg)
	if !strings.Contains(got, "Mode instructions:\nfoo") {
		t.Fatalf("child prompt lost mode instructions:\n%s", got)
	}
}

func TestToolDescriptionWarnsAboutConcurrentEdits(t *testing.T) {
	d := New().Description()
	if !strings.Contains(d, "same files") || !strings.Contains(d, "disjoint") {
		t.Fatalf("description = %s", d)
	}
}

func scripted(turns ...*types.Message) llm.Provider {
	i := 0
	return funcProvider(func(context.Context, llm.Request) *types.Message {
		m := turns[min(i, len(turns)-1)]
		i++
		return m
	})
}

func TestUsageToolSummaryAndCompactText(t *testing.T) {
	call := func(id string) types.ContentBlock {
		return types.ContentBlock{Type: types.ContentToolCall, ID: id, Name: "echo", Arguments: map[string]any{"q": "v"}}
	}
	huge := strings.Repeat("a", maxToolArgsBytes*2)
	u := func(n int) *types.Usage {
		return &types.Usage{Input: n, Output: n, CacheRead: n, CacheWrite: n, Reasoning: n, TotalTokens: 5 * n,
			Cost: types.Cost{Input: float64(n), Output: float64(n), CacheRead: float64(n), CacheWrite: float64(n), Total: float64(4 * n)}}
	}
	p := scripted(
		&types.Message{Role: types.RoleAssistant, StopReason: types.StopToolUse, Usage: u(1), Content: []types.ContentBlock{
			call("1"), call("2"),
			{Type: types.ContentToolCall, ID: "3", Name: "echo", Arguments: map[string]any{"big": huge}},
		}},
		&types.Message{Role: types.RoleAssistant, StopReason: types.StopStop, Usage: u(2), Content: []types.ContentBlock{types.TextBlock("the answer")}},
	)
	cfg := cfgWith(p, tools.NewRegistry(New(), stubTool{"echo"}))
	cfg.SystemPrompt = "SECRET-PARENT-PROMPT"
	task, err := New().PrepareBackground(context.Background(), "c", map[string]any{"prompt": "go"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := task.Run(context.Background())
	rep := msg.Details.(TaskReport)

	if rep.Status != StatusCompleted || rep.Version != ReportVersion {
		t.Fatalf("report = %+v", rep)
	}
	want := types.Usage{Input: 3, Output: 3, CacheRead: 3, CacheWrite: 3, Reasoning: 3, TotalTokens: 15,
		Cost: types.Cost{Input: 3, Output: 3, CacheRead: 3, CacheWrite: 3, Total: 12}}
	if rep.Usage == nil || *rep.Usage != want {
		t.Fatalf("usage = %+v", rep.Usage)
	}
	if rep.ToolUsage["echo"] != 3 {
		t.Fatalf("tool usage = %v", rep.ToolUsage)
	}

	text := msg.Content[0].Text
	lines := strings.Split(text, "\n")
	if lines[0] != completionPreamble || lines[1] != "" || lines[2] != `Subagent task `+rep.TaskID+` finished with status "completed".` {
		t.Fatalf("header lines wrong:\n%s", text)
	}
	for _, sub := range []string{"Model: p/m", "Turns: 2, tool calls: 3", "Tools used: echo x3", "Final result:\nthe answer"} {
		if !strings.Contains(text, sub) {
			t.Errorf("missing %q in:\n%s", sub, text)
		}
	}
	for _, bad := range []string{"{", "SECRET-PARENT-PROMPT", "systemPrompt", "trajectory"} {
		if strings.Contains(text, bad) {
			t.Errorf("model text contains %q:\n%s", bad, text)
		}
	}

	raw, _ := json.Marshal(rep)
	if strings.Contains(string(raw), "SECRET-PARENT-PROMPT") || strings.Contains(string(raw), `"systemPrompt"`) {
		t.Fatal("system prompt persisted in report")
	}
	var found bool
	for _, m := range rep.Trajectory {
		for _, c := range m.ToolCalls {
			if !json.Valid(c.Arguments) || len(c.Arguments) > maxToolArgsBytes+100 {
				t.Fatalf("bad args: %d bytes valid=%v", len(c.Arguments), json.Valid(c.Arguments))
			}
			if strings.Contains(string(c.Arguments), "truncated") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("oversized args were not bounded")
	}
}

func TestTrajectoryBounds(t *testing.T) {
	tr := newTrajectory()
	for i := 0; i < maxTrajectoryEntries+5; i++ {
		tr.add(PublicMessage{Role: "user", Text: "x"})
	}
	got := tr.finish()
	if len(got) != maxTrajectoryEntries+1 || got[len(got)-1].Role != "note" {
		t.Fatalf("len=%d", len(got))
	}

	pm := publicMessage(types.Message{Role: types.RoleToolResult, Content: []types.ContentBlock{types.TextBlock(strings.Repeat("é", maxTrajectoryText))}})
	if !utf8.ValidString(pm.Text) || len(pm.Text) > maxTrajectoryText+100 || !strings.Contains(pm.Text, "[truncated") {
		t.Fatalf("message text not bounded: %d", len(pm.Text))
	}
}
