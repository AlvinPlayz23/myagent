package llm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/myagent/myagent/internal/types"
)

// scriptedProvider is a test Provider whose Stream emits a canned sequence of
// StreamEvents per call, advancing through scripts on each successive call.
type scriptedProvider struct {
	mu      sync.Mutex
	scripts [][]StreamEvent
	calls   int
}

func (p *scriptedProvider) Stream(ctx context.Context, model Model, req Request) (<-chan StreamEvent, error) {
	p.mu.Lock()
	idx := p.calls
	p.calls++
	var script []StreamEvent
	if idx < len(p.scripts) {
		script = p.scripts[idx]
	}
	p.mu.Unlock()

	out := make(chan StreamEvent, len(script)+1)
	go func() {
		defer close(out)
		for _, ev := range script {
			out <- ev
		}
	}()
	return out, nil
}

func (p *scriptedProvider) numCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func retryErr(retryable bool) StreamEvent {
	return StreamEvent{Type: "error", Retryable: retryable, Error: &types.Message{
		Role:         types.RoleAssistant,
		StopReason:   types.StopError,
		ErrorMessage: "boom",
	}}
}

func okStream(text string) []StreamEvent {
	return []StreamEvent{
		{Type: "start", Partial: &types.Message{Role: types.RoleAssistant}},
		{Type: "text_delta", Delta: text},
		{Type: "done", Message: &types.Message{Role: types.RoleAssistant, StopReason: types.StopStop}},
	}
}

func collect(t *testing.T, ch <-chan StreamEvent) []StreamEvent {
	t.Helper()
	var evs []StreamEvent
	for ev := range ch {
		evs = append(evs, ev)
	}
	return evs
}

func fastPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 10, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
}

func TestRetrySucceedsAfterTransientErrors(t *testing.T) {
	inner := &scriptedProvider{scripts: [][]StreamEvent{
		{retryErr(true)},
		{retryErr(true)},
		okStream("hello"),
	}}
	p := NewRetryProvider(inner, fastPolicy())

	ch, err := p.Stream(context.Background(), Model{ID: "m"}, Request{})
	if err != nil {
		t.Fatalf("Stream returned error: %v", err)
	}
	evs := collect(t, ch)

	if got := inner.numCalls(); got != 3 {
		t.Fatalf("inner called %d times, want 3", got)
	}
	var retries, dones int
	for _, ev := range evs {
		switch ev.Type {
		case "retry":
			retries++
		case "done":
			dones++
		case "error":
			t.Fatalf("unexpected error event in output: %+v", ev)
		}
	}
	if retries != 2 {
		t.Fatalf("got %d retry events, want 2", retries)
	}
	if dones != 1 {
		t.Fatalf("got %d done events, want 1", dones)
	}
	// Retry events should carry increasing attempt numbers within the ceiling.
	wantAttempt := 2
	for _, ev := range evs {
		if ev.Type != "retry" {
			continue
		}
		if ev.Attempt != wantAttempt {
			t.Fatalf("retry attempt = %d, want %d", ev.Attempt, wantAttempt)
		}
		if ev.MaxAttempts != 10 {
			t.Fatalf("retry maxAttempts = %d, want 10", ev.MaxAttempts)
		}
		wantAttempt++
	}
}

func TestRetryStopsOnNonRetryableError(t *testing.T) {
	inner := &scriptedProvider{scripts: [][]StreamEvent{
		{retryErr(false)},
	}}
	p := NewRetryProvider(inner, fastPolicy())

	ch, _ := p.Stream(context.Background(), Model{ID: "m"}, Request{})
	evs := collect(t, ch)

	if got := inner.numCalls(); got != 1 {
		t.Fatalf("inner called %d times, want 1 (no retry)", got)
	}
	if len(evs) != 1 || evs[0].Type != "error" {
		t.Fatalf("want a single error event, got %+v", evs)
	}
}

func TestRetryExhaustsMaxAttempts(t *testing.T) {
	// Always transient; must stop after MaxAttempts calls and forward the error.
	scripts := make([][]StreamEvent, 10)
	for i := range scripts {
		scripts[i] = []StreamEvent{retryErr(true)}
	}
	inner := &scriptedProvider{scripts: scripts}
	p := NewRetryProvider(inner, RetryPolicy{MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond})

	ch, _ := p.Stream(context.Background(), Model{ID: "m"}, Request{})
	evs := collect(t, ch)

	if got := inner.numCalls(); got != 4 {
		t.Fatalf("inner called %d times, want 4 (MaxAttempts)", got)
	}
	var retries int
	for _, ev := range evs {
		if ev.Type == "retry" {
			retries++
		}
	}
	if retries != 3 {
		t.Fatalf("got %d retry events, want 3 (MaxAttempts-1)", retries)
	}
	if last := evs[len(evs)-1]; last.Type != "error" {
		t.Fatalf("final event = %q, want error", last.Type)
	}
}

func TestRetryDisabledWhenMaxAttemptsLEOne(t *testing.T) {
	inner := &scriptedProvider{}
	if p := NewRetryProvider(inner, RetryPolicy{MaxAttempts: 1}); p != Provider(inner) {
		t.Fatalf("MaxAttempts<=1 should return the inner provider unchanged")
	}
}

func TestRetryAbortsDuringBackoff(t *testing.T) {
	inner := &scriptedProvider{
		scripts: [][]StreamEvent{{retryErr(true)}, okStream("late")},
	}
	// Long backoff so the cancel below lands while pump is waiting to retry.
	p := NewRetryProvider(inner, RetryPolicy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, _ := p.Stream(ctx, Model{ID: "m"}, Request{})

	// Cancel as soon as the retry is announced (emitted just before the backoff
	// wait), guaranteeing the abort is observed during backoff, not before it.
	var evs []StreamEvent
	for ev := range ch {
		evs = append(evs, ev)
		if ev.Type == "retry" {
			cancel()
		}
	}

	if got := inner.numCalls(); got != 1 {
		t.Fatalf("inner called %d times, want 1 (aborted before retry re-issue)", got)
	}
	last := evs[len(evs)-1]
	if last.Type != "error" || last.Error == nil || last.Error.StopReason != types.StopAborted {
		t.Fatalf("want terminal aborted error, got %+v", last)
	}
}
