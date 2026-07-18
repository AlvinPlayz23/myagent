package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/myagent/myagent/internal/types"
)

// blockKind discriminates a transcript block.
type blockKind int

const (
	blockUser blockKind = iota
	blockAssistant
	blockTool
	blockError
	blockNotice
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
	toolOutput string
	toolErr    bool
	toolDone   bool

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

	// streamingIdx points at the assistant block currently being streamed, or
	// -1 when none.
	streamingIdx int
}

func newTranscript(th *theme, md *mdRenderer) *transcript {
	return &transcript{th: th, md: md, streamingIdx: -1}
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

// toggleExpand flips the global tool expand state and invalidates tool caches.
func (t *transcript) toggleExpand() {
	t.expanded = !t.expanded
	for _, b := range t.blocks {
		if b.kind == blockTool {
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
func (t *transcript) appendAssistantDelta(delta string) {
	if t.streamingIdx < 0 || t.streamingIdx >= len(t.blocks) {
		t.beginAssistant()
	}
	b := t.blocks[t.streamingIdx]
	b.text += delta
	b.cacheValid = false
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
// are separated by a blank line (pi's Spacer(1)).
func (t *transcript) render(width int) string {
	var sb strings.Builder
	for i, b := range t.blocks {
		if i > 0 {
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
		// User = filled neutral block, rendered as markdown inside a bg box.
		body := strings.TrimRight(t.md.render(b.text, max(1, width-2)), "\n")
		out = t.th.userBlock.Render(body)
	case blockAssistant:
		out = strings.TrimRight(t.md.render(b.text, width), "\n")
	case blockError:
		out = t.th.errorText.Render(b.text)
	case blockNotice:
		out = t.th.muted.Render(b.text)
	case blockTool:
		out = t.renderTool(b, width)
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
