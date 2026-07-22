package llm

import (
	"context"
	"math/rand"
	"time"

	"github.com/myagent/myagent/internal/types"
)

// RetryPolicy configures automatic retries of transient provider failures.
// A request is retried only when the provider reports a transient error
// (network failure or a retryable HTTP status) before streaming has begun, so
// no partial content is ever re-emitted.
type RetryPolicy struct {
	MaxAttempts int           // total attempts including the first; <= 1 disables retries
	BaseDelay   time.Duration // delay before the first retry
	MaxDelay    time.Duration // ceiling for the exponential backoff
}

// DefaultRetryPolicy is the policy applied when configuration omits values:
// up to 10 attempts, 1s base delay doubling each attempt, capped at 30s.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 10, BaseDelay: time.Second, MaxDelay: 30 * time.Second}
}

// NewRetryProvider wraps inner so transient failures are retried with backoff
// per policy. When policy.MaxAttempts <= 1 the inner provider is returned
// unchanged, so retries can be disabled with zero overhead.
func NewRetryProvider(inner Provider, policy RetryPolicy) Provider {
	if policy.MaxAttempts <= 1 {
		return inner
	}
	if policy.BaseDelay <= 0 {
		policy.BaseDelay = time.Second
	}
	if policy.MaxDelay <= 0 {
		policy.MaxDelay = 30 * time.Second
	}
	return &retryProvider{inner: inner, policy: policy}
}

type retryProvider struct {
	inner  Provider
	policy RetryPolicy
}

// Stream runs the first attempt synchronously so a programmer-error Go return
// (e.g. empty model id) is preserved, then hands the event channel to pump,
// which retries transient pre-stream errors with backoff.
func (r *retryProvider) Stream(ctx context.Context, model Model, req Request) (<-chan StreamEvent, error) {
	ch, err := r.inner.Stream(ctx, model, req)
	if err != nil {
		return nil, err
	}
	out := make(chan StreamEvent, 64)
	go r.pump(ctx, model, req, ch, out)
	return out, nil
}

// pump forwards events from the inner channel, retrying when the first event of
// an attempt is a transient error. Because every pre-stream failure emits its
// terminal "error" event before any "start"/delta, inspecting the first event
// is sufficient to decide whether a retry can be done without double-emitting.
func (r *retryProvider) pump(ctx context.Context, model Model, req Request, ch <-chan StreamEvent, out chan<- StreamEvent) {
	defer close(out)

	attempt := 1
	for {
		first, ok := <-ch
		if !ok {
			// Inner closed without emitting anything; nothing to forward.
			return
		}

		if r.shouldRetry(ctx, first, attempt) {
			attempt++
			// Announce the upcoming attempt for UIs.
			select {
			case out <- StreamEvent{Type: "retry", Attempt: attempt, MaxAttempts: r.policy.MaxAttempts}:
			case <-ctx.Done():
				emitAborted(out)
				return
			}
			// Backoff, aborting promptly if the run is cancelled.
			select {
			case <-time.After(r.backoff(attempt)):
			case <-ctx.Done():
				emitAborted(out)
				return
			}
			drain(ch)
			nch, err := r.inner.Stream(ctx, model, req)
			if err != nil {
				// A Go-error return is deterministic (not transient); surface it.
				out <- StreamEvent{Type: "error", Error: &types.Message{
					Role:         types.RoleAssistant,
					StopReason:   types.StopError,
					ErrorMessage: err.Error(),
				}}
				return
			}
			ch = nch
			continue
		}

		// No retry: forward this event and stream the remainder live.
		out <- first
		for ev := range ch {
			out <- ev
		}
		return
	}
}

// shouldRetry reports whether ev is a transient error that warrants another
// attempt: it must be a retryable "error" event, the run must not be aborting,
// and the attempt budget must not be exhausted.
func (r *retryProvider) shouldRetry(ctx context.Context, ev StreamEvent, attempt int) bool {
	if ev.Type != "error" || !ev.Retryable {
		return false
	}
	if attempt >= r.policy.MaxAttempts {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	if ev.Error != nil && ev.Error.StopReason == types.StopAborted {
		return false
	}
	return true
}

// backoff returns the delay before the given attempt number (>= 2). It grows
// exponentially from BaseDelay, is capped at MaxDelay, and carries up to ±25%
// jitter to avoid synchronized retries.
func (r *retryProvider) backoff(attempt int) time.Duration {
	d := r.policy.BaseDelay << (attempt - 2) // attempt 2 -> BaseDelay
	if d <= 0 || d > r.policy.MaxDelay {
		d = r.policy.MaxDelay
	}
	jitter := time.Duration(rand.Int63n(int64(d)/2+1)) - d/4
	d += jitter
	if d < 0 {
		d = 0
	}
	return d
}

func drain(ch <-chan StreamEvent) {
	for range ch {
	}
}

func emitAborted(out chan<- StreamEvent) {
	out <- StreamEvent{Type: "error", Error: &types.Message{
		Role:         types.RoleAssistant,
		StopReason:   types.StopAborted,
		ErrorMessage: "Request was aborted",
	}}
}
