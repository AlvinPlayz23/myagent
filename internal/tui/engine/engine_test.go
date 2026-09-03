package engine

import (
	"strings"
	"testing"
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
