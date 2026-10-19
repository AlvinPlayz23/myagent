package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// runResult carries a completed Run's return values across a goroutine.
type runResult struct {
	produced []types.Message
	err      error
}

// guardedEvents is a sink that rejects emits on a cancelled context, like the
// TUI runner and the server session do, and records everything it accepts.
// It proves the final drain emits with a live context: a cancelled-context
// emit would be refused and the report would never be persisted.
type guardedEvents struct {
	mu     sync.Mutex
	events []types.AgentEvent
}

func (g *guardedEvents) sink(ctx context.Context, ev types.AgentEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	g.events = append(g.events, ev)
	g.mu.Unlock()
	return nil
}

func (g *guardedEvents) all() []types.AgentEvent {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]types.AgentEvent(nil), g.events...)
}

func (g *guardedEvents) hasMessageEnd(text string) bool {
	for _, ev := range g.all() {
		if ev.Type == types.EventMessageEnd && ev.Message != nil && textOfMsg(*ev.Message) == text {
			return true
		}
	}
	return false
}

// gatedBGTool runs tasks that ignore cancellation: once released, the work is
// already done, so its report must survive a concurrent abort.
type gatedBGTool struct {
	started  chan string
	finished chan string
	mu       sync.Mutex
	release  map[string]chan struct{}
}

func newGatedBGTool() *gatedBGTool {
	return &gatedBGTool{
		started:  make(chan string, 8),
		finished: make(chan string, 8),
		release:  map[string]chan struct{}{},
	}
}

func (b *gatedBGTool) Name() string               { return "bg" }
func (b *gatedBGTool) Description() string        { return "background tool" }
func (b *gatedBGTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (b *gatedBGTool) Execute(context.Context, string, map[string]any) (*types.ToolResult, error) {
	return nil, context.Canceled // never used: the tool is a BackgroundTool
}

func (b *gatedBGTool) gate(id string) chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.release[id] == nil {
		b.release[id] = make(chan struct{})
	}
	return b.release[id]
}

func (b *gatedBGTool) PrepareBackground(_ context.Context, callID string, _ map[string]any, _ Config) (*BackgroundTask, error) {
	id := "task-" + callID
	gate := b.gate(id)
	return &BackgroundTask{
		ID:  id,
		Ack: types.TextResult("started "+id, nil),
		Run: func(context.Context) (types.Message, error) {
			b.started <- id
			<-gate
			b.finished <- id
			return reportMsg("report " + id), nil
		},
		OnError: func(err error) types.Message {
			return reportMsg("failed " + id + ": " + err.Error())
		},
	}, nil
}

func reportMsg(text string) types.Message {
	return types.Message{
		Role:    types.RoleUser,
		Source:  types.SourceSubagentCompletion,
		Content: []types.ContentBlock{types.TextBlock(text)},
	}
}

func completionsIn(msgs []types.Message) []string {
	var out []string
	for _, m := range msgs {
		if m.Source == types.SourceSubagentCompletion {
			out = append(out, textOfMsg(m))
		}
	}
	return out
}

func eventTypes(events []types.AgentEvent) []types.AgentEventType {
	var out []types.AgentEventType
	for _, ev := range events {
		out = append(out, ev.Type)
	}
	return out
}

// blockingProvider answers the first request with a background tool call and
// then blocks the next stream until the run is cancelled, holding the loop
// inside a provider request where it cannot inject background completions.
type blockingProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *blockingProvider) Stream(ctx context.Context, _ llm.Model, _ llm.Request) (<-chan llm.StreamEvent, error) {
	p.mu.Lock()
	p.calls++
	n := p.calls
	p.mu.Unlock()
	if n > 1 {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	msg := toolCallMsg("1")
	msg.Role = types.RoleAssistant
	out := make(chan llm.StreamEvent, 1)
	out <- llm.StreamEvent{Type: "done", Message: &msg}
	close(out)
	return out, nil
}

// TestAbortDeliversFinishedCompletion pins the abort exit path: a task whose
// report was already waiting must still reach history, the returned messages,
// and the event stream even though the run was cancelled and the run context
// is dead when the loop unwinds.
func TestAbortDeliversFinishedCompletion(t *testing.T) {
	b := newGatedBGTool()
	guarded := &guardedEvents{}
	ctx, cancel := context.WithCancel(context.Background())
	l := New(Config{Provider: &blockingProvider{}, Registry: tools.NewRegistry(b), Model: llm.Model{ID: "m"}}, nil, guarded.sink)

	runDone := make(chan runResult, 1)
	go func() {
		produced, err := l.Run(ctx, []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("go")}}})
		runDone <- runResult{produced: produced, err: err}
	}()

	waitFor(t, b.started, "task start")
	close(b.gate("task-1"))
	waitFor(t, b.finished, "task to finish before the abort")

	cancel()
	res := waitFor(t, runDone, "run to return after cancel")
	if !errors.Is(res.err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", res.err)
	}

	const want = "report task-1"
	if got := completionsIn(l.Messages()); len(got) != 1 || got[0] != want {
		t.Fatalf("history completions = %v, want [%q]", got, want)
	}
	if got := completionsIn(res.produced); len(got) != 1 || got[0] != want {
		t.Fatalf("produced completions = %v, want [%q]", got, want)
	}
	if !guarded.hasMessageEnd(want) {
		t.Fatalf("missing message_end for the finished report; events = %v", eventTypes(guarded.all()))
	}
	if l.sched != nil {
		t.Fatal("scheduler leaked past Run")
	}
}

