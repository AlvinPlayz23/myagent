package kit

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDockEmptyPaintsNothing(t *testing.T) {
	d := NewDock(DockOpts{ID: "dock"})
	if d.Any() || d.Height() != 0 {
		t.Fatalf("empty dock = any %v height %d, want false/0", d.Any(), d.Height())
	}
	if got := d.Render(40, 0); got != "" {
		t.Fatalf("empty dock render = %q, want empty", got)
	}
}

func TestDockRowsRenderAndHeight(t *testing.T) {
	d := NewDock(DockOpts{ID: "dock"})
	d.SetRows([]DockRow{
		{Label: "next", Text: "run the tests after this", Hint: "ctrl+enter queue"},
		{Label: "[image]", Text: "2 attached (1.5 MiB)"},
	})
	if !d.Any() || d.Height() != 2 {
		t.Fatalf("dock = any %v height %d, want true/2", d.Any(), d.Height())
	}
	d.Layout(Rect{X: 0, Y: 10, W: 80, H: 2})
	raw := d.Render(80, 2)
	assertDockFrame(t, raw, 80, 2)
	plain := ansi.Strip(raw)
	for _, want := range []string{"┃", "next", "run the tests after this", "ctrl+enter queue", "[image]", "2 attached"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("dock frame missing %q:\n%s", want, plain)
		}
	}
}

func TestDockSetRowsIdempotent(t *testing.T) {
	d := NewDock(DockOpts{ID: "dock"})
	rows := []DockRow{{Label: "next", Text: "later"}}
	d.SetRows(rows)
	rev := d.Rev()
	d.SetRows(rows)
	if d.Rev() != rev {
		t.Fatalf("identical SetRows bumped rev %d -> %d", rev, d.Rev())
	}
	d.SetRows([]DockRow{{Label: "next", Text: "changed"}})
	if d.Rev() == rev {
		t.Fatal("changed SetRows did not bump the revision")
	}
}

func assertDockFrame(t *testing.T, frame string, w, h int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != h {
		t.Fatalf("dock height = %d, want %d", len(lines), h)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != w {
			t.Fatalf("dock line %d width = %d, want %d (%q)", i, got, w, line)
		}
	}
}
