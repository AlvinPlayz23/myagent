package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/types"
)

var errNothingToCompact = errors.New("there is not enough conversation history to compact")

// agentEventMsg wraps an AgentEvent for delivery into the bubbletea Update
// loop. generation prevents late events from a prior operation repainting a
// transcript after /clear or /new.
type agentEventMsg struct {
	ev         types.AgentEvent
	generation uint64
}

// agentDoneMsg signals that the current agent Run finished (or errored).
type agentDoneMsg struct {
	err        error
	generation uint64
}

type runnerEvent struct {
	ev         types.AgentEvent
	generation uint64
}

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
	events chan runnerEvent

	generation uint64

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
		events:  make(chan runnerEvent, 1024),
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
	r.generation++
	generation := r.generation
	return r.run(ctx, generation, func(loop *agent.Loop) error {
		_, err := loop.Run(ctx, []types.Message{prompt})
		return err
	})
}

// compact runs forced compaction without creating a user message.
func (r *runner) compact(ctx context.Context) tea.Cmd {
	r.generation++
	generation := r.generation
	return r.run(ctx, generation, func(loop *agent.Loop) error {
		compacted, err := loop.Compact(ctx)
		if err != nil {
			return err
		}
		if !compacted {
			return errNothingToCompact
		}
		return nil
	})
}

func (r *runner) run(ctx context.Context, generation uint64, action func(*agent.Loop) error) tea.Cmd {
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
			case r.events <- runnerEvent{ev: ev, generation: generation}:
				return nil
			case <-sctx.Done():
				return sctx.Err()
			}
		}
		loop := agent.New(r.cfg, r.history, sink)
		err := action(loop)
		// Persist the full conversation so subsequent prompts continue it.
		r.history = loop.Messages()
		return agentDoneMsg{err: err, generation: generation}
	}
}

func (r *runner) setModel(provider llm.Provider, model llm.Model) {
	r.cfg.Provider = provider
	r.cfg.Model = model
}

// discardEvents makes buffered events from earlier operations invisible.
func (r *runner) discardEvents() {
	r.generation++
}

func (r *runner) reset() {
	r.resume(nil)
}

func (r *runner) resume(history []types.Message) {
	r.discardEvents()
	r.history = append([]types.Message(nil), history...)
	r.queue.DrainAll()
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
		return agentEventMsg{ev: ev.ev, generation: ev.generation}
	}
}