// TestAbortDeliversUnwindCancellationNotice pins the other half: a task still
// running when the run is aborted is cancelled during close(), and the notice
// it produces while being joined must be delivered too.
func TestAbortDeliversUnwindCancellationNotice(t *testing.T) {
	b := newBGTool()
	prov := &scriptProvider{reply: func(i int, _ llm.Request) types.Message {
		if i == 0 {
			return toolCallMsg("1")
		}
		return textMsg("launched")
	}}
	guarded := &guardedEvents{}
	ctx, cancel := context.WithCancel(context.Background())
	l := New(Config{Provider: prov, Registry: tools.NewRegistry(b), Model: llm.Model{ID: "m"}}, nil, guarded.sink)

	runDone := make(chan runResult, 1)
	go func() {
		produced, err := l.Run(ctx, []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("go")}}})
		runDone <- runResult{produced: produced, err: err}
	}()

	waitFor(t, b.started, "task start")
	cancel()
	res := waitFor(t, runDone, "run to return after cancel")
	if !errors.Is(res.err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", res.err)
	}

	const want = "failed task-1: context canceled"
	if got := completionsIn(l.Messages()); len(got) != 1 || got[0] != want {
		t.Fatalf("history completions = %v, want [%q]", got, want)
	}
	if got := completionsIn(res.produced); len(got) != 1 || got[0] != want {
		t.Fatalf("produced completions = %v, want [%q]", got, want)
	}
	if !guarded.hasMessageEnd(want) {
		t.Fatalf("missing message_end for the cancelled task; events = %v", eventTypes(guarded.all()))
	}
	if l.sched != nil {
		t.Fatal("scheduler leaked past Run")
	}
}

// TestSchedulerCloseKeepsUnwindReports pins the scheduler-level contract the
// loop's final drain relies on: close() joins workers but no longer discards
// the reports they publish while unwinding.
func TestSchedulerCloseKeepsUnwindReports(t *testing.T) {
	s := newBackgroundScheduler(context.Background(), 1)
	b := newBGTool()
	task, _ := b.PrepareBackground(context.Background(), "a", nil, Config{})
	if _, err := s.start(task); err != nil {
		t.Fatal(err)
	}
	waitFor(t, b.started, "task start")
	closed := make(chan struct{})
	go func() {
		s.close()
		close(closed)
	}()
	waitFor(t, closed, "close to join the blocked worker")

	msgs := s.ready()
	if len(msgs) != 1 {
		t.Fatalf("reports after close = %d, want 1", len(msgs))
	}
	if got, want := textOfMsg(msgs[0].msg), "failed task-a: context canceled"; got != want {
		t.Fatalf("report = %q, want %q", got, want)
	}
}

// erroringProvider answers the first request with a background tool call and
// every later one with a model error, so the run reaches agent_end while a
// task is still running.
type erroringProvider struct{}

func (p *erroringProvider) Stream(_ context.Context, _ llm.Model, req llm.Request) (<-chan llm.StreamEvent, error) {
	var msg types.Message
	if len(req.Messages) <= 1 {
		msg = toolCallMsg("1")
	} else {
		msg = types.Message{StopReason: types.StopError, ErrorMessage: "boom"}
	}
	msg.Role = types.RoleAssistant
	out := make(chan llm.StreamEvent, 1)
	out <- llm.StreamEvent{Type: "done", Message: &msg}
	close(out)
	return out, nil
}

