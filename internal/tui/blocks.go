package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
	"github.com/AlvinPlayz23/myagent/internal/types"
	"github.com/mattn/go-runewidth"
)

// Chrome geometry, mirroring the pager: one accent column, two left pad
// columns, content, one right pad column. Entries with a rail draw ┃ down
// their rows in the accent color; vpad rows separate entries.
const (
	chromeAccent = 1
	chromeLeft   = 2
	chromeRight  = 1
	sbVpad       = 1
)

// sbKind discriminates a scrollback entry.
type sbKind int

const (
	sbUser sbKind = iota
	sbAssistant
	sbTool
	sbError
	sbNotice
	sbThinking
)

// diffPreviewLines bounds the proposal diff shown while collapsed.
const diffPreviewLines = 10

// toolPreviewLines bounds collapsed tool output.
const toolPreviewLines = 4

type diffLine struct {
	prefix byte
	text   string
}

// sbEntry is one scrollback entry. Assistant/thinking entries grow in place
// as deltas arrive; tool entries flip between collapsed preview and full
// output on the global ctrl+o toggle.
type sbEntry struct {
	kind sbKind

	text string // user/assistant/error/notice text (markdown for assistant)

	toolCallID string
	toolName   string
	toolArgs   map[string]any
	toolDiff   []diffLine
	toolOutput string
	toolErr    bool
	toolDone   bool

	thinkingDone    bool
	thinkingStarted time.Time
	thinkingElapsed time.Duration

	streaming bool

	cacheWidth  int
	cacheExpand bool
	cacheThink  bool
	rows        [][]engine.Span
	railColor   engine.Color
	railOn      bool
}

// scrollback owns the entry list and follow/scroll state.
type scrollback struct {
	entries      []*sbEntry
	expanded     bool
	showThinking bool
	// pinned follows the tail; unpinned shows the window starting at offset.
	pinned bool
	offset int // rows above the window when unpinned
}

type scrollbackRow struct {
	entry *sbEntry
	spans []engine.Span
}

func newScrollback() *scrollback {
	return &scrollback{pinned: true, showThinking: true}
}

func (s *scrollback) invalidate() {
	for _, e := range s.entries {
		e.cacheWidth = 0
	}
}

// addUser appends a user entry, styling slash commands and file mentions.
func (s *scrollback) addUser(text string) {
	s.entries = append(s.entries, &sbEntry{kind: sbUser, text: text, railColor: s.theme().AccentUser, railOn: true})
}

// beginAssistant starts a streaming assistant entry.
func (s *scrollback) beginAssistant() {
	e := &sbEntry{kind: sbAssistant, streaming: true, railColor: s.theme().AccentAssistant, railOn: true}
	s.entries = append(s.entries, e)
}

// beginThinking starts a streaming thinking entry, removing an empty
// assistant entry opened by message_start first.
func (s *scrollback) beginThinking() {
	if n := len(s.entries); n > 0 {
		last := s.entries[n-1]
		if last.kind == sbAssistant && last.streaming && strings.TrimSpace(last.text) == "" {
			s.entries = s.entries[:n-1]
		}
	}
	s.entries = append(s.entries, &sbEntry{kind: sbThinking, streaming: true, thinkingStarted: time.Now(), railColor: s.theme().AccentThinking, railOn: false})
}

func (s *scrollback) appendThinkingDelta(delta string) {
	e := s.lastOfKind(sbThinking, true)
	if e == nil {
		s.beginThinking()
		e = s.entries[len(s.entries)-1]
	}
	e.text += delta
	e.cacheWidth = 0
}

// endThinking finalizes thinking; empty blocks are removed.
func (s *scrollback) endThinking() {
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		if e.kind != sbThinking {
			break
		}
		if e.streaming {
			e.streaming = false
			e.thinkingDone = true
			e.thinkingElapsed = time.Since(e.thinkingStarted)
			if strings.TrimSpace(e.text) == "" {
				s.entries = append(s.entries[:i], s.entries[i+1:]...)
			}
			break
		}
	}
}

func (s *scrollback) appendAssistantDelta(delta string) {
	n := len(s.entries)
	if n == 0 || s.entries[n-1].kind != sbAssistant || !s.entries[n-1].streaming {
		// Close any open thinking block first.
		if n > 0 && s.entries[n-1].kind == sbThinking && s.entries[n-1].streaming {
			s.endThinking()
		}
		s.beginAssistant()
		n = len(s.entries)
	}
	e := s.entries[n-1]
	e.text += delta
	e.cacheWidth = 0
}

