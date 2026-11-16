package compositor

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// fill returns a w x h block of the same rune, so compositor output is
// deterministic and contains no ANSI styling.
func fill(w, h int, r rune) string {
	line := strings.Repeat(string(r), w)
	rows := make([]string, h)
	for i := range rows {
		rows[i] = line
	}
	return strings.Join(rows, "\n")
}

func hitTestCompositor() *lipgloss.Compositor {
	dialog := lipgloss.NewLayer(fill(10, 4, 'D')).ID("dialog").X(3).Y(1).Z(20)
	inner := lipgloss.NewLayer("iii").ID("inner").X(2).Y(1).Z(25) // absolute (5,2)
	dialog.AddLayers(inner)
	toast := lipgloss.NewLayer(fill(6, 1, 'T')).ID("toast").X(12).Y(4).Z(30)
	unlabeled := lipgloss.NewLayer(fill(4, 1, 'U')).X(0).Y(0).Z(40) // no ID: must never win Hit
	backdrop := lipgloss.NewLayer(fill(20, 6, '#')).ID("backdrop").Z(0)
	backdrop.AddLayers(dialog, toast, unlabeled)
	return lipgloss.NewCompositor(backdrop)
}

func TestHitTopMostID(t *testing.T) {
	c := hitTestCompositor()
	cases := []struct {
		x, y int
		want string
		why  string
	}{
		{13, 4, "toast", "toast overlaps dialog at its top-left corner"},
		{12, 4, "toast", "first toast cell wins over dialog"},
		{17, 4, "toast", "last toast cell"},
		{11, 4, "dialog", "one cell left of toast is dialog"},
		{3, 1, "dialog", "dialog top-left"},
		{12, 1, "dialog", "dialog top-right (x=3+10-1)"},
		{5, 2, "inner", "nested child is positioned relative to dialog"},
		{4, 2, "dialog", "one cell left of nested child"},
		{2, 1, "backdrop", "outside dialog"},
		{0, 0, "backdrop", "ID-less layer at z=40 is skipped by Hit"},
		{19, 5, "backdrop", "backdrop bottom-right"},
	}
	for _, tc := range cases {
		hit := c.Hit(tc.x, tc.y)
		if hit.Empty() {
			t.Fatalf("Hit(%d,%d) empty, want %q (%s)", tc.x, tc.y, tc.want, tc.why)
		}
		if got := hit.ID(); got != tc.want {
			t.Errorf("Hit(%d,%d).ID() = %q, want %q (%s)", tc.x, tc.y, got, tc.want, tc.why)
		}
	}
}

func TestHitOutsideAndClipBounds(t *testing.T) {
	c := hitTestCompositor()
	for _, p := range [][2]int{{20, 5}, {0, 6}, {-1, 0}, {13, 6}} {
		if hit := c.Hit(p[0], p[1]); !hit.Empty() {
			t.Errorf("Hit(%d,%d) = %q, want empty", p[0], p[1], hit.ID())
		}
	}
	// Layer hit bounds are content bounds, not the union with children.
	// inner is at absolute (5,2); dialog still owns (12,4) outside toast?
	if got := c.Hit(12, 4).ID(); got != "toast" {
		t.Errorf("overlap clip: Hit(12,4) = %q, want toast", got)
	}
	if got := c.Hit(12, 3).ID(); got != "dialog" {
		t.Errorf("overlap clip: Hit(12,3) = %q, want dialog", got)
	}
}

func TestRenderThreeLayers(t *testing.T) {
	dialog := lipgloss.NewLayer(fill(10, 4, 'D')).ID("dialog").X(3).Y(1).Z(20)
	toast := lipgloss.NewLayer(fill(6, 1, 'T')).ID("toast").X(12).Y(4).Z(30)
	backdrop := lipgloss.NewLayer(fill(20, 6, '#')).ID("backdrop").Z(0)
	backdrop.AddLayers(dialog, toast)
	c := lipgloss.NewCompositor(backdrop)

	want := strings.Join([]string{
		"####################",
		"###DDDDDDDDDD#######",
		"###DDDDDDDDDD#######",
		"###DDDDDDDDDD#######",
		"###DDDDDDDDDTTTTTT##",
		"####################",
	}, "\n")
	if got := c.Render(); got != want {
		t.Fatalf("Render mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Z is compared as a flat number, not accumulated through parents. A child
// with a lower Z than its parent paints and hits below the parent. Phase 1
// must assign compositor-wide Z bands, not per-container sibling Z.
func TestZIsGlobalNotInherited(t *testing.T) {
	parent := lipgloss.NewLayer(fill(10, 2, 'P')).ID("parent").Z(10)
	child := lipgloss.NewLayer(fill(4, 1, 'C')).ID("child").X(1).Y(0).Z(1)
	parent.AddLayers(child)
	c := lipgloss.NewCompositor(parent)
	if got := c.Hit(2, 0).ID(); got != "parent" {
		t.Fatalf("Hit(2,0) = %q, want parent (child z=1 sorts below parent z=10)", got)
	}
}

// Compositor.Render sizes its canvas from Bounds() but draws at absolute
// coordinates. Without a layer at (0,0), non-zero origins are shifted/clipped.
// Keep a full-screen backdrop as the bottom layer in the app compositor.
func TestRenderRequiresOriginLayer(t *testing.T) {
	only := lipgloss.NewLayer("XX").ID("only").X(2).Y(1).Z(1)
	c := lipgloss.NewCompositor(only)
	if got := c.Render(); got != "" {
		t.Fatalf("Render() = %q; want empty because the canvas is 2x1 at (0,0) while the layer draws at (2,1)", got)
	}
}

func benchCompositor() *lipgloss.Compositor {
	backdrop := lipgloss.NewLayer(fill(200, 60, '#')).ID("backdrop").Z(0)
	dialog := lipgloss.NewLayer(fill(60, 12, 'D')).ID("dialog").X(70).Y(20).Z(20)
	toast := lipgloss.NewLayer(fill(40, 3, 'T')).ID("toast").X(150).Y(2).Z(30)
	backdrop.AddLayers(dialog, toast)
	return lipgloss.NewCompositor(backdrop)
}

var benchSink string
var benchHit lipgloss.LayerHit

func BenchmarkRender3Layers200x60(b *testing.B) {
	c := benchCompositor()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = c.Render()
	}
}

// Baseline: one full-screen layer shows the floor cost of canvas + parse.
func BenchmarkRender1Layer200x60(b *testing.B) {
	c := lipgloss.NewCompositor(lipgloss.NewLayer(fill(200, 60, '#')).ID("backdrop").Z(0))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchSink = c.Render()
	}
}

func BenchmarkHit3Layers200x60(b *testing.B) {
	c := benchCompositor()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchHit = c.Hit(100, 30)
	}
	if benchHit.Empty() {
		b.Fatal("unexpected empty hit")
	}
}
