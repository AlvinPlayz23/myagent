package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/session"
	"github.com/myagent/myagent/internal/types"
)

// scriptedProvider is a fake llm.Provider that replies with canned text. If
// block is non-nil, Stream waits on it (after emitting "start") until closed
// or ctx is canceled, allowing tests to hold a run open.
type scriptedProvider struct {
	mu       sync.Mutex
	requests []llm.Request
	reply    string
	block    chan struct{}
}

func (p *scriptedProvider) Stream(ctx context.Context, model llm.Model, req llm.Request) (<-chan llm.StreamEvent, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	block := p.block
	reply := p.reply
	p.mu.Unlock()

	out := make(chan llm.StreamEvent, 4)
	go func() {
		defer close(out)
		out <- llm.StreamEvent{Type: "start", Partial: &types.Message{Role: types.RoleAssistant}}
		if block != nil {
			select {
			case <-block:
			case <-ctx.Done():
				out <- llm.StreamEvent{Type: "error", Error: &types.Message{
					Role:         types.RoleAssistant,
					StopReason:   types.StopAborted,
					ErrorMessage: "aborted",
				}}
				return
			}
		}
		out <- llm.StreamEvent{Type: "text_delta", Delta: reply}
		out <- llm.StreamEvent{Type: "done", Message: &types.Message{
			Role:       types.RoleAssistant,
			Content:    []types.ContentBlock{types.TextBlock(reply)},
			StopReason: types.StopStop,
		}}
	}()
	return out, nil
}

func (p *scriptedProvider) numRequests() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

// newTestManager builds a Manager over a temp MYAGENT_DIR with the given
// provider.
func newTestManager(t *testing.T, provider llm.Provider) (*Manager, context.CancelFunc) {
	t.Helper()
	t.Setenv("MYAGENT_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	m := NewManager(ctx, Options{
		Resolve: func(providerName, modelID string) (llm.Provider, llm.Model, error) {
			if modelID == "" {
				modelID = "test-model"
			}
			return provider, llm.Model{ID: modelID, Provider: "test", BaseURL: "http://unused"}, nil
		},
		DefaultCwd: t.TempDir(),
	})
	t.Cleanup(func() {
		cancel()
		m.Shutdown()
	})
	return m, cancel
}

// drainUntilDone collects events until a Done event arrives or the timeout
// elapses.
func drainUntilDone(t *testing.T, ss *ServerSession) ([]types.AgentEvent, error) {
	t.Helper()
	var events []types.AgentEvent
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-ss.Events():
			if ev.Done {
				return events, ev.Err
			}
			if ev.AgentEvent != nil {
				events = append(events, *ev.AgentEvent)
			}
		case <-timeout:
			t.Fatal("timed out waiting for Done event")
		}
	}
}

func eventTypes(events []types.AgentEvent) []types.AgentEventType {
	var out []types.AgentEventType
	for _, ev := range events {
		out = append(out, ev.Type)
	}
	return out
}