// endAssistant finalizes streaming; empty entries are dropped.
func (s *scrollback) endAssistant() {
	if n := len(s.entries); n > 0 {
		e := s.entries[n-1]
		if e.kind == sbThinking && e.streaming {
			s.endThinking()
			return
		}
		if e.kind == sbAssistant && e.streaming {
			e.streaming = false
			if strings.TrimSpace(e.text) == "" {
				s.entries = s.entries[:n-1]
			}
		}
	}
}

func (s *scrollback) addErrorText(text string) {
	s.entries = append(s.entries, &sbEntry{kind: sbError, text: text, railColor: s.theme().AccentError, railOn: true})
}

func (s *scrollback) addNotice(text string) {
	s.entries = append(s.entries, &sbEntry{kind: sbNotice, text: text, railColor: s.theme().AccentSystem, railOn: false})
}

func (s *scrollback) startTool(callID, name string, args map[string]any) {
	e := &sbEntry{
		kind:       sbTool,
		toolCallID: callID,
		toolName:   name,
		toolArgs:   args,
		toolDiff:   proposalDiff(name, args),
		railColor:  s.theme().AccentTool,
		railOn:     true,
	}
	e.thinkingStarted = time.Now()
	s.entries = append(s.entries, e)
}

func (s *scrollback) endTool(callID string, result *types.ToolResult, isError bool) {
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		if e.kind == sbTool && e.toolCallID == callID {
			e.toolDone = true
			e.toolErr = isError
			e.toolOutput = resultTextOf(result)
			e.cacheWidth = 0
			return
		}
	}
}

// lastOfKind finds the trailing open entry of a kind.
func (s *scrollback) lastOfKind(kind sbKind, open bool) *sbEntry {
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		if e.kind == kind && (!open || e.streaming) {
			return e
		}
		if e.kind != kind {
			return nil
		}
	}
	return nil
}

func (s *scrollback) theme() *theme { return currentTheme() }

// clear removes displayed entries (the /clear command).
func (s *scrollback) clear() {
	s.entries = nil
	s.pinned = true
	s.offset = 0
}

// toggleExpand flips the global ctrl+o expand state.
func (s *scrollback) toggleExpand() {
	s.expanded = !s.expanded
	s.invalidate()
}

// setShowThinking flips thinking visibility.
func (s *scrollback) setShowThinking(show bool) {
	if s.showThinking == show {
		return
	}
	s.showThinking = show
	s.invalidate()
}

// scrollBy moves the viewport; any scroll unpins from the tail.
func (s *scrollback) scrollBy(delta int, total, viewH int) {
	maxOff := max(0, total-viewH)
	if s.pinned {
		s.offset = maxOff
	}
	s.offset = clamp(s.offset+delta, 0, maxOff)
	s.pinned = s.offset == maxOff
}

// scrollHome jumps to the oldest content.
func (s *scrollback) scrollHome() { s.pinned = false; s.offset = 0 }

// scrollEnd returns to the tail.
func (s *scrollback) scrollEnd() { s.pinned = true; s.offset = 0 }

// layoutRows materializes the exact rows used for both painting and scrolling.
func (s *scrollback) layoutRows(width int) []scrollbackRow {
	th := currentTheme()
	if width <= 0 {
		return nil
	}

	entries := make([]*sbEntry, 0, len(s.entries))
	for _, e := range s.entries {
		if e.kind == sbThinking && !s.showThinking && !e.streaming {
			continue
		}
		entries = append(entries, e)
	}

	rows := make([]scrollbackRow, 0, len(entries)*2)
	for i, e := range entries {
		if i > 0 && (entries[i-1].railOn || e.railOn) {
			for range sbVpad {
				rows = append(rows, scrollbackRow{})
			}
		}
		for _, spans := range e.renderRows(width, s.expanded, s.showThinking, th) {
			rows = append(rows, scrollbackRow{entry: e, spans: spans})
		}
	}
	return rows
}

func (s *scrollback) totalRows(width int) int {
	return len(s.layoutRows(width))
}

