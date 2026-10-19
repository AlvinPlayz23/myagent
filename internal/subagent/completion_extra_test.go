package subagent

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// The background launch acknowledgement must carry the model id so UIs can
// label the subagent tool call live, not only after a resume (where Details is
// decoded back into a map). transcript.subagentModel relies on this shape.
func TestAckDetailsExposeModel(t *testing.T) {
	task, err := New().PrepareBackground(context.Background(), "c", map[string]any{"prompt": "x"}, parentConfig("x"))
	if err != nil {
		t.Fatal(err)
	}
	ack, ok := task.Ack.Details.(AckDetails)
	if !ok {
		t.Fatalf("ack details = %T, want AckDetails", task.Ack.Details)
	}
	if ack.Model != "m" || ack.Status != "running" || ack.TaskID != task.ID {
		t.Fatalf("ack = %+v", ack)
	}
	var decoded map[string]any
	raw, _ := json.Marshal(task.Ack.Details)
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["model"] != "m" {
		t.Fatalf("wire form lost the model: %v", decoded)
	}
}

// The byte budget, not just the entry count, must bound the report.
func TestTrajectoryByteBudgetOmitsExcess(t *testing.T) {
	tr := newTrajectory()
	big := strings.Repeat("x", maxTrajectoryText)
	for i := 0; i < maxTrajectoryBytes/maxTrajectoryText+5; i++ {
		tr.add(PublicMessage{Role: "user", Text: big})
	}
	got := tr.finish()
	if tr.omitted == 0 {
		t.Fatal("byte budget never applied; test constants changed?")
	}
	if last := got[len(got)-1]; last.Role != "note" || !strings.Contains(last.Note, "omitted") {
		t.Fatalf("missing truncation note: %+v", last)
	}
	if kept := len(got) - 1; kept >= maxTrajectoryEntries {
		t.Fatalf("kept %d entries, want fewer than %d", kept, maxTrajectoryEntries)
	}
}

func TestCompletionTextNonCompletedHeading(t *testing.T) {
	text := completionText(TaskReport{
		TaskID: "sub_9", Status: StatusTimedOut, Error: "timed out after 1s",
		Model: "m", Provider: "p", FinalResult: "partial",
		ToolUsage: map[string]int{"bash": 2, "read": 1, "edit": 2},
	})
	for _, want := range []string{`status "timed_out"`, "Error: timed out after 1s", "Partial output:\npartial"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	// Tools used are ordered by count desc, then name.
	if i, j, k := strings.Index(text, "bash x2"), strings.Index(text, "edit x2"), strings.Index(text, "read x1"); !(i < j && j < k) {
		t.Fatalf("tool usage order wrong:\n%s", text)
	}
}

func TestBoundedArgsAlwaysValidJSON(t *testing.T) {
	got := boundedArgs(map[string]any{"bad": make(chan int)})
	var s string
	if err := json.Unmarshal(got, &s); err != nil || !strings.Contains(s, "unserializable") {
		t.Fatalf("marshal-error fallback = %q (%v)", got, err)
	}
	huge := boundedArgs(map[string]any{"q": strings.Repeat("a", maxToolArgsBytes*2)})
	if !json.Valid(huge) {
		t.Fatalf("oversized args are not valid JSON: %.60q", huge)
	}
}

func TestNumberArgTypes(t *testing.T) {
	if v, ok := numberArg(map[string]any{"n": int64(7)}, "n"); !ok || v != 7 {
		t.Fatalf("int64 = %v (ok=%v)", v, ok)
	}
	for _, bad := range []any{math.NaN(), math.Inf(1), "7", nil} {
		if _, ok := numberArg(map[string]any{"n": bad}, "n"); ok {
			t.Fatalf("%v accepted", bad)
		}
	}
}
