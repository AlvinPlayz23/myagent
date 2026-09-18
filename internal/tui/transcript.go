package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AlvinPlayz23/myagent/internal/types"
	"github.com/muesli/reflow/wordwrap"
)

// blockKind discriminates a transcript block.
type blockKind int

const (
	blockUser blockKind = iota
	blockAssistant
	blockTool
	blockError
	blockNotice
	blockThinking
)

// block is a single renderable unit in the transcript. Assistant blocks grow
// in place as streaming deltas arrive (pi's "re-render the whole growing
// markdown each delta"); tool blocks flip between a collapsed preview and full
// output on the global expand toggle.
type block struct {
	kind blockKind

	// assistant/user/error text (markdown for user/assistant, plain for error)
	text string

	// noticeDetail holds expandable extra info for notice blocks (e.g. the
	// underlying provider error for a retry notice). Empty means no expand.
	noticeDetail string

	// tool fields
	toolCallID string
	toolName   string
	toolArgs   map[string]any
	toolDiff   []diffLine // proposal diff for edit/write calls
	toolOutput string
	toolErr    bool
	toolDone   bool
	// toolStart/toolDur feed the kj-style elapsed-time metadata shown after a
	// tool completes. toolTimed is false for resumed history, which has no
	// meaningful start stamp.
	toolStart time.Time
	toolDur   time.Duration
	toolTimed bool

	// thinking fields
	done bool // streaming finished (thinking blocks)
	// thinkStart/thinkDur track elapsed reasoning time for the completed
	// "✻ Thought for Ns" header. thinkTimed is false when the duration is
	// unknown (e.g. resumed history), rendering a plain "✻ Thought".
	thinkStart time.Time
	thinkDur   time.Duration
	thinkTimed bool

	// cache
	cacheWidth  int
	cacheExpand bool
	cached      string
	cacheValid  bool
}

// transcript is the ordered list of blocks plus render settings.
type transcript struct {
	th       *theme
	md       *mdRenderer
	blocks   []*block
	expanded bool // global collapse/expand for tool blocks (pi's ctrl+o)

	// showThinking controls whether thinking blocks are rendered at all.
	// Thinking is still accumulated while hidden, so toggling it on reveals
	// everything captured so far.
	showThinking bool

	// streamingIdx points at the assistant block currently being streamed, or
	// -1 when none.
	streamingIdx int
}

func newTranscript(th *theme, md *mdRenderer) *transcript {
	return &transcript{th: th, md: md, streamingIdx: -1, showThinking: true}
}

// invalidate clears cached renders (e.g. on width change or expand toggle).
func (t *transcript) invalidate() {
	for _, b := range t.blocks {
		b.cacheValid = false
	}
}

// clear removes displayed blocks without changing the underlying conversation.
func (t *transcript) clear() {
	t.blocks = nil
	t.streamingIdx = -1
}

// setShowThinking flips thinking visibility and invalidates caches so the next
// render reflects the new state.
func (t *transcript) setShowThinking(show bool) {
	if t.showThinking == show {
		return
	}
	t.showThinking = show
	for _, b := range t.blocks {
		b.cacheValid = false
	}
}

// toggleExpand flips the global tool expand state and invalidates tool,
// thinking, and expandable-notice caches.
func (t *transcript) toggleExpand() {
	t.expanded = !t.expanded
	for _, b := range t.blocks {
		if b.kind == blockTool || b.kind == blockThinking || (b.kind == blockNotice && b.noticeDetail != "") {
			b.cacheValid = false
		}
	}
}

// addUser appends a user block.
func (t *transcript) addUser(text string) {
	t.blocks = append(t.blocks, &block{kind: blockUser, text: text})
}

// beginAssistant starts a new (empty) streaming assistant block.
func (t *transcript) beginAssistant() {
	t.blocks = append(t.blocks, &block{kind: blockAssistant})
	t.streamingIdx = len(t.blocks) - 1
}

