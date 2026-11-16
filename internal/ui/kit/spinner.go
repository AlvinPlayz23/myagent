package kit

import (
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// Motion timings and fallback glyphs from GOTUI.md §3.0.E.
const (
	// SpinnerInterval is the braille frame period (composer footer: 40 ms).
	SpinnerInterval         = 80 * time.Millisecond
	ComposerSpinnerInterval = 40 * time.Millisecond
	// SpinnerStillGlyph is the §3.0.E no-motion fallback for a labelled
	// spinner: "⋯ {label}".
	SpinnerStillGlyph = "⋯"
	// OneCellStillGlyph is the default still block for the 1×1 spinner.
	OneCellStillGlyph = "▪"
)

// BrailleFrames is the §3.0.B braille cycle.
var BrailleFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner is the labelled text spinner: a braille frame, a space, then the
// label. Animations=false freezes it to "⋯ label" — the §3.0.E fallback — and
// Tick becomes a no-op so an idle app never repaints it.
//
// OpenTUI counterpart: <spinner frames interval>. OpenCode counterpart: the
// spinner branch of component/spinner.tsx.
type Spinner struct {
	WidgetBase

	Label      string
	Frames     []string
	Interval   time.Duration
	Animations bool
	Fg         tui.Token

	frame   int
	elapsed time.Duration
	cache   RenderCache
}

// NewSpinner builds an 80 ms braille spinner with the default accent color.
func NewSpinner(label string) *Spinner {
	return &Spinner{
		WidgetBase: NewWidgetBase(""),
		Label:      label,
		Frames:     BrailleFrames,
		Interval:   SpinnerInterval,
		Animations: true,
	}
}

// NewComposerSpinner builds the 40 ms footer variant.
func NewComposerSpinner(label string) *Spinner {
	s := NewSpinner(label)
	s.Interval = ComposerSpinnerInterval
	return s
}

// SetLabel mutates the label idempotently.
func (s *Spinner) SetLabel(label string) {
	if s.Label == label {
		return
	}
	s.Label = label
	s.MarkDirty()
}

// SetAnimations toggles motion; the cached frame is dropped either way so the
// fallback paints immediately.
func (s *Spinner) SetAnimations(on bool) {
	if s.Animations == on {
		return
	}
	s.Animations = on
	s.MarkDirty()
}

// Tick advances by one nominal interval. It returns false (and dirties nothing)
// when motion is disabled, which is what keeps a still spinner out of the tick
// loop.
func (s *Spinner) Tick() bool {
	period := s.Interval
	if period <= 0 {
		period = SpinnerInterval
	}
	return s.Advance(period)
}

// Advance consumes elapsed time and advances as many frames as the interval
// allows, keeping the remainder for the next call. This lets a 15–20 fps
// TickDriver drive an 80 ms (or 40 ms composer) spinner without drifting.
func (s *Spinner) Advance(d time.Duration) bool {
	if !s.Animations || len(s.Frames) == 0 || d <= 0 {
		return false
	}
	period := s.Interval
	if period <= 0 {
		period = SpinnerInterval
	}
	s.elapsed += d
	steps := int(s.elapsed / period)
	if steps == 0 {
		return false
	}
	s.elapsed %= period
	s.frame = (s.frame + steps) % len(s.Frames)
	s.MarkDirty()
	return true
}

// Glyph is the current frame, or the still fallback.
func (s *Spinner) Glyph() string {
	if !s.Animations || len(s.Frames) == 0 {
		return SpinnerStillGlyph
	}
	return s.Frames[s.frame%len(s.Frames)]
}

// Layout records the assigned rect; the spinner paints one row.
func (s *Spinner) Layout(r Rect) { s.SetBounds(r) }

// Render paints "<glyph> <label>" clipped to the rect.
func (s *Spinner) Render(w, h int) string {
	if !s.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return s.cache.Render(w, h, s.Rev(), func() string {
		th := DefaultTheme()
		fg := tokenOr(s.Fg, th.Text.Action.Primary)
		text := s.Glyph()
		if s.Label != "" {
			text += " " + s.Label
		}
		style := lipgloss.NewStyle().Foreground(fg.Color())
		return fitBlock(style.Render(text), w, h)
	})
}

// Key is a no-op: a spinner has no input behavior.
func (s *Spinner) Key(tea.KeyPressMsg) bool { return false }

// Mouse is a no-op; owners decide whether a spinner is clickable.
func (s *Spinner) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil: a spinner is decoration.
func (s *Spinner) FocusTarget() *FocusTarget { return nil }

// OneCellPace is the fatigue easing: the motion starts at Initial speed, decays
// toward Final after After, over Duration. Both speeds are multiples of the
// nominal frame rate.
// OpenCode: OneCellMotion.pace in ui/one-cell-motion.ts.
type OneCellPace struct {
	Initial  float64
	Final    float64
	After    time.Duration
	Duration time.Duration
}

// OneCellMotion is one 1×1 subcell animation: frames plus an optional intro
// sequence, per-frame dim levels, a one-shot mode, and fatigue pacing.
// OpenCode: OneCellMotion (ui/one-cell-motion.ts).
type OneCellMotion struct {
	Frames   []string
	Interval time.Duration
	Levels   []float64
	Intro    *OneCellMotion
	Once     bool
	Pace     *OneCellPace
}

// OneCellState is the resolved frame for an elapsed duration. Frame is the
// absolute frame count (it keeps growing across cycles); Index is the position
// inside the current cycle.
type OneCellState struct {
	Glyph    string
	Level    float64
	Frame    int
	Index    int
	Complete bool
}

// octantTable maps the documented 2×4 masks onto the legacy glyphs Unicode
// reuses. Masks outside the table use the ascending octant block, exactly like
// OpenCode's subcell.ts.
var octantTable = map[uint8]string{
	0x00: " ",
	0x01: "\U0001CEA8",
	0x02: "\U0001CEAB",
	0x03: "\U0001FB82",
	0x05: "▘",
	0x0a: "▝",
	0x0f: "▀",
	0x14: "\U0001FBE6",
	0x28: "\U0001FBE7",
	0x3f: "\U0001FB85",
	0x40: "\U0001CEA3",
	0x50: "▖",
	0x55: "▌",
	0x5a: "▞",
	0x5f: "▛",
	0x80: "\U0001CEA0",
	0xa0: "▗",
	0xa5: "▚",
	0xaa: "▐",
	0xaf: "▜",
	0xc0: "▂",
	0xf0: "▄",
	0xf5: "▙",
	0xfa: "▟",
	0xfc: "▆",
	0xff: "█",
}

// octantGlyph composes one cell from a row-major 2×4 bitmask.
func octantGlyph(mask uint8) string {
	if g, ok := octantTable[mask]; ok {
		return g
	}
	below := 0
	for m := range octantTable {
		if m < mask {
			below++
		}
	}
	return string(rune(0x1CD00 + int(mask) - below))
}

// OneCellFrame resolves a motion at elapsed time. The pacing integral is ported
// from OpenCode so slowing down never jumps or restarts the cycle.
func OneCellFrame(m OneCellMotion, elapsed time.Duration) OneCellState {
	introDur := time.Duration(0)
	if m.Intro != nil && len(m.Intro.Frames) > 0 {
		introDur = time.Duration(len(m.Intro.Frames)) * m.Intro.Interval
	}
	intro := elapsed < introDur
	clip := m
	if intro {
		clip = *m.Intro
	}
	n := len(clip.Frames)
	if n == 0 || clip.Interval <= 0 {
		return OneCellState{}
	}

	ms := func(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }
	t := ms(elapsed)
	if !intro {
		t -= ms(introDur)
		if m.Pace != nil {
			pace := m.Pace
			progress := 0.0
			if pace.Duration > 0 {
				progress = math.Max(0, math.Min(1, ms(elapsed-pace.After)/ms(pace.Duration)))
			}
			t = t*pace.Initial +
				(pace.Final-pace.Initial)*
					(ms(pace.Duration)*(math.Pow(progress, 3)-math.Pow(progress, 4)/2)+
						math.Max(0, ms(elapsed-pace.After-pace.Duration)))
		}
	}

	frame := int(math.Floor(t / ms(clip.Interval)))
	index := frame % n
	if !intro && m.Once && frame > n-1 {
		index = n - 1
	}
	level := 1.0
	if index < len(clip.Levels) {
		level = clip.Levels[index]
	}
	return OneCellState{
		Glyph:    clip.Frames[index],
		Level:    level,
		Frame:    frame,
		Index:    index,
		Complete: !intro && m.Once && frame >= n-1,
	}
}

// SeedMotion is OpenCode's SEED work pattern: a 40-frame dim cycle with an
// 80-frame intro ramp and a 30 s fatigue decay from 1.25× to 0.5× speed.
// OpenCode: SEED_WORK in ui/one-cell-motion.ts.
func SeedMotion() OneCellMotion {
	const frames = 40
	levels := make([]float64, frames)
	glyphs := make([]string, frames)
	for i := 0; i < frames; i++ {
		glyphs[i] = OneCellStillGlyph
		levels[i] = 0.3 + 0.7*(1-math.Cos(float64(i)/frames*2*math.Pi))/2
	}

	const introFrames = 80
	introGlyphs := make([]string, introFrames)
	introLevels := make([]float64, introFrames)
	for i := 0; i < introFrames; i++ {
		introGlyphs[i] = OneCellStillGlyph
		phase := float64(i) / introFrames
		switch {
		case phase < 0.125:
			introLevels[i] = 0.3 + 0.7*(phase/0.125)
		case phase < 0.375:
			introLevels[i] = 1
		default:
			introLevels[i] = 0.3 + 0.7*math.Pow((1-phase)/0.625, 3)
		}
	}

	return OneCellMotion{
		Frames:   glyphs,
		Interval: 40 * time.Millisecond,
		Levels:   levels,
		Intro: &OneCellMotion{
			Frames:   introGlyphs,
			Interval: 40 * time.Millisecond,
			Levels:   introLevels,
		},
		Pace: &OneCellPace{
			Initial:  1.25,
			Final:    0.5,
			After:    30 * time.Second,
			Duration: 30 * time.Second,
		},
	}
}

// SoftSweepMotion is one of the composed octant work patterns: a 0x14/0x1c/
// 0x3c/0x38/0x28 sweep at 100 ms.
// OpenCode: BLOCK_SOFT_SWEEP in ui/one-cell-motion.ts.
func SoftSweepMotion() OneCellMotion {
	masks := []uint8{0x14, 0x14, 0x14, 0x1c, 0x3c, 0x38, 0x28, 0x28, 0x28, 0x38, 0x3c, 0x1c}
	frames := make([]string, len(masks))
	for i, m := range masks {
		frames[i] = octantGlyph(m)
	}
	return OneCellMotion{
		Frames:   frames,
		Interval: 100 * time.Millisecond,
		Pace: &OneCellPace{
			Initial:  1.2,
			Final:    0.96,
			After:    30 * time.Second,
			Duration: 30 * time.Second,
		},
	}
}

// OneCellSpinner is the 1×1 subcell spinner. It paints exactly one cell whose
// glyph comes from the motion (SEED by default) and whose color dims with the
// frame level. Animations=false holds the still block.
//
// OpenTUI counterpart: a 1×1 box around a spinner. OpenCode counterpart:
// component/one-cell-spinner.tsx.
type OneCellSpinner struct {
	WidgetBase

	Motion     OneCellMotion
	Animations bool
	Fg         tui.Token

	elapsed time.Duration
	state   OneCellState
	cache   RenderCache
}

// NewOneCellSpinner builds the spinner for a motion; the zero motion falls back
// to SEED.
func NewOneCellSpinner(m OneCellMotion) *OneCellSpinner {
	if len(m.Frames) == 0 {
		m = SeedMotion()
	}
	if m.Interval <= 0 {
		m.Interval = 40 * time.Millisecond
	}
	s := &OneCellSpinner{
		WidgetBase: NewWidgetBase(""),
		Motion:     m,
		Animations: true,
	}
	s.state = OneCellFrame(m, 0)
	return s
}

// SetAnimations toggles motion. Disabling it paints the still block and makes
// Advance a no-op, so the widget leaves the tick loop entirely.
func (s *OneCellSpinner) SetAnimations(on bool) {
	if s.Animations == on {
		return
	}
	s.Animations = on
	s.MarkDirty()
}

// Advance moves the animation clock and returns true when the painted state
// changed. The caller drives it from the TickDriver.
func (s *OneCellSpinner) Advance(d time.Duration) bool {
	if !s.Animations || d <= 0 {
		return false
	}
	s.elapsed += d
	next := OneCellFrame(s.Motion, s.elapsed)
	changed := next.Glyph != s.state.Glyph || next.Level != s.state.Level
	s.state = next
	if changed {
		s.MarkDirty()
	}
	return changed
}

// Glyph is the current frame, or the still block when motion is off.
func (s *OneCellSpinner) Glyph() string {
	if !s.Animations {
		return OneCellStillGlyph
	}
	return s.state.Glyph
}

// Level is the frame's dim level in [0,1].
func (s *OneCellSpinner) Level() float64 {
	if !s.Animations {
		return 1
	}
	return s.state.Level
}

// Layout records the assigned rect; owners give it exactly 1×1.
func (s *OneCellSpinner) Layout(r Rect) { s.SetBounds(r) }

// Render paints the single cell, dimmed by the frame level.
func (s *OneCellSpinner) Render(w, h int) string {
	if !s.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return s.cache.Render(w, h, s.Rev(), func() string {
		th := DefaultTheme()
		fg := tokenOr(s.Fg, th.Text.Action.Primary).Color()
		if level := s.Level(); level > 0 && level < 1 {
			fg = blendToward(fg, th.Background.Base.Color(), 1-level)
		}
		cv := lipgloss.NewCanvas(w, h)
		if g := s.Glyph(); g != "" {
			putGlyph(cv, 0, 0, g, fg)
		}
		return padCanvas(cv, w, h)
	})
}

// Key is a no-op.
func (s *OneCellSpinner) Key(tea.KeyPressMsg) bool { return false }

// Mouse is a no-op.
func (s *OneCellSpinner) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil.
func (s *OneCellSpinner) FocusTarget() *FocusTarget { return nil }
