package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/types"
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

// agentTitleMsg carries the isolated title-generator result to the UI. It is
// deliberately not an AgentEvent, so it cannot appear in the transcript.
type agentTitleMsg struct {
	title      string
	generation uint64
}

type runnerEvent struct {
	ev         *types.AgentEvent
	done       *agentDoneMsg
	title      *agentTitleMsg
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

	// generateTitle is invoked once, before the first main-agent request of a
	// fresh session. It must persist the title itself and returns it for UI use.
	generateTitle func(context.Context, string) (string, error)

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
// Completion travels through the same FIFO channel as AgentEvents, ensuring the
// UI cannot become idle before the final transcript event has rendered.
func (r *runner) start(ctx context.Context, prompt types.Message) tea.Cmd {
	r.generation++
	generation := r.generation
	return r.run(ctx, generation, func(loop *agent.Loop) error {
		if len(r.history) == 0 && r.generateTitle != nil {
			if title, err := r.generateTitle(ctx, textOf(prompt)); err == nil && title != "" {
				r.events <- runnerEvent{title: &agentTitleMsg{title: title, generation: generation}, generation: generation}
			}
		}
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
			case r.events <- runnerEvent{ev: &ev, generation: generation}:
				return nil
			case <-sctx.Done():
				return sctx.Err()
			}
		}
		loop := agent.New(r.cfg, r.history, sink)
		err := action(loop)
		// Persist the full conversation so subsequent prompts continue it.
		r.history = loop.Messages()
		done := agentDoneMsg{err: err, generation: generation}
		r.events <- runnerEvent{done: &done, generation: generation}
		return nil
	}
}

func (r *runner) setModel(provider llm.Provider, model llm.Model) {
	r.cfg.Provider = provider
	r.cfg.Model = model
}

func (r *runner) setEffort(effort llm.Effort) {
	r.cfg.Effort = effort
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
		if ev.title != nil {
			return *ev.title
		}
		if ev.done != nil {
			return *ev.done
		}
		return agentEventMsg{ev: *ev.ev, generation: ev.generation}
	}
}
