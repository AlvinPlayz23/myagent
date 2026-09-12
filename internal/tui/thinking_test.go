package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func newThinkingTestTranscript() *transcript {
	return newTranscript(newTheme(), newMDRenderer())
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

	out := tr.render(80)
	plain := ansi.Strip(out)
	if strings.Contains(plain, "secret reasoning") || strings.Contains(plain, "Thought") {
		t.Fatalf("rendered thinking while hidden:\n%s", out)
	}
	if !strings.Contains(plain, "visible answer") {
		t.Fatalf("answer missing from render:\n%s", out)
	}

	// Toggling back on reveals the accumulated text retroactively.
	tr.setShowThinking(true)
	out = tr.render(80)
	if !strings.Contains(ansi.Strip(out), "secret reasoning") {
		t.Fatalf("thinking not revealed after toggle:\n%s", out)
	}
}

func TestThinkingRenderStates(t *testing.T) {
	tr := newThinkingTestTranscript()

	// Streaming state shows the live header.
	tr.beginThinking()
	tr.appendThinkingDelta("musing")
	out := tr.render(80)
	if !strings.Contains(out, "✻ Thinking…") || !strings.Contains(out, "musing") {
		t.Fatalf("streaming render = %q", out)
	}
	tr.endThinking()

	// Completed state flips the header.
	out = tr.render(80)
	if !strings.Contains(out, "✻ Thought") || !strings.Contains(out, "musing") {
		t.Fatalf("completed render = %q", out)
	}

	// Long bodies collapse to a tail preview; ctrl+o expands them.
	long := strings.Repeat("line\n", 20) + "end"
	tr.beginThinking()
	tr.appendThinkingDelta(long)
	tr.endThinking()
	collapsed := tr.render(80)
	if strings.Contains(collapsed, long) || !strings.Contains(collapsed, "ctrl+o to expand") {
		t.Fatalf("collapsed render should preview the tail:\n%s", collapsed)
	}
	tr.toggleExpand()
	expanded := tr.render(80)
	if !strings.Contains(expanded, "end") || !strings.Contains(expanded, "(ctrl+o to collapse)") {
		t.Fatalf("expanded render =\n%s", expanded)
	}
}

func TestThinkingElapsedRendersDuration(t *testing.T) {
	tr := newThinkingTestTranscript()
	tr.beginThinking()
	tr.appendThinkingDelta("musing")
	// Backdate the start so endThinking stamps a real duration through the
	// production path instead of a hand-set field.
	tr.blocks[len(tr.blocks)-1].thinkStart = time.Now().Add(-12 * time.Second)
	tr.endThinking()

	b := tr.blocks[0]
	if !b.thinkTimed || b.thinkDur < 11*time.Second || b.thinkDur > 15*time.Second {
		t.Fatalf("thinkTimed = %v, thinkDur = %v, want timed ~12s", b.thinkTimed, b.thinkDur)
	}
	plain := ansi.Strip(tr.render(80))
	if !strings.Contains(plain, "✻ Thought for 12s") {
		t.Fatalf("completed header missing duration:\n%s", plain)
	}
}

func TestThinkingUntimedRendersPlainHeader(t *testing.T) {
	tr := newThinkingTestTranscript()
	tr.beginThinking()
	tr.appendThinkingDelta("musing")
	tr.endThinking()
	b := tr.blocks[0]
	b.thinkTimed = false
	b.cacheValid = false

	plain := ansi.Strip(tr.render(80))
	for line := range strings.Lines(plain) {
		if strings.Contains(line, "✻") && strings.TrimSpace(line) != "✻ Thought" {
			t.Fatalf("untimed header should be plain, got line %q", line)
		}
	}
}

func TestSeededThinkingRendersWithoutDuration(t *testing.T) {
	history := []types.Message{
		userMessage("prompt"),
		{
			Role: types.RoleAssistant,
			Content: []types.ContentBlock{
				{Type: types.ContentThinking, Thinking: "recalled context"},
				{Type: types.ContentText, Text: "answer"},
			},
			Timestamp: 1,
		},
	}
	tr := newThinkingTestTranscript()
	seedTranscript(tr, history)

	plain := ansi.Strip(tr.render(80))
	for line := range strings.Lines(plain) {
		if strings.Contains(line, "✻") && strings.TrimSpace(line) != "✻ Thought" {
			t.Fatalf("seeded header should be plain, got line %q", line)
		}
	}
}

func TestFormatThinkDur(t *testing.T) {
	cases := map[time.Duration]string{
		-2 * time.Second:   "1s",
		0:                 "1s",
		400 * time.Millisecond: "1s",
		12 * time.Second:   "12s",
		59 * time.Second:   "59s",
		90 * time.Second:   "1m30s",
		5 * time.Minute:     "5m0s",
	}
	for d, want := range cases {
		if got := formatThinkDur(d); got != want {
			t.Errorf("formatThinkDur(%v) = %q, want %q", d, got, want)
		}
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
	out := tr.render(80)
	if strings.Contains(out, "Thinking\u2026") {
		t.Fatalf("transcript still shows streaming header after end:\n%s", out)
	}
	if !strings.Contains(out, "\u273b Thought") || !strings.Contains(out, "half-finished reasoning") {
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
	out := tr.render(80)
	if !strings.Contains(out, "✻ Thought") {
		t.Fatalf("seeded thinking should be complete:\n%s", out)
	}
}

func TestThinkTokensEstimatesCharsPerToken(t *testing.T) {
	if got := thinkTokens(""); got != 0 {
		t.Fatalf("thinkTokens empty got %d want 0", got)
	}
	if got := thinkTokens("abcd"); got != 1 {
		t.Fatalf("thinkTokens 4 chars got %d want 1", got)
	}
	if got := thinkTokens("abcde"); got != 2 {
		t.Fatalf("thinkTokens 5 chars got %d want 2", got)
	}
}

func TestThinkingStreamingHeaderShowsTokens(t *testing.T) {
	tr := newThinkingTestTranscript()
	tr.beginThinking()
	plain := ansi.Strip(tr.render(80))
	if strings.Contains(plain, "tokens") || strings.Contains(plain, "token") {
		t.Fatal("empty streaming header should not show tokens")
	}
	if !strings.Contains(plain, "✻ Thinking…") {
		t.Fatal("empty streaming header missing Thinking")
	}
	tr.appendThinkingDelta("musing along, reasoning about the task at hand")
	plain = ansi.Strip(tr.render(80))
	want := "✻ Thinking… (12 tokens)"
	if !strings.Contains(plain, want) {
		t.Fatal("streaming header missing token count")
	}
}

func TestThinkingCompletedHeaderHidesTokens(t *testing.T) {
	tr := newThinkingTestTranscript()
	tr.beginThinking()
	tr.appendThinkingDelta("musing along, reasoning about the task at hand")
	tr.endThinking()
	plain := ansi.Strip(tr.render(80))
	if strings.Contains(plain, "tokens") || strings.Contains(plain, "(1 token)") {
		t.Fatal("completed header should not show tokens")
	}
	if !strings.Contains(plain, "✻ Thought") {
		t.Fatal("completed header missing Thought")
	}
}

func TestThinkingSingularToken(t *testing.T) {
	tr := newThinkingTestTranscript()
	tr.beginThinking()
	tr.appendThinkingDelta("hi")
	plain := ansi.Strip(tr.render(80))
	want := "✻ Thinking… (1 token)"
	if !strings.Contains(plain, want) {
		t.Fatal("want singular 1 token")
	}
}
