package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// goldenTree is a representative shell: a greedy card in a column, a fixed
// row with a left rail and a rounded pane, and a footer. It exercises every S1
// primitive and the flex grow/gap/padding paths.
func goldenTree() *Root {
	th := DefaultTheme()

	header := NewText(TextOpts{
		ID:      "header",
		Content: "myagent — retained kit",
		Fg:      th.Text.Strong,
		Bold:    true,
	})
	cardBody := NewText(TextOpts{
		ID: "card.body",
		Content: "Assistant · muse · 1.2s · 34.5 tok/s\n\n" +
			"The pack contains many widgets with borders and padded surfaces. " +
			"This paragraph is deliberately long so the golden frame proves that " +
			"width-aware wrapping and the rev-keyed render cache agree at both " +
			"terminal sizes.",
		Fg: th.Text.Base,
	})
	card := NewCard(cardBody)
	card.ID = "card"

	left := NewText(TextOpts{ID: "row.left", Content: "left rail", Fg: th.Text.Muted, Align: AlignCenter})
	paneBody := NewText(TextOpts{ID: "pane.body", Content: "Ready", Fg: th.Text.Feedback.Success.Base})
	pane := NewPane(paneBody, "Status")
	pane.ID = "row.pane"

	row := NewBox(BoxOpts{ID: "row", Direction: Row, Gap: 1})
	row.Add(
		Child{W: left, FlexBasis: 24},
		Child{W: pane, FlexGrow: 1},
	)

	footer := NewText(TextOpts{
		ID:      "footer",
		Content: "tab focus · wheel scroll · click to open",
		Fg:      th.Text.Muted,
	})

	shell := NewBox(BoxOpts{
		ID:        "shell",
		Direction: Column,
		Gap:       1,
		Padding:   InsetsAll(1),
	})
	shell.Add(
		Child{W: header, Fixed: 1},
		Child{W: card, Grow: 1},
		Child{W: row, Fixed: 5},
		Child{W: footer, Fixed: 1},
	)
	return NewRoot(shell)
}

// TestGoldenFrames is the G1 gate. Fixtures are plaintext (ANSI stripped) so
// they are reviewable in a diff; the test separately asserts the raw frame is
// exactly 80x24 / 120x40 cells.
func TestGoldenFrames(t *testing.T) {
	sizes := []struct{ w, h int }{{80, 24}, {120, 40}}
	for _, size := range sizes {
		t.Run(frameName(size.w, size.h), func(t *testing.T) {
			root := goldenTree()
			raw := root.Render(size.w, size.h)
			assertFrameSize(t, raw, size.w, size.h)
			plain := trimRightLines(ansi.Strip(raw))

			path := filepath.Join("testdata", frameName(size.w, size.h)+".txt")
			if os.Getenv("UPDATE_GOLDENS") == "1" {
				if err := os.MkdirAll("testdata", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(plain+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v (run with UPDATE_GOLDENS=1 to create)", err)
			}
			wantStr := strings.TrimSuffix(strings.TrimSuffix(string(want), "\n"), "\r")
			if got := plain; got != wantStr {
				t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, got, wantStr)
			}
		})
	}
}

// TestGoldenLayoutRects pins a few rects from the golden tree against the hit
// registry, proving G1's frame and the layout pass describe the same geometry.
func TestGoldenLayoutRects(t *testing.T) {
	root := goldenTree()
	root.Render(80, 24)

	// Shell padding 1: header at (1,1), card below the 1-cell gap at y=3.
	cases := []struct {
		id   string
		want Rect
	}{
		{"shell", Rect{0, 0, 80, 24}},
		{"header", Rect{1, 1, 78, 1}},
		{"card", Rect{1, 3, 78, 12}},
		{"row", Rect{1, 16, 78, 5}},
		{"row.left", Rect{1, 16, 24, 5}},
		{"row.pane", Rect{26, 16, 53, 5}},
		{"footer", Rect{1, 22, 78, 1}},
	}
	for _, tc := range cases {
		got, ok := root.HitRegistry().Rect(tc.id)
		if !ok {
			t.Errorf("%s not registered", tc.id)
			continue
		}
		if got != tc.want {
			t.Errorf("%s = %+v, want %+v", tc.id, got, tc.want)
		}
	}
}

func frameName(w, h int) string {
	return "frame_" + itoa(w) + "x" + itoa(h)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// assertFrameSize checks the raw (styled) frame is exactly w by h cells and
// has h lines.
func assertFrameSize(t *testing.T, frame string, w, h int) {
	t.Helper()
	lines := strings.Split(frame, "\n")
	if len(lines) != h {
		t.Fatalf("frame height = %d, want %d", len(lines), h)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != w {
			t.Fatalf("line %d width = %d, want %d (%q)", i, got, w, line)
		}
	}
}

// trimRightLines strips trailing spaces per line for reviewable fixtures.
func trimRightLines(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}
