package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

const testWait = 5 * time.Second

// bgTool is a BackgroundTool whose tasks block until released.
type bgTool struct {
	mu      sync.Mutex
	started chan string
	release map[string]chan struct{}
}

func newBGTool() *bgTool {
	return &bgTool{started: make(chan string, 8), release: map[string]chan struct{}{}}
}

func (b *bgTool) Name() string               { return "bg" }
func (b *bgTool) Description() string        { return "background tool" }
func (b *bgTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (b *bgTool) Execute(context.Context, string, map[string]any) (*types.ToolResult, error) {
	return nil, context.Canceled // must not be used when scheduler is active
}

func (b *bgTool) gate(id string) chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.release[id] == nil {
		b.release[id] = make(chan struct{})
	}
	return b.release[id]
}

func (b *bgTool) PrepareBackground(_ context.Context, callID string, _ map[string]any, _ Config) (*BackgroundTask, error) {
	id := "task-" + callID
	gate := b.gate(id)
	return &BackgroundTask{
		ID:  id,
		Ack: types.TextResult("started "+id, nil),
		Run: func(ctx context.Context) (types.Message, error) {
			b.started <- id
			select {
			case <-gate:
			case <-ctx.Done():
				return types.Message{}, ctx.Err()
			}
			return types.Message{
				Role:    types.RoleUser,
				Source:  types.SourceSubagentCompletion,
				Content: []types.ContentBlock{types.TextBlock("report " + id)},
			}, nil
		},
		OnError: func(err error) types.Message {
			return types.Message{Role: types.RoleUser, Source: types.SourceSubagentCompletion,
				Content: []types.ContentBlock{types.TextBlock("failed " + id + ": " + err.Error())}}
		},
	}, nil
}

// scriptProvider replies by request index: reply(i, req) -> assistant message.
type scriptProvider struct {
	mu    sync.Mutex
	reqs  []llm.Request
	reply func(i int, req llm.Request) types.Message
	seen  chan int
}

func (p *scriptProvider) Stream(_ context.Context, _ llm.Model, req llm.Request) (<-chan llm.StreamEvent, error) {
	p.mu.Lock()
	i := len(p.reqs)
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	msg := p.reply(i, req)
	msg.Role = types.RoleAssistant
	out := make(chan llm.StreamEvent, 1)
	out <- llm.StreamEvent{Type: "done", Message: &msg}
	close(out)
	if p.seen != nil {
		p.seen <- i
	}
	return out, nil
}

func (p *scriptProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.reqs)
}

func toolCallMsg(ids ...string) types.Message {
	m := types.Message{StopReason: types.StopToolUse}
	for _, id := range ids {
		m.Content = append(m.Content, types.ContentBlock{Type: types.ContentToolCall, ID: id, Name: "bg", Arguments: map[string]any{}})
	}
	return m
}

func textMsg(s string) types.Message {
	return types.Message{StopReason: types.StopStop, Content: []types.ContentBlock{types.TextBlock(s)}}
}

func waitFor[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(testWait):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}

func TestSchedulerLimitAndCloseJoins(t *testing.T) {
	s := newBackgroundScheduler(context.Background(), 1)
	b := newBGTool()
	task, _ := b.PrepareBackground(context.Background(), "a", nil, Config{})
	if _, err := s.start(task); err != nil {
		t.Fatal(err)
	}
	waitFor(t, b.started, "task start")
	other, _ := b.PrepareBackground(context.Background(), "b", nil, Config{})
	if _, err := s.start(other); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("expected capacity rejection, got %v", err)
	}
	done := make(chan struct{})
	go func() { s.close(); close(done) }()
	waitFor(t, done, "close to join the blocked worker")
	if _, err := s.start(other); err == nil {
		t.Fatal("start after close must fail")
	}
}

func TestLoopBackgroundTasksRunConcurrentlyAndResumeParent(t *testing.T) {
	b := newBGTool()
	seen := make(chan int, 8)
	prov := &scriptProvider{seen: seen, reply: func(i int, req llm.Request) types.Message {
		switch i {
		case 0:
			return toolCallMsg("1", "2")
		case 1:
			return textMsg("launched")
		default:
			return textMsg("all done")
		}
	}}
	var mu sync.Mutex
	var events []types.AgentEvent
	sink := func(_ context.Context, ev types.AgentEvent) error {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
		return nil
	}
	l := New(Config{Provider: prov, Registry: tools.NewRegistry(b), Model: llm.Model{ID: "m"}}, nil, sink)

	runDone := make(chan error, 1)
	go func() {
		_, err := l.Run(context.Background(), []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("go")}}})
		runDone <- err
	}()

	// Both siblings start before either is released, and the parent proceeds
	// to a second provider request without waiting for them.
	got := map[string]bool{waitFor(t, b.started, "first task"): true, waitFor(t, b.started, "second task"): true}
	if !got["task-1"] || !got["task-2"] {
		t.Fatalf("started = %v", got)
	}
	for i := 0; i < 2; i++ {
		waitFor(t, seen, "parent provider request")
	}
	select {
	case err := <-runDone:
		t.Fatalf("run ended while children were running: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if n := prov.count(); n != 2 {
		t.Fatalf("idle parent made extra provider requests: %d", n)
	}

	close(b.gate("task-2"))
	waitFor(t, seen, "resume after first completion")
	close(b.gate("task-1"))
	waitFor(t, seen, "resume after second completion")
	if err := waitFor(t, runDone, "run to finish"); err != nil {
		t.Fatal(err)
	}

	msgs := l.Messages()
	var order []string
	for _, m := range msgs {
		switch {
		case m.Role == types.RoleToolResult:
			order = append(order, "ack:"+textOfMsg(m))
		case m.Source == types.SourceSubagentCompletion:
			order = append(order, textOfMsg(m))
		}
	}
	want := []string{"ack:started task-1", "ack:started task-2", "report task-2", "report task-1"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("history order = %v, want %v", order, want)
	}
	if n := prov.count(); n != 4 {
		t.Fatalf("provider requests = %d, want 4", n)
	}
	if !hasEvent(events, types.EventAgentEnd) {
		t.Fatal("missing agent_end")
	}
}

func TestLoopCancelJoinsBackgroundTasks(t *testing.T) {
	b := newBGTool()
	prov := &scriptProvider{reply: func(i int, _ llm.Request) types.Message {
		if i == 0 {
			return toolCallMsg("1")
		}
		return textMsg("launched")
	}}
	ctx, cancel := context.WithCancel(context.Background())
	l := New(Config{Provider: prov, Registry: tools.NewRegistry(b), Model: llm.Model{ID: "m"}}, nil,
		func(context.Context, types.AgentEvent) error { return nil })
	runDone := make(chan error, 1)
	go func() {
		_, err := l.Run(ctx, []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("go")}}})
		runDone <- err
	}()
	waitFor(t, b.started, "task start")
	cancel()
	if err := waitFor(t, runDone, "run to return after cancel"); err == nil {
		t.Fatal("expected cancellation error")
	}
	if l.sched != nil {
		t.Fatal("scheduler leaked past Run")
	}
}

func TestQueueWakeOnEnqueue(t *testing.T) {
	for _, q := range []*Queue{NewQueue(), {}} {
		w := q.Wake()
		q.EnqueueFollowUp(types.Message{Role: types.RoleUser})
		q.EnqueueSteering(types.Message{Role: types.RoleUser})
		waitFor(t, w, "wake")
		select {
		case <-w:
			t.Fatal("wake notifications must coalesce")
		default:
		}
	}
}
