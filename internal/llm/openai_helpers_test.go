package llm

import (
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func TestConvertMessagesKeepsTextOnlyUserContentAsString(t *testing.T) {
	got := convertMessages("", []types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{types.TextBlock("hello")},
	}})
	if len(got) != 1 || got[0].Content != "hello" {
		t.Fatalf("content = %#v, want string hello", got[0].Content)
	}
}

func TestConvertMessagesSerializesUserImagesAsMultipart(t *testing.T) {
	got := convertMessages("", []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentBlock{
			types.TextBlock("explain"),
			types.ImageBlock("aGVsbG8=", "image/png"),
		},
	}})
	if len(got) != 1 {
		t.Fatalf("messages = %d, want 1", len(got))
	}
	parts, ok := got[0].Content.([]chatContentPart)
	if !ok {
		t.Fatalf("content type = %T, want []chatContentPart", got[0].Content)
	}
	if len(parts) != 2 || parts[0].Type != "text" || parts[0].Text != "explain" {
		t.Fatalf("text part = %#v", parts)
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("image part = %#v", parts[1])
	}
}

func TestConvertMessagesMovesToolResultImagesToFollowingUserMessage(t *testing.T) {
	got := convertMessages("", []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentBlock{{
				Type: types.ContentToolCall,
				ID:   "call-1",
				Name: "read",
			}},
		},
		{
			Role:       types.RoleToolResult,
			ToolCallID: "call-1",
			ToolName:   "read",
			Content:    []types.ContentBlock{types.ImageBlock("aW1hZ2U=", "image/png")},
		},
	})
	if len(got) != 3 {
		t.Fatalf("messages = %#v, want assistant, tool, user image", got)
	}
	if got[1].Role != "tool" || got[1].Content != "(see attached image)" {
		t.Fatalf("tool message = %#v", got[1])
	}
	parts, ok := got[2].Content.([]chatContentPart)
	if !ok || got[2].Role != "user" || len(parts) != 2 {
		t.Fatalf("image message = %#v", got[2])
	}
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != "data:image/png;base64,aW1hZ2U=" {
		t.Fatalf("image part = %#v", parts[1])
	}
}

func TestConvertMessagesGroupsParallelToolResultsBeforeImages(t *testing.T) {
	got := convertMessages("", []types.Message{
		{Role: types.RoleToolResult, ToolCallID: "one", Content: []types.ContentBlock{types.ImageBlock("b25l", "image/png")}},
		{Role: types.RoleToolResult, ToolCallID: "two", Content: []types.ContentBlock{types.TextBlock("two")}},
	})
	if len(got) != 3 || got[0].Role != "tool" || got[1].Role != "tool" || got[2].Role != "user" {
		t.Fatalf("message roles/order = %#v", got)
	}
}