// appendAssistantDelta appends streamed text to the active assistant block.
// A still-open thinking block is closed first: providers do not emit
// thinking_end at the reasoning-to-answer transition (the accumulator only
// sends thinking_end during its finalize pass), so the text delta itself is
// the signal that thinking finished.
func (t *transcript) appendAssistantDelta(delta string) {
	if t.streamingIdx < 0 || t.streamingIdx >= len(t.blocks) || t.blocks[t.streamingIdx].kind != blockAssistant {
		t.endThinking()
		t.beginAssistant()
	}
	b := t.blocks[t.streamingIdx]
	b.text += delta
	b.cacheValid = false
}

// beginThinking starts a new (empty) streaming thinking block. An empty
// assistant block opened by message_start is removed first so it doesn't
// linger above the thinking block when reasoning precedes the reply text.
func (t *transcript) beginThinking() {
	if t.streamingIdx >= 0 && t.streamingIdx == len(t.blocks)-1 {
		if b := t.blocks[t.streamingIdx]; b.kind == blockAssistant && strings.TrimSpace(b.text) == "" {
			t.blocks = t.blocks[:t.streamingIdx]
		}
	}
	t.blocks = append(t.blocks, &block{kind: blockThinking, thinkStart: time.Now(), thinkTimed: true})
	t.streamingIdx = len(t.blocks) - 1
}

// appendThinkingDelta appends streamed reasoning to the active thinking block.
// The block is created on demand so deltas arriving without a start event
// still render.
func (t *transcript) appendThinkingDelta(delta string) {
	if t.streamingIdx < 0 || t.streamingIdx >= len(t.blocks) || t.blocks[t.streamingIdx].kind != blockThinking {
		t.beginThinking()
	}
	b := t.blocks[t.streamingIdx]
	b.text += delta
	b.cacheValid = false
}

// endThinking finalizes the current thinking block, removing it if it never
// received text (mirrors endAssistant). A completed block collapses to its
// "✻ Thought" header until expanded.
func (t *transcript) endThinking() {
	if t.streamingIdx >= 0 && t.streamingIdx < len(t.blocks) {
		b := t.blocks[t.streamingIdx]
		if b.kind == blockThinking {
			b.done = true
			if b.thinkTimed && !b.thinkStart.IsZero() {
				b.thinkDur = time.Since(b.thinkStart)
			}
			b.cacheValid = false
			if strings.TrimSpace(b.text) == "" {
				t.blocks = append(t.blocks[:t.streamingIdx], t.blocks[t.streamingIdx+1:]...)
			}
		}
	}
	t.streamingIdx = -1
}

// endAssistant finalizes the current assistant block. If it never received any
// text (a tool-only turn), it is removed to avoid an empty gap. An active
// thinking block is finalized too: a response can end mid-reasoning (abort,
// provider error) without a thinking_end event or any text delta, and leaving
// the block unfinished would render "✻ Thinking…" forever.
func (t *transcript) endAssistant() {
	if t.streamingIdx >= 0 && t.streamingIdx < len(t.blocks) {
		b := t.blocks[t.streamingIdx]
		if b.kind == blockThinking {
			t.endThinking()
			return
		}
		if b.kind == blockAssistant && strings.TrimSpace(b.text) == "" {
			t.blocks = append(t.blocks[:t.streamingIdx], t.blocks[t.streamingIdx+1:]...)
		}
	}
	t.streamingIdx = -1
}

// addErrorText appends a standalone error line (e.g. aborted / stop reason).
func (t *transcript) addErrorText(text string) {
	t.blocks = append(t.blocks, &block{kind: blockError, text: text})
}

// addNotice appends a muted system-notice block (e.g. compaction summary).
func (t *transcript) addNotice(text string) {
	t.blocks = append(t.blocks, &block{kind: blockNotice, text: text})
}

// addRetryNotice appends a retry notice whose underlying provider error is
// revealed with the global ctrl+o expand toggle.
func (t *transcript) addRetryNotice(text, detail string) {
	t.blocks = append(t.blocks, &block{kind: blockNotice, text: text, noticeDetail: strings.TrimSpace(detail)})
}

