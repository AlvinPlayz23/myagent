package llm

import (
	"reflect"
	"testing"
)

func TestEffortLevelsIsTheSingleSourceOfTruth(t *testing.T) {
	levels := EffortLevels()
	if len(levels) == 0 {
		t.Fatal("no registered effort levels")
	}
	// Every registered level must parse, and clamping relies on ascending order.
	for _, level := range levels {
		if got, err := ParseEffort(string(level)); err != nil || got != level {
			t.Errorf("ParseEffort(%q) = %q, %v", level, got, err)
		}
	}
	if levels[0] != EffortMinimal || levels[len(levels)-1] != EffortMax {
		t.Errorf("levels = %v, want ascending from minimal to max", levels)
	}
	// The returned slice is a copy: mutating it must not corrupt package state.
	levels[0] = "tampered"
	if EffortLevels()[0] != EffortMinimal {
		t.Error("EffortLevels() leaked its backing array to callers")
	}
	if want := "minimal, low, medium, high, xhigh, max"; EffortList() != want {
		t.Errorf("EffortList() = %q, want %q", EffortList(), want)
	}
}

func TestSupportedEffortsForTracksRegisteredLevels(t *testing.T) {
	if got := SupportedEffortsFor(false); got != nil {
		t.Errorf("non-reasoning supported efforts = %v, want nil", got)
	}
	if got, want := SupportedEffortsFor(true), EffortLevels(); !reflect.DeepEqual(got, want) {
		t.Errorf("reasoning supported efforts = %v, want %v", got, want)
	}
	if got := EffortStrings(nil); got != nil {
		t.Errorf("EffortStrings(nil) = %v, want nil so omitempty drops the field", got)
	}
	if got, want := EffortStrings(SupportedEffortsFor(true)), []string{"minimal", "low", "medium", "high", "xhigh", "max"}; !reflect.DeepEqual(got, want) {
		t.Errorf("EffortStrings = %v, want %v", got, want)
	}
}

func TestParseEffort(t *testing.T) {
	tests := map[string]Effort{
		"":        "",
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
	// off and the legacy none alias are no longer registered levels.
	for _, input := range []string{"extreme", "off", "none"} {
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
