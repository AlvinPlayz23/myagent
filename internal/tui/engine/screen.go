package engine

import (
	"github.com/mattn/go-runewidth"
)

// Rect is a screen-space rectangle.
type Rect struct {
	X, Y, W, H int
}

// Cell is one screen cell. Wide runes occupy their own cell plus a following
// spacer cell (Width 2, Ch 0).
type Cell struct {
	Ch    rune
	Width int
	Style Style
}

// Screen is a rectangular cell buffer that tracks damage for diffed output.
type Screen struct {
	W, H  int
	Cells []Cell
	// cursor visibility and position requested by the last frame.
	CursorX, CursorY int
	CursorVisible    bool
}

// NewScreen allocates a blank screen.
func NewScreen(w, h int) *Screen {
	s := &Screen{W: w, H: h, Cells: make([]Cell, w*h)}
	s.Fill(Rect{0, 0, w, h}, Style{})
	return s
}

// Resize re-allocates the buffer for a new size.
func (s *Screen) Resize(w, h int) {
	if w == s.W && h == s.H {
		return
	}
	s.W, s.H = w, h
	s.Cells = make([]Cell, w*h)
	s.Fill(Rect{0, 0, w, h}, Style{})
}

func (s *Screen) idx(x, y int) int { return y*s.W + x }

// InBounds reports whether the point lies inside the screen.
func (s *Screen) InBounds(x, y int) bool { return x >= 0 && y >= 0 && x < s.W && y < s.H }

// CellAt returns the cell at a point (zero value out of bounds).
func (s *Screen) CellAt(x, y int) Cell {
	if !s.InBounds(x, y) {
		return Cell{}
	}
	return s.Cells[s.idx(x, y)]
}

// Fill paints an area with the style, leaving glyphs intact.
func (s *Screen) Fill(r Rect, st Style) {
	for y := r.Y; y < r.Y+r.H && y < s.H; y++ {
		for x := r.X; x < r.X+r.W && x < s.W; x++ {
			if x < 0 || y < 0 {
				continue
			}
			s.Cells[s.idx(x, y)].Style = st
		}
	}
}

// Clear wipes an area to blank cells with the style.
func (s *Screen) Clear(r Rect, st Style) {
	for y := r.Y; y < r.Y+r.H && y < s.H; y++ {
		for x := r.X; x < r.X+r.W && x < s.W; x++ {
			if x < 0 || y < 0 {
				continue
			}
			s.Cells[s.idx(x, y)] = Cell{Ch: ' ', Width: 1, Style: st}
		}
	}
}

// SetCell writes one cell if in bounds.
func (s *Screen) SetCell(x, y int, c Cell) {
	if !s.InBounds(x, y) {
		return
	}
	s.Cells[s.idx(x, y)] = c
}

// SetString writes a string starting at x,y with the style, returning the x
// position just past the last written cell. Wide runes claim two columns and
// are clipped at the right edge.
func (s *Screen) SetString(x, y int, str string, st Style) int {
	for _, r := range str {
		if x >= s.W {
			break
		}
		if r == '\n' {
			break
		}
		w := runewidth.RuneWidth(r)
		if w == 0 {
			w = 0
		}
		if w > 1 && x+w > s.W {
			break
		}
		if w > 0 {
			s.SetCell(x, y, Cell{Ch: r, Width: w, Style: st})
			if w > 1 {
				s.SetCell(x+1, y, Cell{Width: 0, Style: st})
			}
		}
		x += w
	}
	return x
}

// Span is a styled fragment of text on one line.
type Span struct {
	Text  string
	Style Style
}

// SpanBuilder accumulates spans fluently.
type SpanBuilder struct{ Spans []Span }

// Add appends a span.
func (b *SpanBuilder) Add(text string, st Style) {
	if text != "" {
		b.Spans = append(b.Spans, Span{Text: text, Style: st})
	}
}

// Line writes spans starting at x,y and returns the x past the last cell.
func (s *Screen) Line(x, y int, spans []Span) int {
	for _, sp := range spans {
		x = s.SetString(x, y, sp.Text, sp.Style)
	}
	return x
}

// SpansWidth returns the display width of a span list.
func SpansWidth(spans []Span) int {
	w := 0
	for _, sp := range spans {
		w += runewidth.StringWidth(sp.Text)
	}
	return w
}

// TruncateSpans clips spans to width, appending ellipsis when it cuts.
func TruncateSpans(spans []Span, width int) []Span {
	if width <= 0 || SpansWidth(spans) <= width {
		return spans
	}
	out := make([]Span, 0, len(spans))
	used := 0
	budget := width - 1
	for _, sp := range spans {
		rem := budget - used
		if rem <= 0 {
			break
		}
		w := runewidth.StringWidth(sp.Text)
		if w <= rem {
			out = append(out, sp)
			used += w
			continue
		}
		out = append(out, Span{Text: runewidth.Truncate(sp.Text, rem, ""), Style: sp.Style})
		used = budget
		break
	}
	return append(out, Span{Text: "…", Style: spans[len(spans)-1].Style.Dim()})
}

// WrapSpans wraps a span list to width, returning one span list per row.
// Word-wraps on spaces; hard-breaks tokens longer than the width.
func WrapSpans(spans []Span, width int) [][]Span {
	if width <= 0 {
		return [][]Span{spans}
	}
	rows := make([][]Span, 0, 4)
	row := make([]Span, 0, 8)
	rowW := 0

	pushWord := func(word []Span, wordW int) {
		if len(rows) == 0 && len(row) == 0 && wordW > width {
			// Hard-break oversized words.
			for _, sp := range word {
				for _, r := range sp.Text {
					rw := runewidth.RuneWidth(r)
					if rowW+rw > width && rowW > 0 {
						rows = append(rows, row)
						row = make([]Span, 0, 8)
						rowW = 0
					}
					row = appendSpan(row, row, string(r), sp.Style)
					rowW += rw
				}
			}
			return
		}
		if rowW > 0 && rowW+wordW > width {
			rows = append(rows, row)
			row = make([]Span, 0, 8)
			rowW = 0
			// Trim leading spaces on the new row.
			for len(word) > 0 && word[0].Text == " " {
				word = word[1:]
				wordW--
			}
		}
		row = append(row, word...)
		rowW += wordW
	}

	// Split into words (runs of non-space separated by single spaces), keeping
	// span styles intact.
	var word []Span
	wordW := 0
	flushWord := func() {
		if len(word) == 0 {
			return
		}
		pushWord(word, wordW)
		word = nil
		wordW = 0
	}
	for _, sp := range spans {
		start := 0
		for i, r := range sp.Text {
			if r == ' ' {
				if i > start {
					word = append(word, Span{Text: sp.Text[start:i], Style: sp.Style})
					wordW += runewidth.StringWidth(sp.Text[start:i])
				}
				flushWord()
				word = append(word, Span{Text: " ", Style: sp.Style})
				wordW++
				start = i + len(string(r))
			}
		}
		if start < len(sp.Text) {
			word = append(word, Span{Text: sp.Text[start:], Style: sp.Style})
			wordW += runewidth.StringWidth(sp.Text[start:])
		}
	}
	flushWord()
	if len(row) > 0 || len(rows) == 0 {
		rows = append(rows, row)
	}
	return rows
}

func appendSpan(row, _ []Span, text string, st Style) []Span {
	return append(row, Span{Text: text, Style: st})
}
