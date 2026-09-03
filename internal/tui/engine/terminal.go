package engine

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	"github.com/charmbracelet/x/term"
)

// Terminal owns the tty: raw mode, alternate screen, mouse tracking, and the
// cell diff that turns a Screen into ANSI output on every flush.
type Terminal struct {
	in         *os.File
	out        *bufio.Writer
	outMu      sync.Mutex
	oldTio     *term.State
	Prev       *Screen // last flushed frame (nil = full repaint)
	styleKnown bool
	style      Style
	// Mouse enables SGR mouse tracking before entering the alt screen.
	Mouse bool
}

// OpenTerminal prepares the controlling terminal for raw, unbuffered use.
func OpenTerminal() (*Terminal, error) {
	if runtime.GOOS == "windows" {
		return &Terminal{in: os.Stdin, out: bufio.NewWriterSize(os.Stdout, 64*1024)}, nil
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("no controlling terminal: %w", err)
	}
	t := &Terminal{in: tty, out: bufio.NewWriterSize(tty, 64*1024)}
	return t, nil
}

// Input returns the tty read end for the input decoder.
func (t *Terminal) Input() *os.File { return t.in }

// Enter switches to the alternate screen, hides the cursor, and enables the
// requested tracking modes.
func (t *Terminal) Enter() {
	var b strings.Builder
	b.WriteString("\x1b[?1049h") // alt screen
	b.WriteString("\x1b[?1004h") // focus reporting
	if t.Mouse {
		b.WriteString("\x1b[?1000h\x1b[?1002h\x1b[?1006h") // SGR mouse + drag
	}
	b.WriteString("\x1b[?25l") // hide cursor
	fmt.Fprint(t.out, b.String())
	t.out.Flush()
	t.styleKnown = false
}

// Leave restores the primary screen and disables tracking modes.
func (t *Terminal) Leave() {
	var b strings.Builder
	b.WriteString("\x1b[?1004l")
	if t.Mouse {
		b.WriteString("\x1b[?1000l\x1b[?1002l\x1b[?1006l")
	}
	b.WriteString("\x1b[?25h") // show cursor
	fmt.Fprint(t.out, b.String())
	t.out.Flush()
}

// Flush paints cur onto the terminal, emitting only cells that differ from
// the previously flushed frame. It positions the cursor afterwards when the
// caller asked for one (cx, cy >= 0), otherwise leaves it hidden.
func (t *Terminal) Flush(cur *Screen, cx, cy int, showCursor bool) {
	fmt.Fprint(t.out, t.frame(cur, cx, cy, showCursor))
	t.out.Flush()
	t.Prev = cur.Clone()
}

// frame renders the ANSI byte sequence that moves Prev to cur.
func (t *Terminal) frame(cur *Screen, cx, cy int, showCursor bool) string {
	var b strings.Builder
	if t.Prev == nil {
		b.WriteString("\x1b[2J") // clear once so stale content never bleeds
	}
	blank := Style{}
	last := t.style
	if !t.styleKnown {
		b.WriteString("\x1b[0m")
		last = Style{Fg: Def(), Bg: Def()}
		t.styleKnown = true
	}
	movePending := false
	cursorX, cursorY := -1, -1
	for y := 0; y < cur.H; y++ {
		for x := 0; x < cur.W; x++ {
			cell := cur.Cells[y*cur.W+x]
			var prev Cell
			if t.Prev != nil && x < t.Prev.W && y < t.Prev.H {
				prev = t.Prev.Cells[y*t.Prev.W+x]
			} else {
				prev = Cell{Ch: ' ', Width: 1, Style: blank}
			}
			if cell == prev {
				movePending = true
				continue
			}
			if movePending || cursorX != x || cursorY != y {
				fmt.Fprintf(&b, "\x1b[%d;%dH", y+1, x+1)
				movePending = false
			}
			if cell.Style != last {
				b.WriteString(cell.Style.diff(last, ""))
				last = cell.Style
			}
			if cell.Ch == 0 {
				b.WriteRune(' ')
			} else {
				b.WriteRune(cell.Ch)
			}
			width := max(1, cell.Width)
			cursorX, cursorY = x+width, y
			if cell.Width > 1 {
				x += cell.Width - 1
			}
		}
	}
	// Cursor: hide, or show at (cx, cy).
	if showCursor && cx >= 0 && cy >= 0 && cx < cur.W && cy < cur.H {
		fmt.Fprintf(&b, "\x1b[%d;%dH\x1b[?25h", cy+1, cx+1)
	} else {
		b.WriteString("\x1b[?25l")
	}
	t.style = last
	return b.String()
}

// Resize propagates a new size to both frames so the next flush repaints.
func (t *Terminal) Resize(w, h int) {
	if t.Prev != nil && (t.Prev.W != w || t.Prev.H != h) {
		t.Prev = nil
	}
}

// Raw switches the tty into raw mode.
func (t *Terminal) Raw() error {
	state, err := term.MakeRaw(t.in.Fd())
	if err != nil {
		return err
	}
	t.oldTio = state
	return nil
}

// Restore returns the tty to its saved modes and shows the cursor.
func (t *Terminal) Restore() {
	if t.oldTio != nil {
		_ = term.Restore(t.in.Fd(), t.oldTio)
	}
	fmt.Fprint(t.out, "\x1b[?25h\x1b[?1004l\x1b[?1000l\x1b[?1002l\x1b[?1006l\x1b[?1049l")
	t.out.Flush()
}

// TermSize returns the tty window size in cells.
func TermSize(f *os.File) (int, int) {
	w, h, err := term.GetSize(f.Fd())
	if err != nil || w == 0 || h == 0 {
		return 80, 24
	}
	return w, h
}

// Clone deep-copies the screen.
func (s *Screen) Clone() *Screen {
	c := &Screen{W: s.W, H: s.H, Cells: make([]Cell, len(s.Cells)), CursorVisible: s.CursorVisible, CursorX: s.CursorX, CursorY: s.CursorY}
	copy(c.Cells, s.Cells)
	return c
}

// Equals compares two screens cell by cell.
func (s *Screen) Equals(o *Screen) bool {
	if s.W != o.W || s.H != o.H {
		return false
	}
	for i := range s.Cells {
		if s.Cells[i] != o.Cells[i] {
			return false
		}
	}
	return true
}

var _ = fmt.Sprint // keep fmt for diagnostics in tests
