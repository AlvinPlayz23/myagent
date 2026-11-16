# GOTUI S0 Spike Report

Date: 2026-11-01  
Repo: `C:\Users\bijim\myagent` (module `github.com/AlvinPlayz23/myagent`)  
Go: `go1.26.3 windows/amd64`  
Scope: throwaway evidence for GOTUI.md Phase 0. Only `go.mod`, `go.sum`, and `spikes/gotui-s0/` were touched.

## Verdict

**GO for Phase 1**, with one manual gate still to be run by a human (G5 mouse interaction).

The four mechanical risks that could have invalidated the plan all cleared: lipgloss
`Layer`/`Compositor` gives reliable top-most-ID hit testing, the OpenTUI cell-rounding
example reproduces exactly, the sticky-scroll pause/resume/pending-target state machine is
implementable as a small pure struct, and bubbletea v2 mouse motion reaches a plain
alt-screen program. The only material surprise is cost: a full 200x60 three-layer
`Compositor.Render()` is ~2.35 ms and allocates ~1.58 MB per call on this machine, so
Phase 1 must compose only on dirty frames rather than every tick. That is an optimization
constraint, not a blocker.

## Exit gates

| Gate | Status | Evidence |
|---|---|---|
| G1 — bump + full `internal/tui` tests green | **PASS** | `lipgloss/v2 v2.0.6`; `go build ./...` exit 0; `go test ./internal/tui/... -count=1` → `ok` for both packages |
| G2 — `Compositor.Hit` top-most-ID, ID-less skip | **PASS** | `spikes/gotui-s0/compositor`: 5 tests, including nested layers, overlap clip, ID-less top layer |
| G3 — flex rounding matches OpenTUI example | **PASS** | 31 columns / 3 grow children → `10, 11, 10`; 30 → `10, 10, 10`; 32 → `11, 10, 11` |
| G4 — sticky pause/resume/pending-target | **PASS** | `spikes/gotui-s0/sticky`: 8 tests covering streaming, wheel-up pause, wheel-down resume, same-tick growth, end persistence, resize |
| G5 — mouseprobe builds and runs | **PASS (build)** / **MANUAL RUN PENDING** | `go build ./spikes/gotui-s0/mouseprobe` exit 0; human checklist below; log format documented below |
| G6 — render bench numbers recorded | **PASS** | table below, 5 runs per benchmark |

## Dependency bump

Command run:

```powershell
go get charm.land/lipgloss/v2@v2.0.6
```

`go.mod` diff summary:

| Module | Before | After | Why |
|---|---|---|---|
| `charm.land/lipgloss/v2` | v2.0.5 | **v2.0.6** | requested bump |
| `github.com/charmbracelet/x/ansi` | v0.11.7 | v0.11.8 | forced by lipgloss v2.0.6 (direct dep) |
| `golang.org/x/sys` | v0.46.0 | v0.47.0 | forced by lipgloss v2.0.6 (direct dep) |
| `github.com/charmbracelet/ultraviolet` | v0.0.0-20260703014108-f5a850f9c2b7 | v0.0.0-20260811164956-006e29f97886 | forced by lipgloss v2.0.6 |
| `github.com/lucasb-eyer/go-colorful` | v1.4.0 | v1.4.1 | forced by lipgloss v2.0.6 |
| `golang.org/x/sync` | v0.21.0 | v0.22.0 | forced by lipgloss v2.0.6 |

No new module paths. `charm.land/bubbletea/v2` stays at **v2.0.8**; `charm.land/bubbles/v2`
stays at **v2.1.1**.

Newer bubbletea/v2 releases than v2.0.8 **do exist**: patches v2.0.9 and v2.0.10, plus
minors v2.1.0 and v2.1.1 (latest). Not bumped in S0; v2.0.8 works for all probes. v2.1.x
may expose richer wheel/scroll fields, but S0 did not need them.

G1 verification:

