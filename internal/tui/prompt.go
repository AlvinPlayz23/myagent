package tui

import (
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
	"github.com/mattn/go-runewidth"
)

// promptWidget is the composer editor: a text buffer with cursor, word
// operations, prompt history, and a placeholder. It renders into the bordered
// composer box drawn by the app.
type promptWidget struct {
	lines []string // logical lines
	row   int      // cursor line
	col   int      // cursor rune index within the line

	history      []string
	historyLimit int
	historyIdx   int // -1 = editing a fresh draft
	draft        string

	placeholder string
	multiline   bool // ctrl+m toggle: enter inserts newline until off
}

func newPromptWidget() *promptWidget {
	return &promptWidget{lines: []string{""}, historyLimit: promptHistoryLimit, historyIdx: -1, placeholder: "Type a message…"}
}

// value returns the full text.
func (p *promptWidget) value() string { return strings.Join(p.lines, "\n") }

// setValue replaces the text and puts the cursor at the end.
func (p *promptWidget) setValue(s string) {
	if s == "" {
		p.lines = []string{""}
		p.row, p.col = 0, 0
		return
	}
	p.lines = strings.Split(s, "\n")
	p.row = len(p.lines) - 1
	p.col = len([]rune(p.lines[p.row]))
}

// runeCol is the cursor column in display cells.
func (p *promptWidget) runeCol() int {
	line := []rune(p.lines[p.row])
	return runewidth.StringWidth(string(line[:p.col]))
}

// insertString inserts text at the cursor, splitting on newlines.
func (p *promptWidget) insertString(s string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.Split(s, "\n")
	for i, part := range parts {
		if i > 0 {
			p.splitLineAtCursor()
		}
		if part == "" {
			continue
		}
		line := []rune(p.lines[p.row])
		at := p.col
		inserted := []rune(part)
		newLine := string(line[:at]) + part + string(line[at:])
		p.lines[p.row] = newLine
		p.col = at + len(inserted)
	}
}

func (p *promptWidget) splitLineAtCursor() {
	line := []rune(p.lines[p.row])
	head := string(line[:p.col])
	tail := string(line[p.col:])
	p.lines[p.row] = head
	p.lines = append(p.lines[:p.row+1], append([]string{tail}, p.lines[p.row+1:]...)...)
	p.row++
	p.col = 0
}

// backspace deletes the rune before the cursor (or merges lines).
func (p *promptWidget) backspace() {
	if p.col > 0 {
		line := []rune(p.lines[p.row])
		p.lines[p.row] = string(line[:p.col-1]) + string(line[p.col:])
		p.col--
		return
	}
	if p.row > 0 {
		prev := p.lines[p.row-1]
		p.col = len([]rune(prev))
		p.lines[p.row-1] = prev + p.lines[p.row]
		p.lines = append(p.lines[:p.row], p.lines[p.row+1:]...)
		p.row--
	}
}

// delete deletes the rune at the cursor (or merges the next line up).
func (p *promptWidget) deleteKey() {
	line := []rune(p.lines[p.row])
	if p.col < len(line) {
		p.lines[p.row] = string(line[:p.col]) + string(line[p.col+1:])
		return
	}
	if p.row < len(p.lines)-1 {
		p.lines[p.row] += p.lines[p.row+1]
		p.lines = append(p.lines[:p.row+1], p.lines[p.row+2:]...)
	}
}

// move executes cursor-motion keys. Returns true when handled.
func (p *promptWidget) move(code string) bool {
	line := []rune(p.lines[p.row])
	switch code {
	case "left":
		if p.col > 0 {
			p.col--
			return true
		}
		if p.row > 0 {
			p.row--
			p.col = len([]rune(p.lines[p.row]))
			return true
		}
	case "right":
		if p.col < len(line) {
			p.col++
			return true
		}
		if p.row < len(p.lines)-1 {
			p.row++
			p.col = 0
			return true
		}
	case "home":
		p.col = 0
		return true
	case "end":
		p.col = len(line)
		return true
	case "up":
		if p.row > 0 {
			p.row--
			p.clampCol()
			return true
		}
	case "down":
		if p.row < len(p.lines)-1 {
			p.row++
			p.clampCol()
			return true
		}
	case "ctrl+left", "alt+left":
		p.col = p.wordBack(line, p.col)
		return true
	case "ctrl+right", "alt+right":
		p.col = p.wordForward(line, p.col)
		return true
	}
	return false
}

