package tui

import (
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
	"github.com/charmbracelet/x/ansi"
)

func newThinkingTestTranscript() *transcript {
	return newTranscript(newTheme(), newMDRenderer())
}

func renderedThinkingTranscript(tr *transcript) string {
	return ansi.Strip(tr.render(80))
}

func TestThinkingBlockAccumulatesDeltas(t *testing.T) {
	tr := newThinkingTestTranscript()
	tr.beginAssistant()
	tr.beginThinking()
	tr.appendThinkingDelta("step one ")
	tr.appendThinkingDelta("and two")
	tr.endThinking()
	tr.appendAssistantDelta("final answer")
	tr.endAssistant()

	if len(tr.blocks) != 2 {
		t.Fatalf("block count = %d, want 2 (thinking + assistant)", len(tr.blocks))
	}
	if tr.blocks[0].kind != blockThinking || tr.blocks[0].text != "step one and two" {
		t.Fatalf("thinking block = %#v", tr.blocks[0])
	}
	if tr.blocks[1].kind != blockAssistant || tr.blocks[1].text != "final answer" {
		t.Fatalf("assistant block = %#v", tr.blocks[1])
	}
}

func TestBeginThinkingRemovesEmptyAssistantBlock(t *testing.T) {
	tr := newThinkingTestTranscript()
	tr.beginAssistant() // opened by message_start before thinking arrives
	tr.beginThinking()

	for _, b := range tr.blocks {
		if b.kind == blockAssistant {
			t.Fatalf("empty assistant block survived: %#v", b)
		}
	}
	if len(tr.blocks) != 1 || tr.blocks[0].kind != blockThinking {
		t.Fatalf("blocks = %#v, want a single thinking block", tr.blocks)
	}
}

func TestEndThinkingRemovesEmptyBlock(t *testing.T) {
	tr := newThinkingTestTranscript()
	tr.beginThinking()
	tr.endThinking() // no deltas ever arrived
	if len(tr.blocks) != 0 {
		t.Fatalf("empty thinking block not removed: %d blocks", len(tr.blocks))
	}
}

func TestThinkingHiddenByToggle(t *testing.T) {
	tr := newThinkingTestTranscript()
	tr.setShowThinking(false)
	tr.addUser("hello")
	tr.beginThinking()
	tr.appendThinkingDelta("secret reasoning")
	tr.endThinking()
	tr.beginAssistant()
	tr.appendAssistantDelta("visible answer")
	tr.endAssistant()

	out := renderedThinkingTranscript(tr)
	if strings.Contains(out, "secret reasoning") || strings.Contains(out, "Thought") {
		t.Fatalf("rendered thinking while hidden:\n%s", out)
	}
	if !strings.Contains(out, "visible answer") {
		t.Fatalf("answer missing from render:\n%s", out)
	}

	// Toggling back on reveals the accumulated text retroactively.
	tr.setShowThinking(true)
	out = renderedThinkingTranscript(tr)
	if !strings.Contains(out, "secret reasoning") {
		t.Fatalf("thinking not revealed after toggle:\n%s", out)
	}
}

func TestThinkingRenderStates(t *testing.T) {
	tr := newThinkingTestTranscript()

	// Streaming state shows the live header.
	tr.beginThinking()
	tr.appendThinkingDelta("musing")
	out := renderedThinkingTranscript(tr)
	if !strings.Contains(out, "◇ Thinking…") || !strings.Contains(out, "musing") {
		t.Fatalf("streaming render = %q", out)
	}
	tr.endThinking()

	// Completed state flips the header.
	out = renderedThinkingTranscript(tr)
	if !strings.Contains(out, "◆ Thought") || !strings.Contains(out, "musing") {
		t.Fatalf("completed render = %q", out)
	}

	// Long bodies collapse to a tail preview; ctrl+o expands them.
	long := strings.Repeat("line\n", 20) + "end"
	tr.beginThinking()
	tr.appendThinkingDelta(long)
	tr.endThinking()
	collapsed := renderedThinkingTranscript(tr)
	if strings.Contains(collapsed, long) || !strings.Contains(collapsed, "ctrl+o to expand") {
		t.Fatalf("collapsed render should preview the tail:\n%s", collapsed)
	}
	tr.toggleExpand()
	expanded := renderedThinkingTranscript(tr)
	if !strings.Contains(expanded, "end") || !strings.Contains(expanded, "(ctrl+o to collapse)") {
		t.Fatalf("expanded render =\n%s", expanded)
	}
}

func TestEndAssistantFinalizesThinkingOnMidReasoningAbort(t *testing.T) {
	tr := newThinkingTestTranscript()
	tr.beginAssistant()
	tr.appendAssistantDelta("let me think")
	tr.endAssistant()
	tr.beginAssistant() // next turn opens; message_start arrives
	tr.beginThinking()
	tr.appendThinkingDelta("half-finished reasoning")

	// Response ends mid-reasoning (esc abort, provider error): no
	// thinking_end and no text delta ever arrive.
	tr.endAssistant()

	if len(tr.blocks) != 2 {
		t.Fatalf("block count = %d, want 2", len(tr.blocks))
	}
	if !tr.blocks[1].done {
		t.Fatal("thinking block left unfinished after endAssistant")
	}
	out := renderedThinkingTranscript(tr)
	if strings.Contains(out, "Thinking\u2026") {
		t.Fatalf("transcript still shows streaming header after end:\n%s", out)
	}
	if !strings.Contains(out, "\u25c6 Thought") || !strings.Contains(out, "half-finished reasoning") {
		t.Fatalf("completed thinking content missing:\n%s", out)
	}
}

func TestSeedTranscriptPreservesThinkingOrder(t *testing.T) {
	history := []types.Message{
		userMessage("prompt"),
		{
			Role: types.RoleAssistant,
			Content: []types.ContentBlock{
				{Type: types.ContentThinking, Thinking: "first thought"},
				{Type: types.ContentText, Text: "middle answer"},
				{Type: types.ContentThinking, Thinking: "second thought"},
				{Type: types.ContentText, Text: "closing text"},
			},
			Timestamp: 1,
		},
	}
	tr := newThinkingTestTranscript()
	seedTranscript(tr, history)

	var kinds []blockKind
	for _, b := range tr.blocks {
		kinds = append(kinds, b.kind)
	}
	want := []blockKind{blockUser, blockThinking, blockAssistant, blockThinking, blockAssistant}
	if len(kinds) != len(want) {
		t.Fatalf("kinds = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
	if tr.blocks[1].text != "first thought" || tr.blocks[2].text != "middle answer" ||
		tr.blocks[3].text != "second thought" || tr.blocks[4].text != "closing text" {
		t.Fatalf("seeded text mismatch: %q %q %q %q",
			tr.blocks[1].text, tr.blocks[2].text, tr.blocks[3].text, tr.blocks[4].text)
	}

	// Seeded thinking is complete and renders with the Thought header.
	out := renderedThinkingTranscript(tr)
	if !strings.Contains(out, "◆ Thought") {
		t.Fatalf("seeded thinking should be complete:\n%s", out)
	}
}
