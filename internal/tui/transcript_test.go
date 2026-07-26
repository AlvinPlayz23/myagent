package tui

import (
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func TestProposalDiffForWrite(t *testing.T) {
	diff := proposalDiff("write", map[string]any{
		"path":    "internal/tui/new.go",
		"content": "package tui\n\nfunc newFile() {}\n",
	})
	got := plainDiff(diff)
	want := "--- /dev/null\n+++ b/internal/tui/new.go\n@@\n+package tui\n+\n+func newFile() {}\n+"
	if got != want {
		t.Fatalf("proposal diff = %q, want %q", got, want)
	}
}

func TestProposalDiffForEdit(t *testing.T) {
	diff := proposalDiff("edit", map[string]any{
		"path":  "file.go",
		"edits": []any{map[string]any{"oldText": "old\nline", "newText": "new\nline"}},
	})
	got := plainDiff(diff)
	want := "--- a/file.go\n+++ b/file.go\n-old\n-line\n+new\n+line"
	if got != want {
		t.Fatalf("proposal diff = %q, want %q", got, want)
	}
}

func TestRenderDiffCollapsesChangedLines(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.startTool("call", "write", map[string]any{
		"path":    "large.go",
		"content": strings.Repeat("line\n", diffPreviewLines+3),
	})
	tr.endTool("call", types.TextResult("Successfully wrote file", nil), false)

	collapsed := tr.renderTool(tr.blocks[0], 80)
	if !strings.Contains(collapsed, "more changed lines, ctrl+o to expand") {
		t.Fatalf("collapsed diff has no expansion hint: %q", collapsed)
	}
	tr.toggleExpand()
	expanded := tr.renderTool(tr.blocks[0], 80)
	if !strings.Contains(expanded, "(ctrl+o to collapse)") {
		t.Fatalf("expanded diff has no collapse hint: %q", expanded)
	}
}

func TestFailedEditShowsErrorInsteadOfProposalDiff(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.startTool("call", "edit", map[string]any{
		"path":  "file.go",
		"edits": []any{map[string]any{"oldText": "old", "newText": "new"}},
	})
	tr.endTool("call", types.TextResult("oldText not found", nil), true)

	got := tr.renderTool(tr.blocks[0], 80)
	if strings.Contains(got, "--- a/file.go") || strings.Contains(got, "+new") {
		t.Fatalf("failed edit rendered a proposal diff: %q", got)
	}
	if !strings.Contains(got, "oldText not found") {
		t.Fatalf("failed edit did not render its error: %q", got)
	}
}

func plainDiff(lines []diffLine) string {
	var out []string
	for _, line := range lines {
		text := line.text
		if line.prefix != 0 {
			text = string(line.prefix) + text
		}
		out = append(out, text)
	}
	return strings.Join(out, "\n")
}
