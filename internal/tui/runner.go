package tui

import (
	"context"
	"errors"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

var errNothingToCompact = errors.New("there is not enough conversation history to compact")

// runner owns the agent loop and its persistent conversation. A single runner
// backs the whole session; each user prompt starts a new Run on the same
// underlying message history. Events are delivered to the app through
// unbuffered callback wiring rather than a UI framework.
type runner struct {
	cfg     agent.Config
	app     *app
	queue   *msgQueue
	history []types.Message

	generation uint64

	// synchronous executes runs inline instead of spawning a goroutine. It is
	// set by tests so runs are deterministic; the UI always leaves it false.
	synchronous bool

	// generateTitle is invoked once, before the first main-agent request of a
	// fresh session. It must persist the title itself.
	generateTitle func(context.Context, string) (string, error)

	// onEvent is called for every AgentEvent (on the loop goroutine, before
	// the event is forwarded). Used to persist messages and compactions.
	onEvent func(types.AgentEvent) error
}

// newRunner builds a runner over the given agent config and initial history.
func newRunner(cfg agent.Config, queue *msgQueue, history []types.Message) *runner {
	cfg.Queue = queue
	return &runner{
		cfg:     cfg,
		queue:   queue,
		history: append([]types.Message(nil), history...),
	}
}

// bindApp connects the runner to the app's event channel.
func (r *runner) bindApp(a *app) { r.app = a }

// start launches an agent Run for the given prompt in a background goroutine.
// Completion travels through the same FIFO channel as AgentEvents, so the UI
// cannot become idle before the final event has rendered.
func (r *runner) start(ctx context.Context, prompt types.Message) {
	r.generation++
	generation := r.generation
	action := func(loop *agent.Loop) error {
		if len(r.history) == 0 && r.generateTitle != nil {
			if title, err := r.generateTitle(ctx, textOf(prompt)); err == nil && title != "" {
				r.sendTitle(title, generation)
			}
		}
		_, err := loop.Run(ctx, []types.Message{prompt})
		return err
	}
	if r.synchronous {
		r.run(ctx, generation, action)
		return
	}
	go r.run(ctx, generation, action)
}

// compact runs forced compaction without creating a user message.
func (r *runner) compact(ctx context.Context) {
	r.generation++
	generation := r.generation
	action := func(loop *agent.Loop) error {
		compacted, err := loop.Compact(ctx)
		if err != nil {
			return err
		}
		if !compacted {
			return errNothingToCompact
		}
		return nil
	}
	if r.synchronous {
		r.run(ctx, generation, action)
		return
	}
	go r.run(ctx, generation, action)
}

func (r *runner) run(ctx context.Context, generation uint64, action func(*agent.Loop) error) {
	sink := func(sctx context.Context, ev types.AgentEvent) error {
		if r.onEvent != nil {
			if err := r.onEvent(ev); err != nil {
				return err
			}
		}
		select {
		case r.app.agentCh <- agentEventEnvelope{ev: ev, generation: generation}:
			return nil
		case <-sctx.Done():
			return sctx.Err()
		}
	}
	loop := agent.New(r.cfg, r.history, sink)
	err := action(loop)
	r.history = loop.Messages()
	if r.app != nil {
		r.app.loopCh <- loopEvent{done: &agentDoneEvent{err: err, generation: generation}}
	}
}

// sendTitle forwards a generated session title to the UI.
func (r *runner) sendTitle(title string, generation uint64) {
	if r.app != nil {
		r.app.loopCh <- loopEvent{title: &agentTitleEvent{title: title, generation: generation}}
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