// startTool appends a tool block in the pending state.
func (t *transcript) startTool(callID, name string, args map[string]any) {
	t.blocks = append(t.blocks, &block{
		kind:       blockTool,
		toolCallID: callID,
		toolName:   name,
		toolArgs:   args,
		toolDiff:   proposalDiff(name, args),
		toolStart:  time.Now(),
		toolTimed:  true,
	})
}

// endTool records the result on the matching tool block.
func (t *transcript) endTool(callID string, result *types.ToolResult, isError bool) {
	b := t.findTool(callID)
	if b == nil {
		return
	}
	b.toolDone = true
	b.toolErr = isError
	b.toolOutput = resultText(result)
	if b.toolTimed && !b.toolStart.IsZero() {
		b.toolDur = time.Since(b.toolStart)
	}
	b.cacheValid = false
}

func (t *transcript) findTool(callID string) *block {
	for i := len(t.blocks) - 1; i >= 0; i-- {
		if t.blocks[i].kind == blockTool && t.blocks[i].toolCallID == callID {
			return t.blocks[i]
		}
	}
	return nil
}

// render produces the full transcript content string wrapped at width. Blocks
// are separated by a blank line (pi's Spacer(1)). Thinking blocks are omitted
// entirely when thinking is hidden, so no stray separators remain. Runs of
// consecutive completed reads fold into one summary line (kj's folded reads).
func (t *transcript) render(width int) string {
	var sb strings.Builder
	for i := 0; i < len(t.blocks); i++ {
		b := t.blocks[i]
		if b.kind == blockThinking && !t.showThinking {
			continue
		}
		var out string
		// Folding applies in the collapsed view only: ctrl+o restores the
		// individual read blocks with their output previews.
		if fold := t.foldReads(i); !t.expanded && fold != nil {
			out = t.renderFoldedReads(fold)
			i = fold.end
		} else {
			out = t.renderBlock(b, width)
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(out)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (t *transcript) renderBlock(b *block, width int) string {
	if b.cacheValid && b.cacheWidth == width && b.cacheExpand == t.expanded {
		return b.cached
	}
	var out string
	switch b.kind {
	case blockUser:
		// User messages are plain text, not markdown. Glamour emits ANSI style
		// resets which can override the userBlock background behind the text.
		// Give the block an explicit width too, so its neutral background fills
		// every cell of the transcript row, including wrapped-line padding.
		body := strings.TrimRight(wordwrap.String(b.text, max(1, width-2)), "\n")
		out = t.th.userBlock.Width(max(1, width)).Render(body)
	case blockAssistant:
		// Assistant-only clickable URLs: bare links get OSC 8 escapes here so
		// user, tool, and notice blocks never become clickable.
		out = makeURLsClickable(strings.TrimRight(t.md.render(b.text, width), "\n"))
	case blockError:
		// Provider errors often arrive as one giant line; wrap them to the
		// viewport like every other plain-text block so they never run
		// off-screen.
		out = t.th.errorText.Render(wrapPlain(b.text, width))
	case blockNotice:
		out = t.renderNotice(b, width)
	case blockTool:
		out = t.renderTool(b, width)
	case blockThinking:
		out = t.renderThinking(b, width)
	}
	b.cached = out
	b.cacheWidth = width
	b.cacheExpand = t.expanded
	b.cacheValid = true
	return out
}

// renderNotice renders a muted system notice. Retry notices carry the
// underlying provider error, collapsed behind the global ctrl+o toggle.
// Both the headline and the detail are wrapped so long single-line
// provider errors never run off-screen.
func (t *transcript) renderNotice(b *block, width int) string {
	headline := wrapPlain(b.text, width)
	if b.noticeDetail == "" {
		return t.th.muted.Render(headline)
	}
	if !t.expanded {
		return t.th.muted.Render(headline) + "\n" +
			t.th.muted.Render("… (ctrl+o to expand)")
	}
	detail := strings.TrimRight(wordwrap.String(b.noticeDetail, max(1, width-2)), "\n")
	return t.th.muted.Render(headline) + "\n" +
		t.th.muted.Render(detail) + "\n" +
		t.th.muted.Render("(ctrl+o to collapse)")
}

// renderTool renders a collapsible tool block kj-style: a one-line status
// header ("● name target" with a status-colored marker), a dim metadata line
// (elapsed time, result size) once the call completes, and an optional
// preview (collapsed) or full output (expanded).
func (t *transcript) renderTool(b *block, width int) string {
	statusStyle := t.th.toolPending
	switch {
	case !b.toolDone:
		statusStyle = t.th.toolPending
	case b.toolErr:
		statusStyle = t.th.toolError
	default:
		statusStyle = t.th.toolSuccess
	}

	var sb strings.Builder
	sb.WriteString(statusStyle.Render("● "))
	name, target := t.toolHeaderParts(b)
	sb.WriteString(t.th.textPrimary.Bold(true).Render(name))
	if target != "" {
		sb.WriteByte(' ')
		sb.WriteString(t.th.muted.Render(target))
	}
	if meta := t.toolMetaLine(b); meta != "" {
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render("  └ ") + meta)
	}

	// Edit and write calls show their requested change as a Git-style proposal
	// only after the tool succeeds. Failed calls show their error output instead,
	// so the transcript never presents an unapplied change as if it landed.
	if len(b.toolDiff) > 0 && b.toolDone && !b.toolErr {
		sb.WriteByte('\n')
		sb.WriteString(t.renderDiff(b.toolDiff, width))
		return sb.String()
	}

	body := strings.TrimRight(b.toolOutput, "\n")
	if body == "" {
		return sb.String()
	}
	// Tool results are plain text (minified JSON, blobs, long URLs), so wrap
	// to the viewport before indenting — nothing may run off-screen.
	body = wrapPlain(body, max(1, width-2))
	lines := strings.Split(body, "\n")
	const previewLines = 8
	if !t.expanded && len(lines) > previewLines {
		shown := lines[:previewLines]
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render(indentBody(strings.Join(shown, "\n"))))
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render(fmt.Sprintf("… (%d more lines, ctrl+o to expand)", len(lines)-previewLines)))
	} else {
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render(indentBody(body)))
		if t.expanded && len(lines) > previewLines {
			sb.WriteByte('\n')
			sb.WriteString(t.th.muted.Render("(ctrl+o to collapse)"))
		}
	}
	return sb.String()
}