// TestStopErrorDrainsBeforeAgentEnd pins the ordering the final drain must
// respect. agent_end is terminal and carries a Messages snapshot, so a report
// published while the run tears down has to be delivered before it. This is the
// path that got it wrong: the loop emitted agent_end and returned, and the
// report only appeared afterwards — out of order, and absent from the snapshot.
//
// bgTool's task can end two ways, and both must land before agent_end. Here the
// gate is never opened, so the task can only finish via the cancellation that
// drainCompletions' close() issues, which makes the expected report text fixed.
func TestStopErrorDrainsBeforeAgentEnd(t *testing.T) {
	b := newBGTool()
	guarded := &guardedEvents{}
	l := New(Config{Provider: &erroringProvider{}, Registry: tools.NewRegistry(b), Model: llm.Model{ID: "m"}}, nil, guarded.sink)

	if _, err := l.Run(context.Background(), []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("go")}}}); err != nil {
		t.Fatalf("run error = %v, want nil", err)
	}

	seen := eventTypes(guarded.all())
	if last := seen[len(seen)-1]; last != types.EventAgentEnd {
		t.Fatalf("last event = %v, want agent_end; stream = %v", last, seen)
	}
	if !guarded.hasMessageEnd("failed task-1: context canceled") {
		t.Fatalf("missing message_end for the drained report; stream = %v", seen)
	}
	agentEndAt, completionAt := -1, -1
	for i, ev := range guarded.all() {
		switch {
		case ev.Type == types.EventAgentEnd:
			agentEndAt = i
		case ev.Type == types.EventMessageEnd && ev.Message != nil &&
			ev.Message.Source == types.SourceSubagentCompletion && completionAt < 0:
			completionAt = i
		}
	}
	if agentEndAt < 0 || completionAt < 0 || completionAt > agentEndAt {
		t.Fatalf("completion at %d, agent_end at %d; want the completion first", completionAt, agentEndAt)
	}
	if got := completionsIn(l.Messages()); len(got) != 1 || got[0] != "failed task-1: context canceled" {
		t.Fatalf("history completions = %v", got)
	}
}

// TestDrainContextIsLiveButBounded pins both halves of the drain context.
// It must survive a dead run context, or the sinks drop the emits and the report
// is never persisted (TestAbortDeliversFinishedCompletion covers that failure).
// And it must carry a deadline, or a vanished event consumer blocks the run
// goroutine forever inside the sinks' unbounded channel send.
func TestDrainContextIsLiveButBounded(t *testing.T) {
	b := newBGTool()
	prov := &scriptProvider{reply: func(i int, _ llm.Request) types.Message {
		if i == 0 {
			return toolCallMsg("1")
		}
		return textMsg("launched")
	}}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var events []types.AgentEvent
	drainLive, drainBounded := false, false

	sink := func(ctx context.Context, ev types.AgentEvent) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		mu.Lock()
		defer mu.Unlock()
		events = append(events, ev)
		if ev.Type == types.EventMessageEnd && ev.Message != nil && ev.Message.Source == types.SourceSubagentCompletion {
			_, hasDeadline := ctx.Deadline()
			drainLive = runCtx.Err() != nil && ctx.Err() == nil
			drainBounded = hasDeadline
		}
		return nil
	}
	l := New(Config{Provider: prov, Registry: tools.NewRegistry(b), Model: llm.Model{ID: "m"}}, nil, sink)

	runDone := make(chan runResult, 1)
	go func() {
		produced, err := l.Run(runCtx, []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("go")}}})
		runDone <- runResult{produced: produced, err: err}
	}()

	waitFor(t, b.started, "task start")
	cancel()
	if err := waitFor(t, runDone, "run to return after cancel").err; !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context.Canceled", err)
	}
	if !drainLive {
		t.Fatal("drain emitted on an already-dead context; the report would be dropped")
	}
	if !drainBounded {
		t.Fatal("drain context carries no deadline; a vanished consumer could wedge the run goroutine")
	}
}

// TestInjectCompletionsHonoursSuppress pins the delivery-time policy: a report
// its own task suppresses never reaches the event stream or the history, while
// every other report in the same drain still does. Suppression is per task, not
// per run, so it must not become a blanket filter on the queue.
func TestInjectCompletionsHonoursSuppress(t *testing.T) {
	s := newBackgroundScheduler(context.Background(), 4)
	defer s.close()
	s.done = append(s.done,
		completion{task: &BackgroundTask{ID: "task-1", Suppress: func(types.Message) bool { return true }},
			msg: reportMsg("dropped")},
		completion{task: &BackgroundTask{ID: "task-2"}, msg: reportMsg("kept")},
	)

	var emitted []string
	l := New(Config{}, nil, func(_ context.Context, ev types.AgentEvent) error {
		if ev.Type == types.EventMessageEnd && ev.Message != nil {
			emitted = append(emitted, textOfMsg(*ev.Message))
		}
		return nil
	})
	l.sched = s

	var produced []types.Message
	if err := l.injectCompletions(context.Background(), &produced); err != nil {
		t.Fatal(err)
	}

	if len(emitted) != 1 || emitted[0] != "kept" {
		t.Fatalf("emitted = %v, want only the unsuppressed report", emitted)
	}
	if len(produced) != 1 || textOfMsg(produced[0]) != "kept" {
		t.Fatalf("produced = %d entries, want just the unsuppressed report", len(produced))
	}
	if len(l.Messages()) != 1 || textOfMsg(l.Messages()[0]) != "kept" {
		t.Fatalf("history = %v, want just the unsuppressed report", completionsIn(l.Messages()))
	}
	// A suppressed report is dropped, not left queued: an undrained report would
	// keep the scheduler pinned at its task limit for the rest of the run.
	if running, ready := s.state(); running != 0 || ready != 0 {
		t.Fatalf("state = %d running, %d ready; want both drained", running, ready)
	}
}
