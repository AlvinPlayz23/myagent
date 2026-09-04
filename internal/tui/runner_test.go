package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// TestRunnerReturnsOnEventFailure ensures persistence-hook failures stop the
// loop before the event is forwarded to the UI.
func TestRunnerReturnsOnEventFailure(t *testing.T) {
	r := newRunner(agent.Config{}, newMsgQueue(), nil)
	want := errors.New("session write failed")
	r.onEvent = func(types.AgentEvent) error { return want }

	msg := r.start(context.Background(), types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{types.TextBlock("hello")},
	})()
	if msg != nil {
		t.Fatalf("start returned %T, want nil", msg)
	}
	event := <-r.events
	if event.done == nil {
		t.Fatalf("runner event = %#v, want completion", event)
	}
	done := *event.done
	if !errors.Is(done.err, want) {
		t.Fatalf("agentDoneMsg.err = %v, want %v", done.err, want)
	}
	select {
	case ev := <-r.events:
		t.Fatalf("unexpected UI event after completion: %#v", ev)
	default:
	}
}

// TestRunnerDeliversEventsAfterCancellation ensures an aborted run's final
// events still reach the UI when the channel has room. A plain select would
// drop them half the time once the context is done, hiding the abort outcome.
func TestRunnerDeliversEventsAfterCancellation(t *testing.T) {
	r := newRunner(agent.Config{}, newMsgQueue(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // aborted before delivery

	ev := types.AgentEvent{Type: types.EventMessageEnd}
	if err := r.deliver(ctx, runnerEvent{ev: &ev}); err != nil {
		t.Fatalf("deliver after cancellation = %v, want nil (buffered)", err)
	}
	if got := (<-r.events).ev.Type; got != types.EventMessageEnd {
		t.Fatalf("delivered event = %v, want %v", got, types.EventMessageEnd)
	}
}

// TestRunnerDeliverYieldsWhenChannelFull ensures a stalled UI cannot hang the
// loop: once the buffer is full and the context is done, deliver gives up.
func TestRunnerDeliverYieldsWhenChannelFull(t *testing.T) {
	r := newRunner(agent.Config{}, newMsgQueue(), nil)
	r.events = make(chan runnerEvent, 1)
	r.events <- runnerEvent{} // fill the buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ev := types.AgentEvent{Type: types.EventAgentEnd}
	if err := r.deliver(ctx, runnerEvent{ev: &ev}); !errors.Is(err, context.Canceled) {
		t.Fatalf("deliver on full channel = %v, want context.Canceled", err)
	}
}
