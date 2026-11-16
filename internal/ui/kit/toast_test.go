package kit

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// writeGolden compares (or writes) a plaintext fixture. Fixtures are
// ANSI-stripped per the S1 convention; color assertions live in the tests.
func writeGolden(t *testing.T, name, frame string) {
	t.Helper()
	plain := trimRightLines(ansi.Strip(frame))
	path := filepath.Join("testdata", name)
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
		t.Fatalf("read golden %s: %v (run with UPDATE_GOLDENS=1)", path, err)
	}
	wantStr := strings.TrimSuffix(strings.TrimSuffix(string(want), "\n"), "\r")
	if plain != wantStr {
		t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, plain, wantStr)
	}
}

// hexOf renders a resolved color the way the theme table prints it.
func hexOf(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8)
}

// sgrTruecolor is the SGR parameter lipgloss emits for a foreground token.
func sgrTruecolor(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

// hasBold reports the SGR bold attribute in any sequence lipgloss emits.
func hasBold(s string) bool {
	return strings.Contains(s, "\x1b[1m") || strings.Contains(s, "\x1b[1;") || strings.Contains(s, ";1m")
}

// decodeFrame parses a rendered frame back into cells so tests can assert the
// color that actually reached the border/body cells, not just the token value.
func decodeFrame(t *testing.T, frame string, w, h int) *lipgloss.Canvas {
	t.Helper()
	cv := lipgloss.NewCanvas(w, h)
	uv.NewStyledString(frame).Draw(cv, uv.Rect(0, 0, w, h))
	return cv
}

// toastSurface lays a host out in an 80x24 band and returns its own frame.
func toastSurface(t *testing.T, host *ToastHost) string {
	t.Helper()
	host.Layout(Rect{X: 0, Y: 0, W: 80, H: 24})
	r := host.Bounds()
	if r.Empty() {
		return ""
	}
	out := host.Render(r.W, r.H)
	assertFrameSize(t, out, r.W, r.H)
	return out
}

func TestToastWidthCap(t *testing.T) {
	cases := []struct {
		band int
		want int
	}{
		{120, 60},
		{80, 60},
		{65, 59},
		{40, 34},
		{20, 14},
		{16, 10},
		{14, 8},
		{13, 7},
		{12, 0},
		{6, 0},
	}
	for _, c := range cases {
		host := NewToastHost(ToastOpts{})
		host.Show(Toast{Message: "wide"})
		host.Layout(Rect{X: 3, Y: 5, W: c.band, H: 30})
		r := host.Bounds()
		if c.want == 0 {
			if !r.Empty() {
				t.Fatalf("band %d bounds = %+v, want empty", c.band, r)
			}
			continue
		}
		if r.W != c.want {
			t.Fatalf("band %d width = %d, want %d", c.band, r.W, c.want)
		}
		if r.W > min(toastMaxWidth, c.band-6) {
			t.Fatalf("band %d width %d exceeds cap", c.band, r.W)
		}
		if r.X != 3+c.band-toastRightInset-r.W || r.Y != 5+toastTopInset {
			t.Fatalf("band %d rect = %+v", c.band, r)
		}
	}

	host := NewToastHost(ToastOpts{MaxWidth: 20})
	host.Show(Toast{Message: "narrow"})
	host.Layout(Rect{W: 80, H: 24})
	if got := host.Bounds().W; got != 20 {
		t.Fatalf("MaxWidth 20 produced width %d", got)
	}
	host.SetMaxWidth(100)
	host.Layout(Rect{W: 120, H: 24})
	if got := host.Bounds().W; got != toastMaxWidth {
		t.Fatalf("MaxWidth above cap produced width %d", got)
	}
}

func TestToastQueueFIFOAndCounter(t *testing.T) {
	host := NewToastHost(ToastOpts{ID: "toasts"})
	host.Show(Toast{Title: "First", Message: "one"})
	host.Show(Toast{Title: "Second", Message: "two"})
	host.Show(Toast{Title: "Third", Message: "three"})

	if host.Len() != 3 || host.Pending() != 2 {
		t.Fatalf("len=%d pending=%d, want 3/2", host.Len(), host.Pending())
	}
	if cur, ok := host.Current(); !ok || cur.Title != "First" {
		t.Fatalf("current = %q/%v, want First", cur.Title, ok)
	}
	frame := toastSurface(t, host)
	if !strings.Contains(ansi.Strip(frame), "+2 more") {
		t.Fatalf("queue frame missing +2 more:\n%s", ansi.Strip(frame))
	}

	host.Dismiss()
	if cur, _ := host.Current(); cur.Title != "Second" || host.Pending() != 1 {
		t.Fatalf("after dismiss current=%q pending=%d", cur.Title, host.Pending())
	}
}

func TestToastAutoDismissPromotesQueue(t *testing.T) {
	host := NewToastHost(ToastOpts{})
	host.Show(Toast{Message: "short", Duration: time.Second})
	host.Show(Toast{Message: "next"})

	if host.Advance(999 * time.Millisecond) {
		t.Fatal("dismissed before the timer expired")
	}
	if !host.Advance(time.Millisecond) {
		t.Fatal("timer expiry did not dismiss")
	}
	if cur, _ := host.Current(); cur.Message != "next" {
		t.Fatalf("promoted %q, want next", cur.Message)
	}
	if host.Advance(time.Second) {
		t.Fatal("default duration applied to the promoted toast was too short")
	}
	if !host.Advance(4 * time.Second) {
		t.Fatal("default 5s timer did not expire")
	}
	if host.Active() || host.Len() != 0 {
		t.Fatalf("host still active after last toast: len=%d", host.Len())
	}
}

func TestToastHoverPausesTimer(t *testing.T) {
	host := NewToastHost(ToastOpts{})
	host.Show(Toast{Title: "Saved", Message: "written", Duration: 2 * time.Second})
	host.Layout(Rect{X: 0, Y: 0, W: 80, H: 24})
	r := host.Bounds()

	if !host.Pointer(r.X+2, r.Y+1) {
		t.Fatal("pointer enter did not change state")
	}
	if !host.Paused() || !host.hover {
		t.Fatal("hover did not pause the timer")
	}
	if host.Advance(10 * time.Second) {
		t.Fatal("paused toast dismissed")
	}
	if !host.Active() {
		t.Fatal("paused toast disappeared")
	}

	host.Pointer(1, 23)
	if host.Paused() || host.hover {
		t.Fatal("pointer leave did not resume the timer")
	}
	if !host.Advance(2 * time.Second) {
		t.Fatal("resumed toast did not dismiss")
	}
}

func TestToastMouseMotionAndClickAction(t *testing.T) {
	host := NewToastHost(ToastOpts{})
	runs := 0
	host.Show(Toast{Message: "deploy failed", Action: &ToastAction{Label: "retry", Run: func() { runs++ }}})
	host.Layout(Rect{X: 0, Y: 0, W: 80, H: 24})
	r := host.Bounds()

	if !host.Mouse(tea.MouseMotionMsg{X: r.X + 2, Y: r.Y + 1}, r) {
		t.Fatal("motion over toast not consumed")
	}
	if !host.Paused() {
		t.Fatal("motion over toast did not pause")
	}
	if !host.Mouse(tea.MouseClickMsg{X: r.X + 2, Y: r.Y + 1, Button: tea.MouseLeft}, r) {
		t.Fatal("click on toast not consumed")
	}
	if runs != 1 {
		t.Fatalf("action ran %d times, want 1", runs)
	}
	if host.Active() {
		t.Fatal("click did not dismiss the toast")
	}
}

func TestToastVariantTints(t *testing.T) {
	th := DefaultTheme()
	cases := []struct {
		v    ToastVariant
		tint string
	}{
		{ToastInfo, hexOf(th.Text.Feedback.Info.Base.Color())},
		{ToastSuccess, hexOf(th.Text.Feedback.Success.Base.Color())},
		{ToastWarning, hexOf(th.Text.Feedback.Warning.Base.Color())},
		{ToastError, hexOf(th.Text.Feedback.Error.Base.Color())},
	}
	lines := make([]string, 0, len(cases))
	for _, c := range cases {
		host := NewToastHost(ToastOpts{})
		host.Show(Toast{Title: "Notice", Message: "body", Variant: c.v})
		frame := toastSurface(t, host)
		r := host.Bounds()
		cv := decodeFrame(t, frame, r.W, r.H)

		if got := hexOf(toastTint(th, c.v).Color()); got != c.tint {
			t.Fatalf("variant %d tint %s, want %s", c.v, got, c.tint)
		}
		// The variant is a rendered property: every left/right border cell
		// carries the feedback color, not merely a token value in isolation.
		for y := 0; y < r.H; y++ {
			for _, x := range []int{0, r.W - 1} {
				cell := cv.CellAt(x, y)
				if cell == nil || cell.Content != "┃" || cell.Style.Fg == nil {
					t.Fatalf("variant %d border cell (%d,%d) = %+v", c.v, x, y, cell)
				}
				if got := hexOf(cell.Style.Fg); got != c.tint {
					t.Fatalf("variant %d border (%d,%d) = %s, want %s", c.v, x, y, got, c.tint)
				}
			}
		}
		body := cv.CellAt(1, 0)
		if body == nil || body.Style.Bg == nil {
			t.Fatalf("variant %d body cell = %+v", c.v, body)
		}
		if got, want := hexOf(body.Style.Bg), hexOf(th.Background.Raised.High.Color()); got != want {
			t.Fatalf("variant %d body bg = %s, want %s", c.v, got, want)
		}
		if !hasBold(frame) {
			t.Fatalf("variant %d title is not bold", c.v)
		}
		lines = append(lines, fmt.Sprintf("%-8s border=%s body=%s rows=%d",
			variantName(c.v), c.tint, hexOf(body.Style.Bg), r.H))
	}
	writeGolden(t, "s2b_toast_variants.txt", strings.Join(lines, "\n")+"\n")
}

func variantName(v ToastVariant) string {
	switch v {
	case ToastSuccess:
		return "success"
	case ToastWarning:
		return "warning"
	case ToastError:
		return "error"
	default:
		return "info"
	}
}

// TestToastHoverEmphasizesAffordance proves the hover state is real: the same
// plaintext frame paints the affordance bold + primary only while hovered.
func TestToastHoverEmphasizesAffordance(t *testing.T) {
	host := NewToastHost(ToastOpts{})
	host.Show(Toast{Message: "deploy failed", Action: &ToastAction{Label: "retry", Run: func() {}}})
	host.Layout(Rect{X: 0, Y: 0, W: 80, H: 24})
	r := host.Bounds()
	plain := host.Render(r.W, r.H)

	host.Pointer(r.X+2, r.Y+1)
	hovered := host.Render(r.W, r.H)

	if ansi.Strip(plain) != ansi.Strip(hovered) {
		t.Fatalf("hover changed layout:\n%s\n---\n%s", ansi.Strip(plain), ansi.Strip(hovered))
	}
	if hasBold(plain) {
		t.Fatal("affordance is bold before hover")
	}
	if !hasBold(hovered) {
		t.Fatalf("hovered affordance is not bold: plain=%q hovered=%q", plain, hovered)
	}
	if !strings.Contains(hovered, sgrTruecolor(DefaultTheme().Text.Action.Primary.Color())) {
		t.Fatal("hovered affordance is not painted in text.action.primary")
	}
	dismissed := host.Advance(10 * time.Second)
	if dismissed || !host.Active() {
		t.Fatal("hover did not pause the auto-dismiss timer")
	}

	writeGolden(t, "s2b_toast_hover.txt", fmt.Sprintf(
		"paused=%t\nhover=%t\nbold=%t\ndismissed-while-paused=%t\nframe:\n%s\n",
		host.Paused(), host.hover, hasBold(hovered), dismissed, ansi.Strip(hovered)))
}

func TestToastRootBandLifecycle(t *testing.T) {
	base := newProbe("base")
	base.consumeKey = true
	host := NewToastHost(ToastOpts{ID: "toasts"})
	root := NewRoot(base, RegisterBandHost(BandToasts, host, WithBandCapture(false)))

	root.Render(80, 24)
	if hit, ok := root.Hit(20, 2); !ok || hit.Band != BandBase {
		t.Fatalf("closed toast registered a hit: %+v/%v", hit, ok)
	}

	host.Show(Toast{Title: "Saved", Message: "3 files written"})
	frame := root.Render(80, 24)
	if !strings.Contains(ansi.Strip(frame), "Saved") {
		t.Fatalf("toast not composed into the frame:\n%s", ansi.Strip(frame))
	}
	hit, ok := root.Hit(20, 2)
	if !ok || hit.Band != BandToasts || hit.ID != "toasts" {
		t.Fatalf("toast hit = %+v/%v", hit, ok)
	}
	// Outside the surface the base still owns the click.
	if other, ok := root.Hit(2, 20); !ok || other.Band != BandBase {
		t.Fatalf("base hit = %+v/%v", other, ok)
	}
	// WithBandCapture(false) keeps typing on the composer path.
	if !root.Key(tea.KeyPressMsg{Code: 'a', Text: "a"}) || len(base.keys) != 1 {
		t.Fatalf("toast host swallowed a key: %v", base.keys)
	}

	if !host.Advance(ToastDuration) {
		t.Fatal("toast did not expire")
	}
	root.Render(80, 24)
	if hit, ok := root.Hit(20, 2); !ok || hit.Band != BandBase {
		t.Fatalf("expired toast left a hit rect: %+v/%v", hit, ok)
	}
}

func TestToastGoldens(t *testing.T) {
	open := NewToastHost(ToastOpts{ID: "toasts"})
	open.Show(Toast{
		Title:   "Saved",
		Message: "3 files written to the workspace",
		Variant: ToastSuccess,
		Action:  &ToastAction{Label: "undo", Run: func() {}},
	})
	writeGolden(t, "s2b_toast_open.txt", toastSurface(t, open))

	queue := NewToastHost(ToastOpts{ID: "toasts"})
	queue.Show(Toast{Title: "Build", Message: "compiling package kit", Variant: ToastInfo})
	queue.Show(Toast{Title: "Second", Message: "queued update"})
	queue.Show(Toast{Title: "Third", Message: "another queued update"})
	writeGolden(t, "s2b_toast_queue.txt", toastSurface(t, queue))

	closed := NewToastHost(ToastOpts{ID: "toasts"})
	closed.Show(Toast{Message: "temp"})
	closed.Dismiss()
	closed.Layout(Rect{X: 0, Y: 0, W: 80, H: 24})
	frame := toastSurface(t, closed)
	if frame != "" {
		t.Fatalf("closed host painted %q", frame)
	}
	writeGolden(t, "s2b_toast_closed.txt", frame)
}
