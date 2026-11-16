package kit

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestSpinnerAdvanceUsesInterval(t *testing.T) {
	s := NewSpinner("build")
	if s.Advance(79 * time.Millisecond) {
		t.Fatal("80ms spinner advanced early")
	}
	if !s.Advance(time.Millisecond) {
		t.Fatal("80ms spinner missed its frame boundary")
	}
	if got := s.Glyph(); got != BrailleFrames[1] {
		t.Fatalf("frame after 80ms = %q", got)
	}

	composer := NewComposerSpinner("build")
	composer.Advance(120 * time.Millisecond)
	if got := composer.Glyph(); got != BrailleFrames[3] {
		t.Fatalf("40ms spinner after 120ms = %q, want frame 3", got)
	}
}

func TestSpinnerIntervals(t *testing.T) {
	if SpinnerInterval != 80*time.Millisecond {
		t.Fatalf("braille interval = %v", SpinnerInterval)
	}
	if NewComposerSpinner("x").Interval != 40*time.Millisecond {
		t.Fatalf("composer interval = %v", NewComposerSpinner("x").Interval)
	}
}

func TestBrailleCycle(t *testing.T) {
	s := NewSpinner("build")
	if got := s.Glyph(); got != BrailleFrames[0] {
		t.Fatalf("first glyph %q", got)
	}
	for i := 1; i < len(BrailleFrames); i++ {
		if !s.Tick() {
			t.Fatal("tick reported no change")
		}
		if got, want := s.Glyph(), BrailleFrames[i]; got != want {
			t.Fatalf("frame %d = %q, want %q", i, got, want)
		}
	}
	s.Tick()
	if s.Glyph() != BrailleFrames[0] {
		t.Fatal("cycle did not wrap")
	}
}

func TestSpinnerStillFallback(t *testing.T) {
	s := NewSpinner("build")
	s.SetAnimations(false)
	if s.Glyph() != SpinnerStillGlyph {
		t.Fatalf("still glyph %q, want %q", s.Glyph(), SpinnerStillGlyph)
	}
	if s.Tick() {
		t.Fatal("still spinner advanced a frame")
	}
	plain := ansi.Strip(s.Render(16, 1))
	if !strings.Contains(plain, SpinnerStillGlyph+" build") {
		t.Fatalf("still frame = %q", plain)
	}
}

func TestOctantGlyph(t *testing.T) {
	cases := []struct {
		mask uint8
		want string
	}{
		{0x00, " "},
		{0x14, "\U0001FBE6"},
		{0x28, "\U0001FBE7"},
		{0xff, "█"},
		{0xa5, "▚"},
	}
	for _, c := range cases {
		if got := octantGlyph(c.mask); got != c.want {
			t.Fatalf("octant %#02x = %q, want %q", c.mask, got, c.want)
		}
	}
	// Masks outside the legacy table use the ascending octant block: 0x15 is
	// the 14th mask once the eight legacy glyphs below it are removed.
	if got := octantGlyph(0x15); got != string(rune(0x1CD00+0x15-8)) {
		t.Fatalf("composed octant = %U", []rune(got)[0])
	}
}

func TestSeedIntroAndFatigue(t *testing.T) {
	seed := SeedMotion()

	if got := OneCellFrame(seed, 0); got.Glyph != OneCellStillGlyph || got.Level < 0.29 || got.Level > 0.31 {
		t.Fatalf("intro start = %+v", got)
	}
	// The intro holds full brightness from 12.5% to 37.5% of its 80 frames.
	if got := OneCellFrame(seed, 1200*time.Millisecond); got.Level < 0.99 {
		t.Fatalf("intro peak level = %.2f", got.Level)
	}
	// 3.2 s is the intro boundary: the main cycle restarts at its low level.
	if got := OneCellFrame(seed, 3200*time.Millisecond); got.Level < 0.29 || got.Level > 0.31 {
		t.Fatalf("post-intro level = %.2f", got.Level)
	}

	fast := seed
	fast.Pace = nil
	delta := func(m OneCellMotion, at time.Duration) int {
		return OneCellFrame(m, at+time.Second).Frame - OneCellFrame(m, at).Frame
	}
	early := delta(seed, 31*time.Second)
	late := delta(seed, 61*time.Second)
	if late >= delta(fast, 61*time.Second) {
		t.Fatalf("fatigue did not slow the cycle: late=%d nominal=%d", late, delta(fast, 61*time.Second))
	}
	if late >= early {
		t.Fatalf("fatigue did not decay: early=%d late=%d", early, late)
	}
}

func TestOneCellSpinnerIsOneCell(t *testing.T) {
	s := NewOneCellSpinner(SeedMotion())
	s.Layout(Rect{X: 5, Y: 5, W: 1, H: 1})
	out := s.Render(1, 1)
	assertFrameSize(t, out, 1, 1)
	if got := ansi.Strip(out); got != OneCellStillGlyph {
		t.Fatalf("1x1 frame = %q", got)
	}
	if s.Level() < 0.29 || s.Level() > 0.31 {
		t.Fatalf("initial level = %.2f", s.Level())
	}
	if !s.Advance(1200 * time.Millisecond) {
		t.Fatal("advance did not report a paint change")
	}
	if s.Level() < 0.99 {
		t.Fatalf("advance did not reach the intro peak: %.2f", s.Level())
	}
	s.SetAnimations(false)
	if s.Glyph() != OneCellStillGlyph || s.Level() != 1 {
		t.Fatalf("still state = %q/%.2f", s.Glyph(), s.Level())
	}
}

func TestSpinnerGoldens(t *testing.T) {
	s := NewSpinner("build")
	var lines []string
	for range BrailleFrames {
		lines = append(lines, strings.TrimRight(ansi.Strip(s.Render(24, 1)), " "))
		s.Tick()
	}
	writeGolden(t, "s2b_spinner_frames.txt", strings.Join(lines, "\n")+"\n")

	seed := SeedMotion()
	sweep := SoftSweepMotion()
	var one []string
	for _, at := range []time.Duration{
		0, 1200 * time.Millisecond, 1600 * time.Millisecond, 2400 * time.Millisecond,
		3200 * time.Millisecond, 5000 * time.Millisecond, 31 * time.Second,
	} {
		st := OneCellFrame(seed, at)
		one = append(one, fmt.Sprintf("seed  t=%-8s glyph=%s level=%.2f", at, st.Glyph, st.Level))
	}
	for i := 0; i < len(sweep.Frames); i += 3 {
		st := OneCellFrame(sweep, time.Duration(i)*sweep.Interval)
		one = append(one, fmt.Sprintf("sweep t=%-8s glyph=%s", time.Duration(i)*sweep.Interval, st.Glyph))
	}
	writeGolden(t, "s2b_onecell_frames.txt", strings.Join(one, "\n")+"\n")
}
