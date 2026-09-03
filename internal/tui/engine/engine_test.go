package engine

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestStyleDiffEmitsMinimalSGR(t *testing.T) {
	base := Style{}.WithFg(RGB(20, 20, 20))
	next := base.WithAttr(AttrBold)
	got := next.diff(base, "")
	if got != "\x1b[1m" {
		t.Fatalf("diff = %q, want bold only", got)
	}
	if off := base.diff(next, ""); off != "\x1b[22m" {
		t.Fatalf("reset diff = %q, want 22", off)
	}
	if same := base.diff(base, ""); same != "" {
		t.Fatalf("identical styles produced %q", same)
	}
}

func TestColorQuantizeNearest(t *testing.T) {
	cases := []struct {
		in   Color
		want int
	}{
		{RGB(0, 0, 0), 16},
		{RGB(255, 255, 255), 231},
		{RGB(20, 20, 20), 232 + 1}, // dark gray ramp
		{RGB(122, 162, 247), 0},    // resolved below by distance check
	}
	if got := cases[3].in.quantize(); got.IsIdx && got.Idx < 16 {
		t.Fatalf("blue quantized to %d, want a cube/gray entry", got.Idx)
	}
	for i, tc := range cases[:3] {
		if got := tc.in.quantize(); !got.IsIdx || got.Idx != tc.want {
			t.Fatalf("case %d: quantize = %d, want %d", i, got.Idx, tc.want)
		}
	}
}

func TestFrameEmitsOnlyChangedCells(t *testing.T) {
	a := NewScreen(10, 3)
	// First flush with nil Prev emits a clear; subsequent frames diff.
	if first := (&Terminal{}).frame(a, -1, -1, false); !strings.Contains(first, "\x1b[2J") {
		t.Fatal("first frame should clear the screen")
	}

	term := &Terminal{Prev: a.Clone()}
	b := a.Clone()
	b.SetString(0, 1, "hi", Style{}.WithFg(RGB(255, 0, 0)))
	out := term.frame(b, -1, -1, false)
	if !strings.Contains(out, "\x1b[2;1H") {
		t.Fatalf("frame missing move to (1,2): %q", out)
	}
	if strings.Count(out, "\x1b[") > 4 {
		t.Fatalf("frame too chatty for a two-cell change: %q", out)
	}
	// Identical frames emit nothing beyond cursor handling.
	term.Prev = b.Clone()
	if again := term.frame(b, -1, -1, false); strings.Contains(again, "\x1b[2;1H") {
		t.Fatalf("unchanged frame repainted: %q", again)
	}
}

func TestFrameEmitsOnlyChangedCellsIncremental(t *testing.T) {
	a := NewScreen(10, 3)
	term := &Terminal{Prev: a}
	b := a.Clone()
	b.SetString(0, 1, "hi", Style{}.WithFg(RGB(255, 0, 0)))
	out := term.frame(b, -1, -1, false)
	if strings.Contains(out, "\x1b[2J") {
		t.Fatal("incremental frame cleared the screen")
	}
}

func TestFrameDoesNotMoveCursorAcrossAdjacentCells(t *testing.T) {
	prev := NewScreen(10, 3)
	term := &Terminal{Prev: prev.Clone(), styleKnown: true, style: Style{}}
	cur := prev.Clone()
	cur.SetString(1, 1, "abc", Style{})

	out := term.frame(cur, -1, -1, false)
	if got := strings.Count(out, "\x1b[2;2H"); got != 1 {
		t.Fatalf("moves to first changed cell = %d, want 1: %q", got, out)
	}
	if strings.Contains(out, "\x1b[2;3H") || strings.Contains(out, "\x1b[2;4H") {
		t.Fatalf("adjacent cells caused redundant cursor moves: %q", out)
	}
}

func TestFrameTracksWideCellCursorAdvance(t *testing.T) {
	prev := NewScreen(10, 1)
	term := &Terminal{Prev: prev.Clone(), styleKnown: true, style: Style{}}
	cur := prev.Clone()
	cur.SetString(0, 0, "世x", Style{})

	out := term.frame(cur, -1, -1, false)
	if !strings.Contains(out, "\x1b[1;1H世x") {
		t.Fatalf("wide glyph and adjacent cell were not emitted contiguously: %q", out)
	}
	if strings.Contains(out, "\x1b[1;3H") {
		t.Fatalf("wide glyph caused an unnecessary cursor move: %q", out)
	}
}

