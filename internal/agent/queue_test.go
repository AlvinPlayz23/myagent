package agent

import (
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func TestFollowUpReturnsOneMessageAtATimeInFIFOOrder(t *testing.T) {
	q := NewQueue()
	first := types.Message{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("first")}}
	second := types.Message{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("second")}}
	q.EnqueueFollowUp(first)
	q.EnqueueFollowUp(second)

	if got := q.FollowUp(); len(got) != 1 || got[0].Content[0].Text != "first" {
		t.Fatalf("first poll = %#v", got)
	}
	if q.PendingCount() != 1 {
		t.Fatalf("pending count = %d, want 1", q.PendingCount())
	}
	if got := q.FollowUp(); len(got) != 1 || got[0].Content[0].Text != "second" {
		t.Fatalf("second poll = %#v", got)
	}
}
