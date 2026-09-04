package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/AlvinPlayz23/myagent/internal/types"
	"github.com/charmbracelet/x/ansi"
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
	text      string
	timestamp int64 // Unix milliseconds; user turns use it for the card metadata.

	// tool fields
	toolCallID string
	toolName   string
	toolArgs   map[string]any
	toolDiff   []diffLine // proposal diff for edit/write calls
	toolOutput string
	toolErr    bool
	toolDone   bool

	// thinking fields
	done bool // streaming finished (thinking blocks)

	// identity for hit-testing, and a revision bumped on every change so
	// the layout cache can skip unchanged blocks.
	id  int
	rev uint64

	// per-block fold override for tool and thinking blocks; unset follows
	// the global ctrl+o state.
	foldSet bool
	folded  bool

	// laid-out rows cache, keyed on width, revision, and fold state
	layoutRows   []layoutRow
	layoutWidth  int
	layoutRev    uint64
	layoutExpand bool
	layoutValid  bool
}

// touch invalidates the block after a content or fold change.
func (b *block) touch() {
	b.rev++
	b.layoutValid = false
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

	// nextID hands out block identities for hit-testing.
	nextID int
}

func newTranscript(th *theme, md *mdRenderer) *transcript {
	return &transcript{th: th, md: md, streamingIdx: -1, showThinking: true}
}

// add appends a block with a fresh identity.
func (t *transcript) add(b *block) *block {
	t.nextID++
	b.id = t.nextID
	t.blocks = append(t.blocks, b)
	return b
}

