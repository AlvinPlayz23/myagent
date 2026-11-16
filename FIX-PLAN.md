# FIX-PLAN.md — Subagent-side audit & remediation plan

Consolidated record of every finding from the two deep code-review rounds
(2026-11-01) of the subagent/background-task pipeline, plus the fix required
for each. **No code has been changed except F-01, which was found and fixed
during the first review round.** A later read of the code corrected F-06,
F-11, F-13, F-16, and F-17 in place. Do not revive the retracted F-13 `s.mu`
snippet or the retracted F-17 pre-`runLoop` cancel snapshot; both are wrong.

## Audit scope

| Area | Files audited |
|---|---|
| Subagent tool & report | `internal/subagent/subagent.go`, `internal/subagent/completion.go` |
| Background scheduler & delivery | `internal/agent/background.go`, `internal/agent/loop.go` |
| Server session lifecycle | `internal/server/core/session.go`, `internal/server/core/manager.go` |
| WebSocket transport | `internal/server/ws/conn.go` |
| TUI runner & command gates | `internal/tui/runner.go`, `internal/tui/model.go`, `internal/tui/tui.go` |
| Provider layer (shared by parent + children) | `internal/llm/openai.go`, `internal/llm/retry.go`, `internal/llm/zen.go` |
| Tool/shell stack | `internal/tools/tool.go`, `internal/tools/plugin.go`, `internal/tools/*.go` |
| Repo hygiene | project root |

## Status legend

- **FIXED** — fixed during review round 1; regression test added.
- **OPEN-M** — medium: real correctness/robustness bug, fix recommended soon.
- **OPEN-L** — low: narrow-race, embedder-only, or cosmetic; fix when convenient.
- **HYGIENE** — non-code cleanup.
- **RULED OUT** — suspected during review, verified not to be a bug; recorded
  so the same suspicion is not re-litigated.

Verification status at audit time: `go test ./internal/agent ./internal/subagent`
green, `go vet` clean on those packages. `-race` could not run (cgo unconfigured
on this Windows toolchain) — re-run under race when that is available.

---

## Round 1 findings

### F-01 — Scheduler admits duplicate task id once the original is completed-but-undelivered — **FIXED**

- **Where:** `internal/agent/background.go` → `backgroundScheduler.start()`
- **Problem:** The duplicate-id guard consulted only the `running` map. A task
  whose report already sat in `done` (finished, not yet injected into the
  conversation) was invisible, so a second task re-using its id was admitted —
  violating the "duplicate background task id" invariant and breaking anything
  that dedups on task id for delivery.
- **Fix applied:** `start()` now also scans `done` and rejects a re-used id
  (same "duplicate background task id" error).
- **Regression test:** `internal/agent/background_dupid_test.go`
  (`TestSchedulerRejectsDuplicateIdOfUndeliveredCompletion` — failed before the
  fix, passes after). Nothing further to do.

### F-02 — Child work completed before a parent abort is erased from the transcript — **OPEN-M**

- **Where:** `internal/subagent/subagent.go` → `runPrepared()` status switch +
  `Suppress` (cancelled reports are dropped by `loop.injectCompletions`).
- **Problem:** When the *parent* run is aborted after a child has already done
  its work (often file edits on disk), the report is stamped `cancelled` and
  suppressed. The edits persist on disk but neither the model nor the
  transcript records them. The existing test
  (`TestAbortedSubagentLeavesNoCompletion`) covers the child-level abort case;
  parent-level abort with *finished* child work is a different, unhandled case.
- **Fix needed:**
  1. In `runPrepared`, before overwriting status on parent cancel
     (`case ctx.Err() != nil`), check whether the child run had *finished*
     (`runErr == nil` and last assistant `StopReason` is a normal stop). If so,
     tag the report `completed` with a `note` field such as
     `"parent run aborted after child finished"` instead of `cancelled`, so the
     completion still carries the results. `runErr` and the last assistant
     message are already in scope after `runLoop`; do not sample `ctx.Err()`
     before `runLoop` and treat that sample as the cancel signal (see F-17).
  2. Narrow the `Suppress` predicate so only *genuinely* aborted children
     (`rep.Status == StatusCancelled && rep.ToolCallsN == 0 && rep.Turns <= 1`)
     are silenced, or keep suppression but emit a one-line
     "child completed N tool calls before the abort" notice instead of nothing.
  3. Extend `TestAbortedSubagentLeavesNoCompletion` with a variant where the
     child completed its final turn before the parent cancel fires.