// render paints the scrollback into area, returning total content rows.
func (s *scrollback) render(scr *engine.Screen, area engine.Rect) int {
	width := area.W - chromeAccent - chromeLeft - chromeRight
	rows := s.layoutRows(width)
	total := len(rows)
	maxOff := max(0, total-area.H)
	if s.pinned {
		s.offset = maxOff
	} else {
		s.offset = clamp(s.offset, 0, maxOff)
	}
	end := min(s.offset+area.H, total)
	for i := s.offset; i < end; i++ {
		y := area.Y + i - s.offset
		row := rows[i]
		if row.entry == nil {
			paintVpad(scr, area.X, y, area.W, currentTheme())
		} else {
			paintRow(scr, area.X, y, area.W, row.entry, row.spans, currentTheme())
		}
	}
	return total
}

func paintVpad(scr *engine.Screen, x, y, w int, th *theme) {
	st := engine.Style{}.WithBg(th.BGBase)
	for i := 0; i < w; i++ {
		scr.SetCell(x+i, y, engine.Cell{Ch: ' ', Width: 1, Style: st})
	}
}

// paintRow draws one entry row: rail (or not), left pad, content, right pad.
func paintRow(scr *engine.Screen, x, y, w int, e *sbEntry, spans []engine.Span, th *theme) {
	bg := engine.Style{}.WithBg(th.BGBase)
	// Full-row background first.
	for i := 0; i < w; i++ {
		scr.SetCell(x+i, y, engine.Cell{Ch: ' ', Width: 1, Style: bg})
	}
	col := x
	if e.railOn {
		railSt := engine.Style{}.WithFg(e.railColor).WithBg(th.BGBase)
		scr.SetCell(col, y, engine.Cell{Ch: '┃', Width: 1, Style: railSt})
	}
	col += chromeAccent + chromeLeft
	scr.Line(col, y, spans)
}

// renderRows renders (and caches) the content rows of an entry at a width.
func (e *sbEntry) renderRows(width int, expanded bool, showThinking bool, th *theme) [][]engine.Span {
	if e.cacheWidth == width && !e.streaming && e.cacheValidFor(expanded, showThinking) && e.rows != nil {
		return e.rows
	}
	if (e.kind == sbAssistant || e.kind == sbThinking) && e.streaming {
		// Streaming entries recompute every frame.
	} else {
		e.cacheWidth = width
		e.cacheExpand = expanded
		e.cacheThink = showThinking
	}
	switch e.kind {
	case sbUser:
		e.rows = userRows(e.text, width, th)
	case sbAssistant:
		e.rows = renderMarkdown(e.text, width, th)
	case sbError:
		st := engine.Style{}.WithFg(th.Red).WithBg(th.BGBase)
		e.rows = wrapPlain(e.text, width, st)
	case sbNotice:
		st := engine.Style{}.WithFg(th.Comment).WithBg(th.BGBase).Italic()
		e.rows = wrapPlain(e.text, width, st)
	case sbThinking:
		e.rows = thinkingRows(e, width, expanded, th)
	case sbTool:
		e.rows = toolRows(e, width, expanded, th)
	}
	return e.rows
}

func (e *sbEntry) cacheValidFor(expanded, showThinking bool) bool {
	return e.cacheExpand == expanded && e.cacheThink == showThinking
}

// userRows renders prompt text with slash-command and file-mention accents.
func userRows(text string, width int, th *theme) [][]engine.Span {
	base := engine.Style{}.WithFg(th.FG).WithBg(th.BGBase)
	pathSt := engine.Style{}.WithFg(th.Orange).WithBg(th.BGBase)
	cmdSt := engine.Style{}.WithFg(th.Yellow).WithBg(th.BGBase)

	lines := strings.Split(text, "\n")
	out := make([][]engine.Span, 0, len(lines))
	for _, l := range lines {
		var spans []engine.Span
		spans = append(spans, userSpans(l, base, cmdSt, pathSt)...)
		out = append(out, engine.WrapSpans(spans, max(width, 1))...)
	}
	return out
}

// userSpans styles leading slash commands and @mentions inside one line.
func userSpans(l string, base, cmdSt, pathSt engine.Style) []engine.Span {
	var spans []engine.Span
	i := 0
	for i < len(l) {
		switch {
		case i == 0 && strings.HasPrefix(l, "/"):
			if end := strings.IndexAny(l, " \t"); end > 0 {
				spans = append(spans, engine.Span{Text: l[:end], Style: cmdSt})
				i = end
				continue
			}
			spans = append(spans, engine.Span{Text: l, Style: cmdSt})
			i = len(l)
		case l[i] == '@':
			if end := strings.IndexAny(l[i:], " \t"); end > 0 {
				spans = append(spans, engine.Span{Text: l[i : i+end], Style: pathSt})
				i += end
				continue
			}
			spans = append(spans, engine.Span{Text: l[i:], Style: pathSt})
			i = len(l)
		default:
			next := strings.IndexAny(l[i:], "@")
			if next < 0 {
				spans = append(spans, engine.Span{Text: l[i:], Style: base})
				i = len(l)
				continue
			}
			spans = append(spans, engine.Span{Text: l[i : i+next], Style: base})
			i += next
		}
	}
	if len(spans) == 0 {
		spans = append(spans, engine.Span{Text: "", Style: base})
	}
	return spans
}

