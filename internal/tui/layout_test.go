package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/types"
)

func TestLayoutCachesUnchangedBlocks(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.addUser("first")
	tr.beginAssistant()
	tr.appendAssistantDelta("answer one")
	tr.endAssistant()
	tr.addUser("second")

	rows := tr.layout(60)
	if len(rows) == 0 {
		t.Fatal("layout produced no rows")
	}
	stable := tr.blocks[1].layoutRows // the completed assistant block

	tr.beginAssistant()
	tr.appendAssistantDelta("streaming more text")
	tr.layout(60)

	if got := tr.blocks[1].layoutRows; len(got) == 0 || &got[0] != &stable[0] {
		t.Fatal("unchanged block was re-laid-out instead of reused from cache")
	}
	if tr.blocks[0].layoutValid == false {
		t.Fatal("untouched user block lost its layout cache")
	}
}

func TestToggleBlockFoldIndependentlyOfGlobalState(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	output := strings.Repeat("line\n", 20)
	tr.startTool("call", "bash", map[string]any{"command": "long"})
	tr.endTool("call", types.TextResult(output, nil), false)

	collapsed := len(tr.layout(80))
	tr.toggleExpand()
	expanded := len(tr.layout(80))
	if expanded <= collapsed {
		t.Fatalf("global expand did not grow the block: %d -> %d", collapsed, expanded)
	}

	// Fold just this block back; the global state stays expanded.
	tr.toggleBlockFold(tr.blocks[0].id)
	refolded := len(tr.layout(80))
	if refolded >= expanded {
		t.Fatalf("per-block fold did not shrink the block: %d -> %d", expanded, refolded)
	}

	// A global toggle resets overrides, so the block follows the global state.
	tr.toggleExpand()
	if tr.blocks[0].foldSet {
		t.Fatal("global toggle did not clear the per-block fold override")
	}
}

func TestToggleBlockFoldIgnoresNonFoldableBlocks(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.addUser("hello")
	rows := len(tr.layout(60))
	tr.toggleBlockFold(tr.blocks[0].id)
	if got := len(tr.layout(60)); got != rows {
		t.Fatalf("user block changed layout after fold toggle: %d -> %d", rows, got)
	}
}

func TestWrapRowHandlesWideCharacters(t *testing.T) {
	// CJK characters occupy two cells; wrapping must not split them and must
	// keep every row within the limit.
	base := layoutRow{
		kind:    rowNotice,
		blockID: 1,
		spans:   []layoutSpan{{text: "你好世界" + strings.Repeat("x", 30)}},
	}
	rows := wrapRow(base, 11)
	for i, r := range rows {
		if w := r.width(); w > 11 {
			t.Fatalf("row %d width %d exceeds limit 11", i, w)
		}
	}
	joined := copyRowsText(rows, textSelection{
		anchor:  textPoint{row: 0, col: 0},
		current: textPoint{row: len(rows) - 1, col: rows[len(rows)-1].width()},
		dragged: true,
	})
	if want := "你好世界" + strings.Repeat("x", 30); joined != want {
		t.Fatalf("wrapped copy = %q, want %q", joined, want)
	}
}

func TestLayoutSkipsHiddenThinking(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.beginThinking()
	tr.appendThinkingDelta("secret")
	tr.endThinking()
	tr.addUser("prompt")

	tr.setShowThinking(false)
	if got := len(tr.layout(60)); got != 1 {
		t.Fatalf("hidden thinking still rendered: %d rows", got)
	}
	tr.setShowThinking(true)
	if got := len(tr.layout(60)); got != 4 { // header + body + spacer + user row
		t.Fatalf("revealed thinking layout = %d rows, want 4", got)
	}
}

func TestDiffRowsCarryGutterPrefixes(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.startTool("call", "write", map[string]any{"path": "f.txt", "content": "a\nb"})
	tr.endTool("call", types.TextResult("wrote", nil), false)

	rows := tr.layout(80)
	var diffRows int
	for _, r := range rows {
		if r.kind == rowDiff {
			diffRows++
			if r.gutterCols != 1 {
				t.Fatalf("diff row has gutterCols %d, want 1", r.gutterCols)
			}
		}
	}
	if diffRows == 0 {
		t.Fatal("no diff rows in layout")
	}
}

func TestToolHeaderShowsStateGlyphAndFoldArrow(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.startTool("call", "bash", map[string]any{"command": "ls"})
	pending := tr.layout(80)[0]
	if got := ansi.Strip(pending.render()); got != "• $ ls" {
		t.Fatalf("pending header = %q", got)
	}
	if pending.gutterCols != 2 || pending.gutterSuffixCols != 0 {
		t.Fatalf("pending header gutters = %d/%d, want 2/0", pending.gutterCols, pending.gutterSuffixCols)
	}
	// Copying a fully selected header excludes both the state glyph and the
	// fold arrow.
	copied := copyRowsText([]layoutRow{pending}, textSelection{
		anchor:  textPoint{row: 0, col: 0},
		current: textPoint{row: 0, col: pending.width()},
		dragged: true,
	})
	if copied != "$ ls" {
		t.Fatalf("header copy = %q, want %q", copied, "$ ls")
	}

	tr.endTool("call", types.TextResult(strings.Repeat("out\n", 20), nil), false)
	done := tr.layout(80)[0]
	if got := ansi.Strip(done.render()); got != "✓ $ ls ▸" {
		t.Fatalf("success header = %q", got)
	}
	tr.toggleBlockFold(tr.blocks[0].id)
	expanded := tr.layout(80)[0]
	if got := ansi.Strip(expanded.render()); got != "✓ $ ls ▾" {
		t.Fatalf("expanded header = %q", got)
	}

	tr.startTool("call2", "bash", map[string]any{"command": "boom"})
	tr.endTool("call2", types.TextResult("failure", nil), true)
	var failed layoutRow
	for _, r := range tr.layout(80) {
		if r.kind == rowToolHeader && r.blockID == tr.blocks[len(tr.blocks)-1].id {
			failed = r
			break
		}
	}
	if got := ansi.Strip(failed.render()); got != "✗ $ boom ▸" {
		t.Fatalf("error header = %q", got)
	}
}

func TestRefreshViewportReportsUnseenRows(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.hasSessionTitle = true
	m.onResize(60, 10)
	for i := 0; i < 40; i++ {
		m.transcript.addUser(strings.Repeat("x", 10))
	}
	m.refreshViewport()
	m.viewport.GotoBottom()
	m.refreshViewport()
	if m.unseenRows != 0 {
		t.Fatalf("unseenRows at bottom = %d, want 0", m.unseenRows)
	}

	m.viewport.SetYOffset(0)
	m.transcript.addUser("new output while scrolled up")
	m.refreshViewport()
	if m.unseenRows <= 0 {
		t.Fatalf("unseenRows while scrolled up = %d, want positive", m.unseenRows)
	}
	if !strings.Contains(m.statusLine(), "below") {
		t.Fatalf("status line lacks unseen indicator: %q", m.statusLine())
	}

	m.viewport.GotoBottom()
	m.refreshViewport()
	if m.unseenRows != 0 {
		t.Fatalf("unseenRows after returning to bottom = %d, want 0", m.unseenRows)
	}
}