// wrapPlain reflows plain (non-markdown) text to the viewport width so long
// lines wrap instead of running off-screen. Unlike markdown, which glamour
// reflows, thinking bodies, errors, notices, and tool output are raw strings.
// wordwrap breaks on whitespace but leaves unbroken runs (a 300-char token
// blob, minified JSON) intact, so overlong words are hard-split to width.
func wrapPlain(text string, width int) string {
	if width <= 0 {
		return text
	}
	wrapped := wordwrap.String(strings.TrimRight(text, "\n"), width)
	lines := strings.Split(wrapped, "\n")
	changed := false
	for i, line := range lines {
		if len([]rune(line)) > width {
			lines[i] = splitRunes(line, width)
			changed = true
		}
	}
	if !changed {
		return wrapped
	}
	return strings.Join(lines, "\n")
}

// splitRunes hard-splits an unbroken line into width-sized chunks.
func splitRunes(line string, width int) string {
	runes := []rune(line)
	var sb strings.Builder
	for len(runes) > width {
		sb.WriteString(string(runes[:width]))
		sb.WriteByte('\n')
		runes = runes[width:]
	}
	sb.WriteString(string(runes))
	return sb.String()
}

// indentBody indents a multi-line tool output by two spaces so its text starts
// under the metadata line ("  └ …"), matching kj's wrapAndIndent. Empty lines
// stay empty so no trailing whitespace is emitted.
func indentBody(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = "  " + line
		}
	}
	return strings.Join(lines, "\n")
}

