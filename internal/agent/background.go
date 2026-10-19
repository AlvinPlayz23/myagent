package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// DefaultMaxBackgroundTasks bounds outstanding (running + undelivered) tasks
// per run when Config.MaxBackgroundTasks is unset.
const DefaultMaxBackgroundTasks = 16

// BackgroundTool is an optional extension of tools.Tool. When the loop sees a
// tool implementing it, it calls PrepareBackground instead of Execute: the
// tool validates and snapshots everything it needs from the parent config,
// returns immediately with an acknowledgement, and the loop runs the task
// concurrently, injecting its completion message at a safe boundary.
type BackgroundTool interface {
	tools.Tool
	PrepareBackground(ctx context.Context, callID string, args map[string]any, parent Config) (*BackgroundTask, error)
}

// BackgroundTask is a prepared unit of background work.
type BackgroundTask struct {
	ID  string
	Ack *types.ToolResult
	// Run executes the work and returns the completion message to inject.
	Run func(ctx context.Context) (types.Message, error)
	// OnError builds a completion message when Run errors or panics.
	OnError func(err error) types.Message
	// Suppress, when set, reports messages that carry nothing worth keeping in
	// the conversation, so the loop delivers them to nobody. It exists for work
	// the caller deliberately cut short: a task cancelled by an abort has no
	// result to report, and persisting a notice for each one would fill the
	// permanent transcript with a record of an action the user took on purpose
	// instead of with work. nil means deliver every report.
	//
	// It is consulted at delivery time on every path, not only during teardown,
	// so a suppressed report can never leak in through the normal turn loop.
	Suppress func(msg types.Message) bool
}

// completion pairs a published report with the task that produced it, so the
// task's Suppress policy can be applied when the report is delivered.
type completion struct {
	task *BackgroundTask
	msg  types.Message
}

// backgroundScheduler is owned by a single Run. Workers only run task code and
// publish completions; only the parent loop reads and delivers them.
type backgroundScheduler struct {
	ctx    context.Context
	cancel context.CancelFunc
	limit  int

	mu      sync.Mutex
	closed  bool
	running map[string]struct{}
	done    []completion // FIFO by publication order
	wg      sync.WaitGroup
	wakeCh  chan struct{}
}

func newBackgroundScheduler(parent context.Context, limit int) *backgroundScheduler {
	if limit <= 0 {
		limit = DefaultMaxBackgroundTasks
	}
	ctx, cancel := context.WithCancel(parent)
	return &backgroundScheduler{
		ctx:     ctx,
		cancel:  cancel,
		limit:   limit,
		running: map[string]struct{}{},
		wakeCh:  make(chan struct{}, 1),
	}
}

// start admits and launches a task, returning its acknowledgement.
func (s *backgroundScheduler) start(task *BackgroundTask) (*types.ToolResult, error) {
	if task == nil || task.ID == "" || task.Ack == nil || task.Run == nil || task.OnError == nil {
		return nil, errors.New("invalid background task")
	}
	s.mu.Lock()
	switch {
	case s.closed || s.ctx.Err() != nil:
		s.mu.Unlock()
		return nil, errors.New("run is no longer accepting background tasks")
	case len(s.running)+len(s.done) >= s.limit:
		s.mu.Unlock()
		return nil, fmt.Errorf("too many outstanding background tasks (limit %d); wait for completions", s.limit)
	}
	if _, dup := s.running[task.ID]; dup {
		s.mu.Unlock()
		return nil, fmt.Errorf("duplicate background task id %q", task.ID)
	}
	s.running[task.ID] = struct{}{}
	s.wg.Add(1)
	s.mu.Unlock()

	go s.work(task)
	return task.Ack, nil
}

func (s *backgroundScheduler) work(task *BackgroundTask) {
	defer s.wg.Done()
	msg := s.runTask(task)
	s.mu.Lock()
	delete(s.running, task.ID)
	s.done = append(s.done, completion{task: task, msg: msg})
	s.mu.Unlock()
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

func (s *backgroundScheduler) runTask(task *BackgroundTask) (msg types.Message) {
	defer func() {
		if r := recover(); r != nil {
			msg = safeOnError(task, fmt.Errorf("panic: %v", r))
		}
	}()
	if err := s.ctx.Err(); err != nil {
		return safeOnError(task, err)
	}
	msg, err := task.Run(s.ctx)
	if err != nil {
		return safeOnError(task, err)
	}
	return msg
}

func safeOnError(task *BackgroundTask, err error) (msg types.Message) {
	defer func() {
		if r := recover(); r != nil {
			msg = types.Message{
				Role:    types.RoleUser,
				Source:  types.SourceSubagentCompletion,
				Content: []types.ContentBlock{types.TextBlock(fmt.Sprintf("Background task %s failed: %v", task.ID, err))},
			}
		}
	}()
	return task.OnError(err)
}

// ready returns a snapshot of undelivered completions in FIFO order.
func (s *backgroundScheduler) ready() []completion {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]completion(nil), s.done...)
}

// delivered drops the oldest completion after it was committed to history.
func (s *backgroundScheduler) delivered() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.done) > 0 {
		s.done = s.done[1:]
	}
}

// state atomically reports running and undelivered counts.
func (s *backgroundScheduler) state() (running, ready int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.running), len(s.done)
}

func (s *backgroundScheduler) wake() <-chan struct{} { return s.wakeCh }

// close cancels all workers and joins them. Reports published before or during
// the join stay in done for the owner to drain — Loop.drainCompletions closes
// before it reads, so this is what lets a report survive its own abort, and it
// is why the launch acknowledgement's promise to report every task holds. It is
// safe to call more than once: cancel is idempotent and the counter is zero by
// the time the first call returns.
//
// closed only gates start: once close has run no new task may be admitted.
func (s *backgroundScheduler) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.cancel()
	s.wg.Wait()
}