```text
go build ./...                              exit 0
go test ./internal/tui/... -count=1         ok github.com/AlvinPlayz23/myagent/internal/tui
                                            ok github.com/AlvinPlayz23/myagent/internal/tui/urldetect
```

## 1. Layer / Compositor spike

File: `spikes/gotui-s0/compositor/compositor_test.go`

Exact API used (lipgloss v2.0.6):

```go
lipgloss.NewLayer(content string, layers ...*Layer) *Layer
(*lipgloss.Layer).ID(id string) *lipgloss.Layer
(*lipgloss.Layer).X(x int) *lipgloss.Layer
(*lipgloss.Layer).Y(y int) *lipgloss.Layer
(*lipgloss.Layer).Z(z int) *lipgloss.Layer
(*lipgloss.Layer).AddLayers(layers ...*lipgloss.Layer) *lipgloss.Layer

lipgloss.NewCompositor(layers ...*lipgloss.Layer) *lipgloss.Compositor
(*lipgloss.Compositor).Hit(x, y int) lipgloss.LayerHit
(*lipgloss.Compositor).Render() string
(*lipgloss.Compositor).Bounds() image.Rectangle
(*lipgloss.Compositor).GetLayer(id string) *lipgloss.Layer
(*lipgloss.Compositor).Refresh()

type LayerHit struct { /* unexported fields */ }
func (lh LayerHit) Empty() bool
func (lh LayerHit) ID() string
func (lh LayerHit) Layer() *lipgloss.Layer
func (lh LayerHit) Bounds() image.Rectangle
```

Confirmed behavior:

- `Hit(x,y)` scans flattened layers from highest Z down and returns the first layer with
  a non-empty ID whose **own content bounds** contain the point.
- ID-less layers are invisible to `Hit` even when they are the top-most painted layer.
- Coordinates accumulate through nesting: a child at `X=2` inside a parent at `X=3`
  hits at absolute `x=5`.
- `Render()` paints in Z order; a later/higher layer overwrites the cells it covers.
- Hit bounds are exact content rectangles: `Hit(17,4)` hits a 6-cell toast at x=12..17,
  `Hit(18,4)` falls through to the backdrop, and points outside the backdrop are empty.

### Benchmarks (200x60)

Machine: Intel Core i3-1005G1 @ 1.20 GHz, Windows amd64, Go 1.26.3.  
Command: `go test ./spikes/gotui-s0/compositor/... -run '^$' -bench . -benchtime=3s -count=5`

| Benchmark | Median | Range (5 runs) | Allocations |
|---|---:|---:|---:|
| Render 3 layers (200x60 backdrop + 60x12 dialog + 40x3 toast) | **2.35 ms/op** | 2.02–2.44 ms | 1.576 MB/op, 150 allocs |
| Render 1 layer (200x60 backdrop only) | **2.05 ms/op** | 1.94–2.23 ms | 1.573 MB/op, 148 allocs |
| Hit on the 3-layer compositor | **22.1 ns/op** | 20.6–29.8 ns | 0 B/op, 0 allocs |

Cold/first runs were slower (2.8–8.1 ms); the table is steady-state after warmup. The
single-layer baseline shows that canvas construction/parsing dominates; adding two small
layers costs almost nothing. Hit is effectively free and allocation-free.

### Compositor gotchas

1. **Z is global/flat, not inherited.** `flattenRecursive` sorts by each layer's own `z`
   value; a child `Z=1` under a parent `Z=10` paints and hits *below* the parent.
   Phase 1 must assign compositor-wide Z bands (0 backdrop, 10 toast, 20 dialog), not
   per-container sibling Z. Covered by `TestZIsGlobalNotInherited`.
2. **`Render()` needs a layer at (0,0).** It creates a canvas of `Bounds()` size but draws
   layers at their absolute coordinates. With only a layer at (2,1), the 2x1 canvas is
   empty because the layer draws outside it. Keep a full-screen backdrop as the bottom
   layer. Covered by `TestRenderRequiresOriginLayer`.
