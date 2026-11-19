package kit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestTabsTinyLayoutClipsHitTargetsAndKeepsTitle(t *testing.T) {
	tabs := NewTabs(TabsOpts{Items: []TabItem{{ID: "one", Title: "project", Busy: true, Unread: true}}})
	tabs.Layout(Rect{X: 4, Y: 2, W: 1, H: 1})
	if len(tabs.rects) != 1 || tabs.rects[0] != (Rect{X: 4, Y: 2, W: 1, H: 1}) {
		t.Fatalf("tiny tab hit rects = %#v, want the single visible cell", tabs.rects)
	}
	line := ansi.Strip(tabs.Render(1, 1))
	if got := ansi.StringWidth(line); got != 1 {
		t.Fatalf("tiny tab width = %d, want 1 (%q)", got, line)
	}
	if !strings.Contains(line, "…") {
		t.Fatalf("tiny tab should keep a clipped title instead of badges: %q", line)
	}

	tabs.Layout(Rect{X: 4, Y: 2, W: 0, H: 1})
	if len(tabs.rects) != 0 {
		t.Fatalf("zero-width rail retained hit rects: %#v", tabs.rects)
	}
}

func TestComposerFooterPreservesClippedRightStatus(t *testing.T) {
	footer := NewComposerFooter(ComposerFooterOpts{})
	footer.SetState(false, "/workspace/a-long-directory", "report.md", false, "")
	footer.Layout(Rect{W: 5, H: 1})
	line := ansi.Strip(footer.Render(5, 1))
	if got := ansi.StringWidth(line); got != 5 {
		t.Fatalf("footer width = %d, want 5 (%q)", got, line)
	}
	if !strings.Contains(line, "…") || strings.Contains(line, "workspace") {
		t.Fatalf("narrow footer should show clipped file status without overlapping cwd: %q", line)
	}
}
