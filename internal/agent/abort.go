package agent

import "context"

// runWasAborted centralizes cancellation checks at agent lifecycle boundaries.
// The context remains the sole source of truth, so this adds no conversation
// state and cannot affect normal history or compaction behavior.
func runWasAborted(ctx context.Context) bool { return ctx.Err() != nil }
