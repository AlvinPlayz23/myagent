# TUI Upgrade Plan

Goal: make the interactive TUI polished, composable, keyboard-friendly, and visually
coherent, taking the public Grok Build pager (`xai-org/grok-build`, crate
`xai-grok-pager`, Apache-2.0) as a *behavioral and architectural reference only*.
The result stays recognizably myagent: Go + Bubble Tea v2, same agent runtime,
provider transport, session format, server protocol, and print-mode contract.

No Grok source code, branding, logos, or implementation details are copied. Nothing
is ported, so no `THIRD_PARTY_NOTICES` entry is required; if that ever changes, the
port must be isolated, notices retained, and Apache-2.0 terms verified first.

> Status: implemented. Phases 0–6 landed on the thread branch together with
> unit, render, and PTY tests; see "Landing order" below for what each commit
> carries and the README's TUI sections for user-facing behavior.

## Phase 0 — Baseline audit (recorded 2026-09-04)

Commands and results on this checkout (HEAD `d03cfef`):

    mise exec go@1.26.8 -- go test ./...

- 15 of 17 packages pass.
- `internal/tui` **fails to build**: commit `d03cfef` replaced
  `ruledComposerMaxRows = 10` with a duplicate `ruledComposerRules = 2`, leaving
  `ruledGrowthLimit` referencing an undefined constant.
- `internal/tools` fails 3 tests on Linux (`TestShellArgsFor`, `TestIsWSLStub`,
  `TestShellConfigMyagentShellPrecedence`): `shellArgsFor`/`isWSLStub` use
  `filepath.Base`/`filepath.ToSlash`, which are no-ops for backslash paths on
  non-Windows platforms. The tests encode the intended cross-platform behavior;
  the code is wrong and gets a minimal platform-robust fix in Phase 0.

