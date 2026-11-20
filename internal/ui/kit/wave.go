package kit

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/AlvinPlayz23/myagent/internal/tui"
)

// LoadingWave is the composer footer's indeterminate animation: a short comet
// of dots that travels left to right beside the cwd. It replaces the single
// pulsing cell so a long turn reads as motion rather than a heartbeat.
const (
	// LoadingWaveCells is the comet's painted width.
	LoadingWaveCells = 5
	// LoadingWaveGlyph is the dot every visible cell of the trail uses.
	LoadingWaveGlyph = "●"
	// LoadingWaveTrail is how many cells stay lit behind the head.
	LoadingWaveTrail = 3
	// LoadingWaveInterval is the step period for the head. It matches the app
	// tick's 100 ms idle cadence so every tick paints a new cell.
	LoadingWaveInterval = 100 * time.Millisecond
)

// loadingWaveSpan is the frame count: the head enters at cell 0, walks past the
// last cell, and the tail clears before the loop restarts. A wrapped trail
// would split into two groups at the edges; the span avoids that.
const loadingWaveSpan = LoadingWaveCells + LoadingWaveTrail - 1

// LoadingWave paints a moving dot trail. Advance drives the clock from the app
// tick; Animations=false holds the shared still block in one cell.
type LoadingWave struct {
	WidgetBase

	Animations bool
	Fg         tui.Token

	elapsed time.Duration
	frame   int
	cache   RenderCache
}

// NewLoadingWave builds a motion-on comet.
func NewLoadingWave() *LoadingWave {
	return &LoadingWave{
		WidgetBase: NewWidgetBase(""),
		Animations: true,
	}
}

// SetAnimations toggles the trail. Disabling it collapses the widget to the
// one-cell still block and leaves the tick loop entirely.
func (w *LoadingWave) SetAnimations(on bool) {
	if w.Animations == on {
		return
	}
	w.Animations = on
	w.MarkDirty()
}

// Width is the painted cell count for the current motion setting.
func (w *LoadingWave) Width() int {
	if !w.Animations {
		return 1
	}
	return LoadingWaveCells
}

// Advance moves the head by one cell per interval and reports a painted change.
func (w *LoadingWave) Advance(d time.Duration) bool {
	if !w.Animations || d <= 0 {
		return false
	}
	w.elapsed += d
	frame := int(w.elapsed/LoadingWaveInterval) % loadingWaveSpan
	if frame == w.frame {
		return false
	}
	w.frame = frame
	w.MarkDirty()
	return true
}

// level returns the dim level for cell i: the head is full strength, the next
// two cells fade, and the rest of the run is blank.
func (w *LoadingWave) level(i int) float64 {
	distance := w.frame - i
	if distance < 0 || distance >= LoadingWaveTrail {
		return 0
	}
	return 1 - 0.35*float64(distance)
}

// Layout records the assigned rect.
func (w *LoadingWave) Layout(r Rect) { w.SetBounds(r) }

// Render paints the trail, or the still block when motion is off.
func (w *LoadingWave) Render(width, h int) string {
	if !w.Visible || width <= 0 || h <= 0 {
		return ""
	}
	return w.cache.Render(width, h, w.Rev(), func() string {
		th := DefaultTheme()
		cv := lipgloss.NewCanvas(width, h)
		if !w.Animations {
			putGlyph(cv, 0, 0, OneCellStillGlyph, tokenOr(w.Fg, th.Agent(0)).Color())
			return padCanvas(cv, width, h)
		}
		head := tokenOr(w.Fg, th.Agent(0)).Color()
		for i := 0; i < min(width, LoadingWaveCells); i++ {
			level := w.level(i)
			if level <= 0 {
				continue
			}
			fg := blendToward(head, th.Background.Base.Color(), 1-level)
			putGlyph(cv, i, 0, LoadingWaveGlyph, fg)
		}
		return padCanvas(cv, width, h)
	})
}

// Key is a no-op.
func (w *LoadingWave) Key(tea.KeyPressMsg) bool { return false }

// Mouse is a no-op.
func (w *LoadingWave) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil.
func (w *LoadingWave) FocusTarget() *FocusTarget { return nil }