func TestPromptStreamsEventsAndPersists(t *testing.T) {
	provider := &scriptedProvider{reply: "hello there"}
	m, _ := newTestManager(t, provider)

	ss, err := m.Create("conn1", CreateParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.Prompt("hi"); err != nil {
		t.Fatal(err)
	}
	events, runErr := drainUntilDone(t, ss)
	if runErr != nil {
		t.Fatalf("run error: %v", runErr)
	}

	// Expect ordered lifecycle: agent_start, turn_start, prompt message pair,
	// then assistant stream, then agent_end.
	typesSeen := eventTypes(events)
	if typesSeen[0] != types.EventAgentStart {
		t.Errorf("first event = %v, want agent_start", typesSeen[0])
	}
	last := typesSeen[len(typesSeen)-1]
	if last != types.EventAgentEnd {
		t.Errorf("last event = %v, want agent_end", last)
	}

	// Persistence: reopen the JSONL and check user + assistant messages.
	reopened, err := session.ResumeByID(ss.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	msgs := reopened.Messages()
	if len(msgs) != 2 {
		t.Fatalf("persisted %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != types.RoleUser || msgs[1].Role != types.RoleAssistant {
		t.Errorf("roles = %v/%v, want user/assistant", msgs[0].Role, msgs[1].Role)
	}
	if got := msgs[1].Content[0].Text; got != "hello there" {
		t.Errorf("assistant text = %q, want %q", got, "hello there")
	}
}

func TestPromptWhileRunningReturnsBusy(t *testing.T) {
	provider := &scriptedProvider{reply: "ok", block: make(chan struct{})}
	m, _ := newTestManager(t, provider)

	ss, err := m.Create("conn1", CreateParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.Prompt("first"); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, ss)

	if err := ss.Prompt("second"); !errors.Is(err, ErrBusy) {
		t.Errorf("second Prompt error = %v, want ErrBusy", err)
	}
	if err := ss.Compact(); !errors.Is(err, ErrBusy) {
		t.Errorf("Compact error = %v, want ErrBusy", err)
	}
	close(provider.block)
	if _, err := drainUntilDone(t, ss); err != nil {
		t.Fatalf("run error: %v", err)
	}
}

func TestAbortCancelsRun(t *testing.T) {
	provider := &scriptedProvider{reply: "ok", block: make(chan struct{})}
	m, _ := newTestManager(t, provider)

	ss, err := m.Create("conn1", CreateParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.Prompt("hi"); err != nil {
		t.Fatal(err)
	}
	waitRequests(t, provider, 1) // provider mid-stream, blocked
	ss.Abort()

	// The run always terminates with a Done event; the abort surfaces either
	// as an aborted assistant message or as a context-canceled run error
	// (depending on which emit observes cancellation first — same semantics
	// as the TUI runner).
	_, runErr := drainUntilDone(t, ss)
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		t.Errorf("run error = %v, want nil or context.Canceled", runErr)
	}
	if ss.Running() {
		t.Error("session still running after abort completion")
	}
}

func TestSteerRequiresActiveRun(t *testing.T) {
	provider := &scriptedProvider{reply: "ok", block: make(chan struct{})}
	m, _ := newTestManager(t, provider)

	ss, err := m.Create("conn1", CreateParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.Steer("nope"); !errors.Is(err, ErrNotRunning) {
		t.Errorf("Steer while idle = %v, want ErrNotRunning", err)
	}
	if err := ss.FollowUp("nope"); !errors.Is(err, ErrNotRunning) {
		t.Errorf("FollowUp while idle = %v, want ErrNotRunning", err)
	}

	if err := ss.Prompt("hi"); err != nil {
		t.Fatal(err)
	}
	// Wait until the provider is mid-stream so the steering message cannot be
	// drained into the first turn's initial poll.
	waitRequests(t, provider, 1)
	if err := ss.Steer("also do this"); err != nil {
		t.Errorf("Steer while running = %v, want nil", err)
	}
	close(provider.block)
	if _, err := drainUntilDone(t, ss); err != nil {
		t.Fatalf("run error: %v", err)
	}
	// The steering message was injected: the provider saw a second request
	// containing it.
	if provider.numRequests() < 2 {
		t.Errorf("provider requests = %d, want >= 2 (steering injects a turn)", provider.numRequests())
	}
}

func TestOwnershipAndRelease(t *testing.T) {
	provider := &scriptedProvider{reply: "ok"}
	m, _ := newTestManager(t, provider)

	ss, err := m.Create("conn1", CreateParams{})
	if err != nil {
		t.Fatal(err)
	}

	// Another connection cannot act on the owned session.
	if _, err := m.Get("conn2", ss.ID()); !errors.Is(err, ErrNotOwner) {
		t.Errorf("Get from conn2 = %v, want ErrNotOwner", err)
	}

	// After release (disconnect), conn2 can claim it via Resume.
	m.ReleaseOwner("conn1")
	got, err := m.Resume("conn2", ss.ID())
	if err != nil {
		t.Fatalf("Resume from conn2 after release: %v", err)
	}
	if got != ss {
		t.Error("Resume returned a different live session instance")
	}
}

func TestReleaseAbortsActiveRun(t *testing.T) {
	provider := &scriptedProvider{reply: "ok", block: make(chan struct{})}
	m, _ := newTestManager(t, provider)

	ss, err := m.Create("conn1", CreateParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.Prompt("hi"); err != nil {
		t.Fatal(err)
	}
	waitRequests(t, provider, 1)
	m.ReleaseOwner("conn1")
	if _, err := drainUntilDone(t, ss); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("run error after release = %v, want nil or context.Canceled", err)
	}
	if ss.Running() {
		t.Error("run still active after ReleaseOwner")
	}
}

func TestResumeFromDiskRoundTrip(t *testing.T) {
	provider := &scriptedProvider{reply: "persisted reply"}
	m, _ := newTestManager(t, provider)

	ss, err := m.Create("conn1", CreateParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.Prompt("remember me"); err != nil {
		t.Fatal(err)
	}
	if _, err := drainUntilDone(t, ss); err != nil {
		t.Fatal(err)
	}
	id := ss.ID()
	if err := m.Close("conn1", id); err != nil {
		t.Fatal(err)
	}

	// Resume re-opens from disk with full history.
	resumed, err := m.Resume("conn2", id)
	if err != nil {
		t.Fatal(err)
	}
	msgs := resumed.Messages()
	if len(msgs) != 2 {
		t.Fatalf("resumed %d messages, want 2", len(msgs))
	}
	if !strings.Contains(msgs[0].Content[0].Text, "remember me") {
		t.Errorf("resumed first message = %q, want prompt text", msgs[0].Content[0].Text)
	}

	// Continue the conversation on the resumed session.
	if err := resumed.Prompt("and again"); err != nil {
		t.Fatal(err)
	}
	if _, err := drainUntilDone(t, resumed); err != nil {
		t.Fatal(err)
	}
	if got := len(resumed.Messages()); got != 4 {
		t.Errorf("messages after second turn = %d, want 4", got)
	}
}

func TestResumeUnknownSession(t *testing.T) {
	provider := &scriptedProvider{reply: "ok"}
	m, _ := newTestManager(t, provider)
	if _, err := m.Resume("conn1", "no-such-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Resume unknown = %v, want ErrNotFound", err)
	}
}

func TestConcurrentSessions(t *testing.T) {
	provider := &scriptedProvider{reply: "parallel"}
	m, _ := newTestManager(t, provider)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			connID := "conn" + string(rune('a'+i))
			ss, err := m.Create(connID, CreateParams{})
			if err != nil {
				t.Error(err)
				return
			}
			if err := ss.Prompt("hi"); err != nil {
				t.Error(err)
				return
			}
			if _, err := drainUntilDone(t, ss); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
}

func TestSetModelBusyGuard(t *testing.T) {
	provider := &scriptedProvider{reply: "ok", block: make(chan struct{})}
	m, _ := newTestManager(t, provider)

	ss, err := m.Create("conn1", CreateParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.Prompt("hi"); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, ss)
	if err := m.SetModel("conn1", ss.ID(), "test", "other-model"); !errors.Is(err, ErrBusy) {
		t.Errorf("SetModel while running = %v, want ErrBusy", err)
	}
	close(provider.block)
	if _, err := drainUntilDone(t, ss); err != nil {
		t.Fatal(err)
	}
	if err := m.SetModel("conn1", ss.ID(), "test", "other-model"); err != nil {
		t.Errorf("SetModel while idle = %v, want nil", err)
	}
	if got := ss.ModelID(); got != "test/other-model" {
		t.Errorf("ModelID = %q, want test/other-model", got)
	}
}

func TestManagerShutdownCancelsRuns(t *testing.T) {
	provider := &scriptedProvider{reply: "ok", block: make(chan struct{})}
	m, cancel := newTestManager(t, provider)

	ss, err := m.Create("conn1", CreateParams{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ss.Prompt("hi"); err != nil {
		t.Fatal(err)
	}
	waitRunning(t, ss)
	cancel()
	m.Shutdown()

	deadline := time.After(5 * time.Second)
	for ss.Running() {
		select {
		case <-deadline:
			t.Fatal("run did not stop after shutdown")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := ss.Prompt("again"); !errors.Is(err, ErrClosed) {
		t.Errorf("Prompt after shutdown = %v, want ErrClosed", err)
	}
}

// waitRunning polls until the session reports an active run.
func waitRunning(t *testing.T, ss *ServerSession) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !ss.Running() {
		select {
		case <-deadline:
			t.Fatal("run never started")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// waitRequests polls until the provider has served at least n Stream calls,
// i.e. the run has actually reached the provider (not just started).
func waitRequests(t *testing.T, p *scriptedProvider, n int) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for p.numRequests() < n {
		select {
		case <-deadline:
			t.Fatalf("provider never reached %d requests", n)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
