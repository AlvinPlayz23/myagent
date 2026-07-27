package tui

import (
	"context"
	"testing"
)

func TestAbortActiveRunCancelsAndDrainsQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	q := newMsgQueue()
	q.EnqueueSteering(userMessage("steer"))
	q.EnqueueFollowUp(userMessage("later"))
	m := newModel(context.Background(), nil, q, newTheme(), newMDRenderer(), "model", "")
	m.working = true
	m.cancel = cancel
	m.queuedFollowUps = []queuedMessage{{display: "later", message: userMessage("later")}}

	if !m.abortActiveRun() {
		t.Fatal("active run was not aborted")
	}
	if ctx.Err() == nil {
		t.Fatal("run context was not canceled")
	}
	if got := q.PendingCount(); got != 0 {
		t.Fatalf("pending queue = %d, want 0", got)
	}
	if len(m.queuedFollowUps) != 0 {
		t.Fatalf("pending follow-up cards = %#v, want empty", m.queuedFollowUps)
	}
	if !m.working || !m.abortRequested {
		t.Fatal("run was marked complete before its goroutine finished")
	}
	if m.statusMsg != "Aborting…" {
		t.Fatalf("status = %q", m.statusMsg)
	}
	if !m.abortActiveRun() {
		t.Fatal("repeated abort should remain handled")
	}
}
