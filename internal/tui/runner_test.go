package tui

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/llm"
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

// TestRunnerConfigConcurrentAccess covers the subagent base accessor reading
// the live config while the UI goroutine mutates it; run with -race.
func TestRunnerConfigConcurrentAccess(t *testing.T) {
	r := newRunner(agent.Config{}, newMsgQueue(), nil)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = r.config()
				}
			}
		}()
	}
	for i := 0; i < 2000; i++ {
		r.setEffort(llm.EffortHigh)
		r.setModel(nil, llm.Model{ID: "m"})
		r.setTools(nil, "prompt")
		r.setSessionID("sess")
	}
	close(stop)
	wg.Wait()
	if got := r.config(); got.Effort != llm.EffortHigh || got.Model.ID != "m" || got.Model.SessionID != "sess" || got.SystemPrompt != "prompt" {
		t.Fatalf("config = %+v", got)
	}
}
