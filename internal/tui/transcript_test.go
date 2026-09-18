package tui

import (
	"strings"
	"testing"
	"time"

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

	// Changed rows are text-colored only (foreground, no background fills).
	if strings.Contains(out, "48;") {
		t.Fatalf("diff rows should not carry background fills: %q", out)
	}
	// The - and + rows must each carry a distinct foreground color, and those
	// colors must differ from each other and from the header rows, so a theme
	// change cannot silently collapse add/remove/context into one color.
	colorOf := func(line string) string {
		idx := strings.Index(out, line)
		if idx < 0 {
			t.Fatalf("line %q not found in %q", line, out)
		}
		seg := out[:idx]
		start := strings.LastIndex(seg, "\x1b[")
		end := strings.Index(seg[start:], "m")
		return seg[start : start+end+1]
	}
	addColor, removeColor, metaColor := colorOf("+new"), colorOf("-old"), colorOf("--- a/file.go")
	if addColor == removeColor || addColor == metaColor || removeColor == metaColor {
		t.Fatalf("diff rows need distinct add/remove/context colors, got add=%q remove=%q meta=%q in %q", addColor, removeColor, metaColor, out)
	}

	plain := ansi.Strip(out)
	lines := strings.Split(plain, "\n")
	if len(lines) != len(diff) {
		t.Fatalf("rendered %d lines, want %d:\n%q", len(lines), len(diff), plain)
	}
	for i, line := range lines {
		want := "  " + diff[i].text
		if diff[i].prefix != 0 {
			want = "  " + string(diff[i].prefix) + diff[i].text
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

func TestToolHeaderParts(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	cases := []struct {
		name       string
		args       map[string]any
		wantName   string
		wantTarget string
	}{
		{"read", map[string]any{"path": "a.go"}, "read", "a.go"},
		{"read", map[string]any{"file_path": "b.go"}, "read", "b.go"},
		{"edit", map[string]any{"path": "c.go"}, "edit", "c.go"},
		{"write", map[string]any{"path": "d.go"}, "write", "d.go"},
		{"bash", map[string]any{"command": "go build ./..."}, "$", "go build ./..."},
		{"bash", map[string]any{"command": "one\ntwo"}, "$", "one …"},
		{"grep", nil, "grep", ""},
	}
	for _, tc := range cases {
		name, target := tr.toolHeaderParts(&block{kind: blockTool, toolName: tc.name, toolArgs: tc.args})
		if name != tc.wantName || target != tc.wantTarget {
			t.Errorf("toolHeaderParts(%q) = (%q, %q), want (%q, %q)", tc.name, name, target, tc.wantName, tc.wantTarget)
		}
	}
}

func TestRenderToolShowsMarkerAndMetadata(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.startTool("call", "read", map[string]any{"path": "main.go"})
	b := tr.blocks[0]
	b.toolDur = 1500 * time.Millisecond
	b.toolTimed = true
	tr.endTool("call", types.TextResult("line one\nline two", nil), false)

	plain := ansi.Strip(tr.renderTool(b, 80))
	if !strings.Contains(plain, "● read main.go") {
		t.Fatalf("missing kj-style header: %q", plain)
	}
	// endTool overwrites the pinned duration with the real elapsed time, so
	// assert the metadata shape rather than an exact duration value.
	if !strings.Contains(plain, "2 lines") || !strings.Contains(plain, "17 B") {
		t.Fatalf("missing line/byte metadata: %q", plain)
	}
	if !strings.Contains(plain, "└") {
		t.Fatalf("missing metadata leader: %q", plain)
	}
}

func TestRenderToolPendingHasNoMetadata(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.startTool("call", "bash", map[string]any{"command": "go test ./..."})
	plain := ansi.Strip(tr.renderTool(tr.blocks[0], 80))
	if strings.Contains(plain, "└") {
		t.Fatalf("pending tool rendered metadata: %q", plain)
	}
	if !strings.Contains(plain, "● $ go test ./...") {
		t.Fatalf("pending tool missing header: %q", plain)
	}
}

func TestFoldReadsCollapsesConsecutiveReads(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	for _, path := range []string{"a.go", "b.go", "c.go"} {
		id := "call-" + path
		tr.startTool(id, "read", map[string]any{"path": path})
		tr.blocks[len(tr.blocks)-1].toolTimed = false
		tr.endTool(id, types.TextResult("contents", nil), false)
	}

	out := ansi.Strip(tr.render(80))
	if !strings.Contains(out, "● read 3 files") {
		t.Fatalf("reads were not folded: %q", out)
	}
	for _, path := range []string{"a.go", "b.go", "c.go"} {
		if !strings.Contains(out, path) {
			t.Fatalf("folded reads missing path %q: %q", path, out)
		}
	}

	// ctrl+o restores the individual read blocks.
	tr.toggleExpand()
	expanded := ansi.Strip(tr.render(80))
	if strings.Contains(expanded, "● read 3 files") {
		t.Fatalf("expanded view kept the fold: %q", expanded)
	}
	if !strings.Contains(expanded, "● read a.go") {
		t.Fatalf("expanded view missing individual read: %q", expanded)
	}
}

func TestFoldReadsSkipsIncompleteAndDuplicateRuns(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	// A pending read breaks the run.
	tr.startTool("p1", "read", map[string]any{"path": "a.go"})
	tr.startTool("p2", "read", map[string]any{"path": "b.go"})
	tr.endTool("p2", types.TextResult("x", nil), false)
	if fold := tr.foldReads(0); fold != nil {
		t.Fatalf("folded a pending read: %+v", fold)
	}

	// Duplicate paths stay separate.
	tr2 := newTranscript(newTheme(), newMDRenderer())
	for _, id := range []string{"d1", "d2"} {
		tr2.startTool(id, "read", map[string]any{"path": "same.go"})
		tr2.endTool(id, types.TextResult("x", nil), false)
	}
	if fold := tr2.foldReads(0); fold != nil {
		t.Fatalf("folded duplicate-path reads: %+v", fold)
	}
}

func TestFormatToolDuration(t *testing.T) {
	if got := formatToolDuration(350 * time.Millisecond); got != "350ms" {
		t.Errorf("350ms = %q, want 350ms", got)
	}
	if got := formatToolDuration(1500 * time.Millisecond); got != "1.5s" {
		t.Errorf("1.5s = %q, want 1.5s", got)
	}
	if got := formatToolDuration(25 * time.Second); got != "25s" {
		t.Errorf("25s = %q, want 25s", got)
	}
	if got := formatToolDuration(90 * time.Second); got != "1m30s" {
		t.Errorf("90s = %q, want 1m30s", got)
	}
}

func TestFormatByteCount(t *testing.T) {
	if got := formatByteCount(512); got != "512 B" {
		t.Errorf("512 = %q, want 512 B", got)
	}
	if got := formatByteCount(2048); got != "2.0 KB" {
		t.Errorf("2048 = %q, want 2.0 KB", got)
	}
}

func TestWrapPlainBreaksLongWords(t *testing.T) {
	blob := strings.Repeat("k", 100)
	out := wrapPlain(blob, 30)
	for _, line := range strings.Split(out, "\n") {
		if len([]rune(line)) > 30 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
	// Multiline input keeps its breaks and its words; nothing is lost.
	out = wrapPlain("aaa bbb\ncccc dddd", 5)
	if !strings.Contains(out, "aaa") || !strings.Contains(out, "dddd") {
		t.Fatalf("wrap dropped content: %q", out)
	}
	// Short text is untouched.
	if got := wrapPlain("hello", 80); got != "hello" {
		t.Fatalf("short text changed: %q", got)
	}
}

func TestLongSingleLineBlocksWrapToWidth(t *testing.T) {
	blob := strings.Repeat("ksdn", 60) // 240 unbroken runes
	tr := newTranscript(newTheme(), newMDRenderer())
	const width = 40

	tr.beginThinking()
	tr.appendThinkingDelta(blob)
	tr.endThinking()
	for i, line := range strings.Split(ansi.Strip(tr.render(width)), "\n") {
		if len([]rune(line)) > width {
			t.Fatalf("thinking line %d exceeds width: %q", i, line)
		}
	}

	tr.addErrorText(blob)
	tr.addNotice(blob)
	tr.startTool("c1", "read", map[string]any{"path": "f.go"})
	tr.blocks[len(tr.blocks)-1].toolTimed = false
	tr.endTool("c1", types.TextResult(blob, nil), false)
	for i, line := range strings.Split(ansi.Strip(tr.render(width)), "\n") {
		if len([]rune(line)) > width {
			t.Fatalf("transcript line %d exceeds width: %q", i, line)
		}
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
