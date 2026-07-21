// Package core implements the transport-agnostic session-manager for
// myagent's server mode. It wraps agent.Loop instances with per-run
// lifecycle, JSONL persistence, and an event channel, without knowing
// anything about WebSockets or JSON-RPC — transports (ws today, possibly ACP
// later) call the same Manager/ServerSession API.
package core

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/llm"
	"github.com/myagent/myagent/internal/session"
	"github.com/myagent/myagent/internal/types"
)

// Sentinel errors mapped to JSON-RPC application codes by transports.
var (
	ErrBusy       = errors.New("session is busy with an active run")
	ErrNotRunning = errors.New("session has no active run")
	ErrNotFound   = errors.New("session not found")
	ErrNotOwner   = errors.New("session is owned by another connection")
	ErrClosed     = errors.New("session is closed")
)

// Event flows out of a ServerSession's Events() channel. Exactly one of
// AgentEvent (a streamed agent event) or Done (run completion, with Err set
// on failure) is populated.
type Event struct {
	SessionID  string
	AgentEvent *types.AgentEvent
	Done       bool
	Err        error
}

// eventBufferSize matches the TUI runner's generously-buffered event channel
// so streaming deltas rarely block the loop goroutine.
const eventBufferSize = 1024

// ServerSession wraps one persisted conversation and at most one active agent
// run. All exported methods are safe for concurrent use.
type ServerSession struct {
	id      string
	baseCtx context.Context // manager lifetime; cancels runs on shutdown

	mu        sync.Mutex
	sess      *session.Session
	history   []types.Message
	queue     *agent.Queue
	cfg       agent.Config
	modelID   string // "provider/model-id" for client display
	cwd       string
	running   bool
	cancelRun context.CancelFunc
	owner     string // connection id; "" = unowned
	closed    bool

	events  chan Event
	closeCh chan struct{}
}

// newServerSession builds a session wrapper over an open session file.
func newServerSession(baseCtx context.Context, sess *session.Session, cfg agent.Config, modelID, cwd string) *ServerSession {
	queue := agent.NewQueue()
	cfg.Queue = queue
	return &ServerSession{
		id:      sess.ID(),
		baseCtx: baseCtx,
		sess:    sess,
		history: append([]types.Message(nil), sess.Messages()...),
		queue:   queue,
		cfg:     cfg,
		modelID: modelID,
		cwd:     cwd,
		events:  make(chan Event, eventBufferSize),
		closeCh: make(chan struct{}),
	}
}

// ID returns the session id.
func (s *ServerSession) ID() string { return s.id }

// Cwd returns the session's working directory.
func (s *ServerSession) Cwd() string { return s.cwd }

// ModelID returns the "provider/model-id" ref currently configured.
func (s *ServerSession) ModelID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.modelID
}

// Events returns the session's event stream. The channel is never closed;
// consumers should also select on a shutdown signal of their own.
func (s *ServerSession) Events() <-chan Event { return s.events }

// Messages returns a copy of the persisted conversation (kept in sync with
// the loop's history via per-message appends).
func (s *ServerSession) Messages() []types.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]types.Message(nil), s.sess.Messages()...)
}

// Running reports whether an agent run is in flight.
func (s *ServerSession) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Prompt starts an agent run for the given user text. It returns immediately;
// progress streams through Events() and completion is signaled with a Done
// event. Returns ErrBusy if a run is already active.
func (s *ServerSession) Prompt(text string) error {
	return s.startRun(func(loop *agent.Loop, runCtx context.Context) error {
		_, err := loop.Run(runCtx, []types.Message{userMessage(text)})
		return err
	})
}

// Compact starts a forced compaction run. Like Prompt it returns immediately;
// compaction_start/compaction_end events and a final Done event stream
// through Events(). Returns ErrBusy if a run is active.
func (s *ServerSession) Compact() error {
	return s.startRun(func(loop *agent.Loop, runCtx context.Context) error {
		compacted, err := loop.Compact(runCtx)
		if err != nil {
			return err
		}
		if !compacted {
			return errors.New("there is not enough conversation history to compact")
		}
		return nil
	})
}

