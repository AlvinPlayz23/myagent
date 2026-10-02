package tui

import (
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func completionMsg(text string) types.Message {
	return types.Message{
		Role:    types.RoleUser,
		Source:  types.SourceSubagentCompletion,
		Content: []types.ContentBlock{types.TextBlock(text)},
	}
}

func TestCompletionMessageIsNoticeNotHumanInput(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	queued := userMessage("later")
	m.queuedFollowUps = []queuedMessage{{display: "later", message: queued}}

	msg := completionMsg("preamble\n\nSubagent task sub_1 finished with status \"completed\".\n\n{}")
	m.onAgentEvent(types.AgentEvent{Type: types.EventMessageStart, Message: &msg})
	m.onAgentEvent(types.AgentEvent{Type: types.EventMessageEnd, Message: &msg})

	if len(m.queuedFollowUps) != 1 {
		t.Fatalf("completion consumed a queued human prompt: %#v", m.queuedFollowUps)
	}
	var notices int
	for _, b := range m.transcript.blocks {
		if b.kind == blockUser {
			t.Fatal("completion rendered as a user block")
		}
		if b.kind == blockNotice && strings.Contains(b.text, "sub_1") {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("notices = %d, want 1", notices)
	}
}

func TestSeedTranscriptRendersCompletionAsNotice(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	seedTranscript(m.transcript, []types.Message{completionMsg("Subagent task sub_2 finished with status \"failed\".")})
	if len(m.transcript.blocks) != 1 || m.transcript.blocks[0].kind != blockNotice {
		t.Fatalf("blocks = %#v", m.transcript.blocks)
	}
}
