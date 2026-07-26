package session

import (
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func TestTitleUsesFirstUserPrompt(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentBlock{types.TextBlock("hello")}},
		{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("First prompt")}},
		{Role: types.RoleUser, Content: []types.ContentBlock{types.TextBlock("Later prompt")}},
	}
	if got, want := Title(messages), "First prompt"; got != want {
		t.Fatalf("Title() = %q, want %q", got, want)
	}
}

func TestTitleForEmptyConversation(t *testing.T) {
	if got, want := Title(nil), "new"; got != want {
		t.Fatalf("Title() = %q, want %q", got, want)
	}
}
