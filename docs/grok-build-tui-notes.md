# Grok Build TUI study notes

Facts extracted by reading the `xai-org/grok-build` source (Apache-2.0; local
reference checkout under `/grok-build/`, gitignored). This is the spec the
myagent TUI's Grok-style chrome follows. Reference paths are relative to
`crates/codegen/`.

## Palette (`xai-grok-pager-render/src/theme/groknight.rs`)

- Grayscale ramp: bg `#141414` (terminal `#0a0a0a`), highlight `#242424`,
  text `#e1e1e1` / `#c8c8c8`, muted `#6c6c6c`, dim `#414141`, bright gray `#787878`.
- TokyoNight Night accents: blue `#7aa2f7`, cyan `#7dcfff`, green `#9ece6a`,
  magenta `#bb9af7`, orange `#ff9e64`, red `#f7768e`, yellow `#e0af68`,
  teal `#1abc9c`, purple `#9d7cd8`.
- Role colors: user `#c8c8c8`, assistant/thinking/brand/running accent magenta,
  tool bright gray, system blue, error red, success green, commands yellow,
  paths orange, warnings yellow.
- Chrome: prompt border `#323237` dim / `#505058` focused; scrollbar track
  `#111111`, thumb `#242424`; diff insert green-on-`#063806`, delete
  red-on-`#420e14`.
- Colors are defined as RGB and quantized to the terminal's capability at
  startup (Bubble Tea does the equivalent for myagent).

## Glyphs (`xai-grok-pager-render/src/glyphs.rs`)

- Prompt arrow `❯ ` (2 cols); check `✓`; ballot X `✗`; fold arrows `▸`/`▾`;
  chevrons `›`/`‹`; accent rail `┃`; timeline diamonds `◆`/`◇`; token arrow
  `⇣`; braille spinner `⠋⠙⠹⠸⠼⠴⠦⠧`; copy `⧉`; enlarge `↗`.
- Every glyph has an ASCII/CP437 fallback for legacy Windows ConHost.

## Agent screen frame (`xai-grok-pager/src/views/agent.rs`)

Rows top→bottom: status bar (top), optional panes (tasks/catalog/todo/queue/
dock), scrollback (with a 1-col gap + 1-col scrollbar track on the right, or a
timeline rail instead), turn status, banner/CTA/follow-up rows, prompt gap,
prompt (boxed), shortcuts bar, bottom status line. Short-terminal rule: extra
chrome rows collapse first; the prompt and scrollback are never starved.

## Scrollbar (`xai-grok-pager-render/src/render/scrollbar.rs`)

- Reserves 1 gap column + 1 track column on the right edge.
- Visibility signals follow mode: very dim while following (at bottom),
  brighter once the user scrolls up, to advertise the non-following state.

## Boxed prompt (`xai-grok-pager/src/views/prompt_widget/mod.rs`)

- Full rounded-border box `╭─╮ │ ╰─╯` around the input; border dim when
  unfocused, `prompt_border_active` when focused.
- Session title is inlined in the top border, right-aligned ending 2 cells
  before `╮`, drawn as ` {title} ` so padding blanks the adjacent `─`.
- `❯` prefix drawn at the first text row in the user accent; `? ` during
  history search, `! ` for bash mode.
- Bottom border doubles as an info line: right-aligned
  ` model · flags · multiline ` with 1-cell padding against the corners;
  model name in a blended text-secondary caption color, separators ` · ` in
  gray_dim, usage warnings in yellow when critical.

## Turn status (`xai-grok-pager/src/views/turn_status.rs`)

- Left: spinner frame + activity label (tool verbs gray + command-colored
  detail), ` · N queued` hints, phase timer ` M:SS`.
- Right: turn timer (gray), `bg` and `cancel` buttons (gray at rest, red on
  hover for cancel). Running accent is cyan for the spinner.

## Status line (`xai-grok-pager/src/views/status_line/`)

- One dim row of segments joined by ` │ `; builtin segments include cwd
  (≤40 cols), model (≤30), session name (≤40), token/cost counts; warn tone
  past 80% context.

## Top bar / welcome (`xai-grok-pager/src/views/welcome/`)

- Top bar: `{branch-icon} {branch} [{worktree} ]{cwd}` — git info dim/bold,
  cwd in gray_dim.
- Centered braille-art logo (hidden below 22 rows), hero box, and a menu of
  `label …… key` rows: labels bold text-primary, keys gray_bright, selection
  highlighted with `bg_highlight`.

## Modals (`xai-grok-pager/src/views/modal_window.rs`)

Centered bordered windows above the screen with dim borders; title/caption
styled like the prompt caption. Lists keep `›` cursors and dim descriptions.

## Transcript blocks (`xai-grok-pager/src/scrollback/`)

- User: `❯ ` prompt arrow + message, continuation indented 2 cols; recognized
  `/commands` tinted with the skill accent.
- Assistant: streaming markdown; tool blocks show verb headers with
  state styling and fold behavior; thinking is its own foldable block.
- One blank row between blocks; tool output folds to a preview with an
  expand affordance.

## What myagent adopts vs. deliberately omits

Adopted: palette, glyph set, boxed prompt with caption/info line, segment
status/footer rows, turn-status layout (left activity, right timer),
centered modal windows, welcome hero box + menu, right-edge scrollbar with
follow-state brightness, `❯`-style user transcript blocks, Grok diff colors.

Omitted (documented in the PR): Grok's timeline rail, panes/dock system,
voice/monitor widgets, dashboard, and its exact logo art (myagent keeps its
own wordmark and animated welcome styles). No Grok code is copied; the
implementation is original Go.
