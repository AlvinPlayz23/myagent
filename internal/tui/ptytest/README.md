# ptytest: deterministic PTY integration tests for the myagent TUI

This package launches the real `myagent` binary in interactive (TUI) mode
under a Linux pseudo-terminal and drives it end to end, with every provider
call served by a fake OpenAI-compatible streaming server on loopback. No real
credentials, no network, and no timing races: mid-stream scenarios are
controlled with server-side gates instead of sleeps.

## Running

```
go test ./internal/tui/ptytest/ -v -count 1
```

Notes:

- The suite builds the `myagent` binary once per test run with `go build` and
  reuses it for every scenario. The build is retried briefly because this
  worktree is edited concurrently; only a successful build is cached.
- Tests must run on a Unix system with `/dev/ptmx` (the package compiles
  empty on Windows).
- Each scenario gets its own `MYAGENT_DIR`, sessions, working directory, and
  fake server, so runs are isolated and `-count` repetitions are safe.
- A failed wait dumps the projected screen, the child's stderr, its exit
  state, and the full raw PTY output (path logged) for debugging.

## How it works

### Fake server

`Server` (fakeserver.go) listens on `127.0.0.1:0` and implements the two
endpoints myagent uses:

- `POST {base}/v1/chat/completions` — SSE streaming. Chunks mirror exactly
  what `internal/llm/openai.go` parses: `delta.reasoning_content` for
  thinking, `delta.content` for text, an explicit `finish_reason` chunk, a
  usage chunk, then `data: [DONE]`.
- `GET {base}/v1/models` — a one-model list.

Requests are classified: myagent's isolated title-generation call is the only
request sent without tools and with a small `max_completion_tokens`, so it is
answered with a fixed title. Every other (agent) request consumes the next
scripted response FIFO; with no script queued, a default reply is served.

`Script` controls one response: `Thinking` deltas, `Text` deltas, and
`GateAfter`. When `GateAfter > 0`, the server parks the stream after that
many deltas until `Server.Release()` is called. That makes "the turn is
running but not finished" a deterministic, observable state: the test waits
for the first delta to render, acts (Esc, queue a prompt, resize), and only
then releases the stream.

### Configuration

`NewEnv` (env.go) writes a temp `MYAGENT_DIR` with:

- `config.json` — a custom provider `ptyfake` of type `openai-compatible`
  whose `baseUrl` points at the fake server, with `default_model` set to
  `ptyfake/ptyfake-stream`. Shape matches `internal/config`.
- `models.json` — a fresh catalog cache (models + providers, recent
  `checkedAt`) so the TUI does not try to refresh the model catalog against
  models.dev at startup. Shape matches `internal/models`.

The child also gets `HOME` set to the temp dir and
`MYAGENT_MODEL`/`OPENAI_*`/`MYAGENT_SESSIONS_DIR` stripped from its
environment, so nothing from the host can override the isolated config. No
auth store is written: the custom provider carries its own fake key in
`config.json`.

### Launching and driving

`Launch`/`LaunchSized` (harness.go) open a PTY, set its size, and start the
binary as a session leader with the PTY as controlling terminal (so resizes
deliver SIGWINCH to the TUI). The child's stdin/stdout are the PTY; stderr is
captured separately so panics are assertable. Helpers:

- `Send(keys)` — raw keys to the PTY (`"hello\r"`, `"\x03"` ctrl+c,
  `"\x1b"` esc, `"\x1b[5~"` PageUp).
- `RequireContains`/`RequireGone`/`WaitFor` — poll a plain-text projection of
  the screen with a deadline. They fail fast once the child has exited.
- `Resize(w, h)` — resize the PTY and the projection.
- `Signal`, `WaitExit`, `RequireAlive`, `RequireNoPanic`, `WaitIdle`,
  `QuitClean` (wait idle, ctrl+c, require exit status 0 and no panic).

The screen projection (screen.go) is a small ANSI terminal emulator: it
interprets cursor addressing, erase display/line, insert/delete lines and
characters, scroll regions, and deferred autowrap, and skips SGR/OSC/DCS and
private-mode sequences. `ScreenText()` is therefore the visible text of the
screen, which is what the assertions use — no pixel inspection.

### Scenarios

| Test | Verifies |
| ---- | -------- |
| `TestStartupAndCleanExit` | welcome screen renders; ctrl+c quits with status 0, terminal restored ("Resume this session" printed), no panic |
| `TestPromptReceivesMockedResponse` | typed prompt streams the scripted thinking + assistant reply into the transcript |
| `TestAbortWithEscWhileStreaming` | Esc mid-stream aborts the turn: the working status clears, the gated tail never renders, the session records `stopReason:"aborted"`, and the app stays alive |
| `TestQueueFollowUpWhileStreaming` | a prompt typed while a turn runs queues ("↳ next" indicator); after the turn ends the follow-up gets its own reply |
| `TestResizeWhileStreaming` | resizing mid-stream does not crash and content keeps rendering |
| `TestHelpOverlayOpenAndDismiss` | "/" opens the command-picker overlay, Esc dismisses it, `/help` appends the command reference |
| `TestPageUpScrollKeepsAppResponsive` | PageUp scrolls a long transcript to its top and the app stays responsive (clean quit) |
| `TestScreenProjectionStripsANSI` | focused unit test of the ANSI projector |

## Known app race the abort scenario avoids

`internal/tui/runner.go`'s event sink forwards loop events to the UI through
`select { case r.events <- ...: ; case <-sctx.Done(): }`. Once Esc cancels the
run context, both cases are ready and Go picks one at random, so the
`message_end` event carrying the aborted stop reason — the one that renders the
"Operation aborted" transcript notice — reaches the UI only about half the
time (it is always persisted first, which is why the session file is the
deterministic abort evidence the test asserts). Fixing that belongs to the
TUI, not this harness; the scenario deliberately asserts only deterministic
behavior.
