//go:build unix

package ptytest

import (
	"strings"
	"unicode/utf8"
)

// Screen is a small ANSI terminal emulator that projects PTY output onto a
// plain-text grid, so tests can assert on what is visible without pixel
// inspection. It implements the subset of VT100/xterm sequences Bubble Tea's
// renderer emits: cursor addressing, erase display/line, insert/delete of
// lines and characters, scroll regions, deferred autowrap, and skipping of
// SGR/OSC/DCS and private-mode sequences.
type Screen struct {
	width, height int
	cells         [][]rune

	row, col    int
	wrapPending bool
	top, bottom int // DECSTBM scroll region, 0-based inclusive
	savedRow    int
	savedCol    int

	state   parseState
	params  []int
	prefix  byte
	inter   []byte
	pending []byte // incomplete UTF-8 carried between Feed calls
}

type parseState int

const (
	stateGround parseState = iota
	stateEsc
	stateCSI
	stateOSC
	stateStr
	stateStrEsc
	stateCharset
)

// NewScreen returns a blank screen of the given size.
func NewScreen(width, height int) *Screen {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	s := &Screen{width: width, height: height}
	s.resetGrid()
	return s
}

func (s *Screen) resetGrid() {
	s.cells = make([][]rune, s.height)
	for i := range s.cells {
		s.cells[i] = s.blankRow()
	}
	s.top, s.bottom = 0, s.height-1
	s.row, s.col = 0, 0
	s.wrapPending = false
	s.savedRow, s.savedCol = 0, 0
}

func (s *Screen) blankRow() []rune {
	row := make([]rune, s.width)
	for i := range row {
		row[i] = ' '
	}
	return row
}

func (s *Screen) Width() int  { return s.width }
func (s *Screen) Height() int { return s.height }

// Text returns the visible screen as plain text: rows joined by newlines,
// trailing blanks trimmed.
func (s *Screen) Text() string {
	var sb strings.Builder
	for i, row := range s.cells {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(strings.TrimRight(string(row), " "))
	}
	return sb.String()
}

// Resize reallocate the grid for a new terminal size, preserving the
// top-left content.
func (s *Screen) Resize(width, height int) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	if width == s.width && height == s.height {
		return
	}
	old := s.cells
	oldW, oldH := s.width, s.height
	s.width, s.height = width, height
	s.cells = make([][]rune, height)
	for i := range s.cells {
		s.cells[i] = s.blankRow()
	}
	for i := 0; i < min(oldH, height); i++ {
		copy(s.cells[i], old[i][:min(oldW, width)])
	}
	s.top, s.bottom = 0, height-1
	s.clampCursor()
	s.wrapPending = false
}

// Feed interprets p as terminal output, updating the grid.
func (s *Screen) Feed(p []byte) {
	buf := p
	if len(s.pending) > 0 {
		buf = append(s.pending, p...)
		s.pending = s.pending[:0]
	}
	for i := 0; i < len(buf); {
		r, size := utf8.DecodeRune(buf[i:])
		if r == utf8.RuneError && size == 1 {
			if s.state == stateGround && !utf8.FullRune(buf[i:]) {
				s.pending = append(s.pending, buf[i:]...)
				return
			}
			i++ // drop invalid byte
			continue
		}
		i += size
		s.handleRune(r)
	}
}

func (s *Screen) handleRune(r rune) {
	switch s.state {
	case stateGround:
		s.ground(r)
	case stateEsc:
		s.esc(r)
	case stateCSI:
		s.csiCollect(byte(r))
	case stateOSC, stateStr:
		switch r {
		case 0x1b:
			s.state = stateStrEsc
		case 0x07:
			s.state = stateGround
		}
	case stateStrEsc:
		if r == '\\' {
			s.state = stateGround
		} else {
			s.state = stateEsc
			s.esc(r)
		}
	case stateCharset:
		s.state = stateGround
	}
}

func (s *Screen) ground(r rune) {
	switch r {
	case '\r':
		s.col = 0
		s.wrapPending = false
	case '\n', '\v', '\f':
		s.index()
	case '\b':
		if s.col > 0 {
			s.col--
		}
		s.wrapPending = false
	case '\t':
		s.col = min((s.col/8+1)*8, s.width-1)
		s.wrapPending = false
	case 0x1b:
		s.state = stateEsc
	case 0x00, 0x07:
		// ignore
	default:
		s.print(r)
	}
}

