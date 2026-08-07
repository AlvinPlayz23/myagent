package llm

import (
	"encoding/json"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func TestBuildRequestBodyReasoningEffort(t *testing.T) {
	for _, effort := range []Effort{"", EffortNone, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax} {
		body, err := buildRequestBody(Model{ID: "test"}, Request{Effort: effort})
		if err != nil {
			t.Fatalf("buildRequestBody(%q): %v", effort, err)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		value, present := got["reasoning_effort"]
		if effort == "" {
			if present {
				t.Errorf("unset effort serialized as %#v", value)
			}
			continue
		}
		if value != string(effort) {
			t.Errorf("reasoning_effort = %#v, want %q", value, effort)
		}
	}
}

func TestBuildRequestBodyCanonicalOffUsesProviderDisableValue(t *testing.T) {
	for _, model := range []Model{{ID: "gpt"}, {ID: "gpt", Provider: "openrouter"}} {
		body, err := buildRequestBody(model, Request{Effort: EffortOff})
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if model.Provider == "openrouter" {
			reasoning := got["reasoning"].(map[string]any)
			if reasoning["effort"] != "none" {
				t.Fatalf("openrouter off = %#v", reasoning)
			}
		} else if got["reasoning_effort"] != "none" {
			t.Fatalf("openai off = %#v", got)
		}
	}
}

func TestBuildRequestBodyOpenRouterReasoning(t *testing.T) {
	body, err := buildRequestBody(Model{ID: "openai/gpt-5", Provider: "openrouter"}, Request{Effort: EffortXHigh})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if _, present := got["reasoning_effort"]; present {
		t.Fatalf("OpenRouter request includes reasoning_effort: %s", body)
	}
	reasoning, ok := got["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "xhigh" {
		t.Fatalf("OpenRouter reasoning = %#v, want xhigh", got["reasoning"])
	}
}

func TestBuildRequestBodyDetectsOpenRouterByURL(t *testing.T) {
	body, err := buildRequestBody(Model{ID: "model", Provider: "custom", BaseURL: "https://openrouter.ai/api/v1"}, Request{Effort: EffortLow})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["reasoning"]; !ok {
		t.Fatalf("OpenRouter URL did not produce nested reasoning: %s", body)
	}
}

func TestBuildRequestBodyReasoningDialectOverridesDetection(t *testing.T) {
	body, err := buildRequestBody(Model{
		ID:               "model",
		Provider:         "custom",
		BaseURL:          "https://gateway.internal/v1",
		ReasoningDialect: ReasoningDialectOpenRouter,
	}, Request{Effort: EffortHigh})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["reasoning"]; !ok {
		t.Fatalf("explicit OpenRouter dialect did not produce nested reasoning: %s", body)
	}

	body, err = buildRequestBody(Model{
		ID:               "model",
		Provider:         "openrouter",
		BaseURL:          "https://openrouter.ai/api/v1",
		ReasoningDialect: ReasoningDialectOpenAI,
	}, Request{Effort: EffortHigh})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["reasoning_effort"] != "high" {
		t.Fatalf("explicit OpenAI dialect did not override detection: %s", body)
	}
}

func TestBuildRequestBodyDeepSeekReasoning(t *testing.T) {
	tests := []struct {
		effort       Effort
		wantEffort   string
		wantThinking string
	}{
		{effort: EffortLow, wantEffort: "low", wantThinking: "enabled"},
		{effort: EffortMedium, wantEffort: "high", wantThinking: "enabled"},
		{effort: EffortMax, wantEffort: "max", wantThinking: "enabled"},
		{effort: EffortNone, wantThinking: "disabled"},
	}
	for _, tt := range tests {
		body, err := buildRequestBody(Model{ID: "deepseek-v4-pro", BaseURL: "https://api.deepseek.com"}, Request{Effort: tt.effort})
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		thinking, ok := got["thinking"].(map[string]any)
		if !ok || thinking["type"] != tt.wantThinking {
			t.Errorf("effort %q thinking = %#v, want %q", tt.effort, got["thinking"], tt.wantThinking)
		}
		if tt.wantEffort == "" {
			if _, present := got["reasoning_effort"]; present {
				t.Errorf("effort none serialized reasoning_effort: %s", body)
			}
		} else if got["reasoning_effort"] != tt.wantEffort {
			t.Errorf("effort %q reasoning_effort = %#v, want %q", tt.effort, got["reasoning_effort"], tt.wantEffort)
		}
		if _, present := got["reasoning"]; present {
			t.Errorf("DeepSeek request includes OpenRouter reasoning: %s", body)
		}
	}
}

func TestBuildRequestBodyReplaysReasoningForToolContinuations(t *testing.T) {
	messages := []types.Message{{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{
			{Type: types.ContentThinking, Thinking: "inspect the repository"},
			{Type: types.ContentToolCall, ID: "call-1", Name: "read", Arguments: map[string]any{"path": "README.md"}},
		},
	}}

	for _, model := range []Model{
		{ID: "deepseek-v4-pro", Provider: "deepseek"},
		{ID: "deepseek/deepseek-r1", Provider: "openrouter"},
	} {
		body, err := buildRequestBody(model, Request{Messages: messages, Effort: EffortHigh})
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Messages []chatMessage `json:"messages"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Messages) != 1 || got.Messages[0].ReasoningContent != "inspect the repository" {
			t.Errorf("provider %q reasoning replay = %#v", model.Provider, got.Messages)
		}
	}

	body, err := buildRequestBody(Model{ID: "gpt-5", Provider: "openai"}, Request{Messages: messages, Effort: EffortHigh})
	if err != nil {
		t.Fatal(err)
	}
	if json.Valid(body) && string(body) != "" {
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		serialized := got["messages"].([]any)[0].(map[string]any)
		if _, present := serialized["reasoning_content"]; present {
			t.Fatalf("generic provider received reasoning_content: %s", body)
		}
	}
}

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
