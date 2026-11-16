package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

// start() guards task ids against duplicates, but its lookup consults only the
// running map. A task that has already finished (its report sits undelivered
// in done) no longer appears there, so a second task reusing the id is
// admitted, violating the "duplicate background task id" invariant the same
// error already enforces for running tasks. Features that assign
// human/LLM-chosen ids to background work rely on that invariant for
// delivery dedup.
func TestSchedulerRejectsDuplicateIdOfUndeliveredCompletion(t *testing.T) {
	s := newBackgroundScheduler(context.Background(), 8)
	newTask := func() *BackgroundTask {
		return &BackgroundTask{
			ID:  "dup",
			Ack: types.TextResult("started", nil),
			Run: func(ctx context.Context) (types.Message, error) {
				return types.Message{
					Role:    types.RoleUser,
					Source:  types.SourceSubagentCompletion,
					Content: []types.ContentBlock{types.TextBlock("report")},
				}, nil
			},
			OnError: func(err error) types.Message {
				return types.Message{
					Role:    types.RoleUser,
					Content: []types.ContentBlock{types.TextBlock("failed")},
				}
			},
		}
	}
	if _, err := s.start(newTask()); err != nil {
		t.Fatal(err)
	}
	// Wait for the worker to publish its report into done (still undelivered).
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ready := s.state(); ready == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completion never published")
		}
		time.Sleep(1 * time.Millisecond)
	}
	if _, err := s.start(newTask()); err == nil {
		t.Fatal("duplicate id of a completed, undelivered task must be rejected")
	} else if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected error: %v", err)
	}
}