func (s *Screen) esc(r rune) {
	s.state = stateGround
	switch r {
	case '[':
		s.state = stateCSI
		s.params = s.params[:0]
		s.prefix = 0
		s.inter = s.inter[:0]
	case ']':
		s.state = stateOSC
	case 'P', 'X', '^', '_':
		s.state = stateStr
	case '(', ')', '*', '+':
		s.state = stateCharset
	case '7':
		s.savedRow, s.savedCol = s.row, s.col
	case '8':
		s.row, s.col = s.savedRow, s.savedCol
		s.clampCursor()
		s.wrapPending = false
	case 'D':
		s.index()
	case 'E':
		s.col = 0
		s.index()
	case 'M':
		s.reverseIndex()
	case 'c':
		s.resetGrid()
	}
	// '=', '>', and unknown escapes are ignored.
}

func (s *Screen) csiCollect(b byte) {
	switch {
	case b >= '0' && b <= '9':
		if len(s.params) == 0 {
			s.params = append(s.params, 0)
		}
		s.params[len(s.params)-1] = s.params[len(s.params)-1]*10 + int(b-'0')
	case b == ';' || b == ':':
		s.params = append(s.params, 0)
	case b >= 0x3c && b <= 0x3f: // private markers (?, >, <, =)
		s.prefix = b
	case b >= 0x20 && b <= 0x2f: // intermediates
		s.inter = append(s.inter, b)
	case b >= 0x40 && b <= 0x7e:
		s.state = stateGround
		s.csiDispatch(b)
	default:
		s.state = stateGround // malformed; resynchronize
	}
}

// csiDispatch applies one final CSI byte. n returns param i, or def when the
// parameter is missing or zero; raw returns param i as sent (0 allowed).
func (s *Screen) csiDispatch(fin byte) {
	n := func(i, def int) int {
		if i < len(s.params) && s.params[i] > 0 {
			return s.params[i]
		}
		return def
	}
	raw := func(i int) int {
		if i < len(s.params) {
			return s.params[i]
		}
		return 0
	}
	switch fin {
	case 'H', 'f':
		s.row = n(0, 1) - 1
		s.col = n(1, 1) - 1
		s.clampCursor()
		s.wrapPending = false
	case 'A':
		s.row -= n(0, 1)
		s.clampCursor()
		s.wrapPending = false
	case 'B':
		s.row += n(0, 1)
		s.clampCursor()
		s.wrapPending = false
	case 'C':
		s.col += n(0, 1)
		s.clampCursor()
		s.wrapPending = false
	case 'D':
		s.col -= n(0, 1)
		s.clampCursor()
		s.wrapPending = false
	case 'E':
		s.row += n(0, 1)
		s.clampCursor()
		s.col = 0
		s.wrapPending = false
	case 'F':
		s.row -= n(0, 1)
		s.clampCursor()
		s.col = 0
		s.wrapPending = false
	case 'G', '`':
		s.col = n(0, 1) - 1
		s.clampCursor()
		s.wrapPending = false
	case 'd':
		s.row = n(0, 1) - 1
		s.clampCursor()
		s.wrapPending = false
	case 'J':
		s.eraseDisplay(raw(0))
	case 'K':
		s.eraseLine(raw(0))
	case 'L':
		s.insertLines(n(0, 1))
	case 'M':
		s.deleteLines(n(0, 1))
	case '@':
		s.insertChars(n(0, 1))
	case 'P':
		s.deleteChars(n(0, 1))
	case 'X':
		s.eraseChars(n(0, 1))
	case 'S':
		s.scrollUp(n(0, 1))
	case 'T':
		s.scrollDown(n(0, 1))
	case 'r':
		top := n(0, 1) - 1
		bottom := n(1, s.height) - 1
		if top < 0 {
			top = 0
		}
		if bottom >= s.height {
			bottom = s.height - 1
		}
		if bottom > top {
			s.top, s.bottom = top, bottom
		} else {
			s.top, s.bottom = 0, s.height-1
		}
		s.row, s.col = 0, 0
		s.wrapPending = false
	case 's':
		s.savedRow, s.savedCol = s.row, s.col
	case 'u':
		s.row, s.col = s.savedRow, s.savedCol
		s.clampCursor()
		s.wrapPending = false
	}
	// 'm' (SGR), 'h'/'l' (modes), and query finals ('n', 'c', 't', 'q') are
	// deliberately ignored.
}

