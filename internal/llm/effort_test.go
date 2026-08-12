package llm

import "testing"

func TestParseEffort(t *testing.T) {
	tests := map[string]Effort{
		"":       "",
		" LOW ":  EffortLow,
		"Medium": EffortMedium,
		"high":   EffortHigh,
		"XHIGH":  EffortXHigh,
		"max":    EffortMax,
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
	// off, minimal and the legacy none alias are no longer registered levels.
	for _, input := range []string{"extreme", "off", "minimal", "none"} {
		if _, err := ParseEffort(input); err == nil {
			t.Errorf("ParseEffort(%q) should fail", input)
		}
	}
}

func TestNormalizeEffortUsesModelCapabilities(t *testing.T) {
	nonReasoning := Model{ID: "plain", ReasoningKnown: true}
	if _, err := NormalizeEffort(nonReasoning, EffortHigh); err == nil {
		t.Fatal("expected non-reasoning model to reject high effort")
	}
	if got, err := NormalizeEffort(nonReasoning, ""); err != nil || got != "" {
		t.Fatalf("unspecified effort on non-reasoning model = %q, %v", got, err)
	}
	reasoning := Model{ID: "reasoning", ReasoningKnown: true, Reasoning: true, SupportedEfforts: []Effort{EffortLow, EffortHigh}}
	if got, err := NormalizeEffort(reasoning, EffortMedium); err != nil || got != EffortHigh {
		t.Fatalf("clamped effort = %q, %v", got, err)
	}
	if got, err := NormalizeEffort(reasoning, EffortMax); err != nil || got != EffortHigh {
		t.Fatalf("clamped-down effort = %q, %v", got, err)
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
