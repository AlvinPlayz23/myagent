package models

import (
	"reflect"
	"testing"
	"time"
)

func TestNormalizeIncludesOnlyCompatibleToolModels(t *testing.T) {
	compatible := provider{ID: "zenmux", Name: "ZenMux", NPM: "@ai-sdk/openai-compatible", Models: map[string]sourceModel{
		"good":     {ID: "good", Name: "Good", ToolCall: true, Reasoning: true},
		"no-tools": {ID: "no-tools", ToolCall: false},
	}}

	source := map[string]provider{
		"zenmux":     compatible,
		"native":     {ID: "native", NPM: "@ai-sdk/anthropic", Models: map[string]sourceModel{"claude": {ID: "claude", ToolCall: true}}},
		"openrouter": {ID: "openrouter", Name: "OpenRouter", NPM: "@openrouter/ai-sdk-provider", Models: map[string]sourceModel{"auto": {ID: "auto", ToolCall: true}}},
		"aihubmix":   {ID: "aihubmix", Name: "AIHubMix", NPM: "@aihubmix/ai-sdk-provider", Models: map[string]sourceModel{"qwen": {ID: "qwen", ToolCall: true}}},
	}

	got := normalize(source)
	refs := make([]string, len(got))
	for i, model := range got {
		refs[i] = model.Ref()
	}
	if want := []string{"aihubmix/qwen", "openrouter/auto", "zenmux/good"}; !reflect.DeepEqual(refs, want) {
		t.Fatalf("models = %v, want %v", refs, want)
	}
}

func TestCatalogFiltersConfiguredProvidersAndExpires(t *testing.T) {
	c := &Catalog{data: cache{
		CheckedAt: time.Now().Add(-5 * time.Hour),
		Models:    []Model{{Provider: "openrouter", ID: "a"}, {Provider: "zenmux", ID: "b"}},
	}}
	if !c.NeedsRefresh(time.Now()) {
		t.Fatal("five-hour-old catalog should need refresh")
	}
	got := c.Models(map[string]struct{}{"zenmux": {}})
	if want := []Model{{Provider: "zenmux", ID: "b"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered models = %#v, want %#v", got, want)
	}
}
