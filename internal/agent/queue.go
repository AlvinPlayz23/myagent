// Package agent: Queue is a concurrency-safe implementation of MessageQueue,
// shared by the TUI and server modes.
//
// Ported from pi's interactive message-queue semantics:
//   - Steering messages are injected before the next assistant response within
//     an in-progress run (Enter while the agent is working).
//   - Follow-up messages are processed after the agent would otherwise stop
//     (Alt+Enter while the agent is working).
//
// The agent loop polls Steering()/FollowUp() between turns; the UI or server
// goroutine enqueues concurrently. All access is mutex-guarded.
package agent

import (
	"sync"

	"github.com/myagent/myagent/internal/types"
)

// Queue is a concurrency-safe MessageQueue implementation.
type Queue struct {
	mu       sync.Mutex
	steering []types.Message
	followUp []types.Message
}

// NewQueue returns an empty queue.
func NewQueue() *Queue { return &Queue{} }

// EnqueueSteering adds a steering message (delivered mid-run, before the next
// assistant turn).
func (q *Queue) EnqueueSteering(m types.Message) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.steering = append(q.steering, m)
}

// EnqueueFollowUp adds a follow-up message (delivered after the current work
// completes).
func (q *Queue) EnqueueFollowUp(m types.Message) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.followUp = append(q.followUp, m)
}

// Steering drains and returns any queued steering messages.
func (q *Queue) Steering() []types.Message {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.steering) == 0 {
		return nil
	}
	out := q.steering
	q.steering = nil
	return out
}

// FollowUp drains and returns any queued follow-up messages.
func (q *Queue) FollowUp() []types.Message {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.followUp) == 0 {
		return nil
	}
	out := q.followUp
	q.followUp = nil
	return out
}

// PendingCount returns how many messages are queued (for UI hints).
func (q *Queue) PendingCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.steering) + len(q.followUp)
}

// DrainAll removes and returns every queued message (steering first, then
// follow-up). Used when the user aborts and we want to restore queued text.
func (q *Queue) DrainAll() []types.Message {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := append(append([]types.Message{}, q.steering...), q.followUp...)
	q.steering = nil
	q.followUp = nil
	return out
}
