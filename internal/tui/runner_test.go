package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/types"
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
	done, ok := msg.(agentDoneMsg)
	if !ok {
		t.Fatalf("start returned %T, want agentDoneMsg", msg)
	}
	if !errors.Is(done.err, want) {
		t.Fatalf("agentDoneMsg.err = %v, want %v", done.err, want)
	}
	select {
	case ev := <-r.events:
		t.Fatalf("unexpected UI event after persistence failure: %s", ev.ev.Type)
	default:
	}
}
