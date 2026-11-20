package kit

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// SliderOpts configures a Slider.
type SliderOpts struct {
	ID    string
	Stops []string
	Value int
}

// Slider is a host-authoritative horizontal gauge: a plain rail with one tick
// per stop, the selected stop's tick drawn bright and heavy, and a caption that
// rides above that tick with the selected stop's name. There is no fill, no
// frame and no dot; the rail is the whole control.
//
// Keys, commits, and cancellation stay with the owner; the widget only paints
// and maps cells back to stop indices for click selection.
type Slider struct {
	WidgetBase

	// Stops are the selectable positions, left to right.
	Stops []string
	// Value is the selected stop index.
	Value int

	cache RenderCache
}

// NewSlider builds an empty slider. SetState feeds it.
func NewSlider(opts SliderOpts) *Slider {
	id := opts.ID
	if id == "" {
		id = "slider"
	}
	s := &Slider{WidgetBase: NewWidgetBase(id)}
	s.SetState(opts.Stops, opts.Value)
	return s
}

// SetState re-feeds the stops and selection idempotently.
func (s *Slider) SetState(stops []string, value int) {
	value = clampInt(value, 0, max(0, len(stops)-1))
	if s.Value == value && sameStops(s.Stops, stops) {
		return
	}
	s.Stops = append(s.Stops[:0], stops...)
	s.Value = value
	s.MarkDirty()
}

func sameStops(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// PreferredHeight is the caption row plus the rail row.
func (s *Slider) PreferredHeight(_, _ int) int { return 2 }

// trackXW returns the rail's left cell and width inside a w-cell row.
func trackXW(w int) (x, width int) {
	if w >= 12 {
		return 1, w - 2
	}
	return 0, w
}

// sliderPad keeps the end stops off the rail's ends.
const sliderPad = 1

// stopRun returns the cell run the stops travel over inside the rail.
func stopRun(trackX, trackW int) (x, width int) {
	if trackW >= 2*sliderPad+1 {
		return trackX + sliderPad, trackW - 2*sliderPad
	}
	return trackX, trackW
}

// stopCenter returns the cell for stop i inside a stop run.
func stopCenter(i, n, runX, runW int) int {
	if n <= 1 {
		return runX
	}
	return runX + (clampInt(i, 0, n-1)*(runW-1)+(n-1)/2)/(n-1)
}

// IndexAtX maps a widget-local cell to the nearest stop index. It is the click
// hook the owner uses to move the selection.
func (s *Slider) IndexAtX(x int) int {
	n := len(s.Stops)
	if n <= 1 {
		return 0
	}
	trackX, trackW := trackXW(s.Bounds().W)
	runX, runW := stopRun(trackX, trackW)
	if runW <= 1 {
		return 0
	}
	rel := clampInt(x-runX, 0, runW-1)
	return clampInt((rel*(n-1)+(runW-1)/2)/(runW-1), 0, n-1)
}

// Layout records the assigned rect.
func (s *Slider) Layout(r Rect) { s.SetBounds(r) }

// Render paints the caption row above a one-row rail.
func (s *Slider) Render(w, h int) string {
	if !s.Visible || w <= 0 || h <= 0 {
		return ""
	}
	return s.cache.Render(w, h, subtreeRev(s), func() string {
		th := DefaultTheme()
		cv := lipgloss.NewCanvas(w, h)
		n := len(s.Stops)
		if n == 0 {
			return padCanvas(cv, w, h)
		}
		trackX, trackW := trackXW(w)
		if trackW < 1 {
			return padCanvas(cv, w, h)
		}
		value := clampInt(s.Value, 0, n-1)
		runX, runW := stopRun(trackX, trackW)
		thumb := stopCenter(value, n, runX, runW)

		faint := th.Text.Faint.Color()
		for x := trackX; x < trackX+trackW; x++ {
			putGlyph(cv, x, 1, "─", faint)
		}
		for i := 0; i < n; i++ {
			x := stopCenter(i, n, runX, runW)
			if x == thumb {
				putGlyph(cv, x, 1, "┃", th.Text.Strong.Color())
				continue
			}
			putGlyph(cv, x, 1, "│", faint)
		}

		drawSliderCaption(cv, s.Stops[value], thumb, trackX, trackW)
		return padCanvas(cv, w, h)
	})
}

// drawSliderCaption centers the selected stop's name over the thumb, moving it
// back inside the rail at the ends and truncating when the rail is narrow.
func drawSliderCaption(cv *lipgloss.Canvas, note string, thumb, trackX, trackW int) {
	th := DefaultTheme()
	muted := lipgloss.NewStyle().Foreground(th.Text.Muted.Color())
	note = ansi.Truncate(note, max(0, trackW), "…")
	width := ansi.StringWidth(note)
	if note == "" || width == 0 {
		return
	}
	x := clampInt(thumb-(width-1)/2, trackX, max(trackX, trackX+trackW-width))
	drawStyled(cv, x, 0, width, note, muted)
}

// Key is a no-op: the owner's panel handles navigation and commit.
func (s *Slider) Key(tea.KeyPressMsg) bool { return false }

// Mouse is a no-op; owners map clicks through IndexAtX.
func (s *Slider) Mouse(tea.MouseMsg, Rect) bool { return false }

// FocusTarget is nil: the dialog owns the focus stop.
func (s *Slider) FocusTarget() *FocusTarget { return nil }