// startRun spawns the run goroutine shared by Prompt and Compact, mirroring
// tui/runner.run: build a fresh Loop over the current history, stream events
// through the sink, and persist messages/compactions as they complete.
func (s *ServerSession) startRun(action func(*agent.Loop, context.Context) error) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	if s.running {
		s.mu.Unlock()
		return ErrBusy
	}
	runCtx, cancel := context.WithCancel(s.baseCtx)
	s.running = true
	s.cancelRun = cancel
	history := s.history
	cfg := s.cfg
	s.mu.Unlock()

	go func() {
		defer cancel()
		sink := func(sctx context.Context, ev types.AgentEvent) error {
			// Persist messages/compactions before forwarding, so the session
			// file stays in sync with the loop's in-memory history (same
			// ordering as printmode and the TUI runner).
			if err := s.persist(ev); err != nil {
				return err
			}
			evCopy := ev
			select {
			case s.events <- Event{SessionID: s.id, AgentEvent: &evCopy}:
				return nil
			case <-sctx.Done():
				return sctx.Err()
			}
		}
		loop := agent.New(cfg, history, sink)
		err := action(loop, runCtx)

		s.mu.Lock()
		s.history = loop.Messages()
		s.running = false
		s.cancelRun = nil
		s.mu.Unlock()

		// Done event: always delivered (buffered channel), but never wedge the
		// goroutine if the consumer is gone.
		select {
		case s.events <- Event{SessionID: s.id, Done: true, Err: err}:
		case <-s.closeCh:
		case <-s.baseCtx.Done():
		}
	}()
	return nil
}

// persist applies session-file writes for a single agent event.
func (s *ServerSession) persist(ev types.AgentEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Type {
	case types.EventMessageEnd:
		if ev.Message != nil {
			return s.sess.AppendMessage(*ev.Message)
		}
	case types.EventCompactionEnd:
		if ev.Compaction != nil && ev.Message != nil {
			return s.sess.ApplyCompaction(*ev.Compaction, *ev.Message)
		}
	}
	return nil
}

// Steer enqueues a mid-run steering message. Returns ErrNotRunning when no
// run is active so clients get explicit feedback instead of a silently
// deferred message.
func (s *ServerSession) Steer(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return ErrNotRunning
	}
	s.queue.EnqueueSteering(userMessage(text))
	return nil
}

// FollowUp enqueues a message processed after the current run would stop.
// Returns ErrNotRunning when idle (clients should use Prompt instead).
func (s *ServerSession) FollowUp(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return ErrNotRunning
	}
	s.queue.EnqueueFollowUp(userMessage(text))
	return nil
}

// Abort cancels the active run's context. It is a no-op when idle. Queued
// steering/follow-up messages are discarded, mirroring the TUI's abort.
func (s *ServerSession) Abort() {
	s.mu.Lock()
	cancel := s.cancelRun
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.queue.DrainAll()
}

// SetModel switches the provider/model used for subsequent runs. Fails with
// ErrBusy while a run is active.
func (s *ServerSession) SetModel(provider llm.Provider, model llm.Model) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return ErrBusy
	}
	s.cfg.Provider = provider
	s.cfg.Model = model
	s.modelID = model.Provider + "/" + model.ID
	return nil
}

// claim binds the session to connID. Fails with ErrNotOwner when a different
// live connection already owns it; unowned sessions are claimed on access.
func (s *ServerSession) claim(connID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if s.owner != "" && s.owner != connID {
		return ErrNotOwner
	}
	s.owner = connID
	return nil
}

// release clears ownership if held by connID and aborts any active run — a
// disconnected client is no longer watching tool side effects, so stopping is
// the safe default. The session stays resumable (JSONL is durable).
func (s *ServerSession) release(connID string) {
	s.mu.Lock()
	if s.owner != connID {
		s.mu.Unlock()
		return
	}
	s.owner = ""
	cancel := s.cancelRun
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.queue.DrainAll()
}

// close aborts any run and closes the session file. Idempotent.
func (s *ServerSession) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	cancel := s.cancelRun
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	close(s.closeCh)

	// Wait briefly for the run goroutine to finish so the file isn't closed
	// under an in-flight AppendMessage. The run observes ctx cancellation at
	// the next emit; persist() holds s.mu so appends never race the Close.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		running := s.running
		s.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	s.mu.Lock()
	_ = s.sess.Close()
	s.mu.Unlock()
}

// userMessage wraps text as a user Message.
func userMessage(text string) types.Message {
	return types.Message{
		Role:      types.RoleUser,
		Content:   []types.ContentBlock{types.TextBlock(text)},
		Timestamp: time.Now().UnixMilli(),
	}
}