// invalidate clears cached renders (e.g. on width change or expand toggle).
func (t *transcript) invalidate() {
	for _, b := range t.blocks {
		b.layoutValid = false
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
	t.showThinking = show
}

// toggleExpand flips the global tool expand state and invalidates tool and
// thinking caches.
func (t *transcript) toggleExpand() {
	t.expanded = !t.expanded
	for _, b := range t.blocks {
		if b.kind == blockTool || b.kind == blockThinking {
			b.foldSet = false
			b.touch()
		}
	}
}

// addUser appends a user block.
func (t *transcript) addUser(text string) {
	t.addUserAt(text, 0)
}

// addUserAt appends a user block with its durable message timestamp. Keeping
// this in the block means resumed and live sessions use the same card chrome.
func (t *transcript) addUserAt(text string, timestamp int64) {
	t.add(&block{kind: blockUser, text: text, timestamp: timestamp})
}

// beginAssistant starts a new (empty) streaming assistant block.
func (t *transcript) beginAssistant() {
	t.add(&block{kind: blockAssistant})
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
	b.touch()
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
	t.add(&block{kind: blockThinking})
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
	b.touch()
}

// endThinking finalizes the current thinking block, removing it if it never
// received text (mirrors endAssistant). A completed block collapses to its
// "✻ Thought" header until expanded.
func (t *transcript) endThinking() {
	if t.streamingIdx >= 0 && t.streamingIdx < len(t.blocks) {
		b := t.blocks[t.streamingIdx]
		if b.kind == blockThinking {
			b.done = true
			b.touch()
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
	t.add(&block{kind: blockError, text: text})
}

// addNotice appends a muted system-notice block (e.g. compaction summary).
func (t *transcript) addNotice(text string) {
	t.add(&block{kind: blockNotice, text: text})
}

// startTool appends a tool block in the pending state.
func (t *transcript) startTool(callID, name string, args map[string]any) {
	t.add(&block{
		kind:       blockTool,
		toolCallID: callID,
		toolName:   name,
		toolArgs:   args,
		toolDiff:   proposalDiff(name, args),
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
	b.touch()
}

func (t *transcript) findTool(callID string) *block {
	for i := len(t.blocks) - 1; i >= 0; i-- {
		if t.blocks[i].kind == blockTool && t.blocks[i].toolCallID == callID {
			return t.blocks[i]
		}
	}
	return nil
}

// render lays out every visible block at width and joins the rows. The
// trailing newline mirrors the historical block-per-line output.
func (t *transcript) render(width int) string {
	rows := t.layout(width)
	if len(rows) == 0 {
		return ""
	}
	return renderRows(rows) + "\n"
}

// layout lays out every visible block into terminal rows at width. Rows are
// cached per block, so streaming deltas re-wrap only the growing block.
func (t *transcript) layout(width int) []layoutRow {
	var rows []layoutRow
	for _, b := range t.blocks {
		if b.kind == blockThinking && !t.showThinking {
			continue
		}
		if len(rows) > 0 {
			rows = append(rows, layoutRow{kind: rowSpacer})
		}
		rows = append(rows, t.layoutBlock(b, width)...)
	}
	return rows
}

// layoutBlock returns one block's cached rows, rebuilding only when its
// content revision, width, or fold state changed.
func (t *transcript) layoutBlock(b *block, width int) []layoutRow {
	expand := t.effectiveExpand(b)
	if b.layoutValid && b.layoutWidth == width && b.layoutRev == b.rev && b.layoutExpand == expand {
		return b.layoutRows
	}
	var rows []layoutRow
	switch b.kind {
	case blockUser:
		rows = t.layoutUser(b, width)
	case blockAssistant:
		rows = t.layoutAssistant(b, width)
	case blockError:
		rows = plainRows(b.text, t.th.errorText, rowError, b.id, width)
	case blockNotice:
		rows = plainRows(b.text, t.th.muted, rowNotice, b.id, width)
	case blockTool:
		rows = t.layoutTool(b, width)
	case blockThinking:
		rows = t.layoutThinking(b, width)
	}
	b.layoutRows, b.layoutWidth, b.layoutRev, b.layoutExpand, b.layoutValid = rows, width, b.rev, expand, true
	return rows
}

// layoutUser renders a user message as a Grok-style prompt: a `❯` arrow in
// the user accent followed by the message text, with wrapped continuation
// lines indented to the arrow's width. The arrow and indent are explicit
// gutter chrome so selection columns line up with rendered cells and copying
// never picks them up.
func (t *transcript) layoutUser(b *block, width int) []layoutRow {
	inner := max(1, width-2)
	body := strings.TrimRight(wordwrap.String(b.text, inner), "\n")
	lines := strings.Split(body, "\n")
	rows := make([]layoutRow, 0, len(lines))
	for i, line := range lines {
		prefix := "  "
		if i == 0 {
			prefix = "❯ "
		}
		meta := ""
		if i == 0 && b.timestamp > 0 && width >= 36 {
			meta = time.UnixMilli(b.timestamp).Local().Format("3:04 PM")
		}
		fill := max(0, width-2-ansi.StringWidth(line))
		metaGap := 0
		if meta != "" {
			metaWidth := ansi.StringWidth(meta)
			if fill > metaWidth {
				metaGap = fill - metaWidth
				fill = 0
			} else {
				meta = ""
			}
		}
		spans := []layoutSpan{
			{text: prefix, style: t.th.userPrefix},
			{text: line + strings.Repeat(" ", fill+metaGap), style: t.th.userText},
		}
		suffixCols := 0
		if meta != "" {
			spans = append(spans, layoutSpan{text: meta, style: t.th.userMeta})
			suffixCols = ansi.StringWidth(meta)
		}
		rows = append(rows, layoutRow{
			kind:             rowUser,
			blockID:          b.id,
			lineIdx:          i,
			spans:            spans,
			gutterCols:       2,
			gutterSuffixCols: suffixCols,
		})
	}
	return wrapRows(rows, width)
}

// layoutAssistant renders assistant Markdown. Glamour already wraps to width,
// so each source line is one row carrying the renderer's ANSI verbatim.
func (t *transcript) layoutAssistant(b *block, width int) []layoutRow {
	out := strings.TrimRight(t.md.render(b.text, width), "\n")
	if out == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	rows := make([]layoutRow, 0, len(lines))
	for i, line := range lines {
		rows = append(rows, layoutRow{
			kind:    rowAssistant,
			blockID: b.id,
			lineIdx: i,
			spans:   []layoutSpan{{text: line, raw: true}},
		})
	}
	return rows
}

// renderTool renders a tool block as a string. The interactive path consumes
// layout rows directly; this wrapper keeps block rendering easy to test.
func (t *transcript) renderTool(b *block, width int) string {
	return renderRows(t.layoutTool(b, width))
}

// layoutTool builds a collapsible tool block: a one-line status header plus a
// proposal diff (successful edit/write) or folded output. Status is conveyed
// by the header color (pending/success/error); the header row toggles the
// block's fold on click.
func (t *transcript) layoutTool(b *block, width int) []layoutRow {
	expand := t.effectiveExpand(b)
	statusStyle := t.th.toolSuccess
	switch {
	case !b.toolDone:
		statusStyle = t.th.toolPending
	case b.toolErr:
		statusStyle = t.th.toolError
	}
	// The state glyph and fold arrow are visual-only: status also reads
	// through the header color, and the fold direction is chrome. The title
	// stays neutral gray; shell commands read in the command yellow.
	glyph := "✓ " // check: succeeded
	switch {
	case !b.toolDone:
		glyph = "• " // bullet: running
	case b.toolErr:
		glyph = "✗ " // ballot x: failed
	}
	titleStyle := t.th.toolTitle
	if strings.HasPrefix(t.toolHeader(b), "$ ") {
		titleStyle = t.th.toolCommand
	}
	body := strings.TrimRight(b.toolOutput, "\n")
	hasBody := (len(b.toolDiff) > 0 && b.toolDone && !b.toolErr) || body != ""
	fold := ""
	if hasBody {
		if expand {
			fold = " ▾" // expanded
		} else {
			fold = " ▸" // folded
		}
	}
	rows := []layoutRow{{
		kind:    rowToolHeader,
		blockID: b.id,
		spans: []layoutSpan{
			{text: glyph, style: statusStyle},
			{text: t.toolHeader(b) + fold, style: titleStyle},
		},
		gutterCols:       2,
		gutterSuffixCols: len([]rune(fold)),
		toggle:           true,
	}}

	// Edit and write calls show their requested change as a Git-style proposal
	// only after the tool succeeds. Failed calls show their error output
	// instead, so the transcript never presents an unapplied change as if it
	// landed.
	if len(b.toolDiff) > 0 && b.toolDone && !b.toolErr {
		return append(rows, t.diffRows(b, expand, width)...)
	}

	if body == "" {
		return rows
	}
	lines := strings.Split(body, "\n")
	const previewLines = 8
	if !expand && len(lines) > previewLines {
		for i, line := range lines[:previewLines] {
			rows = append(rows, toolRow(b, i, line, t.th.muted))
		}
		rows = append(rows, toolRow(b, previewLines,
			fmt.Sprintf("… (%d more lines, ctrl+o to expand)", len(lines)-previewLines), t.th.muted))
		return wrapRows(rows, width)
	}
	for i, line := range lines {
		rows = append(rows, toolRow(b, i, line, t.th.muted))
	}
	if expand && len(lines) > previewLines {
		rows = append(rows, toolRow(b, len(lines), "(ctrl+o to collapse)", t.th.muted))
	}
	return wrapRows(rows, width)
}

func toolRow(b *block, lineIdx int, text string, style lipgloss.Style) layoutRow {
	return layoutRow{
		kind:    rowToolOutput,
		blockID: b.id,
		lineIdx: lineIdx,
		spans:   []layoutSpan{{text: text, style: style}},
	}
}

// renderThinking renders a thinking block as a string (tests).
func (t *transcript) renderThinking(b *block, width int) string {
	return renderRows(t.layoutThinking(b, width))
}

// layoutThinking builds a collapsible thinking block: an accent header that
// reads "Thinking…" while streaming and "Thought" once complete, plus a muted
// body preview governed by the fold state. The header row toggles the fold.
func (t *transcript) layoutThinking(b *block, width int) []layoutRow {
	expand := t.effectiveExpand(b)
	label := "Thought"
	// State reads through both glyph and color: hollow diamond while
	// streaming, filled once complete, in the violet shared with the brand.
	glyph := "◆"
	headerStyle := t.th.orbBright
	if !b.done {
		label = "Thinking…"
		glyph = "◇"
	}
	body := strings.TrimRight(b.text, "\n")
	fold := ""
	if body != "" {
		if expand {
			fold = " ▾"
		} else {
			fold = " ▸"
		}
	}
	rows := []layoutRow{{
		kind:             rowThinkingHeader,
		blockID:          b.id,
		spans:            []layoutSpan{{text: glyph + " " + label + fold, style: headerStyle}},
		gutterCols:       2, // the marker glyph is visual-only chrome
		gutterSuffixCols: len([]rune(fold)),
		toggle:           true,
	}}
	if body == "" {
		return rows
	}
	lines := strings.Split(body, "\n")
	const previewLines = 6
	if !expand && len(lines) > previewLines {
		shown := lines[len(lines)-previewLines:]
		hidden := len(lines) - previewLines
		rows = append(rows, thinkingRow(b, -1, fmt.Sprintf("… %d earlier lines", hidden), t.th.muted))
		for i, line := range shown {
			rows = append(rows, thinkingRow(b, len(lines)-previewLines+i, line, t.th.muted))
		}
		rows = append(rows, thinkingRow(b, len(lines),
			fmt.Sprintf("… (%d more lines, ctrl+o to expand)", hidden), t.th.muted))
		return wrapRows(rows, width)
	}
	for i, line := range lines {
		rows = append(rows, thinkingRow(b, i, line, t.th.muted))
	}
	if expand && len(lines) > previewLines {
		rows = append(rows, thinkingRow(b, len(lines), "(ctrl+o to collapse)", t.th.muted))
	}
	return wrapRows(rows, width)
}

func thinkingRow(b *block, lineIdx int, text string, style lipgloss.Style) layoutRow {
	return layoutRow{
		kind:    rowThinking,
		blockID: b.id,
		lineIdx: lineIdx,
		spans:   []layoutSpan{{text: text, style: style}},
	}
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

// diffRows renders a proposal diff. Prefix characters are gutter spans, so
// copying selects the changed text without the +/- chrome. File headers and
// hunk markers are always retained; collapsed previews cap changed lines.
func (t *transcript) diffRows(b *block, expand bool, width int) []layoutRow {
	lines := b.toolDiff
	hidden := 0
	if !expand {
		changeCount := 0
		visible := make([]diffLine, 0, len(lines))
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
		lines = visible
	}
	rows := make([]layoutRow, 0, len(lines)+1)
	for i, line := range lines {
		var style lipgloss.Style
		kind := rowDiffMeta
		switch {
		case line.prefix == '+':
			style, kind = t.th.diffAdd, rowDiff
		case line.prefix == '-':
			style, kind = t.th.diffRemove, rowDiff
		case strings.HasPrefix(line.text, "@@"):
			style = t.th.diffHunk
		default:
			style = t.th.diffMeta
		}
		text := line.text
		gutter := 0
		if line.prefix != 0 {
			text = string(line.prefix) + text
			gutter = 1 // the +/- prefix is visual-only chrome
		}
		rows = append(rows, layoutRow{
			kind:       kind,
			blockID:    b.id,
			lineIdx:    i,
			spans:      []layoutSpan{{text: text, style: style}},
			gutterCols: gutter,
		})
	}
	if hidden > 0 {
		rows = append(rows, layoutRow{kind: rowDiffMeta, blockID: b.id, lineIdx: len(rows),
			spans: []layoutSpan{{text: fmt.Sprintf("… (%d more changed lines, ctrl+o to expand)", hidden), style: t.th.muted}}})
	} else if expand && len(b.toolDiff) > diffPreviewLines {
		rows = append(rows, layoutRow{kind: rowDiffMeta, blockID: b.id, lineIdx: len(rows),
			spans: []layoutSpan{{text: "(ctrl+o to collapse)", style: t.th.muted}}})
	}
	return wrapRows(rows, width)
}

// effectiveExpand reports whether a block shows its full content: the global
// ctrl+o state unless the user folded or unfolded this block directly.
func (t *transcript) effectiveExpand(b *block) bool {
	if b.foldSet {
		return !b.folded
	}
	return t.expanded
}

// toggleBlockFold flips one block's fold, leaving other blocks and the
// global state alone. Clicking a tool or thinking header routes here.
func (t *transcript) toggleBlockFold(id int) {
	b := t.blockByID(id)
	if b == nil || (b.kind != blockTool && b.kind != blockThinking) {
		return
	}
	if !b.foldSet {
		// Capture what is currently shown *before* marking the override:
		// folded=true means collapsed, so storing the current display state
		// inverts it.
		currently := t.effectiveExpand(b)
		b.foldSet = true
		b.folded = currently
	} else {
		b.folded = !b.folded
	}
	b.touch()
}

// blockByID finds a block by its layout identity.
func (t *transcript) blockByID(id int) *block {
	for _, b := range t.blocks {
		if b.id == id {
			return b
		}
	}
	return nil
}

// toolHeader builds the bold one-line summary per tool, echoing pi's forms:
//
//	read path[:range]
//	edit path
//	write path
//	$ <cmd>
//	<name> {json args}
func (t *transcript) toolHeader(b *block) string {
	name := b.toolName
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
	switch name {
	case "read":
		p := arg("path")
		if p == "" {
			p = arg("file_path")
		}
		return "read " + p
	case "edit":
		p := arg("path")
		if p == "" {
			p = arg("file_path")
		}
		return "edit " + p
	case "write":
		p := arg("path")
		if p == "" {
			p = arg("file_path")
		}
		return "write " + p
	case "bash":
		cmd := arg("command")
		if cmd == "" {
			cmd = arg("cmd")
		}
		return "$ " + firstLine(cmd)
	default:
		if len(b.toolArgs) == 0 {
			return name
		}
		raw, _ := json.Marshal(b.toolArgs)
		return name + " " + string(raw)
	}
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
