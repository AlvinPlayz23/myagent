package llm

import "testing"

func TestParseEffort(t *testing.T) {
	tests := map[string]Effort{
		"":        "",
		"none":    EffortNone,
		"off":     EffortOff,
		"minimal": EffortMinimal,
		" LOW ":   EffortLow,
		"Medium":  EffortMedium,
		"high":    EffortHigh,
		"XHIGH":   EffortXHigh,
		"max":     EffortMax,
	}
	for input, want := range tests {
		got, err := ParseEffort(input)
		if err != nil {
			t.Fatalf("ParseEffort(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("ParseEffort(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestParseEffortRejectsInvalidValue(t *testing.T) {
	if _, err := ParseEffort("extreme"); err == nil {
		t.Fatal("ParseEffort(extreme) should fail")
	}
}

func TestNormalizeEffortUsesModelCapabilities(t *testing.T) {
	nonReasoning := Model{ID: "plain", ReasoningKnown: true}
	if _, err := NormalizeEffort(nonReasoning, EffortHigh); err == nil {
		t.Fatal("expected non-reasoning model to reject high effort")
	}
	if got, err := NormalizeEffort(nonReasoning, EffortNone); err != nil || got != EffortOff {
		t.Fatalf("off normalization = %q, %v", got, err)
	}
	reasoning := Model{ID: "reasoning", ReasoningKnown: true, Reasoning: true, SupportedEfforts: []Effort{EffortOff, EffortLow, EffortHigh}}
	if got, err := NormalizeEffort(reasoning, EffortMedium); err != nil || got != EffortHigh {
		t.Fatalf("clamped effort = %q, %v", got, err)
	}
	withoutOff := Model{ID: "reasoning-without-off", ReasoningKnown: true, Reasoning: true, SupportedEfforts: []Effort{EffortLow, EffortHigh}}
	if got, err := NormalizeEffort(withoutOff, EffortOff); err != nil || got != EffortOff {
		t.Fatalf("explicit off normalization = %q, %v", got, err)
	}
}

func TestParseReasoningDialect(t *testing.T) {
	tests := map[string]ReasoningDialect{
		"":           ReasoningDialectAuto,
		" auto ":     ReasoningDialectAuto,
		"OPENAI":     ReasoningDialectOpenAI,
		"openrouter": ReasoningDialectOpenRouter,
		"deepseek":   ReasoningDialectDeepSeek,
	}
	for input, want := range tests {
		got, err := ParseReasoningDialect(input)
		if err != nil {
			t.Fatalf("ParseReasoningDialect(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("ParseReasoningDialect(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := ParseReasoningDialect("all"); err == nil {
		t.Fatal("ParseReasoningDialect(all) should fail")
	}
}
