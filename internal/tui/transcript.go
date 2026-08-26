package tui

import (
	"encoding/json"
	"fmt"
	"strings"

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

// toggleExpand flips the global tool expand state and invalidates tool and
// thinking caches.
func (t *transcript) toggleExpand() {
	t.expanded = !t.expanded
	for _, b := range t.blocks {
		if b.kind == blockTool || b.kind == blockThinking {
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
	t.blocks = append(t.blocks, &block{kind: blockThinking})
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
			b.cacheValid = false
			if strings.TrimSpace(b.text) == "" {
				t.blocks = append(t.blocks[:t.streamingIdx], t.blocks[t.streamingIdx+1:]...)
			}
		}
	}
	t.streamingIdx = -1
}

// endAssistant finalizes the current assistant block. If it never received any
// text (a tool-only turn), it is removed to avoid an empty gap.
func (t *transcript) endAssistant() {
	if t.streamingIdx >= 0 && t.streamingIdx < len(t.blocks) {
		b := t.blocks[t.streamingIdx]
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

// startTool appends a tool block in the pending state.
func (t *transcript) startTool(callID, name string, args map[string]any) {
	t.blocks = append(t.blocks, &block{
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
// entirely when thinking is hidden, so no stray separators remain.
func (t *transcript) render(width int) string {
	var sb strings.Builder
	for _, b := range t.blocks {
		if b.kind == blockThinking && !t.showThinking {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(t.renderBlock(b, width))
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
		out = strings.TrimRight(t.md.render(b.text, width), "\n")
	case blockError:
		out = t.th.errorText.Render(b.text)
	case blockNotice:
		out = t.th.muted.Render(b.text)
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

// renderTool renders a collapsible tool block: a one-line status header plus an
// optional preview (collapsed) or full output (expanded). Status is conveyed by
// the header color (pending/success/error), matching pi.
func (t *transcript) renderTool(b *block, width int) string {
	header := t.toolHeader(b)
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
	sb.WriteString(statusStyle.Render(header))

	// Edit and write calls show their requested change as a Git-style proposal
	// only after the tool succeeds. Failed calls show their error output instead,
	// so the transcript never presents an unapplied change as if it landed.
	if len(b.toolDiff) > 0 && b.toolDone && !b.toolErr {
		sb.WriteByte('\n')
		sb.WriteString(t.renderDiff(b.toolDiff))
		return sb.String()
	}

	body := strings.TrimRight(b.toolOutput, "\n")
	if body == "" {
		return sb.String()
	}
	lines := strings.Split(body, "\n")
	const previewLines = 8
	if !t.expanded && len(lines) > previewLines {
		shown := lines[:previewLines]
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render(strings.Join(shown, "\n")))
		sb.WriteByte('\n')
		sb.WriteString(t.th.muted.Render(fmt.Sprintf("… (%d more lines, ctrl+o to expand)", len(lines)-previewLines)))
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

// renderThinking renders a collapsible thinking block: an accent header that
// reads "Thinking…" while streaming and "Thought" once complete, plus a muted
// body preview governed by the global ctrl+o expand toggle.
func (t *transcript) renderThinking(b *block, width int) string {
	header := "✻ Thought"
	headerStyle := t.th.toolSuccess
	if !b.done {
		header = "✻ Thinking…"
		headerStyle = t.th.accent
	}

	body := strings.TrimRight(b.text, "\n")
	if body == "" {
		return headerStyle.Render(header)
	}

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
func (t *transcript) renderDiff(lines []diffLine) string {
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
