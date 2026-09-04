package tui

// abortActiveRun is the single TUI cancellation path used by Esc and Ctrl+C.
// Cancellation is deliberately cooperative: working remains true until the
// runner goroutine has actually stopped, preventing a second run from
// overlapping a tool that is still unwinding.
func (m *model) abortActiveRun() bool {
	if !m.working || m.cancel == nil {
		return false
	}
	if !m.abortRequested {
		m.abortRequested = true
		// Queued steering/follow-up text belongs to the run being aborted. Do not
		// allow it to leak into the next run.
		if m.queue != nil {
			m.queue.DrainAll()
		}
		m.queuedSteering = nil
		m.queuedFollowUps = nil
		m.activePrompt = nil
		m.cancel()
	}
	m.statusMsg = "Aborting…"
	m.updateLayout()
	return true
}
