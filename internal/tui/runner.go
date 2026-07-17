package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/types"
)

// agentEventMsg wraps an AgentEvent for delivery into the bubbletea Update
// loop. The UI reads one event per waitForEvent command and re-arms.
type agentEventMsg struct{ ev types.AgentEvent }

// agentDoneMsg signals that the current agent Run finished (or errored).
type agentDoneMsg struct{ err error }

// eventChannelClosedMsg is delivered if the event channel is ever closed. In
// practice r.events is a single long-lived pump that is never closed, so this
// is defensive/dead code kept only so waitForEvent has a well-defined return
// for a closed channel.
type eventChannelClosedMsg struct{}

// runner owns the agent loop and its persistent conversation. A single runner
// backs the whole session; each user prompt starts a new Run on the same
// underlying message history, mirroring pi where the interactive loop keeps
// one conversation alive across turns.
type runner struct {
	cfg     agent.Config
	queue   *msgQueue
	history []types.Message

	// events carries AgentEvents from the loop goroutine to the UI. It is
	// buffered generously so streaming deltas rarely block the loop; the UI
	// drains it continuously via waitForEvent.
	events chan types.AgentEvent

	// onEvent, if set, is called for every AgentEvent (on the loop goroutine,
	// before the event is forwarded to the UI channel). Used to persist
	// messages and compactions to the session file as they complete, so the
	// session stays in sync with the loop's in-memory history.
	onEvent func(types.AgentEvent) error
}

// newRunner builds a runner over the given agent config and initial history.
func newRunner(cfg agent.Config, queue *msgQueue, history []types.Message) *runner {
	cfg.Queue = queue
	return &runner{
		cfg:     cfg,
		queue:   queue,
		history: history,
		events:  make(chan types.AgentEvent, 1024),
	}
}

// start launches an agent Run for the given prompt in a background goroutine.
// The returned tea.Cmd yields agentDoneMsg when the run completes. Events flow
// separately through r.events (consumed by waitForEvent). Because start and
// waitForEvent are distinct commands running on separate goroutines,
// agentDoneMsg may be processed by Update slightly before the last buffered
// events are drained; those trailing events still render correctly. The UI
// gates starting a new run on working==false, so runs never overlap and their
// events never interleave on the shared channel.
func (r *runner) start(ctx context.Context, prompt types.Message) tea.Cmd {
	return func() tea.Msg {
		sink := func(sctx context.Context, ev types.AgentEvent) error {
			// Persist messages/compactions to the session before forwarding to
			// the UI, so the session stays in sync with the loop's history.
			if r.onEvent != nil {
				if err := r.onEvent(ev); err != nil {
					return err
				}
			}
			select {
			case r.events <- ev:
				return nil
			case <-sctx.Done():
				return sctx.Err()
			}
		}
		loop := agent.New(r.cfg, r.history, sink)
		_, err := loop.Run(ctx, []types.Message{prompt})
		// Persist the full conversation so subsequent prompts continue it.
		r.history = loop.Messages()
		return agentDoneMsg{err: err}
	}
}

// waitForEvent returns a command that blocks until the next AgentEvent is
// available (or the channel drains during idle). It re-arms itself from Update
// after each delivered event, forming a continuous pump.
func (r *runner) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-r.events
		if !ok {
			return eventChannelClosedMsg{}
		}
		return agentEventMsg{ev: ev}
	}
}