func TestFrameRestoresRemainingIntensityAfterSharedReset(t *testing.T) {
	both := Style{}.Bold().Dim()
	for _, tt := range []struct {
		name string
		next Style
		want string
	}{
		{name: "bold", next: Style{}.Bold(), want: "\x1b[22;1m"},
		{name: "dim", next: Style{}.Dim(), want: "\x1b[22;2m"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prev := NewScreen(2, 1)
			prev.SetString(0, 0, "x", both)
			cur := prev.Clone()
			cur.SetString(0, 0, "x", tt.next)
			term := &Terminal{Prev: prev.Clone(), styleKnown: true, style: both}

			if out := term.frame(cur, -1, -1, false); !strings.Contains(out, tt.want) {
				t.Fatalf("frame = %q, want intensity transition %q", out, tt.want)
			}
			if term.style != tt.next {
				t.Fatalf("tracked style = %#v, want %#v", term.style, tt.next)
			}
		})
	}
}

func TestWrapSpansWordWraps(t *testing.T) {
	spans := []Span{{Text: "the quick brown fox", Style: Style{}}}
	rows := WrapSpans(spans, 10)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	w1 := SpansWidth(rows[0])
	w2 := SpansWidth(rows[1])
	if w1 > 10 || w2 > 10 {
		t.Fatalf("row widths %d/%d exceed 10", w1, w2)
	}
	if SpansWidth(rows[0])+SpansWidth(rows[1]) != 18 {
		t.Fatalf("wrapped text lost characters: %d", SpansWidth(rows[0])+SpansWidth(rows[1]))
	}
}

func TestWrapSpansHardBreaksLongWords(t *testing.T) {
	spans := []Span{{Text: "aaaaaaaaaaaa", Style: Style{}}}
	rows := WrapSpans(spans, 5)
	if len(rows) < 3 {
		t.Fatalf("rows = %d, want hard break into 3+", len(rows))
	}
	for i, row := range rows {
		if SpansWidth(row) > 5 {
			t.Fatalf("row %d width %d exceeds 5", i, SpansWidth(row))
		}
	}
}

func TestDecodeShiftEnter(t *testing.T) {
	d := &Decoder{}
	// Kitty CSI u: ESC [ 13 ; 2 u
	d.buf = []byte("\x1b[13;2u")
	ev, used := d.decodeOne()
	if used != len("\x1b[13;2u") || ev == nil || ev.Key == nil || ev.Key.Code != "shift+enter" {
		t.Fatalf("shift+enter decode = %#v used %d", ev, used)
	}
}

func TestDecodeBracketedPaste(t *testing.T) {
	d := &Decoder{}
	payload := "hello\r\nworld"
	d.buf = []byte("\x1b[200~" + payload + "\x1b[201~")
	ev, used := d.decodeOne()
	if ev == nil || ev.Paste == nil || ev.Paste.Text != "hello\nworld" {
		t.Fatalf("paste decode = %#v", ev)
	}
	if used != len("\x1b[200~"+payload+"\x1b[201~") {
		t.Fatalf("paste consumed %d bytes", used)
	}
}

func TestDecodeSGRMouseWheel(t *testing.T) {
	d := &Decoder{}
	d.buf = []byte("\x1b[<64;10;5M") // wheel up at 10,5
	ev, _ := d.decodeOne()
	if ev == nil || ev.Mouse == nil || ev.Mouse.Action != MouseWheelUp || ev.Mouse.X != 9 || ev.Mouse.Y != 4 {
		t.Fatalf("mouse decode = %#v", ev)
	}
}

func TestDecodeCtrlCAndAltEnter(t *testing.T) {
	d := &Decoder{}
	d.buf = []byte{0x03}
	ev, _ := d.decodeOne()
	if ev.Key == nil || ev.Key.Code != "ctrl+c" {
		t.Fatalf("ctrl+c = %#v", ev)
	}
	d.buf = []byte{0x1b, '\r'}
	ev, _ = d.decodeOne()
	if ev.Key == nil || ev.Key.Code != "alt+enter" {
		t.Fatalf("alt+enter = %#v", ev)
	}
}

