package llm

import (
	"encoding/json"

	"github.com/myagent/myagent/internal/types"
)

// accumulator builds up the streamed assistant message block-by-block and emits
// the corresponding StreamEvents. Ported from pi's ensure*Block / finishBlock
// logic (packages/ai/src/api/openai-completions.ts).
type accumulator struct {
	output *types.Message
	out    chan<- StreamEvent

	textIndex     int
	thinkingIndex int
	hasText       bool
	hasThinking   bool

	// tool-call bookkeeping: index-first, id-second (pi ensureToolCallBlock).
	byStreamIndex map[int]int    // provider stream index -> content index
	byID          map[string]int // tool call id -> content index
	partialArgs   map[int]string // content index -> accumulated raw args
}

func newAccumulator(output *types.Message, out chan<- StreamEvent) *accumulator {
	return &accumulator{
		output:        output,
		out:           out,
		textIndex:     -1,
		thinkingIndex: -1,
		byStreamIndex: map[int]int{},
		byID:          map[string]int{},
		partialArgs:   map[int]string{},
	}
}

func (a *accumulator) emit(ev StreamEvent) {
	ev.Partial = cloneMessage(a.output)
	a.out <- ev
}

func (a *accumulator) appendText(delta string) {
	if !a.hasText {
		a.hasText = true
		a.output.Content = append(a.output.Content, types.ContentBlock{Type: types.ContentText})
		a.textIndex = len(a.output.Content) - 1
		a.emit(StreamEvent{Type: "text_start", ContentIndex: a.textIndex})
	}
	a.output.Content[a.textIndex].Text += delta
	a.emit(StreamEvent{Type: "text_delta", ContentIndex: a.textIndex, Delta: delta})
}

func (a *accumulator) appendThinking(delta string) {
	if !a.hasThinking {
		a.hasThinking = true
		a.output.Content = append(a.output.Content, types.ContentBlock{Type: types.ContentThinking})
		a.thinkingIndex = len(a.output.Content) - 1
		a.emit(StreamEvent{Type: "thinking_start", ContentIndex: a.thinkingIndex})
	}
	a.output.Content[a.thinkingIndex].Thinking += delta
	a.emit(StreamEvent{Type: "thinking_delta", ContentIndex: a.thinkingIndex, Delta: delta})
}

func (a *accumulator) applyToolCall(tc deltaToolCall) {
	// Resolve or create the content block. Index-first, then id.
	idx, ok := a.byStreamIndex[tc.Index]
	if !ok && tc.ID != "" {
		idx, ok = a.byID[tc.ID]
	}
	if !ok {
		a.output.Content = append(a.output.Content, types.ContentBlock{
			Type:      types.ContentToolCall,
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: map[string]any{},
		})
		idx = len(a.output.Content) - 1
		a.byStreamIndex[tc.Index] = idx
		if tc.ID != "" {
			a.byID[tc.ID] = idx
		}
		a.emit(StreamEvent{Type: "toolcall_start", ContentIndex: idx})
	}

	block := &a.output.Content[idx]
	if block.ID == "" && tc.ID != "" {
		block.ID = tc.ID
		a.byID[tc.ID] = idx
	}
	if block.Name == "" && tc.Function.Name != "" {
		block.Name = tc.Function.Name
	}
	if tc.Function.Arguments != "" {
		a.partialArgs[idx] += tc.Function.Arguments
		block.Arguments = parseStreamingJSON(a.partialArgs[idx])
	}
	a.emit(StreamEvent{Type: "toolcall_delta", ContentIndex: idx, Delta: tc.Function.Arguments})
}

// finish emits the *_end events for every open block, doing a final parse of
// tool-call arguments. Mirrors pi's finalize pass.
func (a *accumulator) finish() {
	for i := range a.output.Content {
		block := &a.output.Content[i]
		switch block.Type {
		case types.ContentText:
			a.emit(StreamEvent{Type: "text_end", ContentIndex: i})
		case types.ContentThinking:
			a.emit(StreamEvent{Type: "thinking_end", ContentIndex: i})
		case types.ContentToolCall:
			if raw, ok := a.partialArgs[i]; ok {
				block.Arguments = parseStreamingJSON(raw)
			}
			a.emit(StreamEvent{Type: "toolcall_end", ContentIndex: i})
		}
	}
}

// parseStreamingJSON tolerantly parses accumulated tool-call arguments,
// returning an empty object on failure. Mirrors pi's parseStreamingJson: never
// throws, always returns an object.
func parseStreamingJSON(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err == nil && m != nil {
		return m
	}
	if repaired, ok := repairPartialJSON(raw); ok {
		var m2 map[string]any
		if err := json.Unmarshal([]byte(repaired), &m2); err == nil && m2 != nil {
			return m2
		}
	}
	return map[string]any{}
}

// repairPartialJSON attempts a best-effort completion of a truncated JSON
// object by closing any open strings, arrays, and objects. This is a
// simplified stand-in for pi's partial-json parser: good enough to salvage
// arguments during streaming; the final parse usually sees complete JSON.
func repairPartialJSON(s string) (string, bool) {
	var stack []byte
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if !inString && len(stack) == 0 {
		return s, false // nothing to repair
	}
	b := []byte(s)
	if inString {
		b = append(b, '"')
	}
	for i := len(stack) - 1; i >= 0; i-- {
		b = append(b, stack[i])
	}
	return string(b), true
}
