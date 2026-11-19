# GOTUI.md — Retained-Mode Widget TUI for myagent

*Architecture & implementation plan: achieving an OpenCode/OpenTUI-style "blocky, clickable" TUI in Go on the current charm stack, without leaving Bubbletea.*

---

## 1. Goal and context

### What we want

The modern OpenCode TUI (v2.x) feels like a terminal DOM: blocks with computed bounds,
z-ordered overlays, clickable regions, a dialog manager with dimmed backdrops, toasts,
tab rails, hover states, and per-part streaming caches. We want that feel in myagent.
**The north star is OpenCode's app itself, not OpenTUI's API** — §3.0 pins the style
contract mined from its actual source; OpenTUI semantics survive only as the §3.0.1
mechanics substrate.

### Why this is now achievable in Go

| OpenTUI/OpenCode capability | Go equivalent available today |
|---|---|
| Retained component tree (Solid JS) | Widget structs holding their own state (kit below) |
| Flexbox layout (row/column/grow/gap) | Small `HBox`/`VBox` weights layout (~200 LOC; we need a subset, not CSS flexbox) |
| Box / Text renderables | `lipgloss` styles, borders, padding (already in use) |
| ScrollBox | `bubbles/v2` Viewport wrapped as a widget |
| Z-order compositing, topmost input | **`lipgloss.Layer` + `lipgloss.Compositor`** (in lipgloss v2, stable Feb 2026) with `.X().Y().Z()`, `Compositor.Hit(x, y)` returning the topmost layer ID |
| Dialog stack + backdrop | `DialogStack` widget over compositor layers |
| Clickable blocks / menus | bubbletea v2 mouse events → hit routing → widget dispatch |
| Efficient repaint | Bubbletea v2 "Cursed Renderer" (ncurses-style ANSI diffing; full-frame diffs are cheap) |

Everything above is in `go.mod` already:

- `charm.land/bubbletea/v2 v2.0.8`
- `charm.land/lipgloss/v2 v2.0.5` (current latest ~v2.0.6 — bump recommended)
- `charm.land/bubbles/v2 v2.1.1`
- `glamour` (markdown), `charmbracelet/x/ansi` (width/join primitives)

Charm itself uses the v2 `Layer`/`Compositor` pair "heavily" for Crush's modals — it is the
maintained, documented path for overlays in lipgloss v2.

### What we deliberately do NOT copy

- OpenCode's old Go TUI (v0.15.x): same immediate-mode string-transcript architecture as
  our current `transcript.go`. Not the target.
- Full CSS flexbox (only grow/gap/fixed), per-cell damage tracking, SIXEL/image support,
  text wrapping inside every node — not needed for this app's shape.

---

## 2. Current state of myagent's TUI (what we keep vs replace)

### Keep (already OpenCode-pattern in spirit)

- `internal/tui/transcript.go` — `block` model (user/assistant/tool/error/notice/thinking),
  tool diffs, expand toggle, **per-block render cache keyed (width, expand)**.
- `internal/tui/mouse.go`, `selection.go`, `hyperlink.go` — mouse, OSC-8 links, yank.
- `internal/tui/file_mentions.go`, `picker_row.go`, `welcome.go` — composer typing surface.
- `theme.go` — token theming; keep as the single color source.

### Replace / add (the missing "widget" chrome)

| Gap | OpenCode counterpart | Plan |
|---|---|---|
| No dialog manager: pickers are bespoke full-replacements | `ui/dialog.tsx` stacked, dimmed backdrop, ESC-eats keys | `DialogStack` widget (Phase 2) |
| Hand-rolled hit-testing (`hit.go` region table) | element-level hit | rect registry from layout pass (Phase 1) |
| No toasts | `toast.tsx` top-right, TTL | `ToastHost` (Phase 2) |
| No session tabs rail | `session-tabs.tsx` | `Tabs` widget (Phase 4) |
| No sidebar/usage rail | `sidebar.tsx` | `Sidebar` (Phase 4) |
| No todo block | `todo-item.tsx` | `TodoBlock` (Phase 4) |
| No hover feedback | hover on tool cards/tabs | `Hoverable` via motion events (Phase 4) |
| Composer spinner is multi-line region | `one-cell-spinner.tsx` | single-cell ticked spinner (Phase 4) |

---

## 3. Kit architecture (`internal/ui/kit`)

### 3.0 OpenCode TUI style contract (the north star)

**Source of record:** `opencode/packages/tui/src` (the running OpenCode TUI). OpenCode's
app is the *what*; OpenTUI mechanics (§3.0.1 below) are only the *how*. Cell counts and
glyph literals below are taken from source, not guessed.

**A. Shell anatomy** (`routes/session`, `component/session-frame.tsx`)
- Root: full-terminal column, bg `background.base`. Optional left session-tab rail
  (width 42; shown only when terminal ≥ 106; drag-resizable 5–72) above the main column.
- Session row: main pane (grow 1) + right pane (`flexShrink: 0`): sidebar fixed 42, or a
  drag-resizable panel via a 1-cell handle; when the total is too narrow the sidebar
  becomes an absolute overlay over a 70-alpha scrim.
- Content column: horizontal padding 2 (1 when terminal < 44); transcript `ScrollBox`
  fills; a 1-row status line below it (muted "Loading session history…" or clickable
  "Jump to latest ↓"); the bottom stack (queued-prompt dock → composer / permission
  prompt) is shrink-0.
- Message row grammar: `marginTop=1` between entries, message indent **3 cells**, inline
  tool icon column **2 cells**.
- Home: vertically centered block (logo, gap, update notice with 200 ms fade-in, prompt
  `maxWidth=75`); dialogs live at z 3000 over everything.

**B. Visual vocabulary** (`ui/border.ts`, `routes/session`)
- The card idiom everywhere: a **left-only heavy border `┃` (U+2503)** over a raised
  background, padding 2 — user message, block tool, thinking, composer, queued dock.
  Full boxes are rare: dialogs and toasts are borderless surfaces separated by
  background color; compaction banners are top-border-only rules with a centered label.