func TestDecoderEmitsLoneEscape(t *testing.T) {
	d, write := newPipeDecoder(t, 10*time.Millisecond)
	if _, err := write.Write([]byte{0x1b}); err != nil {
		t.Fatalf("write escape: %v", err)
	}

	select {
	case event := <-d.Events():
		if event.Key == nil || event.Key.Code != "esc" {
			t.Fatalf("lone escape = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("lone escape was not emitted")
	}
}

func TestDecoderWaitsForEscapeSequenceContinuation(t *testing.T) {
	d, write := newPipeDecoder(t, 80*time.Millisecond)
	if _, err := write.Write([]byte{0x1b}); err != nil {
		t.Fatalf("write escape prefix: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := write.Write([]byte("[B")); err != nil {
		t.Fatalf("write down sequence: %v", err)
	}

	select {
	case event := <-d.Events():
		if event.Key == nil || event.Key.Code != "down" {
			t.Fatalf("escape continuation = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("escape continuation was not emitted")
	}

	select {
	case event := <-d.Events():
		t.Fatalf("unexpected extra event after escape sequence: %#v", event)
	case <-time.After(2 * d.escDelay):
	}
}

func TestDecoderIgnoresStaleEscapeTimer(t *testing.T) {
	d := &Decoder{out: make(chan Event, 1), escDelay: time.Hour}
	d.mu.Lock()
	d.buf = []byte{0x1b}
	d.scheduleEscLocked()
	staleEpoch := d.escEpoch
	d.stopEscTimerLocked()
	d.buf = []byte{0x1b}
	d.scheduleEscLocked()
	currentEpoch := d.escEpoch
	currentTimer := d.escTimer
	d.mu.Unlock()
	currentTimer.Stop()

	d.resolveEscTimer(staleEpoch)
	select {
	case event := <-d.Events():
		t.Fatalf("stale timer emitted %#v", event)
	default:
	}

	d.resolveEscTimer(currentEpoch)
	select {
	case event := <-d.Events():
		if event.Key == nil || event.Key.Code != "esc" {
			t.Fatalf("current timer emitted %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("current timer did not emit escape")
	}
}

func TestDecoderSeparatesInputAfterEscapeDeadline(t *testing.T) {
	d := &Decoder{out: make(chan Event, 2)}
	d.mu.Lock()
	d.buf = []byte{0x1b, 'a'}
	d.escReady = true
	d.mu.Unlock()
	d.drain()

	first := <-d.Events()
	if first.Key == nil || first.Key.Code != "esc" {
		t.Fatalf("resolved escape = %#v", first)
	}
	second := <-d.Events()
	if second.Key == nil || second.Key.Code != "rune" || second.Key.Text != "a" {
		t.Fatalf("input after resolved escape = %#v", second)
	}
}

func TestDecoderChecksEscapeDeadlineBeforeTimerCallback(t *testing.T) {
	d := &Decoder{
		out:         make(chan Event, 2),
		buf:         []byte{0x1b, 'a'},
		escDeadline: time.Now().Add(-time.Millisecond),
	}
	d.drain()

	first := <-d.Events()
	if first.Key == nil || first.Key.Code != "esc" {
		t.Fatalf("expired escape = %#v", first)
	}
	second := <-d.Events()
	if second.Key == nil || second.Key.Code != "rune" || second.Key.Text != "a" {
		t.Fatalf("input after expired escape = %#v", second)
	}
}

func newPipeDecoder(t *testing.T, delay time.Duration) (*Decoder, *os.File) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	d := &Decoder{out: make(chan Event, 4), escDelay: delay}
	go d.readLoop(read)
	t.Cleanup(func() {
		_ = write.Close()
		_ = read.Close()
	})
	return d, write
}

func TestSetStringWideRunes(t *testing.T) {
	s := NewScreen(10, 1)
	end := s.SetString(0, 0, "世x", Style{})
	if end != 3 {
		t.Fatalf("end = %d, want 3 (2 wide + 1)", end)
	}
	if s.CellAt(0, 0).Width != 2 || s.CellAt(1, 0).Width != 0 {
		t.Fatalf("wide cell layout wrong: %+v %+v", s.CellAt(0, 0), s.CellAt(1, 0))
	}
}

func TestTruncateSpansAppendsEllipsis(t *testing.T) {
	spans := []Span{{Text: "abcdef", Style: Style{}}}
	got := TruncateSpans(spans, 4)
	if SpansWidth(got) != 4 || !strings.HasSuffix(got[len(got)-1].Text, "…") {
		t.Fatalf("truncate = %#v", got)
	}
}
