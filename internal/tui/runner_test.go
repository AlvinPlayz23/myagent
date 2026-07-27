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