- Border color = the agent's categorical color (else `border.base`); hover = `decrease(bg)`;
  streaming/pending states fade the accent (tint with alpha).
- Glyph grammar: `✓` done, `✗` failed, `│` running, `›` generic rows/actions, `↳`
  continuation, `→` resolved, `◈` notices, `◎` goals, `◇` queued, `−/+` disclosure,
  `⋯` no-animation fallback, `⌕` search, braille spinner `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`, quadrant
  spinner `◴◷◶◵`, unread dots `• ● ▪ ■`, half-block `▀▄` shading caps, and
  `·`-separated muted metadata rows.
- Emphasis hierarchy: BOLD only for titles/selected; everything secondary is `text.muted`;
  order of presence: base > muted > alpha-faded (animation) > feedback (status outcomes
  only). DENIED tools get strikethrough.

**C. Theme discipline** (`theme/*`, OpenCode AGENTS.md rules)
- Token groups: `text.{base, muted, feedback.{error,warning,success,info}, action, formfield}`,
  `background.{base, raised{base,high,max}, action, formfield, feedback}`, `border.base`,
  per-agent `categorical[]`, plus `diff.*`, `syntax.*`, `markdown.*` palettes.
- Stateful accessors: `…$focused/$hovered/$disabled/$selected` (e.g.
  `text.action.primary.focused`).
- Color math helpers `decrease()/tint()` derive hover/ghost shades from base tokens —
  never raw hue references in components.
- Rule: pick tokens **by semantic role, never by current color**; if no token fits,
  extend the theme schema rather than repurpose the nearest-looking token; verify dark,
  light, and custom-fallback paths.
- myagent mapping: `theme.go` gains these groups + state accessors; existing per-agent
  naming maps into `categorical`.

**D. Component specs** (anatomy + interactivity)
- *User card*: `┃` border in agent color, raised bg, pad 2, hover dims; attachment/skill
  pills (`" file "` / `" skill "` accent chips); click opens message dialog (edit / retry /
  resend); queued variant click → move-to-queue / delete select.
- *Assistant*: markdown, pad-left 3; per-step footer `Agent · model · 1.2s · 34.5 tok/s ·
  interrupted` (agent colored, rest muted, `·` separators).
- *Thinking*: collapsed = warning-colored `+ Thought: … · 4s`; running = "Thinking:
  {title}" spinner; expanded = muted markdown in a dim `┃` box.
- *Tools duality*: inline one-liners in the flow (2-cell icon; hover brightens when
  clickable; failed = red + expandable error; denied = strikethrough) vs block `┃` cards
  (raised bg; header `← Edit <path>` with inline spinner while streaming, warning color
  while permission-pending; expanded `key: value` details at pad 5).
- *Diff card*: `diff.*` tokens; 1-cell gutter + line numbers; hunk header bar `@@ -x,y
  +a,b @@` on tinted bg; unified at ≤ 120 cols else split; virtualized in 128-line
  chunks past 3000 lines.
- *Todo*: single rows `[✓]` done / `[•]` in-progress (warning) / `[-]` strikethrough
  faint / `[ ]` muted — no box.
- *Composer*: `┃` accent border (agent tint, faded when unfocused), custom bottom-left
  `╹`, body bg = dimmed raised, textarea `maxHeight = h/3`, image previews (h 4–8),
  subtoken metadata row (agent, auto, model, provider, `· variant`, context limit) that
  alpha-fades and truncates to fit, bottom cap row of `▀` half-blocks, footer: left
  spinner / cwd (clickable menu), right file-context chip (accent while pending).
- *Dialog stack*: provider-owned stack; `replace()` clears; Esc/Ctrl+C pops; backdrop
  click closes (with text-selection guard); refocuses previous. Scrim alpha 150, z 3000,
  `paddingTop = h/4` unless centered; widths 60/88/116; side inset 3; header BOLD +
  right-clickable muted "esc"; footer rows `cancel esc` / `confirm enter`, lowercase,
  primary right-aligned; hover pads buttons 1 cell.
- *Dialog select*: 1-cell gutter with `●`/`▪` current marker; selection bg
  `background.action.primary.focused`; category headers group rows; fuzzy search with
  title weighted 2×; height cap `h/2 − 6` with "…"; per-row footers (shortcuts, `$x left`
  in error color).
- *Command palette*: DialogSelect over all keymap commands; "Suggested" pseudo-group when
  unfiltered; footer = first keybind.
- *Toasts*: top-right (`top 1, right 2`, content-fit width ≤ min(60, w−6), anchored
  to the main column beside the sidebar); one visible + FIFO queue with "+N more";
  single left `┃` rail tinted by variant; raised bg; variant glyph + bold optional
  title + word-wrapped message; 5000 ms auto-dismiss, hover pauses, click activates
  the trailing `› action` row.
- *Session tabs*: top bar or left rail (42, compact 5); active = bg-pulse; busy = spinner
  frames; unread = fading dot + glow (0.12 tint); titles marquee on hover; drag to
  reorder; half-block `▄/▀` edges; `⌕` opens the session switcher.

**E. Motion** — everything gated by `config.animations` (toggle exists)
- Braille spinner 80 ms (40 ms in composer footer); `———— ShimmerText` 1200 ms loop;
  FadeIn 200 ms sweep for notices; **one-cell spinner** (1×1 cell, subcell octant
  glyphs ▖/▘ composed, 40 ms patterns, intro easing, ages/fatigues after 30 s); tab
  unread glow decay; marquee. No-animation fallbacks everywhere: `⋯ {label}`, `▪`,
  `[⋯]`.

**F. Interaction feel** (`config/keybind.ts`)
- Leader `ctrl+x`; Enter submits; Shift/Ctrl+J newline; Ctrl+C clears (or exits when
  focused+empty, or pops dialog); **Esc = interrupt running session / pop top dialog**
  (the universal "home"); `ctrl+p` palette; `ctrl+o` menu; PageUp/Down scroll (Ctrl+Alt =
  line/half); Alt+End latest message; Ctrl+G first message.
- Mouse-first when enabled: hover brightening, click-to-disclose, wheel scroll with
  acceleration, drag-resize panes, drag-reorder tabs, right-click copy, hyperlink click.