func (p *promptWidget) clampCol() {
	n := len([]rune(p.lines[p.row]))
	if p.col > n {
		p.col = n
	}
}

func (p *promptWidget) wordBack(line []rune, col int) int {
	i := col
	for i > 0 && isSpace(line[i-1]) {
		i--
	}
	for i > 0 && !isSpace(line[i-1]) {
		i--
	}
	return i
}

func (p *promptWidget) wordForward(line []rune, col int) int {
	i := col
	for i < len(line) && isSpace(line[i]) {
		i++
	}
	for i < len(line) && !isSpace(line[i]) {
		i++
	}
	return i
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

// killBack deletes from the word boundary to the cursor.
func (p *promptWidget) killWordBack() {
	line := []rune(p.lines[p.row])
	from := p.wordBack(line, p.col)
	p.lines[p.row] = string(line[:from]) + string(line[p.col:])
	p.col = from
}

// killLine deletes to the start of the line.
func (p *promptWidget) killLine() {
	line := []rune(p.lines[p.row])
	p.lines[p.row] = string(line[p.col:])
	p.col = 0
}

// addHistory records a submitted prompt.
func (p *promptWidget) addHistory(text string) {
	if text == "" {
		return
	}
	for i, h := range p.history {
		if h == text {
			p.history = append(p.history[:i], p.history[i+1:]...)
			break
		}
	}
	p.history = append(p.history, text)
	if len(p.history) > p.historyLimit {
		p.history = p.history[len(p.history)-p.historyLimit:]
	}
	p.historyIdx = -1
	p.draft = ""
}

// navigateHistory walks up (-1) / down (+1) through history.
func (p *promptWidget) navigateHistory(dir int) bool {
	if len(p.history) == 0 {
		return false
	}
	idx := p.historyIdx
	if dir < 0 {
		if idx == -1 {
			p.draft = p.value()
			idx = len(p.history) - 1
		} else if idx > 0 {
			idx--
		} else {
			return false
		}
	} else {
		if idx == -1 {
			return false
		}
		idx++
		if idx >= len(p.history) {
			p.historyIdx = -1
			p.setValue(p.draft)
			return true
		}
	}
	p.historyIdx = idx
	p.setValue(p.history[idx])
	return true
}

// historyActive reports whether the cursor is walking history.
func (p *promptWidget) historyActive() bool { return p.historyIdx >= 0 }

// render paints the editor rows into the content area of the composer box.
// When the text overflows the box it shows the tail and tracks the cursor.
func (p *promptWidget) render(scr *engine.Screen, area engine.Rect, th *theme) {
	if area.W <= 0 || area.H <= 0 {
		return
	}
	st := engine.Style{}.WithFg(th.FG).WithBg(th.BGBase)
	if p.value() == "" {
		ph := engine.Style{}.WithFg(th.GrayDim).WithBg(th.BGBase)
		scr.SetString(area.X, area.Y, p.placeholder, ph)
		return
	}
	// Flatten to wrapped display rows tagged with (line, startCol).
	type dRow struct {
		spans []engine.Span
		line  int
		start int
	}
	var rows []dRow
	for i, l := range p.lines {
		wrapped := engine.WrapSpans([]engine.Span{{Text: l, Style: st}}, area.W)
		start := 0
		for _, wr := range wrapped {
			w := 0
			for _, sp := range wr {
				w += runewidth.StringWidth(sp.Text)
			}
			rows = append(rows, dRow{spans: wr, line: i, start: start})
			start += w
		}
	}
	// Keep the tail visible.
	from := 0
	if len(rows) > area.H {
		from = len(rows) - area.H
	}
	scr.CursorVisible = false
	for i := from; i < len(rows); i++ {
		y := area.Y + i - from
		scr.Line(area.X, y, rows[i].spans)
		if rows[i].line == p.row {
			c := p.runeCol()
			w := 0
			for _, sp := range rows[i].spans {
				w += runewidth.StringWidth(sp.Text)
			}
			if c >= rows[i].start && c <= rows[i].start+w {
				scr.CursorX = area.X + (c - rows[i].start)
				scr.CursorY = y
				scr.CursorVisible = true
			}
		}
	}
}
