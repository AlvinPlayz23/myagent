package eventbus

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/myagent/myagent/internal/types"
)

// TestNoDroppedEvents verifies guaranteed delivery: every subscriber receives
// every published event, in order, even when one subscriber is slow.
func TestNoDroppedEvents(t *testing.T) {
	const nEvents = 1000
	const nSubs = 3

	b := New()
	defer b.Close()

	var wg sync.WaitGroup
	counts := make([]int, nSubs)
	for i := 0; i < nSubs; i++ {
		ch, _ := b.Subscribe()
		wg.Add(1)
		slow := i == 0
		go func(idx int, ch <-chan types.AgentEvent, slow bool) {
			defer wg.Done()
			prev := -1
			for ev := range ch {
				if slow {
					time.Sleep(time.Microsecond) // apply backpressure
				}
				got := ev.Args["seq"].(int)
				if got != prev+1 {
					t.Errorf("sub %d: out-of-order or dropped event: got %d after %d", idx, got, prev)
				}
				prev = got
				counts[idx]++
			}
		}(i, ch, slow)
	}

	ctx := context.Background()
	for i := 0; i < nEvents; i++ {
		if err := b.Publish(ctx, types.AgentEvent{
			Type: types.EventMessageUpdate,
			Args: map[string]any{"seq": i},
		}); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}
	b.Close()
	wg.Wait()

	for i, c := range counts {
		if c != nEvents {
			t.Errorf("sub %d received %d events, want %d", i, c, nEvents)
		}
	}
}

// TestUnsubscribe verifies that unsubscribing stops delivery and closes the
// channel, and that Publish no longer blocks on the removed subscriber.
func TestUnsubscribe(t *testing.T) {
	b := New()
	defer b.Close()

	ch, unsub := b.Subscribe()
	unsub()

	// Channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel after unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after unsubscribe")
	}

	// Publish must not block now that there are no subscribers.
	done := make(chan struct{})
	go func() {
		_ = b.Publish(context.Background(), types.AgentEvent{Type: types.EventTurnStart})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked with no subscribers")
	}
}

// TestSubscribeAfterClose returns an already-closed channel.
func TestSubscribeAfterClose(t *testing.T) {
	b := New()
	b.Close()
	ch, _ := b.Subscribe()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel when subscribing after close")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed when subscribing after close")
	}
}
