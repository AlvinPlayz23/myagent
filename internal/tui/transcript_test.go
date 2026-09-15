package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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

func TestRenderDiffUsesTextColoring(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	diff := []diffLine{
		{text: "--- a/file.go"},
		{text: "+++ b/file.go"},
		{prefix: '-', text: "old"},
		{prefix: '+', text: "new"},
	}
	const width = 20
	out := tr.renderDiff(diff, width)

	// Changed rows are text-colored only (TokyoNight truecolor green/red,
	// with no background fills).
	if strings.Contains(out, "48;") {
		t.Fatalf("diff rows should not carry background fills: %q", out)
	}
	if !strings.Contains(out, "38;2;158;206;106m") || !strings.Contains(out, "38;2;247;118;142m") {
		t.Fatalf("diff rows missing green/red foreground coloring: %q", out)
	}

	plain := ansi.Strip(out)
	lines := strings.Split(plain, "\n")
	if len(lines) != len(diff) {
		t.Fatalf("rendered %d lines, want %d:\n%q", len(lines), len(diff), plain)
	}
	for i, line := range lines {
		want := diff[i].text
		if diff[i].prefix != 0 {
			want = string(diff[i].prefix) + diff[i].text
		}
		if line != want {
			t.Errorf("line %d = %q, want %q", i, line, want)
		}
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

func TestRetryNoticeExpandsWithToggle(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.addRetryNotice("∼ Provider error, retrying… (attempt 2/10)", "429: rate limited")

	collapsed := ansi.Strip(tr.renderBlock(tr.blocks[0], 80))
	if !strings.Contains(collapsed, "attempt 2/10") {
		t.Fatalf("collapsed retry missing header: %q", collapsed)
	}
	if strings.Contains(collapsed, "rate limited") {
		t.Fatalf("collapsed retry leaked detail: %q", collapsed)
	}
	if !strings.Contains(collapsed, "ctrl+o to expand") {
		t.Fatalf("collapsed retry missing expand hint: %q", collapsed)
	}

	tr.toggleExpand()
	expanded := ansi.Strip(tr.renderBlock(tr.blocks[0], 80))
	if !strings.Contains(expanded, "rate limited") {
		t.Fatalf("expanded retry missing detail: %q", expanded)
	}
	if !strings.Contains(expanded, "(ctrl+o to collapse)") {
		t.Fatalf("expanded retry missing collapse hint: %q", expanded)
	}
}

func TestPlainNoticeHasNoExpandHint(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.addNotice("∼ Context compacted: 10 → 5 tokens (kept recent history).")

	got := ansi.Strip(tr.renderBlock(tr.blocks[0], 80))
	if strings.Contains(got, "ctrl+o") {
		t.Fatalf("plain notice should not mention ctrl+o: %q", got)
	}
	tr.toggleExpand()
	got = ansi.Strip(tr.renderBlock(tr.blocks[0], 80))
	if strings.Contains(got, "ctrl+o") {
		t.Fatalf("plain notice should not mention ctrl+o after expand: %q", got)
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