### F-03 — Tool description overpromises what the model sees — **OPEN-L**

- **Where:** `internal/subagent/subagent.go` → `Tool.Description()` (`subagent.go:105-107`).
- **Problem:** The description promises "its full report (prompt, trajectory,
  final answer) arrives automatically as a system-generated message", but the
  model-visible text (`completionText()`) contains only stats + final result;
  prompt and trajectory travel in `Details` for the UI only. A model steered by
  this description may expect trajectory content in-context and plan calls
  accordingly.
- **Fix needed:** Edit the `Description()` string to say a *summary and the
  final answer* arrive (one line change), and keep "full report in the UI
  details" out of the model contract. Optionally also assert the wording in
  `TestToolDescriptionWarnsAboutConcurrentEdits`.

### F-04 — `wrap()` creates the subagent base accessor before `ss` exists — **OPEN-L**

- **Where:** `internal/server/core/manager.go` → `wrap()`
  (`baseRegistry(cwd, sessID, func() agent.Config { return ss.Config() })` while
  `ss` is nil; assigned later via `newServerSession`).
- **Problem:** Safe today purely because the accessor cannot be invoked before
  `wrap` returns (tools only run inside a startRun). Any future edit that
  invokes a tool during registry/prompt setup (e.g. a plugin `Apply` path that
  introspects tools) would nil-dereference. The TUI $(internal/tui/tui.go
  `$st.SetBase(...)`) uses an explicit post-construction `SetBase` instead —
  the server path should match that shape.
- **Fix needed:** Add a defensive accessor:
  `func() agent.Config { if ss == nil { return agent.Config{} } ; return ss.Config() }`
  plus a comment on `newBase` stating when the accessor becomes live —
  or restructure so the registry is attached to the session after
  construction, mirroring the TUI's `SetBase` ordering.

### F-05 — In-flight background children are not recoverable across a crash/restart — **OPEN-L (document away)**

- **Where:** `internal/subagent/subagent.go` ack text + `internal/server/core`
  persistence model.
- **Problem:** A session crash mid-run leaves the persisted ack
  ("its full report will arrive automatically…") that will never be honored —
  the child's work is lost, and the dangling-pair repair in `abort.go` cannot
  fix it (the ack tool-result is a complete pair).
- **Fix needed:** No code fix required if documented. Add one paragraph to
  `README.md` / `PLUGINS.md`: "background subagent tasks are in-memory per run;
  a process restart loses them, and any ack in the transcript is a dead
  promise." If persistence is later desired, the right shape is a session-file
  marker (`TaskReport` with status `pending`) written at PrepareBackground and
  marked `lost` on resume — out of scope for now.

### F-06 — Repo-root artifacts — **HYGIENE**

- **Where:** project root: `nul` (0 bytes), `3131333.txt`, `myagent.exe`.
- **Problem:** All three files are still on disk. `.gitignore` already lists
  `*.exe`, `nul`, and `3131333.txt`, which is why `git status` shows none of
  them. Adding those patterns again would be a no-op.
