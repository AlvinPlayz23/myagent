// Package eventbus provides a guaranteed-delivery pub/sub bus for agent events.
//
// Design rule 1 from myagent-plan.md: never drop lifecycle events. Each
// subscriber gets its own buffered channel; a slow subscriber applies
// backpressure (Publish blocks) rather than losing events. Subscriptions are
// protected by a mutex and every subscription returns an Unsubscribe func so
// callers never leak goroutines.
package eventbus

import (
	"context"
	"sync"

	"github.com/myagent/myagent/internal/types"
)

// defaultBuffer is the per-subscriber channel capacity. Large enough to absorb
// bursts of streaming deltas without forcing the publisher to block on every
// event, small enough to bound memory.
const defaultBuffer = 256

// Bus fans agent events out to any number of subscribers with no dropped
// events.
type Bus struct {
	mu     sync.Mutex
	subs   map[int]*subscriber
	nextID int
	closed bool
	buffer int
}

type subscriber struct {
	ch chan types.AgentEvent
}

// New creates an empty Bus.
func New() *Bus {
	return &Bus{subs: make(map[int]*subscriber), buffer: defaultBuffer}
}

// Subscribe registers a new subscriber and returns its event channel plus an
// Unsubscribe function. The channel is closed when the caller unsubscribes or
// the bus is closed. Callers MUST drain the channel (or unsubscribe) to avoid
// blocking Publish.
func (b *Bus) Subscribe() (<-chan types.AgentEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := b.nextID
	b.nextID++
	sub := &subscriber{ch: make(chan types.AgentEvent, b.buffer)}

	if b.closed {
		// Bus already closed: hand back an already-closed channel so the caller
		// sees termination immediately.
		close(sub.ch)
		return sub.ch, func() {}
	}

	b.subs[id] = sub

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if s, ok := b.subs[id]; ok {
				delete(b.subs, id)
				close(s.ch)
			}
		})
	}
	return sub.ch, unsubscribe
}

// Publish delivers an event to every current subscriber. Delivery is
// guaranteed: if a subscriber's buffer is full, Publish blocks until the
// subscriber drains or the context is cancelled. Returns ctx.Err() if the
// context is cancelled before delivery completes.
func (b *Bus) Publish(ctx context.Context, event types.AgentEvent) error {
	// Snapshot the channels under lock so we don't hold the mutex while
	// (potentially) blocking on a send.
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	channels := make([]chan types.AgentEvent, 0, len(b.subs))
	for _, s := range b.subs {
		channels = append(channels, s.ch)
	}
	b.mu.Unlock()

	for _, ch := range channels {
		select {
		case ch <- event:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Close terminates the bus and closes every subscriber channel. Subsequent
// Publish calls are no-ops and subsequent Subscribe calls return a closed
// channel.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, s := range b.subs {
		delete(b.subs, id)
		close(s.ch)
	}
}