func wrapPlain(text string, width int, st engine.Style) [][]engine.Span {
	out := make([][]engine.Span, 0, 4)
	for _, l := range strings.Split(text, "\n") {
		out = append(out, engine.WrapSpans([]engine.Span{{Text: l, Style: st}}, max(width, 1))...)
	}
	return out
}

// thinkingRows renders the thinking header plus its body. Finished thinking
// shows the most recent lines (ctrl+o expands to everything), mirroring the
// previous transcript's preview behavior inside the new chrome.
func thinkingRows(e *sbEntry, width int, expanded bool, th *theme) [][]engine.Span {
	head := "✻ Thought"
	if e.streaming {
		head = "✻ Thinking…"
	} else if e.thinkingElapsed > 0 {
		head = fmt.Sprintf("✻ Thought for %.0fs", e.thinkingElapsed.Seconds())
	}
	st := engine.Style{}.WithFg(th.Magenta).WithBg(th.BGBase)
	rows := [][]engine.Span{{{Text: head, Style: st}}}
	if !e.streaming && strings.TrimSpace(e.text) == "" {
		return rows
	}
	bodySt := engine.Style{}.WithFg(th.FGDark).WithBg(th.BGBase).Italic().Dim()
	body := strings.TrimRight(e.text, "\n")
	if body == "" {
		return rows
	}
	lines := strings.Split(body, "\n")
	const previewLines = 6
	if !expanded && !e.streaming && len(lines) > previewLines {
		shown := lines[len(lines)-previewLines:]
		rows = append(rows, [][]engine.Span{{{Text: fmt.Sprintf("… %d earlier lines", len(lines)-previewLines), Style: th.muted()}}}...)
		rows = append(rows, wrapPlain(strings.Join(shown, "\n"), width, bodySt)...)
		rows = append(rows, [][]engine.Span{{{Text: fmt.Sprintf("… (%d more lines, ctrl+o to expand)", len(lines)-previewLines), Style: th.muted()}}}...)
		return rows
	}
	if e.streaming {
		// Show the tail while streaming so the newest reasoning is visible.
		if len(lines) > 3 {
			lines = lines[len(lines)-3:]
		}
		body = strings.Join(lines, "\n")
	}
	rows = append(rows, wrapPlain(body, width, bodySt)...)
	if expanded && len(lines) > previewLines {
		rows = append(rows, [][]engine.Span{{{Text: "(ctrl+o to collapse)", Style: th.muted()}}}...)
	}
	return rows
}

// toolRows renders the tool header, preview/output, and diff.
func toolRows(e *sbEntry, width int, expanded bool, th *theme) [][]engine.Span {
	headSt := engine.Style{}.WithFg(th.GrayBright).WithBg(th.BGBase)
	nameSt := headSt.Bold()
	outSt := engine.Style{}.WithFg(th.Comment).WithBg(th.BGBase)
	errSt := engine.Style{}.WithFg(th.Red).WithBg(th.BGBase)

	rows := [][]engine.Span{append(
		[]engine.Span{{Text: "◆ ", Style: engine.Style{}.WithFg(e.bulletColor(th)).WithBg(th.BGBase)}},
		toolSpans(e, nameSt, headSt)...)}

	if e.toolDiff != nil && len(e.toolDiff) > 0 && !e.toolDone {
		rows = append(rows, renderDiffRows(e.toolDiff, width, expanded, th)...)
	}
	if e.toolOutput != "" {
		out := e.toolOutput
		lines := strings.Split(out, "\n")
		st := outSt
		if e.toolErr {
			st = errSt
		}
		shown := lines
		truncated := 0
		if !expanded && len(lines) > toolPreviewLines {
			shown = lines[:toolPreviewLines]
			truncated = len(lines) - toolPreviewLines
		}
		for _, l := range shown {
			rows = append(rows, engine.WrapSpans([]engine.Span{{Text: "  " + l, Style: st}}, max(width, 1))...)
		}
		if truncated > 0 {
			rows = append(rows, [][]engine.Span{{{Text: fmt.Sprintf("… (%d more lines, ctrl+o to expand)", truncated), Style: th.muted()}}}...)
		} else if expanded && len(lines) > toolPreviewLines {
			rows = append(rows, [][]engine.Span{{{Text: "(ctrl+o to collapse)", Style: th.muted()}}}...)
		}
	}
	return rows
}