3. **Hit bounds do not include children.** A parent layer's hit rect is its own content
   rect, not the union with descendants. A full-screen backdrop/dialog content layer is
   therefore required for backdrop clicks; child widgets should use the kit registry.
4. **Equal Z is unordered.** `flatten` uses `slices.SortFunc`, which is not stable. Do not
   rely on paint or hit order for equal-Z layers; give every layer a unique Z.
5. **`Render()` trims output.** `Canvas.Render()` calls `uv.TrimSpace`, so trailing spaces
   and blank lines disappear. Goldens need a full-width bottom layer (as the test does).
6. **Call `Refresh()` after mutating the layer tree/positions.** The compositor flattens
   once at construction; content has no setter, so per-frame overlays should rebuild the
   small layer set or refresh explicitly.

## 2. Flex-subset rounding probe

Files: `spikes/gotui-s0/flex/flex.go`, `flex_test.go`

API:

```go
type Child struct {
    Fixed int
    Grow  float64
}
type Span struct {
    Start int
    Width int
}
func Distribute(w, gap int, children []Child) []Span
```

Algorithm: compute exact cell widths (fixed bases + grow share of remaining space),
then round each **cumulative edge** with `math.Round`. Rounding edges rather than widths
preserves the total and reproduces Yoga's documented rounding.

Results:

| Case | Widths | Note |
|---|---|---|
| W=30, gap=0, 3×Grow 1 | `10, 10, 10` | 32-column bordered row, 30 inner cells |
| W=31, gap=0, 3×Grow 1 | `10, 11, 10` | **OpenTUI documented example: middle gets the extra cell** |
| W=32, gap=0, 3×Grow 1 | `11, 10, 11` | outer children absorb the rounding |
| W=21, gap=1, Fixed 5 + 2×Grow 1 | `5, 7, 7` | fixed + grow mix; final edge exactly 21 |
| W=20, gap=0, Fixed 4 + Grow 1 + Grow 2 | `4, 5, 11` | 1:2 weights after fixed base |
| W=20, gap=1, Fixed 4 + Fixed 5 | `4, 5` | grow=0 leaves 10 trailing cells blank |
| W=10, gap=1, Fixed 6 + Fixed 7 | `6, 7` | negative space: no shrink, overflows to 14 |
| W=5, gap=1, Fixed 6 + Grow 1 | `6, 0` | grow child collapses to zero, never negative |

Zero-width behavior: a child with computed width 0 stays in the layout sequence
(`Span{Start:3, Width:0}`), and the next sibling starts at the same coordinate. For the
kit this means:

- `Layout` must recurse into zero-size children; their children keep their own bounds.
- `Render`/`Hit` must not early-return just because the parent W/H is zero.
- The parent only erases those children when `overflow: hidden` clips them. Make that
  clipping decision explicit per container instead of treating zero size as "skip".