// toolMetaLine builds the dim "└ …" metadata shown under a completed tool
// header (kj's formatToolResultMetadata + withDuration): elapsed time, plus a
// result-size summary derived from the output.
func (t *transcript) toolMetaLine(b *block) string {
	if !b.toolDone {
		return ""
	}
	if b.toolErr {
		return t.th.toolError.Render("error")
	}
	var parts []string
	if b.toolTimed && b.toolDur > 0 {
		parts = append(parts, formatToolDuration(b.toolDur))
	}
	if body := strings.TrimRight(b.toolOutput, "\n"); body != "" {
		lines := strings.Count(body, "\n") + 1
		if lines > 1 {
			parts = append(parts, fmt.Sprintf("%d lines", lines))
		}
		parts = append(parts, formatByteCount(len(body)))
	}
	return t.th.muted.Render(strings.Join(parts, " · "))
}

// formatToolDuration renders an elapsed tool run kj-style: whole milliseconds
// under a second (avoids float-rounding artifacts like 0.35→"0.3s"), tenths
// of a second under ten seconds, whole seconds under a minute, and the Go
// "NmNs" form above.
func formatToolDuration(d time.Duration) string {
	if d < time.Millisecond {
		return "0ms"
	}
	switch {
	case d >= time.Minute:
		return fmt.Sprintf("%dm%ds", int(d/time.Minute), int((d%time.Minute)/time.Second))
	case d >= 10*time.Second:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d >= time.Second:
		return fmt.Sprintf("%.1fs", float64(d)/float64(time.Second))
	default:
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
}

// formatByteCount renders a byte count kj-style: raw bytes under 1 KB,
// one-decimal KB/MB above.
func formatByteCount(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

// thinkTokens estimates reasoning tokens for the live thinking header using
// the same chars-per-token heuristic as context estimation (see
// internal/agent/compaction EstimateMessageTokens). It is display-only:
// providers do not report per-delta usage while streaming, so we approximate
// from the accumulated thinking text.
func thinkTokens(text string) int {
	n := utf8.RuneCountInString(text)
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}

// renderThinking renders a collapsible thinking block: an accent header that
// reads "Thinking… (N tokens)" while streaming and "Thought [for Ns]"
// once complete, plus a muted body preview governed by the global ctrl+o
// expand toggle. The token count is streaming-only and never shown after
// completion.
func (t *transcript) renderThinking(b *block, width int) string {
	header := "✻ Thought"
	headerStyle := t.th.toolSuccess
	if !b.done {
		header = "✻ Thinking…"
		headerStyle = t.th.accent
		if n := thinkTokens(b.text); n > 0 {
			unit := "tokens"
			if n == 1 {
				unit = "token"
			}
			header = fmt.Sprintf("%s (%d %s)", header, n, unit)
		}
	} else if b.thinkTimed {
		header = "✻ Thought for " + formatThinkDur(b.thinkDur)
	}

	body := strings.TrimRight(b.text, "\n")
	if body == "" {
		return headerStyle.Render(header)
	}

	// Thinking streams are raw reasoning text with no markdown reflow, so
	// wrap them to the viewport — a long unbroken token run must break
	// across lines instead of running off-screen.
	body = wrapPlain(body, max(1, width-2))
	lines := strings.Split(body, "\n")
	var sb strings.Builder
	sb.WriteString(headerStyle.Render(header))
	const previewLines = 6
	if !t.expanded && len(lines) > previewLines {
		shown := lines[len(lines)-previewLines:]
		hidden := len(lines) - previewLines
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render(fmt.Sprintf("… %d earlier lines", hidden)))
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render(strings.Join(shown, "\n")))
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render(fmt.Sprintf("… (%d more lines, ctrl+o to expand)", hidden)))
	} else {
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render(body))
		if t.expanded && len(lines) > previewLines {
			sb.WriteByte('\n')
			sb.WriteString(t.th.muted.Render("(ctrl+o to collapse)"))
		}
	}
	return sb.String()
}

