package kit

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestLoadingWaveTravelsAndFades(t *testing.T) {
	w := NewLoadingWave()
	if got := w.Width(); got != LoadingWaveCells {
		t.Fatalf("width = %d, want %d", got, LoadingWaveCells)
	}
	w.Layout(Rect{W: LoadingWaveCells, H: 1})
	plai := strings.TrimRight(ansi.Strip(w.Render(LoadingWaveCells, 1)), " ")
	if plai != LoadingWaveGlyph {
		t.Fatalf("first frame = %q, want the head dot only", plai)
	}
	raw := w.Render(LoadingWaveCells, 1)

	if !w.Advance(LoadingWaveInterval) {
		t.Fatal("wave did not advance one cell")
	}
	if w.frame != 1 {
		t.Fatalf("frame = %d, want 1", w.frame)
	}
	if w.Render(LoadingWaveCells, 1) == raw {
		t.Fatal("advance did not repaint the trail")
	}
	if got := strings.TrimRight(ansi.Strip(w.Render(LoadingWaveCells, 1)), " "); strings.Count(got, LoadingWaveGlyph) != 2 {
		t.Fatalf("second frame = %q, want head plus one trail dot", got)
	}
	if w.level(1) != 1 || w.level(0) >= 1 || w.level(2) >= w.level(1) {
		t.Fatalf("wave has no brightness falloff: %v", []float64{w.level(0), w.level(1), w.level(2)})
	}

	w.SetAnimations(false)
	if w.Width() != 1 {
		t.Fatalf("still width = %d, want 1", w.Width())
	}
	if got := ansi.Strip(w.Render(1, 1)); !strings.Contains(got, OneCellStillGlyph) {
		t.Fatalf("still frame = %q, want %q", got, OneCellStillGlyph)
	}
	if w.Advance(time.Second) {
		t.Fatal("still wave advanced")
	}
}