func (s *Screen) clampCursor() {
	s.row = max(0, min(s.row, s.height-1))
	s.col = max(0, min(s.col, s.width-1))
}

func (s *Screen) print(r rune) {
	if r < ' ' {
		return
	}
	if s.wrapPending {
		s.wrapPending = false
		s.col = 0
		s.index()
	}
	if s.row < 0 || s.row >= s.height || s.col < 0 || s.col >= s.width {
		return
	}
	s.cells[s.row][s.col] = r
	s.col++
	if s.col >= s.width {
		s.col = s.width - 1
		s.wrapPending = true
	}
}

func (s *Screen) index() {
	s.wrapPending = false
	if s.row == s.bottom {
		s.scrollUp(1)
		return
	}
	if s.row < s.height-1 {
		s.row++
	}
}

func (s *Screen) reverseIndex() {
	if s.row == s.top {
		s.scrollDown(1)
		return
	}
	if s.row > 0 {
		s.row--
	}
}

func (s *Screen) scrollUp(n int) {
	if n <= 0 {
		return
	}
	n = min(n, s.bottom-s.top+1)
	for i := s.top; i+n <= s.bottom; i++ {
		s.cells[i] = s.cells[i+n]
	}
	for i := s.bottom - n + 1; i <= s.bottom; i++ {
		s.cells[i] = s.blankRow()
	}
}

func (s *Screen) scrollDown(n int) {
	if n <= 0 {
		return
	}
	n = min(n, s.bottom-s.top+1)
	for i := s.bottom; i-n >= s.top; i-- {
		s.cells[i] = s.cells[i-n]
	}
	for i := s.top; i < s.top+n; i++ {
		s.cells[i] = s.blankRow()
	}
}

func (s *Screen) eraseDisplay(mode int) {
	switch mode {
	case 0:
		s.eraseLine(0)
		for i := s.row + 1; i < s.height; i++ {
			s.cells[i] = s.blankRow()
		}
	case 1:
		for i := 0; i < s.row; i++ {
			s.cells[i] = s.blankRow()
		}
		s.eraseLine(1)
	case 2, 3:
		for i := range s.cells {
			s.cells[i] = s.blankRow()
		}
	}
}

func (s *Screen) eraseLine(mode int) {
	if s.row < 0 || s.row >= s.height {
		return
	}
	row := s.cells[s.row]
	switch mode {
	case 0:
		for i := s.col; i < s.width; i++ {
			row[i] = ' '
		}
	case 1:
		for i := 0; i <= s.col; i++ {
			row[i] = ' '
		}
	case 2:
		for i := range row {
			row[i] = ' '
		}
	}
}

func (s *Screen) insertLines(n int) {
	if s.row < s.top || s.row > s.bottom {
		return
	}
	n = min(n, s.bottom-s.row+1)
	for i := s.bottom; i >= s.row+n; i-- {
		s.cells[i] = s.cells[i-n]
	}
	for i := s.row; i < s.row+n; i++ {
		s.cells[i] = s.blankRow()
	}
}

func (s *Screen) deleteLines(n int) {
	if s.row < s.top || s.row > s.bottom {
		return
	}
	n = min(n, s.bottom-s.row+1)
	for i := s.row; i+n <= s.bottom; i++ {
		s.cells[i] = s.cells[i+n]
	}
	for i := s.bottom - n + 1; i <= s.bottom; i++ {
		s.cells[i] = s.blankRow()
	}
}

func (s *Screen) insertChars(n int) {
	if s.row < 0 || s.row >= s.height {
		return
	}
	n = min(n, s.width-s.col)
	row := s.cells[s.row]
	copy(row[s.col+n:], row[s.col:])
	for i := s.col; i < s.col+n; i++ {
		row[i] = ' '
	}
}

func (s *Screen) deleteChars(n int) {
	if s.row < 0 || s.row >= s.height {
		return
	}
	n = min(n, s.width-s.col)
	row := s.cells[s.row]
	copy(row[s.col:], row[s.col+n:])
	for i := s.width - n; i < s.width; i++ {
		row[i] = ' '
	}
}

func (s *Screen) eraseChars(n int) {
	if s.row < 0 || s.row >= s.height {
		return
	}
	end := min(s.col+n, s.width)
	row := s.cells[s.row]
	for i := s.col; i < end; i++ {
		row[i] = ' '
	}
}