// formatThinkDur renders an elapsed thinking duration for the completed
// header: whole seconds below a minute, Go duration form ("1m30s") above.
func formatThinkDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int64(d.Round(time.Second).Seconds())
	if secs < 1 {
		secs = 1
	}
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	return (time.Duration(secs) * time.Second).String()
}

// diffLine is one display line in a proposal-style unified diff.
type diffLine struct {
	prefix byte
	text   string
}

const diffPreviewLines = 8

// proposalDiff turns edit/write tool arguments into a Git-style diff without
// reading the filesystem. A write is deliberately represented as a new file:
// it is a preview of the requested content, not a claim about prior contents.
func proposalDiff(name string, args map[string]any) []diffLine {
	path := toolArg(args, "path")
	if path == "" {
		path = toolArg(args, "file_path")
	}
	if path == "" {
		return nil
	}
	newPath := "b/" + strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")

	switch name {
	case "write":
		content, ok := args["content"].(string)
		if !ok {
			return nil
		}
		lines := []diffLine{{text: "--- /dev/null"}, {text: "+++ " + newPath}, {text: "@@"}}
		return append(lines, prefixedDiffLines('+', content)...)
	case "edit":
		rawEdits, ok := args["edits"].([]any)
		if !ok {
			return nil
		}
		lines := []diffLine{{text: "--- a/" + strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")}, {text: "+++ " + newPath}}
		for _, raw := range rawEdits {
			edit, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			oldText, oldOK := edit["oldText"].(string)
			newText, newOK := edit["newText"].(string)
			if !oldOK || !newOK {
				continue
			}
			lines = append(lines, prefixedDiffLines('-', oldText)...)
			lines = append(lines, prefixedDiffLines('+', newText)...)
		}
		if len(lines) == 2 {
			return nil
		}
		return lines
	default:
		return nil
	}
}

func toolArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	s, _ := args[key].(string)
	return s
}

func prefixedDiffLines(prefix byte, text string) []diffLine {
	// strings.Split intentionally retains a final empty line: an added or
	// removed trailing newline is meaningful in this compact preview.
	parts := strings.Split(text, "\n")
	lines := make([]diffLine, len(parts))
	for i, part := range parts {
		lines[i] = diffLine{prefix: prefix, text: part}
	}
	return lines
}

// renderDiff applies Git-like line coloring and the transcript's global
// ctrl+o preview limit. File headers and hunk markers are always retained.
func (t *transcript) renderDiff(lines []diffLine, width int) string {
	_ = width // retained for call-site compatibility; diff rows are text-colored only.
	visible := lines
	hidden := 0
	if !t.expanded {
		changeCount := 0
		visible = make([]diffLine, 0, len(lines))
		for _, line := range lines {
			if line.prefix != 0 {
				if changeCount >= diffPreviewLines {
					hidden++
					continue
				}
				changeCount++
			}
			visible = append(visible, line)
		}
	}

	var sb strings.Builder
	for i, line := range visible {
		if i > 0 {
			sb.WriteByte('\n')
		}
		text := line.text
		if line.prefix != 0 {
			text = string(line.prefix) + text
		}
		// Diff rows sit two cells right so they line up under the metadata
		// line with the indented body text above.
		text = "  " + text
		switch {
		case line.prefix == '+':
			sb.WriteString(t.th.diffAdd.Render(text))
		case line.prefix == '-':
			sb.WriteString(t.th.diffRemove.Render(text))
		case strings.HasPrefix(line.text, "@@"):
			sb.WriteString(t.th.diffHunk.Render(text))
		default:
			sb.WriteString(t.th.diffMeta.Render(text))
		}
	}
	if hidden > 0 {
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render(fmt.Sprintf("… (%d more changed lines, ctrl+o to expand)", hidden)))
	} else if t.expanded && len(lines) > diffPreviewLines {
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render("(ctrl+o to collapse)"))
	}
	return sb.String()
}