- **Fix needed:** Delete the three files. `nul` is a Windows reserved name, so
  a normal `Remove-Item` will not see it; delete it via the `\\?\` prefix or
  from WSL. Do not edit `.gitignore` for this item.

### F-07 — `newTaskID` fallback is collision-prone — **OPEN-L**

- **Where:** `internal/subagent/subagent.go` → `newTaskID()` UnixNano fallback
  when `crypto/rand` fails.
- **Problem:** Two tasks started in the same nanosecond on a rand-unavailable
  host collide. The scheduler now (since F-01) rejects such duplicates, but the
  failure would surface to the model as a confusing error.
- **Fix needed:** Replace the fallback with `sub_<UnixNano>_<atomic counter>`
  (or reuse `crypto/sha1` of time+pid). Tiny change in `newTaskID()`.

### F-08 — Trajectory byte accounting ignores small header fields — **OPEN-L**

- **Where:** `internal/subagent/completion.go` → `trajectory.add()`
- **Problem:** `size` counts `Text`, `Note` and tool-call `Arguments`/`Name`,
  but not `Role`, `ToolCallID`, `IsError` serialization overhead, so a run near
  the 200 KB cap can slightly exceed it in the JSON.
- **Fix needed:** Not worth code churn. Either (a) document the margin in the
  `maxTrajectoryBytes` comment, or (b) add `len(m.Role)+len(m.ToolCallID)+8`
  (bool + separators) to `size`. Option (b) is a 2-line change in
  `completion.go` plus updating the trajectory-bound tests.

### F-09 — `resolveModel` compound error joins two failures via `fmt.Errorf("%w; ... %w")` — **OPEN-L**

- **Where:** `internal/subagent/subagent.go` → `resolveModel()`.
- **Problem:** Works, but the rendered error is harder to read than the
  standard multi-error rendering of `errors.Join`.
- **Fix needed:** Replace with
  `errors.Join(fmt.Errorf("resolve %q as provider/model: %w", ref, err),
  fmt.Errorf("resolve %q as model id: %w", ref, err2))` — one-line change; no
  consumer parses that string.

---

## Round 2 findings

### F-10 — Provider stream goroutine leaks: unbounded sends that never observe cancellation — **OPEN-M**

- **Where:** `internal/llm/openai.go` → `run()` / `emitError()` / the SSE
  forwarding loop (every `out <- StreamEvent{...}` is a plain send on a
  64-buffered channel), and `internal/llm/retry.go` → `pump()` (same pattern).
- **Problem:** `loop.streamAssistant` (`internal/agent/loop.go`) can abandon a
  stream mid-flight (`return types.Message{}, err` on any emit error, e.g. a
  session-file write failure). Once 64 events buffer, the provider goroutine
  blocks on the next `out <-` **and never unblocks, even after ctx
  cancellation**, because the send does not poll ctx — `defer close(out)` never
  runs and the HTTP SSE reader wedges with it. Parent + all sibling children
  share the same provider instance, so one wedged stream is per-provider, not
  per-run. Robustness bug; no data corruption.
- **Fix needed:**
  1. Make every send in `openai.run()`/`emitError`/accumulator and
     `retry.pump` ctx-aware:
     ```go
     select {
     case out <- ev:
     case <-ctx.Done():
         emitAborted(out) // terminal event so the consumer sees an end
         return
     }
     ```
     (`emitAborted` already exists in `retry.go` — reuse it.)
  2. Ensure `openai.run()` reaches `defer close(out)` on all paths once the
     sends are abortable.
  3. Regression test: a provider that emits > 64 events with a consumer that
     stops after N; assert the goroutine exits when ctx is cancelled
     (e.g. via a `done` channel closed after `close(out)` is observed, or
     runtime goroutine count comparison).
- **Note:** explicitly NOT a fix for the loop-side abandon; fixing the sends is
  sufficient to bound the leak.

### F-11 — Server close deadline (2 s) is shorter than the drain budget (5 s) — **OPEN-M**

- **Where:** `internal/server/core/session.go` → `close()` wait loop
  (`deadline := time.Now().Add(2 * time.Second)`) vs
  `drainTimeout = 5 * time.Second` in `internal/agent/loop.go`.
- **Problem:** A run whose deferred `drainCompletions` finishes between 2 s and
  5 s after close writes to a *closed* session file. Persist errors during
  drain are deliberately swallowed (`_ = l.injectCompletions(...)`), so
  completed-but-undelivered subagent reports are silently lost on `/close`
  and on `Shutdown` (which calls `close()`). `release` is not part of this
  window: it cancels the run and `DrainAll`s the queue, but it never closes
  the session file.
- **Fix needed:**
  1. Introduce one shared constant (e.g. `backgroundDrainBudget`), either in
     `agent` (exported) or `core`, and have `drainTimeout` in `loop.go` and the
     `close()` wait deadline in `session.go` both derive from it, with the
     session wait = drain budget + small margin (e.g. +1 s).
  2. While at it: when `drainCtx` expires or the inject errors, emit a
     one-line `types.EventError`-style notice (or log at minimum) instead of
     total silence — currently even a failed drain is invisible to operators.
  3. Test: set `drainTimeout` short via a seam (test override if needed) and a
     session `close()` that runs concurrently with a slow sink; assert the
     report ends up either persisted or explicitly logged, never silently
     dropped.

### F-12 — WS event pump leaks per closed session — **OPEN-M**

- **Where:** `internal/server/ws/conn.go` → `startEventPump()`; only exit is
  `c.ctx.Done()`.
- **Problem:** `ServerSession.Events()` is never closed by design, so after a
  client closes a session mid-connection a goroutine stays parked on
  `ss.Events()` forever (and `ServerSession` stays reachable through it) — one
  leak per `/close` for the life of the connection.
- **Fix needed:**
  1. Give `ServerSession` a shutdown signal consumers can select on —
     `closeCh` already exists; expose `func (s *ServerSession) Done() <-chan struct{}`.
  2. In the pump's select, add `case <-ss.Done(): return` (and remove the
     pump from `c.pumps` first so a later resume can restart it — the
     existing deferred cleanup already handles that).
  3. Alternative: have `core.Manager.Close()`/`Shutdown()` notify the conn —
     but the `Done()` channel shape is simpler because the pump already has
     the `ss` in scope.
  4. Test: create + close a session on one connection, assert the pump
     goroutine count returns to baseline (use `runtime.NumGoroutine()` with a
     settle window, or a test-only channel closed from the pump on exit).

### F-13 — Steer/FollowUp accepted after the loop's last poll is delivered to the next run — **OPEN-M**

- **Where:** `internal/server/core/session.go` → `SteerContent()`,
  `FollowUpContent()`, `startRun()`; `internal/agent/loop.go` → `runLoop()`
  idle exit; `internal/agent/queue.go`.
- **Retracted diagnosis:** The `!s.running` check and the enqueue are already
  one critical section. `SteerContent` and `FollowUpContent` hold `s.mu`
  across both, and `startRun` clears `running` under the same mutex. The old
  "hold `s.mu` around the check and enqueue" snippet is the current code and
  fixes nothing. Do not implement it.
- **Actual problem:** The session queue outlives the run (`newServerSession`
  installs one `*agent.Queue` on `cfg.Queue` and every `startRun` reuses it).
  `runLoop` stops polling after the idle loop's last empty `Steering()` /
  `FollowUp()` check, then drains completions and returns. `s.running` stays
  true until `action` returns, so a steer or follow-up accepted in that window
  is not seen by the ending run. Nothing on the normal exit path calls
  `DrainAll` (only `Abort` and `release` do). The next run takes a leftover
  steer at `pending := l.steering()` and injects it before the assistant
  responds; a leftover follow-up waits until that later run is about to stop.
  The client was told the message was accepted for the run that is ending.
  The same leftover survives `Abort` / `release` when the enqueue lands after
  `DrainAll` and before `running` is cleared, because `DrainAll` is not paired
  with a seal.
- **Fix needed:** Make intake atomic with the loop's decision to stop polling,
  on the queue mutex — not on `s.mu`.
  1. `EnqueueSteering` / `EnqueueFollowUp` return an error once intake is
     sealed. `SteerContent` / `FollowUpContent` map that error to
     `ErrNotRunning` (they already return `ErrNotRunning` when `!s.running`).
  2. The loop's exit check is one `q.mu` section: if steering or follow-up is
     nonempty, take it and keep running; if both are empty, seal and exit. An
     enqueue must not be able to land between "empty" and "sealed".
  3. Seal on every exit that will not poll again (clean stop, error, abort),
     including `Abort` / `release`'s `DrainAll`, under the same lock.
  4. `startRun` unseals before it publishes `running == true`, so the next run
     accepts new messages and does not inherit the previous seal.
  5. Test both windows (completion, and enqueue-after-`DrainAll`). The call
     either still affects the ending run or returns `ErrNotRunning`. A later
     run must not see the message unless a new, accepted call put it there.

### F-14 — Sync `Execute` path diverges in vendor session affinity — **OPEN-L**

- **Where:** `internal/subagent/subagent.go` — `PrepareBackground` sets
  `p.cfg.Model.SessionID = id`; blocking `Execute` does not.
- **Problem:** Zen free-tier requests carry an `x-opencode-session`
  affinity header (`internal/llm/zen.go` `setZenHeaders`). Background children
  get their own session id; direct-`Execute` children inherit the *parent's*.
  The loop always uses `PrepareBackground`, so this only affects embedders,
  but the two paths disagree for the same tool contract.
- **Fix needed:** One line in `Execute`: after `prepare`, set
  `p.cfg.Model.SessionID = taskID` (same rule as PrepareBackground) — plus a
  test asserting both paths set the child session id.

### F-15 — Unbounded dispatch goroutines per WS connection — **OPEN-L**

- **Where:** `internal/server/ws/conn.go` → `run()` (`c.wg.Add(1); go c.dispatch(req)`).
- **Problem:** No cap or queue on per-request goroutines. Session-state
  mutation is serialized by core locks, so impact is load, not corruption —
  but a fast/abusive client can spawn an unlimited number of concurrent
  handlers doing provider resolution and file I/O.
- **Fix needed:** Bound it — either (a) a buffered channel of `*rpc.Request`
  with N dispatcher goroutines (2–4 is plenty for one connection), or (b) a
  `semaphore`-style channel acquired before `go` and released on done, so
  excess requests serialize instead of growing. Given handler latency is
  dominated by provider resolution, option (b) is simplest.

### F-16 — `emitError` shares the accumulator with the error event — **RULED OUT**

- **Where:** `internal/llm/openai.go` → `emitError()`
- **Suspected:** `output.StopReason` / `ErrorMessage` are written on the
  accumulator, and the `error` event carries that same pointer. A consumer
  that kept the event would observe later writes.
- **Why it is not a bug:** Every `emitError(...)` call site in `run()` returns
  immediately. Nothing in that goroutine writes `output` after the send. A
  retry calls `run()` again and allocates a new accumulator; it does not
  mutate the message already sent. No code change. Do not fold a clone into
  the F-10 send rewrite. Re-open only if a call site continues after
  `emitError`, or a consumer retains the pointer across a later write.

### F-17 — Narrow race: parent cancel between child completion and the status switch — **OPEN-L**

- **Where:** `internal/subagent/subagent.go` → `runPrepared()` status switch
  (`case ctx.Err() != nil` evaluated after the child loop returns).
- **Problem:** The switch re-reads the parent context after `runLoop` returns,
  and that case outranks a successful result. A parent cancel after the child
  has already finished (`runErr == nil`, normal stop) stamps the report
  `cancelled`, and `Suppress` drops every `StatusCancelled` report. Same
  observable effect as F-02, narrower window. The child sink ignores context,
  so a cancel during the child's final `drainCompletions` / `agent_end` still
  returns `runErr == nil` and hits this case. Design the fix with F-02; do not
  ship a separate snippet.
- **Rejected fix (do not implement):** `parentCancelled := ctx.Err() != nil`
  sampled **before** `runLoop`, with the case replaced by
  `parentCancelled && lastAssistant(msgs) == nil`. That sample is false for a
  cancel that arrives during the run. The tool-boundary abort returns
  `ctx.Err()` from `runWasAborted`, misses the new case, and falls through to
  `runErr != nil` → `StatusFailed`. `Suppress` only matches `StatusCancelled`,
  so the abort is injected into the transcript — the opposite of the comment
  on `Suppress`. A `StopAborted` message with a nil `runErr` would still hit
  the default branch and stay `cancelled`; the broken path is the non-nil
  `runErr`.
- **Fix needed:** In the F-02 status change:
  1. Classify from the child result after `runLoop` (`runErr`, `runCtx.Err()`,
     last assistant `StopReason`). A finished success must not become
     `cancelled` just because the parent context is cancelled at the switch.
  2. Keep genuine in-run aborts as `StatusCancelled`, including
     `errors.Is(runErr, context.Canceled)` and `StopAborted`, so `Suppress`
     still drops them.
  3. Do not use a pre-`runLoop` `ctx.Err()` sample as the cancel signal.
  4. Tests: cancel after the child has returned a successful final turn
     (report stays completed, per F-02). Cancel during tool execution (status
     stays `cancelled`, and `Suppress` still drops it).

### F-18 — Drain injects after `maybeCompact` in the turn loop — recorded as reviewed (no action)

- **Where:** `internal/agent/loop.go` → `runLoop` inner iteration
  (`injectCompletions` → `maybeCompact` → `streamAssistant`).
- **Status:** **RULED OUT.** A freshly injected completion is recent history
  and the compaction cut-point logic (`compaction.PrepareCompaction`) preserves
  a recent budget, so a just-delivered report cannot be summarized away in
  practice. No code change planned; re-check if `Settings.KeepRecent` is made
  user-configurable.

---

## Items explicitly ruled out (recorded so they stay out)

| Suspicion | Why it is not a bug |
|---|---|
| `ShellTool` etc. shared between parent/children | All core tools are stateless value receivers over an immutable `Cwd`; no per-instance mutable state. |
| Shared `OpenAIProvider`/`retryProvider` across concurrent streams | Both hold no per-stream state (only an `APIKey`/`http.Client` and `inner`/`policy`); safe for concurrent `Stream`. |
| `internal/llm/zen.go` races | Pure functions; no state. |
| `/new` or `/resume` mid-run corrupting `runner.history` | Slashes commands are gated while working (`internal/tui/model.go:1819` — "Cancel the current run before using slash commands."), so the `r.history = loop.Messages()` write in `runner.run` cannot race a reset. |
| `queue.FollowUp()` slice aliasing | Re-slicing + later appends write beyond the retained view; never overwrite it. Covered by `TestFollowUpReturnsOneMessageAtATimeInFIFOOrder`. |
| `drainCompletions` double-delivery | `ready()` + `delivered()` are lock-paired and single-owner (loop goroutine); close joins workers before the drain reads. Covered by `TestSchedulerCloseKeepsUnwindReports`, `TestInjectCompletionsHonoursSuppress`. |
| F-13 as originally written (check and enqueue are separate critical sections; hold `s.mu` around both) | `SteerContent` / `FollowUpContent` already hold `s.mu` across the `!s.running` check and the enqueue, and `startRun` clears `running` under that same mutex. The old snippet is the current code. The live bug is the post-last-poll leftover described in F-13 now. |
| F-16 clone-before-send | Every `emitError` call site in `openai.run` returns immediately, so the shared accumulator is not written again after the error event is sent. See F-16. |
| F-17 pre-`runLoop` `ctx.Err()` snapshot | A sample taken before `runLoop` misses cancels that arrive during the run and turns tool-boundary aborts into `StatusFailed`, which `Suppress` does not drop. See F-17. |

---

## Suggested fix order (work items, not sizes!)

Each fix is independent; group only where noted.

| Order | Item | Why this order |
|---|---|---|
| 1 | F-01 (already done) | Regression test landed with the fix. |
| 2 | F-10 | Removes a real resource leak; touches only the provider layer. F-16 is not part of this pass. |
| 3 | F-11 | Aligns two constants, unblocks F-02's completion visibility from the server side. `release` is out of scope. |
| 4 | F-13 | Seal queue intake atomically with the loop's final poll. Not an `s.mu` change. |
| 5 | F-02 + F-17 (subagent status logic, one conceptual change) | Restores "finished work is never lost" semantics. Do not snapshot `ctx.Err()` before `runLoop`. |
| 6 | F-12 | WS pump leak — needs session `Done()` channel; also benefits long-lived dashboard connections. |
| 7 | F-03, F-05, F-07, F-09 | Documentation/string fixes. |
| 8 | F-04, F-08, F-14, F-15 | Low-risk robustness polish. |
| 9 | F-06 | Delete the three ignored root files. `.gitignore` already covers them. |

## Verification plan after fixes

1. `go test -count=1 ./internal/subagent/... ./internal/agent/... ./internal/server/...` — all green.
2. New regression tests above (F-01 done, one per fix).
3. Run `go test -race` once the toolchain has cgo configured — the findings
   around F-13/F-17/F-10 are races/concurrency and deserve the race detector.
4. Optional soak: loop 100 runs with 4 concurrent subagents over a scripted
   provider and assert per-provider goroutine count returns to baseline
   (guards F-10).