// bulletColor animates the running tool diamond by wave brightness.
func (e *sbEntry) bulletColor(th *theme) engine.Color {
	if e.toolDone {
		if e.toolErr {
			return th.Red
		}
		return th.GrayBright
	}
	return th.Cyan
}

// toolSpans builds the header spans: name accent + argument summary.
func toolSpans(e *sbEntry, nameSt, argSt engine.Style) []engine.Span {
	arg := func(k string) string {
		if e.toolArgs == nil {
			return ""
		}
		if v, ok := e.toolArgs[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	path := arg("path")
	if path == "" {
		path = arg("file_path")
	}
	pathSt := engine.Style{}.WithFg(currentTheme().Orange).WithBg(currentTheme().BGBase)
	switch e.toolName {
	case "read":
		return []engine.Span{{Text: "read ", Style: nameSt}, {Text: path, Style: pathSt}}
	case "edit":
		return []engine.Span{{Text: "edit ", Style: nameSt}, {Text: path, Style: pathSt}}
	case "write":
		return []engine.Span{{Text: "write ", Style: nameSt}, {Text: path, Style: pathSt}}
	case "bash":
		cmd := arg("command")
		if cmd == "" {
			cmd = arg("cmd")
		}
		return []engine.Span{{Text: "$ ", Style: nameSt}, {Text: firstLineOf(cmd), Style: argSt}}
	default:
		if len(e.toolArgs) == 0 {
			return []engine.Span{{Text: e.toolName, Style: nameSt}}
		}
		raw, _ := json.Marshal(e.toolArgs)
		return []engine.Span{{Text: e.toolName + " ", Style: nameSt}, {Text: string(raw), Style: argSt}}
	}
}

// renderDiffRows colors a proposal diff with GrokNight diff roles.
func renderDiffRows(lines []diffLine, width int, expanded bool, th *theme) [][]engine.Span {
	visible := lines
	hidden := 0
	if !expanded {
		changeCount := 0
		visible = make([]diffLine, 0, len(lines))
		for _, l := range lines {
			if l.prefix != 0 {
				if changeCount >= diffPreviewLines {
					hidden++
					continue
				}
				changeCount++
			}
			visible = append(visible, l)
		}
	}
	out := make([][]engine.Span, 0, len(visible)+1)
	for _, l := range lines {
		_ = l
		break
	}
	for _, dl := range visible {
		text := dl.text
		if dl.prefix != 0 {
			text = string(dl.prefix) + text
		}
		var st engine.Style
		switch {
		case dl.prefix == '+':
			st = engine.Style{}.WithFg(th.Green).WithBg(th.GreenDark)
		case dl.prefix == '-':
			st = engine.Style{}.WithFg(th.Red).WithBg(th.RedDark)
		case strings.HasPrefix(dl.text, "@@"):
			st = engine.Style{}.WithFg(th.Blue).WithBg(th.BGBase)
		default:
			st = engine.Style{}.WithFg(th.Comment).WithBg(th.BGBase)
		}
		out = append(out, engine.WrapSpans([]engine.Span{{Text: text, Style: st}}, max(width, 1))...)
	}
	if hidden > 0 {
		out = append(out, [][]engine.Span{{{Text: fmt.Sprintf("… (%d more changed lines, ctrl+o to expand)", hidden), Style: th.muted()}}}...)
	} else if expanded && len(lines) > diffPreviewLines {
		out = append(out, [][]engine.Span{{{Text: "(ctrl+o to collapse)", Style: th.muted()}}}...)
	}
	return out
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// resultTextOf flattens a ToolResult's content for display.
func resultTextOf(r *types.ToolResult) string {
	if r == nil {
		return ""
	}
	var parts []string
	for _, c := range r.Content {
		switch c.Type {
		case types.ContentText:
			parts = append(parts, c.Text)
		case types.ContentImage:
			parts = append(parts, "[image]")
		}
	}
	return strings.Join(parts, "\n")
}

var _ = runewidth.StringWidth