Phase 0 repair (before any TUI work): restore `ruledComposerMaxRows = 10` in
`internal/tui/model.go`; make `shellArgsFor`/`isWSLStub` normalize `\` to `/`
before parsing in `internal/tools/bash.go`. No test edits.

### Current architecture map

- Entry: `tui.Run` (`internal/tui/tui.go`) builds `runner` + `model`, seeds the
  transcript from session history, wires persistence via `runner.onEvent`
  (`EventMessageEnd` → `sess.AppendMessage`, `EventCompactionEnd` →
  `sess.ApplyCompaction`), and runs `tea.NewProgram(m, tea.WithContext(ctx))`.
  Alt-screen, cell-motion mouse, and keyboard-enhancement flags are set in
  `model.View()`.
- Event bridge: agent loop → `runner.events` (buffered chan of `runnerEvent`
  carrying `AgentEvent`/done/title + a generation counter) → `waitForEvent`
  pump → `agentEventMsg` / `agentDoneMsg` / `agentTitleMsg` → `model.Update` →
  `onAgentEvent` → transcript blocks. `tickMsg` (100 ms) drives spinner/welcome
  animation; `clipboardResultMsg` carries paste results; `modelsDiscoveredMsg`
  carries live model discovery.
- Transcript (`internal/tui/transcript.go`): semantic blocks
  (user/assistant/tool/error/notice/thinking) with a per-block string render
  cache keyed by width + global expand flag; glamour markdown; Git-style
  proposal diffs for edit/write; global `ctrl+o` expand.
- Selection (`internal/tui/selection.go`): `textPoint{row,col}` over
  ANSI-stripped rendered lines; overlay style + copy-on-release via
  `atotto/clipboard`. Byte/row-index based — no wrapped-row or gutter awareness.
- Composer: bubbles `textarea` (two prompt styles), prompt history (100),
  slash-command picker (`commands.go`), `@` file picker (`file_mentions.go`),
  clipboard image attachments (`image_attachments.go`).
- Overlays today are **not** a system: eight independent picker structs
  (command, file, session, model, effort, provider, customize, export) plus
  export-name/overwrite/key-entry states, each with its own key routing branch
  inside `onKey` and its own `render*` + `panelHeight` case.
- Central problem: `internal/tui/model.go` is 2,626 lines and holds root state,
  all pickers, layout math, key/mouse dispatch, welcome animations, and every
  render path. It must shrink, not grow.

CLI dispatch (`main.go`): `sessions` / `auth` / `serve` / `tui` / print mode
(`-p`) — all preserved untouched. `serve.go` is untouched by this work.

## Phase 1 — Explicit UI architecture

Introduce typed routing while keeping behavior identical:

- `actions.go`: normalize raw `tea.KeyPressMsg` / mouse / paste / resize into
  semantic `uiAction` values. `Update` becomes: normalize → route.
- Routing order: topmost overlay → focused composer/scrollback → global.
  Agent/provider messages (`agentEventMsg`, `agentDoneMsg`, `agentTitleMsg`,
  `clipboardResultMsg`, `modelsDiscoveredMsg`, ticks) stay a separate stream
  and never masquerade as actions.
- State domains: root `model` keeps `AppState` (screen, terminal capabilities,
  theme, global status); a new `agentScreen` groups one session's composer +
  scrollback + queue + turn state; an `overlayStack` owns focus, dismissal,
  and hit-testing for all modal UI. Effects remain `tea.Cmd`s so nothing blocks
  the loop.
- Render invalidation: a `dirty` set on the layout layer keyed by
  (block identity, content revision, width, theme, expansion); streaming deltas
  invalidate only the streaming block.

## Phase 2 — Layout-aware transcript

Keep semantic blocks; add a layout layer producing rows/spans with: visible
text + style runs, source block/line identity, row kind (user, assistant,
thinking, tool header/output, diff, error, notice, spacer, continuation),
selectable ranges, wrap-join flags for copying, hit-test rectangles, and total
content height + viewport mapping.

- Width-aware wrapping via `uniseg`/`ansi` display width (both already indirect
  deps); Markdown styling preserved through styled span runs.
- Viewport: line/page scroll, follow-output mode while streaming, stable
  position when content above changes, "new output below" indicator when
  scrolled away, resize without corrupting selection mapping, and cached
  layout so large transcripts don't re-render per keypress.
- Selection works across wrapped rows and wide chars, excludes visual-only
  prefixes/gutters (diff gutters, tool markers, timestamps), and joins wrapped
  continuation rows correctly when copied.

## Phase 3 — Polished transcript experience

- Theme roles (background, foreground, muted, accent, user, assistant, thinking,
  tool pending/success/error, diff add/remove/hunk, selection, modal border,
  warning, status) with a readable monochrome/low-color fallback driven by
  detected color capability.
- Compact user messages; readable assistant Markdown; collapsible thinking
  (existing `/thinking` setting kept).
- Tool blocks: concise header (name, path/command, state, fold state),
  per-block fold, compact proposal diff for successful edit/write, error text
  (never a success diff) for failures, foldable long output.
- Header line (session title, provider/model, turn state, notices), footer key
  hints, non-jumping streaming indicator, queued-follow-up presentation,
  "new output below" affordance, restrained welcome screen, terminal title
  updates (existing `internal/terminal`).

## Phase 4 — First-class composer

Explicit composer states: idle, editing, slash completion, file completion,
history search, attachment preview, disabled/busy. Behavior preserved and made
testable: Enter send/queue, Alt+Enter steer, Esc abort, Ctrl+C clear-then-quit,
multiline insert (Ctrl+Enter / Ctrl+J / Shift+Enter where the terminal reports
it), history Up/Down only at empty cursor context, `/` and `@` completion menus,
paste + image attachments with visible chips, bounded growth with graceful
folding, predictable focus for Tab/Esc/click/scroll.

## Phase 5 — Overlay stack

One reusable overlay system (focus, dismissal, resize, dimming, mouse
hit-testing) with clean extension points. Migrate: command palette/help, model/
effort/provider pickers, session/resume picker, customize panel, export flow
(format → name → overwrite confirm), provider key entry. New: full-screen
tool-output viewer; confirmation dialog primitive. Future features
(search-in-transcript, tasks pane) extend the stack instead of adding
root-model conditionals.

## Phase 6 — Mouse, clipboard, terminal correctness

Normalized mouse events; drag selection; copy on release; click-to-toggle tool
blocks; wheel scrolling isolated to the region under the pointer; bracketed
paste (already via `tea.PasteMsg`); OSC 52 clipboard write when available with
safe local fallback; capability detection (truecolor/mouse/OSC 52/bracketed
paste) with graceful degradation; resize correctness; terminal state restored
on Ctrl+C, panic, error, and signals.

## Phase 7 — Testing and acceptance

- Unit tests: action routing, layout/wrapping, selection (wrapped rows, wide
  chars, gutter exclusion), hit-testing, overlay open/dismiss/focus/resize,
  composer completion + history + queue/abort/steer, resize invariants.
- Deterministic render tests at fixed sizes comparing normalized rows/spans,
  not raw ANSI: welcome, short/long prompts, thinking→assistant streaming,
  tool pending/success/error/diff, folded/expanded, output while scrolled up
  vs following, narrow→wide→narrow resize, Unicode/wide chars, Markdown/tables,
  selection across continuation rows, overlay states, completions, queued
  prompts, pastes.
- PTY integration tests (fake provider/event source, controllable ticks, no
  wall-clock dependence, no credentials): startup/clean exit; prompt → mocked
  streaming response; abort; queue follow-up; resize while streaming; overlay
  open/dismiss; scroll + select/copy; terminal restoration after interruption.
- All existing tests keep passing; `go vet`, `gofmt`, and `-race` on affected
  packages run clean.

## Landing order

Small reviewable stages, behavior preserved at each step:

1. Phase 0 repairs + this plan (baseline green).
2. Phase 1 action/router + overlay stack extraction (model.go shrinks).
3. Phase 2 layout layer + viewport + selection.
4. Phase 3+4 visual polish + composer states.
5. Phase 5 viewers/overlays.
6. Phase 6+7 capability handling + PTY scenarios.

No feature flag is needed: every stage keeps the same observable behavior, and
the build stays green throughout.