**G. Port priorities (the feel-defining set)**
1. Semantic tokens + state accessors · 2. `┃` raised-card idiom + hover `decrease()` ·
3. Composer anatomy (alpha metadata row, `▀` cap, footer) · 4. Indent grammar
(3-cell column / 2-cell icons) · 5. Tool inline/block duality with `✓ ✗ │ ›` lifecycle ·
6. Thinking treatment · 7. Diff tokens + hunk bar · 8. Dialog stack dimensions (scrim/
widths/Esc footers) · 9. Dialog-select gutter + marker · 10. Toast feel · 11. Braille +
one-cell spinners, shimmer · 12. "Jump to latest ↓" status row · 13. Tab rail behaviors ·
14. Bold-only-titles + `·` metadata rows · 15. Keybind house style.

#### 3.0.1 Mechanics substrate (OpenTUI semantics inherited by OpenCode)

The kit implements these OpenTUI mechanics 1:1 — currently hidden plumbing under the
OpenCode look above. Vocabulary follows OpenTUI component names (Box, Text, ScrollBox,
Select, TabSelect, Markdown, Diff, Input/Textarea) so OpenTUI's own docs transfer
directly. `VBox`/`HBox` in §3.2 remain as sugar over `Box` + direction.

| OpenTUI 0.8.1 semantic | Adopt? | Go implementation |
|---|---|---|
| Retained renderable tree; mutate properties, never rebuild per frame; setters request re-render | Yes | Widgets persist across frames; `SetContent`/`SetWidth`/`SetVisible` setIdempotent dirty flags; transcript blocks already cache |
| `visible = false` keeps node in tree, takes no layout space; hidden focused node blurs | Yes | Layout pass skips invisible children; focus ring blur hook |
| Flexbox subset on a **cell grid** (columns/rows, never chars) | Same property names | `box.go`: `Direction` (column default, like Yoga), `FlexGrow/FlexShrink/FlexBasis`, `MinW/MaxW/MinH/MaxH`, `Align`/`Justify` subset (flex-start/center/flex-end/stretch/space-between), `Gap/margin*/padding*` shorthand + `X/Y/Left/Right/Top/Bottom`, absolute `Position` for overlays; **round cumulative edges, not individual widths** (S0: this reproduces OpenTUI's 31 → 10/11/10) |
| Ported defaults: numeric size ⇒ `flexShrink: 0`; `flexGrow: 0`; `align: stretch`; `justify: flex-start`; zero-height child paints nothing but children still paint unless clipped | Yes, as shared Go constants in `layout.go` | Prevents the classic OpenTUI header-shrink bug (§ ScrollBox docs) |
| `overflow: visible / hidden` (clip paint **and** mouse hit bounds) | hidden only | ANSI clip to rect in paint; hit registry clips rects |
| `zIndex` changes sibling paint **and** hit order; `translateX/Y` move paint+hit without layout | Yes | Children painted in z-order; hit registry resolved z-first; ignore `translate` initially |
| ScrollBox native scroll state: `stickyScroll` + `stickyStart` (bottom), pauses when user scrolls away, resumes at edge; `scrollBy`/`scrollTo` target applied after next layout; `scrollChildIntoView` (DOM "nearest"); keyboard 1/5 viewport, Page 1/2, Home/End; **viewport culling** ≥16 direct children skips paint of offscreen subtrees; stylable scrollbars | Yes | `scrollbox.go` wraps the transcript: culling = existing block windowing; sticky bottom = existing tail-follow, now with OpenTUI pause/resume semantics; scroll-target queue consulted post-layout. S0-verified rules: **end targets** (e.g. `scrollTo(scrollHeight)`) persist across content growth, **absolute targets are one-shot**; content is staged before layout, so same-tick growth flushes pending targets correctly. **S3 divergence:** canonical keys and culling are §3.1.1 #12 (line ±1 / modifier+arrow ±5 / page h−2 / Home-End; culling at every child count, +1 overscan) |
| `anchor(child)` keeps a child pinned while older content loads above; `afterLayout(cb)` runs post-layout pre-paint | Phase 5 (defer) | Needed for jump-to-message and "load older messages"; not day-1 |
| Component set parity | adopted set | Box, Text, Select, TabSelect, Markdown, Diff, Input, Textarea, ScrollBox, ScrollBar, Editor. Later: Slider, TextTable, AsciiFont |
| Dialogs, toasts, tabs rail, sidebar | App-level (that's OpenCode territory, not core) | `DialogStack`, `ToastHost`, `Tabs`, `Sidebar` per §3.2 unchanged |

**Consciously not adopted** (the honest deltas): per-cell frame buffers and Zig paint
hooks; Yoga's full percent/measure machinery (Go text measure = `libgloss`/`x/ansi` width
+ wrap modes; percent resolves only against a definite parent, same caveat as OpenTUI);
the Timeline animation engine (TickDriver subset instead); images/video/3D/audio;
embedded terminal.

Builder flavor mirrors the OpenTUI quickstart, but struct-first (Go idiom):

```go
panel := kit.NewBox(kit.BoxOpts{ Width: 34, Height: 5, Border: true,
    BorderColor: theme.Border, Padding: 1, Gap: 2, Direction: kit.Row })
panel.Add(fixed, growing)           // reparenting semantics like OpenTUI `add()`
panel.InsertBefore(status, dormant) // index + anchor insertion

status := kit.NewText(kit.TextOpts{ Content: "Waiting", Fg: theme.Text })
status.SetContent("Ready")          // mutation, not rebuild — next frame paints it
```

### 3.1 Core types

```go
// rect.go
type Rect struct{ X, Y, W, H int }
func (r Rect) Contains(x, y int) bool

// widget.go
type Widget interface {
    // Layout assigns absolute bounds; called top-down each frame.
    Layout(r Rect)
    // Render paints the widget's visible string. Height = r.H, width = r.W
    // from the last Layout. Must be pure w.r.t. state.
    Render(w, h int) string
    // Key: return true if consumed (focus target handles first).
    Key(ev tea.KeyPressMsg) bool
    // Mouse: return true if consumed. r = widget's absolute bounds.
    Mouse(ev tea.MouseMsg, r Rect) bool
    // Focusable returns non-nil if this widget can take focus.
    FocusTarget() *FocusTarget
}
```

Containers implement `Widget` and recurse:
`Container.Layout` distributes `Rect` among children per its layout algorithm,
then calls `child.Layout(subRect)`. A shared registry records `id → Rect`
during layout for hit-testing (`hitregistry.go`).

#### 3.1.1 Canonical contract amendments (S1 freeze — binding on all later agents)

1. **Layout signature stands.** `Layout(r Rect)` receives only the absolute Rect — no
   parent/clip/registry parameters. `Root` rebuilds hit bounds + clips by walking the tree
   after layout, using the optional `Children() []Widget` container extension (not part of
   the frozen five-method interface).
2. **Mouse hands over the raw event**: `Mouse(ev tea.MouseMsg, r Rect)`; wheel normalization
   is a helper (`WheelDelta`, ±1), not a synthetic event.
3. **Z is band/sibling-scoped**, resolving the S0 flat-Z trap: Root owns flat bands
   (transcript Z0 / toasts Z10 / dialogs Z20); widget `Z` orders siblings within a band.
   Never rely on nesting to inherit Z.
4. **`Root.Render(w,h)` lays out lazily** on size change; explicit `Layout` remains valid.
5. **Visible footgun**: zero value is `false`; construct via `NewWidgetBase(id)` or call
   `Show()`. Documented, not changed.
6. Dialogs/toasts are **band hosts**, not kit implementations — DialogStack/ToastHost land
   in Phase 2; a single visible-dialog modal gate exists in Root.
7. Box adopts `overflow: hidden` only (clips paint + hit bounds).
8. Kit text drawing works around a pinned **ultraviolet `StyledString.Lines` bug** for
   unbounded strings by drawing into a scratch canvas; ultraviolet is now a direct
   dependency.
9. **Band-host API (S2a, canonical — verbatim):**
   ```go
   func RegisterBandHost(band int, w Widget, opts ...BandHostOption) RootOption
   func WithBandCapture(on bool) BandHostOption
   ```
   Composed as `NewRoot(base, RegisterBandHost(BandToasts, host), ...)`; `WithToasts` /
   `WithDialogs` are thin wrappers. Root's modal gate asks `DialogStack.Modal()`; the scrim
   is applied by Root from `ScrimProvider` before the BandDialogs host paints. S2b-style
   consumers register via the same factory (a *Root* method was the old spec — the RootOption
   factory is final).
   **Capture default (S2b cross-review, binding):** the underlying default is
   `capture = band > BandBase`. Modal bands keep it; passive overlay wrappers must pin it
   off — `WithToasts` is hardwired to `WithBandCapture(false)` because ToastHost is
   non-focusable with a no-op Key. New wrapper for a passive band ⇒ same rule.
10. **S2a LOC ruling:** dialog system = 1,647 LOC (dialog.go 1,253 + list.go 394) vs ~900
    soft target — accepted; `dialog.go` may be split (`select.go`) if S4 growth demands it.
11. **Measure-in-layout (S3, amends §3.5 "never render in Layout"):** scroll containers may
    measure by consulting width-keyed render caches in `Layout`/`MeasureHeight` (render-to-
    measure, painting stays in `Render`). Because measure and render share the same cache,
    the cost is one render per revision — the S3 transcript path relies on this. Never paint
    from Layout; never invalidate the transcript `cacheWidth/cacheValid` contract from kit.
12. **ScrollBox keys (S3, canonical — supersedes the OpenTUI row's "1/5 viewport, Page 1/2"):**
    up/down = line ±1; ctrl/shift/alt+arrow = ±5 cells; page = h−2; Home/End. Wheel = ±1 via
    `WheelDelta`. Culling runs at every child count with +1 overscan (not the ≥16 threshold).
    Scrollbar column always reserved in transcript mode; full-height thumb painted when
    content fits (visual review in S4).
13. **Single live ScrollBox is a documented invariant, not runtime enforcement** (amends
    §3.2). Root gains no registry hook for it; the app shell creates exactly one. Also
    canonical from S3: welcome is static (no tick, excluded from the dirty fingerprint);
    selection click/drag/release delegates to the existing machinery; multi-click
    word/line expansion was promoted into the kit mouse path in S4 (legacy path unchanged).
14. **Root entry/focus API (S4, canonical — verbatim):**
    ```go
    func WithInitialFocus(id string) RootOption
    func (r *Root) QueueFocus(id string)   // applied after the first layout
    func (r *Root) PendingFocus() string
    func (r *Root) HandleKey(ev tea.KeyPressMsg) bool   // shell entry; never dispatches into baseWidget
    func (r *Root) HandleMouse(ev tea.MouseMsg, rect Rect) bool
    ```
    `Root.Key/Mouse` remain valid for direct hosts and tests; `Shell.Key/Mouse` route through
    `Handle*`. `DialogSelect` gained host-refresh API (`SetQuery`/`SelectID`/`SelectedID`) so
    typing is never clobbered by host refreshes. The jump affordance reused
    `ScrollToEnd`/`StickyActive` — ScrollBox API did **not** grow. Thinking keeps the legacy
    collapsed/running/expanded treatment as canonical (no kit duplicate). Tool-card header
    restyle (rounded border, title-in-border, traffic-light accent) was evaluated in S5 and
    deferred again (post-Phase-5): it fights the cached block paint/height contract (§5). The
    Hoverable Surface pass remains the kit-mode treatment; legacy block rendering stays
    byte-identical.
15. **Panel/dock API (P6, canonical):** optional `KitDockHost` — hosts that do not
    implement it paint no dock (S3/S4 doubles stay golden-stable). Panel lifecycle rules:
    kind/mode changes replace the dialog with cancel suppressed; `openConfirmPanel` chains
    `OnDismiss` and owns `OnClose` so a host sync cannot pop a successor dialog;
    Esc/backdrop cancel only while the host still reports the panel; click/wheel is
    select-first-then-activate. `/commands` and `/models` stay on the S4 KitPickerSelect
    bridge; `/export` chains format → name → `kit.NewConfirm`; `/customize` has no confirm
    step (Enter = legacy save action — do not invent one). Picker **logic**
    (`model_picker.go`, `tools_picker.go`, `tools_scope.go`) is retained as the projection
    source for future ports.

### 3.2 Primitives

| Widget | Responsibility |
|---|---|
| `VBox` / `HBox` | Flex column/row. Children declared with `Child{W Widget, Grow int, Fixed int}`. Distributes main-axis space by weight, `Gap` cells between. Sequences child `Layout` calls. |
| `Pane` | Bordered box (rounded, dim border by default) + title in top-right. Wraps one child. |
| `ScrollBox` | Viewport over a taller child with tail-follow + scrollbar column; one live instance anywhere in the tree at a time (documented invariant — see §3.1.1 #13; S3 keys/culling per §3.1.1 #12). |
| `Spacer` | Fixed or flexible empty space. |
| `Label` | Styled single-style text; width-aware. |
| `Markdown` | Glamour-rendered static block (wraps existing `mdRenderer`); cache-stable by construction. |
| `Hoverable` | Wraps a child; swaps render style on mouse-motion enter/leave. |
| `List` | Scrollable selectable rows (up/down, enter) with optional recency order — generalizes `model_picker.go` / `tools_picker.go` internals. |
| `Tabs` | Horizontal rail of session tabs, active tab accent, click-to-switch. |
| `DialogStack` | Modal manager: dimmed backdrop + stacked dialogs; intercepts all unhandled keys/mouse until empty (see 3.4). |
| `ToastHost` | Overlay column top-right (`top 1, right 2`, content-fit width ≤ min(60, w−6)), anchored to the main column, single left rail tinted by variant, FIFO one-visible + `+N more`, 5000 ms TTL with hover-pause, click dismisses and runs `› action`; auto-drain via tick driver — key-passive band (see 3.0.D). |
| `TickDriver` | Single `tea.Tick` loop at ~15–20 fps **only while animations/toasts are active**; widgets subscribe with an `Invalidator` callback. |

### 3.3 Render pipeline per frame

```
1. Layout:    root.Layout(Rect{0,0,term.W,term.H})   → abs rects + hit registry
2. Paint:     shell := root.Render(W, H)             → main "screen" content
              (transcript blocks render from their own caches)
3. Layers:    lipgloss.NewCompositor(
                  lipgloss.NewLayer(shell),          // Z = 0
                  toastLayer,                         // Z = 10
                  dialogLayer,                        // Z = 20 (backdrop + stacked dialogs)
              ).Render()
4. View():    composed string → bubbletea v2 renders; Cursed Renderer diffs ANSI.
```

Frame cost notes:
- Step 3 stays at 1–3 layers total; per-widget hit-testing uses the registry, not
  thousands of compositor layers (Compositor flattens per layer — keep it coarse).
- Cached sub-strings keep paint cheap: blocks reuse `cache(cached, cacheWidth, cacheValid)`;
  kit widgets get the same `cache(w, h, rev)` pattern via `cache.go`.
- **S0 evidence** (`spikes/gotui-s0/REPORT.md`): a 3-layer 200×60 `Compositor.Render` costs
  ~2.35 ms / 1.58 MB alloc (i3-1005G1) — ≈14% of a 60 fps budget; `Hit` is 22 ns / 0 allocs.
- `Render()` requires an **explicit full-screen base layer at (0,0)** or output shifts/clips,
  and it trims trailing whitespace — the root must always paint full W×H.
- **Z is global and flat**: layers do not inherit Z from nesting. Our band model
  (transcript Z0 / toasts Z10 / dialogs Z20) is unaffected, but never rely on equal-Z
  ordering or nested Z.

### 3.4 Input routing

**Mouse** (order matters):
1. If `DialogStack` is open → topmost dialog only (else discard).
2. `Compositor-layer checks` handled by construction (dialog is a layer; events already gated by step 1).
3. Otherwise: registry hit from **highest z band → deepest/reverse document order**;
   deepest widget's `Mouse` runs; if it returns false, bubble up to ancestors.
4. Motion events re-route to `Hoverable.enter/leave` and set hover state; drag events are
   suppressed for route #3 while an active selection drag is in progress
   (interplay with `selection.go`; Shift+drag keeps old text-selection behavior).

  Wheel note (S0 finding, bubbletea v2.0.8): wheel arrives as a button event with no
  fractional delta — the registry route derives ±1 line steps; no per-event wheel magnitude.

**Keyboard** (first-responder model):
1. `DialogStack` (top dialog) — consumes everything incl. Esc (pop).
2. Global commands (current `commands.go` dispatch, prefix `/`).
3. Composer textarea if focused.
4. `FocusTarget` chain: Tab / arrows move focus ring; click sets focus target and ring.

Esc semantics mirror OpenCode: Esc pops the top dialog only; a second Esc on an empty
stack goes to the app's usual cancel/quit path.

### 3.5 Caching rules (the performance contract)

Every widget with expensive paint holds `renderCache { key: struct{W,H,Rev int}, out string }`.
`Rev` increments when widget state changes. Rules:

- Transcript blocks: keep existing width-keyed cache; only the streaming block re-renders.
- Markdown/glamour: cache by (width, content) as today.
- Never render in `Layout`; layout is geometry only.
- Streaming updates funnel through the existing coalescer (render-once-per-screenful),
  wrapping `tea.Cmd` render off the UI goroutine — same discipline OpenCode v0.x used,
  now bounding the frame paint, not the transcript build.
- **Compose-on-dirty only** (S0 rule): the §3.3 pipeline runs solely when a widget is
  dirty or the terminal resized; an idle app must produce zero compositing work
  (measured: 2.35 ms / 1.58 MB per composite at 200×60).

---

## 4. App composition (target shape)

```go
// internal/ui/app/shell.go
func NewShell(deps Deps) kit.Widget {
    tabs := chrome.NewSessionTabs(deps.Sessions)          // Phase 4
    transcript := chrome.NewTranscriptScroll(deps.Transcript) // ScrollBox over cached blocks
    composer := chrome.NewComposer(deps.Editor)            // textarea + one-cell spinner
    status := chrome.NewStatusBar(deps.Status)

    main := kit.VBox(
        kit.Child{W: tabs, Fixed: 2},        // hidden until multiple sessions
        kit.Child{W: transcript, Grow: 1},
        kit.Child{W: composer, Fixed: 0},    // composer computes its own height
        kit.Child{W: status, Fixed: 1},
    )
    return kit.Root(main, kit.WithToastHost(deps.Toasts), kit.WithDialogStack(deps.Dialogs))
}
```

Sidebar variant (Phase 4): the session row becomes `HBox(sidebar Fixed:28, VBox(...))`,
matching OpenCode's rail/main split.

Dialogs migrate from bespoke pickers into consumable widgets with the same model logic:

| Existing | New home |
|---|---|
| `model_picker.go` model list | `DialogStack` + `List` dialog |
| `commands.go` `/` palette | `DialogStack` + fuzzy `List` dialog (like OpenCode's command palette) |
| `model_candidates.go` | dialog variant |
| confirm/abort prompts | `ui/kit` `Confirm` dialog helper |

---

## 5. Implementation plan (phases)

> **Status:** Phase 0 complete — verdict **GO** (evidence, benchmarks, API signatures in
> `spikes/gotui-s0/REPORT.md`). Outstanding: G5 human mouse run —
> `go run ./spikes/gotui-s0/mouseprobe` in Windows Terminal (clicks, wheel ±1, motion rate).
> Amendments from S0 are applied in §3.3, §3.4, §3.5.
> **Phase 1 complete** — kit contract frozen in `internal/ui/kit` (1,494 code LOC; the ~25%
> overage over the 1,200 soft budget is *accepted* as-is: full flex/canvas + API docs, no trim).
> Canonical contract amendments are recorded in §3.1.1; all gates G1–G6 PASS; kit is NOT yet
> wired into `internal/tui` View().
> **Phase 2a complete** — dialog system verified (23 PASS + goldens, vet/gofmt clean):
> DialogStack / DialogLayout / DialogFooter / DialogSelect / List / Confirm + band-host API
> (§3.1.1 #9). Phase 2b (toasts/tick/spinners) runs against this API.
> **Phase 2b complete** — toasts/tick/spinners verified (21 S2b tests, 71 kit tests total,
> 8 goldens `s2b_*`): ToastHost (top 1/right 2, min(60,w−6) cap, 5 s TTL, hover-pause,
> `› action`), TickDriver (inert without subscribers, self-cancel-safe), braille spinners
> 80/40 ms + 1×1 octant OneCellSpinner (OpenCode port). 781 code LOC vs ~700 (+11.6%)
> accepted. Cross-review fixes applied post-merge: `WithToasts` now forces
> `WithBandCapture(false)` (was inheriting capture=true from the band>Base default — wrong
> for a key-passive surface; S2b's explicit workaround in tests is now the default), and the
> stale §3.2/parity "bottom-left" toast rows are corrected to the §3.0.D top-right spec.
> **Phase 3 complete** — strangler shell landed, all gates PASS (30 packages + `-race`):
> `MYAGENT_UI=kit` (exact value) was the opt-in; legacy was default and byte-identical
> *(historical; Phase 5 flips the default)*. Kit ScrollBox (sticky port, staged items, culling +1 overscan, scrollbar
> column, keys per §3.1.1 #12), transcript blocks as widgets over the untouched block cache,
> composer widget, band wiring with empty stacks (picker migration = S4). Goldens `s3_*`
> (80×24 + 120×40, welcome + transcript). Bench @200×60: streaming 12.1–14.3 ms/event at
> 1.000 compose/event, idle 1.5–3.6 µs/frame at 0 composes. Production 1,463 LOC; bent
> rules ruled in §3.1.1 #11–13. Kit hooks queued to S4: queued initial focus,
> Root HandleKey/HandleMouse entry, scrollbar thumb token (pending approval),
> multi-click selection parity.
> **Phase 4 complete** — chrome parity verified (30 packages + `-race` PASS, S4 files
> gofmt clean): Root entry/focus hooks (§3.1.1 #14), picker migration into DialogStack
> (kit mode only, legacy inline pickers untouched), tabs rail / sidebar / todo block /
> tool-card hover / composer footer one-cell spinner / "Jump to latest ↓" row / one live
> toast emitter, multi-click selection parity. Goldens `s4_chrome_{80x24,120x40}`.
> New-file source 1,164 LOC; overage vs ~1,800 accepted. Live-host caveats: KitTabs and
> KitTodos return nil (no multi-session store / todo event source yet — widgets and wiring
> fully tested, inert live); sidebar hidden below 106 columns. Open into S5:
> scrollbar.thumb approval, tool-card header restyle (kit-mode-only styling constraint),
> legacy retirement audit + cutover (kit becomes default).
> **Phase 5 complete — kit is the default shell (S5).** `MYAGENT_UI` flipped meaning:
> unset and every value except the exact escape hatch `MYAGENT_UI=legacy` select the
> retained kit renderer; `legacy` keeps the pre-cutover frame byte for byte for one
> release. `kit` remains a no-op alias, and `SetKitUIFactory(nil)` still forces the
> legacy fallback. The legacy `View` path was not edited, and the existing S3/S4
> goldens remain unchanged (no new golden was needed for the flag flip). Polish decisions:
> scrollbar.thumb is **resolved-wontfix** (no approval in the launch notes; the thumb
> keeps borrowing `Text.Strong`); the tool-card rounded/title-in-border/traffic-light
> restyle was evaluated and skipped because it fights the block cache —
> `transcript.renderBlock` returns opaque ANSI cached by `(width, expand)`, so a
> border/title row needs a parallel kit-only renderer plus a new measured-height/status
> contract and would break selection row math. The existing `Hoverable` Surface pass
> stays as the kit-mode tool-card treatment.
>
> **Final bench @200×60** (i3-1005G1/Windows; 5 runs each, one streaming outlier
> excluded): streaming 10.23–11.75 ms/event (median 11.53) at **1.000 compose/event**,
> 6.52–6.83 MB/op, 2,378–2,397 allocs/op; idle 4.15–8.31 µs/frame (median 4.42) at
> **0.000 compose/frame**, 1,520 B/op, 4 allocs/op. Tick audit: `TickDriver` is inert
> at zero subscribers (`NextTick() == nil`), pinned by
> `TestTickDriverOnlyRunsWhileSubscribed` and `TestTickDriverGoldenProof`; kit idle
> frames compose nothing. The legacy model's 10 Hz `tickMsg` still schedules for the
> escape hatch and welcome animation, but it does not compose kit frames.
>
> **Legacy retirement audit (not executed; one escape-hatch release remains).**
> Deletion surface ≈1,070 LOC: `internal/tui/hit.go` 80/203 (hitRegion/routeViewY,
> hover state/link lookup), `internal/tui/model.go` ≈947/3,697 (legacy View composition
> 40, mouse/panel routing 260, panel/layout helpers 53, inline picker renderers 475,
> status/footer/context-gauge 84, composer/queued tails 27, hover fields 8),
> `picker_row.go` 16/16, `image_attachments.go` 27/124 (attachment render/byte
> formatting tail). Picker **logic** is retained: `model_picker.go` 123,
> `tools_picker.go` 113, `tools_scope.go` 129 feed the future DialogStack ports.
> Test impact: delete/rewrite ~176 LOC of `mouse_test.go` (legacy inline/hover/wheel
> cases) and ~447 LOC of `model_test.go` (viewport/status/panel/footer/context-gauge
> cases); rewrite panel assertions in `commands_test.go` ~76, `tools_picker_test.go`
> 51, `customize_crash_test.go` 62, `tools_scope_test.go` 21, plus ~52 LOC of
> shared-dispatch mouse cases that move to `kit_mouse_test.go`. Welcome, transcript,
> thinking, selection, theme, and kit tests stay.
>
> **Post-Phase-5:** migrate the remaining inline panels (`/sessions`, `/effort`,
> `/providers`, `/customize`, `/tools`, `/export`, provider-key entry, file mentions)
> to DialogStack; add kit dock rows for image attachments and queued follow-ups; decide
> whether the kit welcome animates (then remove the legacy 10 Hz tick); scrollbar.thumb
> only with token approval.
>
> **Orchestrator acceptance — PORT COMPLETE.** Full suite re-verified (30 packages ok),
> `-race` green on tui/app/kit, cutover semantics confirmed in code
> (`UIKitEnabled() = env != "legacy"`); unported panels keep their legacy inline surface
> inside the kit frame (kit.go picker-bridge docs) — no functional hole on the default
> path, they are simply not DialogStack-styled yet. scrollbar.thumb and the tool-card
> restyle are closed **wontfix** (rationale accepted, §3.1.1 #14). Totals for the port:
> ~6,700 production LOC + ~4,500 test LOC across internal/ui/kit, internal/ui/app and the
> internal/tui bridge; ~18 goldens; final bench streaming median 11.53 ms/event at
> 1.000 compose/event, idle 4.42 µs/frame at 0 composes, TickDriver inert unsubscribed.
> All remaining work is post-5: legacy deletion (≈1,070 LOC, audited above, gated on the
> escape-hatch release), DialogStack panel ports, kit docks, welcome-animation decision.
> **Post-5 (P6) complete — panel ports + kit docks.** Every remaining inline panel
> (`/sessions`, `/effort`, `/providers`, `/customize`, `/tools`, `/export`, provider-key,
> file mentions) routes through DialogStack/Confirm via `internal/tui/kitpanels.go`; the
> kit dock renders attachment + queued-follow-up rows above the composer. New files
> ≈1,335 production + 795 test LOC (overage vs ~1,200 accepted, precedent). Goldens
> `s6_panel_{80x24,120x40}`; **forced-regeneration check by the orchestrator: all 8 app
> goldens byte-identical (SHA-256), 30 packages + `-race` green.** Panel/dock lifecycle
> rules canonized in §3.1.1 #15. Still open: legacy deletion after escape-hatch soak;
> kit welcome animation decision (legacy 10 Hz tick retained — kit itself uses tick
> cadence for footer spinner + toast TTL, so it is not legacy-only).

> Cutover (S5): done. Kit is the default; `MYAGENT_UI=legacy` is the one-release
> strangler escape hatch. The earlier opt-in value (`MYAGENT_UI=kit`) remains a no-op
> alias until the legacy path is deleted.

### Phase 0 — Spike (risks first)
- Bump `lipgloss/v2` → v2.0.6; run a 100-line spike: 2 layers + `Compositor.Hit`,
  verify hit returns ids and click parity in Windows Terminal (and ssh session).
- Verify bubbletea v2 motion events reach us with mouse enabled; measure event noise.
- Prototype the flex subset vs cell-grid rounding from §3.0 and the ScrollBox
  sticky pause/resume behavior (the OpenTUI header-shrink pitfall) before freezing `layout.go`.

### Phase 1 — Kit core (`internal/ui/kit`)
Files: `rect.go`, `widget.go`, `box.go` (Yoga-subset flex per §3.0; `VBox`/`HBox` sugar),
`hitregistry.go`, `focus.go`, `cache.go`, `pane.go`, `label.go` → `text.go`, `root.go`.
- Golden tests: render trees at fixed W×H, assert strings; assert registry rects.

### Phase 2 — Overlays
Files: `dialog.go` (DialogStack + backdrop dimming), `confirm.go`, `list.go`, `toast.go`, `tickdriver.go`.
- Migrate **one** existing picker (model picker) as proof; keep `/` dispatch.

### Phase 3 — Shell + transcript into the kit
Files: `internal/ui/app/shell.go`, lifecycle glue in `internal/tui`.
- `ScrollBox` wraps the existing transcript blocks (cache untouched); terminal
  resize path exercises relayout.
- Delete hand-rolled `hit.go` region table behind the flag; registry takes over.
- Click on a tool block header = expand/collapse (the first "clickablish" win).

### Phase 4 — Chrome parity with OpenCode
This phase lands most of the §3.0 style contract (port priorities 1–13); it is where the
TUI starts looking and behaving like OpenCode rather than working like OpenTUI.
- `Tabs` session rail; `Sidebar` (usage/context stats); `TodoBlock`;
  `Hoverable` tool cards; one-cell spinner in composer; status toasts for
  plugin/agent notices (existing eventbus events).
- Block headers styled OpenCode-like: dim rounded border, title in border, traffic-light
  accent column, elapsed-time "✓ 0.4s" suffix.

### Phase 5 — Polish + perf
- Mouse-mode vs text-selection UX pass; ssh/tmux/Windows Terminal matrix.
- Bench: frames/sec at streaming cadence with 1k+ blocks; tune batching.
- Legacy retirement audited in the Phase 5 status (≈1,070 LOC: `hit.go` hover/region
  tables, legacy View/mouse/panels, picker render rows, attachment render tail). Deletion
  deferred one release behind `MYAGENT_UI=legacy`; picker state/logic is retained for the
  DialogStack ports.

---

## 6. Risks & mitigations

| Risk | Mitigation |
|---|---|
| Hover storms from mouse motion | Only track enter/leave across registry bands; drop intermediate moves. |
| Mouse-mode steals text selection | Shift-drag bypass (like OpenCode); mouse-mode toggle command; selection code already exists. |
| Scroll-in-scroll conflicts | One live ScrollBox rule, enforced at runtime. |
| Compositor cost if used per-widget | Registry for inner widgets; Compositor only for app-level layers (≤3). |
| Windmill animations re-render too much | TickDriver runs only with active animations; spinner/shimmer confined to ≤2 regions. |
| Windows Terminal quirks (hover, click deltas) | Phase 0 spike before committing; selection/mouse already proven in repo. |
| Dependency immaturity (RuneTUI/tuigo/ZeroTUI) | Not used as foundations; kept as design references only. |
| ultraviolet `StyledString.Lines` bug (unbounded strings) | Kit draws into a scratch canvas (see §3.1.1 #8); pinned dep — revisit on bumps. |

---

## 7. Testing strategy

- Layout goldens: `kit` renders deterministic strings (lipgloss v2 styles are values,
  color downsampling happens at output) — assert full frames at fixed sizes without a TTY.
- Hit-table: mouse coords → expected widget ids (pure functions of layout), no TTY.
- DialogStack: priority, Esc-pop, backdrop gating covered as unit tests.
- Existing `transcript_test.go` cache/expand tests remain the contract for blocks.

---

## 8. References

- `docs` on lipgloss layer/canvas + mouse-click detection on layers:
  https://pkg.go.dev/charm.land/lipgloss/v2 and `examples/canvas/main.go` (upstream)
- Charm v2 announcement (Cursed Renderer): https://charm.land/blog/v2
- Crush (charmbracelet) — production Go agent TUI using Layer/Compositor modals:
  https://github.com/charmbracelet/crush
- OpenCode v2 TUI component set for look-and-behavior reference (in this clone):
  `opencode/packages/tui/src/ui/dialog.tsx`, `component/{panel-host,session-frame,session-tabs}.tsx`,
  `component/{todo-item,patch-diff,usage}.tsx`, `ui/toast.tsx`, `component/prompt/*`
- Design-only references (do not adopt): RuneTUI (on-bubbletea declarative kit, early),
  tuigo / retui / ZeroTUI (pre-1.0, own renderers), rivo/tview (mature but wrong aesthetic
  and weak at streaming re-render).


## OpenCode visual parity

The default gotui shell follows OpenCode v2.3.0, checked against the running
upstream binary and source revision `73de67f` (2026-11-16). This is a Go port of
the component composition, not an embedded web view or a dependency on OpenCode.
The myagent wordmark, commands, providers, tools and existing customizations stay
native to myagent.

Source-to-port map (paths below are in `anomalyco/opencode/packages/tui/src`):

| Upstream component | myagent equivalent |
| --- | --- |
| `routes/home.tsx`, `component/logo.tsx` | `internal/tui/welcome.go`, centered home group in `internal/ui/app/shell.go` |
| `component/prompt/index.tsx`, `component/prompt/metadata.tsx` | Raised left-accent composer in `internal/tui/kit.go`; textarea sizing in `internal/tui/model.go` |
| `routes/session/index.tsx`, `routes/session/message-parts.tsx` | Transcript message, thinking and tool renderers in `internal/tui/transcript.go` |
| `ui/dialog.tsx`, `ui/dialog-select.tsx`, `component/command-palette.tsx` | `internal/ui/kit/dialog.go`, `internal/ui/app/picker.go` |
| `theme/assets/v2/opencode.json` | Semantic tokens in `internal/tui/theme.go` |

The home prompt has a 75-cell maximum width and shares its horizontal bounds
with the logo. Session content uses a two-cell outer inset. The prompt's wrap
width is applied before its height is measured, so resizing, a usage rail or a
long draft cannot make the composer overlap the transcript. The original ruled
prompt and alternate welcome styles remain available through `/customize`.

Verification includes exact-cell frame goldens, narrow/wide composer sizing,
home centering and session rail suppression, input/selection interaction tests,
and screenshots of both real CLIs in a 120×40 terminal. The earlier byte-identical
legacy/golden claims above describe the initial gotui migration, before this
intentional visual restyle.

## 2026-11-19 — TUI polish pass

- The shell paints directly into the shared frame, skipping an intermediate
  composition. The 200×60 streaming benchmark allocated about 35% less memory
  per update (9.4 MB → 6.1 MB); cached idle frames still did not recompose.
- Transcript clicks stay aligned with the visible rows after scrolling, and
  scrolling against the viewport edge no longer forces a redraw.
- Short pickers keep the selected choice visible before optional hints or help.
  Narrow tabs clip their click targets to the visible rail, and the composer
  footer keeps file or hint text from overlapping the working directory.

The TUI tests and a live terminal smoke test passed, covering command
navigation, resize, dialog dismissal, and transcript scrolling.