// toolHeaderParts splits the one-line summary into a bold tool name and a
// dim target, echoing pi/kj's forms:
//
//	read path[:range]      → ("read", "path[:range]")
//	edit path              → ("edit", "path")
//	write path             → ("write", "path")
//	$ <cmd>                → ("$", "<cmd>")
//	<name> {json args}     → ("<name>", "{json args}")
func (t *transcript) toolHeaderParts(b *block) (name, target string) {
	name = b.toolName
	arg := func(k string) string {
		if b.toolArgs == nil {
			return ""
		}
		if v, ok := b.toolArgs[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}
	pathArg := func() string {
		if p := arg("path"); p != "" {
			return p
		}
		return arg("file_path")
	}
	switch name {
	case "read", "edit", "write":
		return name, pathArg()
	case "bash":
		cmd := arg("command")
		if cmd == "" {
			cmd = arg("cmd")
		}
		return "$", firstLine(cmd)
	default:
		if len(b.toolArgs) == 0 {
			return name, ""
		}
		raw, _ := json.Marshal(b.toolArgs)
		return name, string(raw)
	}
}

// readFold groups a run of consecutive completed read calls on distinct paths
// so they render as one "● read N files" line (kj's FormatFoldedReads).
type readFold struct {
	count int
	end   int // index of the last block in the run
}

// foldReads returns the fold starting at index i when two or more consecutive
// read blocks follow, or nil. Reads of the same path (polling, retries) stay
// separate so repeated attention to one file remains visible.
func (t *transcript) foldReads(i int) *readFold {
	b := t.blocks[i]
	if b.kind != blockTool || b.toolName != "read" || !b.toolDone || b.toolErr {
		return nil
	}
	fold := &readFold{count: 1, end: i}
	seen := map[string]struct{}{t.readPath(b): {}}
	for j := i + 1; j < len(t.blocks); j++ {
		nb := t.blocks[j]
		if nb.kind != blockTool || nb.toolName != "read" || !nb.toolDone || nb.toolErr {
			break
		}
		p := t.readPath(nb)
		if _, dup := seen[p]; dup {
			break
		}
		seen[p] = struct{}{}
		fold.count++
		fold.end = j
	}
	if fold.count < 2 {
		return nil
	}
	return fold
}

// readPath extracts the path argument of a read call.
func (t *transcript) readPath(b *block) string {
	if p := toolArg(b.toolArgs, "path"); p != "" {
		return p
	}
	return toolArg(b.toolArgs, "file_path")
}

// renderFoldedReads renders a folded run of completed reads:
//
//	● read 3 files
//	  └ a.go · b.go · c.go · 1.2s
func (t *transcript) renderFoldedReads(fold *readFold) string {
	var sb strings.Builder
	sb.WriteString(t.th.toolSuccess.Render("● "))
	sb.WriteString(t.th.textPrimary.Bold(true).Render("read"))
	sb.WriteByte(' ')
	sb.WriteString(t.th.muted.Render(fmt.Sprintf("%d files", fold.count)))

	paths := make([]string, 0, fold.count)
	var total time.Duration
	timed := true
	for j := fold.end - fold.count + 1; j <= fold.end; j++ {
		b := t.blocks[j]
		if p := t.readPath(b); p != "" {
			paths = append(paths, p)
		}
		if !b.toolTimed {
			timed = false
		}
		total += b.toolDur
	}
	var meta []string
	if timed && total > 0 {
		meta = append(meta, formatToolDuration(total))
	}
	meta = append(meta, paths...)
	sb.WriteByte('\n')
	sb.WriteString(t.th.muted.Render("  └ " + strings.Join(meta, " · ")))
	return sb.String()
}

// resultText flattens a ToolResult's content into text for display.
func resultText(r *types.ToolResult) string {
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

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