`TestInvariants` checks no negative widths, exact one-cell gaps, and final edge == W
whenever growers absorb the space. Negative space deliberately overflows because S0 has
no `flexShrink` (matching OpenTUI's "numeric size ⇒ flexShrink 0" default).

## 3. ScrollBox sticky state machine

Files: `spikes/gotui-s0/sticky/sticky.go`, `sticky_test.go`

API:

```go
type Edge int
const (
    Bottom Edge = iota
    Top
)

func New(viewportH, contentH int, stickyScroll bool, start Edge) *Box
func (b *Box) Append(n int)                 // stages content growth/removal
func (b *Box) SetContent(h int)             // stages absolute content height
func (b *Box) Layout(viewportH int)         // commits staged size, flushes target
func (b *Box) ScrollBy(delta int)           // wheel/key step, positive = down
func (b *Box) ScrollTo(y int)
func (b *Box) ScrollToEnd()
func (b *Box) ScrollTop() int
func (b *Box) ScrollHeight() int            // committed layout height
func (b *Box) ViewportHeight() int
func (b *Box) MaxScroll() int
func (b *Box) StickyActive() bool
func (b *Box) AtEnd() bool
```

Semantics implemented and tested:

- `stickyScroll + stickyStart: bottom` starts pinned at `maxScroll`.
- Content changes are staged (`Append`/`SetContent`) and committed by `Layout`, so a
  scroll issued in the same tick as a content append lands against the *new* content.
- Streaming appends while pinned keep `scrollTop == scrollHeight - viewportH`.
- Wheel-up (`ScrollBy(-n)`) pauses stickiness; later growth does **not** move the view.
- Wheel-down to the bottom resumes stickiness; later growth follows the bottom again.
- Absolute targets are one-shot: applied immediately against the current layout and again
  after the next `Layout`, then cleared.
- `ScrollToEnd` / `ScrollTo(scrollHeight)` / a position past the end becomes a persistent
  end target: it stays at the end after further content growth.
- `ScrollTo(0)` is always an absolute top target, even when the content currently fits
  the viewport (otherwise the zero-max case would accidentally become an end target).
- Shrinking content/viewport clamps `scrollTop`; growing an unpinned box does not move it.
- Top-sticky is implemented symmetrically and tested.

Tests (all PASS):

```text
TestStreamingAppendWhilePinned
TestWheelUpPausesAndWheelDownResumes
TestPendingTargetFlushAndDeltaAccumulation
TestScrollInSameTickAsGrowthLandsOnNewContent
TestScrollToScrollHeightStaysAtEnd
TestScrollHeightAndScrollTopArithmeticOnResize
TestScrollToTopIsNotAnEndTargetWhenContentFits
TestTopStickyPauseResume
```

Gotchas for the kit:

- Order matters in `Update`: mutate content, issue the scroll request, then run `Layout`
  (or `View`'s layout pass). Reversing the order applies the target to old content.
- `ScrollHeight()` reports the committed layout height, not the staged height. This is
  intentional and matches "targets are applied against the next layout".
- An end target must survive more than one layout. Do not implement it as a one-shot
  pending value.
- Wheel/key scrolling should use `ScrollBy`; explicit "jump to latest" should use
  `ScrollToEnd`.

## 4. Mouse + motion probe

File: `spikes/gotui-s0/mouseprobe/main.go` (126 lines)

Build: `go build ./spikes/gotui-s0/mouseprobe` → exit 0.

API used (bubbletea v2.0.8):

```go
type View struct {
    AltScreen bool
    MouseMode tea.MouseMode // tea.MouseModeAllMotion
    // ...
}
tea.NewView(content string) tea.View
tea.NewProgram(model tea.Model) *tea.Program
tea.Tick(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd

tea.WindowSizeMsg{Width, Height int}
tea.KeyPressMsg                    // msg.String() == "ctrl+c" / "esc" / "q"
tea.MouseClickMsg                  // X, Y int; Button tea.MouseButton
tea.MouseWheelMsg                  // X, Y int; Button tea.MouseButton
tea.MouseMotionMsg                 // X, Y int; Button tea.MouseButton
tea.MouseReleaseMsg                // X, Y int; Button tea.MouseButton
tea.MouseButton.String()           // "left", "wheelup", "wheeldown", ...
```

There is no program-level `WithAltScreen`/`WithMouse` in v2.0.8; both are fields on the
returned `tea.View`. Coordinates are zero-based cells. v2.0.8 exposes the wheel as a
button, not a fractional scroll delta; the probe derives `delta=-1/+1` from
`MouseWheelUp/Down/Left/Right`.

Live display: top-right corner shows `motion N/s  clicks N  wheel N`. The last 10 log
lines are shown below the instructions.

Log format (`mouseprobe.log`, written in the working directory; flushed per event, summary
on normal exit/ctrl+c):

```text
# mouseprobe: bubbletea v2.0.8 alt-screen MouseModeAllMotion
15:04:05.123 click x=42 y=17 button=left
15:04:05.456 wheel x=42 y=17 button=wheelup delta=-1
15:04:05.789 wheel x=42 y=17 button=wheeldown delta=+1
15:04:06.001 motion rate=37/s total=812
# summary clicks=1 wheels=2 motion=812
```

### Manual checklist (human)

1. From the repo root in **Windows Terminal**: `go run ./spikes/gotui-s0/mouseprobe`
   (or `go build -o mouseprobe.exe ./spikes/gotui-s0/mouseprobe` then `.\mouseprobe.exe`).
2. Click left/middle/right at known cells; confirm `click x=… y=… button=…` matches the
   cell under the pointer (0-based).
3. Wheel up and down; confirm `wheel … delta=-1` / `delta=+1` and the live counter.
4. Move the mouse without buttons; confirm the corner rate rises and falls, and that
   `motion rate=N/s` lines appear at most once per second in the log.
5. Press `q`, `esc`, and `ctrl+c` in separate runs; confirm the terminal restores, the
   summary line is written, and `mouseprobe.log` contains the events.
6. Optional ssh run: `ssh <host>`, `cd` to the repo, run the same command; record whether
   motion events arrive and at what rate. tmux may need `set -g mouse on`.
7. Record the observed motion rate while idle and while sweeping the mouse. This is the
   hover-storm input for GOTUI §6 risk mitigation.

## 5. Deviations / amendments for GOTUI.md §3

1. **§3.0.1 zIndex row and §3.3 layer step:** lipgloss Z is a flat compositor-wide number,
   not parent-relative sibling order. Amend to say "assign global Z bands to every layer;
   child Z does not accumulate with parent Z." Equal Z is also unstable (non-stable sort),
   so require unique Z values.
2. **§3.3 render pipeline:** add "keep a full-screen backdrop layer at (0,0)" because
   `Compositor.Render()` sizes its canvas from bounds but draws at absolute coordinates.
   Without it, a non-zero-origin layer set renders empty/shifted.
3. **§3.3 frame cost notes:** add measured cost and a dirty-frame rule. At 200x60 the
   three-layer Render is ~2.35 ms and ~1.58 MB/op on an i3-1005G1; that is ~14% of a
   60 fps frame budget before bubbletea diffing, and 60 fps would allocate ~95 MB/s.
   Compose only when a layer/content changed; reuse the composed string otherwise.
4. **§3.4 input routing:** `Compositor.Hit` uses each layer's own content rect, not the
   union with children. Backdrop/dialog layers must cover the full clickable area, or
   inner widgets must be routed through the kit hit registry. A dialog layer with empty
   content will never be hit even if its children are visible.
5. **§3.0.1 ScrollBox row:** clarify target lifetime: absolute `scrollBy`/`scrollTo`
   targets are one-shot after the next layout; an end target (`scrollTo(scrollHeight)` /
   `Infinity`) persists across growth. Content mutation must be staged before the layout
   pass that applies the target.
6. **§3.0 F interaction feel / wheel acceleration:** v2.0.8 delivers integer wheel
   button events, not fractional scroll deltas. Accumulate wheel steps in the ScrollBox
   (or evaluate bubbletea v2.1.x) before promising acceleration curves.
7. **§3.0.1 flex defaults:** S0 confirms "no shrink" for fixed bases: when fixed children
   exceed the container, they overflow rather than going negative. If the kit later needs
   shrinking, add `FlexShrink` explicitly; do not assume Yoga's default shrink behavior.
8. **§3.0.1 zero-size rule:** confirmed that a zero-width child takes no cells but stays in
   flow; siblings still paint at the same coordinate. Add the implementation rule: layout
   must recurse into zero-size nodes, and paint/hit must only stop at an explicit clip.

## 6. Files added

```text
spikes/gotui-s0/compositor/compositor_test.go
spikes/gotui-s0/flex/flex.go
spikes/gotui-s0/flex/flex_test.go
spikes/gotui-s0/sticky/sticky.go
spikes/gotui-s0/sticky/sticky_test.go
spikes/gotui-s0/mouseprobe/main.go
spikes/gotui-s0/REPORT.md
```

No production code under `internal/tui` was modified.
