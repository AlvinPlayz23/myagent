package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestComputeTUILayoutKeepsNamedRectsOrdered(t *testing.T) {
	l := computeTUILayout(tuiLayoutParams{
		width: 120, height: 40,
		topBarRows: 1, panelRows: 4, queueRows: 2, attachmentRows: 1,
		composerRows: 4, statusRows: 1, shortcutRows: 1, footerRows: 2,
		timelineWidth: 1,
	})
	if got, want := l.scrollback.Height, 24; got != want {
		t.Fatalf("scrollback height = %d, want %d", got, want)
	}
	ordered := []tuiRect{l.topBar, l.panel, l.scrollback, l.queue, l.attachments, l.statusLine, l.composer, l.shortcuts, l.footer}
	last := 0
	for _, r := range ordered {
		if r.empty() {
			continue
		}
		if r.Y < last {
			t.Fatalf("rect %v overlaps previous row ending at %d", r, last)
		}
		last = r.Y + r.Height
	}
	if got := l.rows(); got != 40 {
		t.Fatalf("layout rows = %d, want 40", got)
	}
	if got, want := l.scrollbackContent.Width, 119; got != want {
		t.Fatalf("scrollback content width = %d, want %d", got, want)
	}
	if l.timeline.empty() {
		t.Fatal("timeline lane is empty despite timeline reservation")
	}
}

func TestComputeTUILayoutReservesFallbackScrollbarLane(t *testing.T) {
	l := computeTUILayout(tuiLayoutParams{
		width: 120, height: 24, composerRows: 4, statusRows: 1,
		scrollbarWidth: fallbackScrollbarLaneWidth,
	})
	if l.timeline != (tuiRect{}) || l.scrollbar.empty() {
		t.Fatalf("fallback lane geometry = timeline=%#v scrollbar=%#v", l.timeline, l.scrollbar)
	}
	if got, want := l.scrollbackContent.Width, l.scrollback.Width-fallbackScrollbarLaneWidth; got != want {
		t.Fatalf("fallback content width = %d, want %d", got, want)
	}
}

func TestComputeTUILayoutDoesNotCreatePartialFallbackLane(t *testing.T) {
	l := computeTUILayout(tuiLayoutParams{
		width: 2, height: 12, composerRows: 2,
		scrollbarWidth: fallbackScrollbarLaneWidth,
	})
	if !l.scrollbar.empty() {
		t.Fatalf("narrow layout kept an incomplete scrollbar lane: %#v", l.scrollbar)
	}
	if got, want := l.scrollbackContent.Width, l.scrollback.Width; got != want {
		t.Fatalf("narrow content width = %d, want full width %d", got, want)
	}
}

func TestComputeTUILayoutClampsOptionalRowsOnShortTerminal(t *testing.T) {
	l := computeTUILayout(tuiLayoutParams{
		width: 80, height: 12,
		topBarRows: 1, panelRows: 10, queueRows: 2, attachmentRows: 1,
		composerRows: 4, statusRows: 1, shortcutRows: 1, footerRows: 2,
	})
	if !l.shortTerminal || !l.compact {
		t.Fatalf("short=%v compact=%v, want both true", l.shortTerminal, l.compact)
	}
	if !l.footer.empty() {
		t.Fatalf("short terminal kept footer: %#v", l.footer)
	}
	if !l.panel.empty() || !l.queue.empty() || !l.attachments.empty() {
		t.Fatalf("short terminal kept optional rows: %#v", l)
	}
	if l.scrollback.Height < minScrollbackRows || l.composer.Height != 4 || l.shortcuts.Height != 1 {
		t.Fatalf("short layout starved required rows: %#v", l)
	}
}

func TestComputeTUILayoutNeverUsesNegativeGeometry(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {1, 2}, {4, 4}} {
		l := computeTUILayout(tuiLayoutParams{
			width: size[0], height: size[1], composerRows: 8, footerRows: 2, shortcutRows: 1,
			scrollbarWidth: 5,
		})
		for _, r := range []tuiRect{l.topBar, l.panel, l.scrollback, l.scrollbackContent, l.timeline, l.scrollbar, l.composer, l.shortcuts, l.footer} {
			if r.Width < 0 || r.Height < 0 {
				t.Fatalf("size %v produced negative rect %#v", size, r)
			}
		}
	}
}

func TestComputeTUILayoutNeverOverflowsTerminalHeight(t *testing.T) {
	for height := 1; height <= 10; height++ {
		l := computeTUILayout(tuiLayoutParams{
			width: 80, height: height,
			topBarRows: 1, panelRows: 20, queueRows: 4, attachmentRows: 3,
			composerRows: 8, statusRows: 1, shortcutRows: 1, footerRows: 2,
		})
		if got := l.rows(); got > height {
			t.Fatalf("height %d produced %d rows: %#v", height, got, l)
		}
		if height > 1 && l.scrollback.Height < min(minScrollbackRows, height) {
			t.Fatalf("height %d lost scrollback floor: %#v", height, l)
		}
	}
}

func TestComputeTUILayoutPutsTurnStatusBeforeComposer(t *testing.T) {
	l := computeTUILayout(tuiLayoutParams{
		width: 80, height: 30, composerRows: 4, statusRows: 1, shortcutRows: 1,
	})
	if l.statusLine.Y >= l.composer.Y {
		t.Fatalf("status line %#v should precede composer %#v", l.statusLine, l.composer)
	}
}

func TestViewFitsShortTerminalRows(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	for _, height := range []int{1, 2, 12, 16, 20} {
		m.onResize(80, height)
		content := m.View().Content
		rows := strings.Split(content, "\n")
		if len(rows) > height {
			t.Fatalf("height %d rendered %d rows", height, len(rows))
		}
		for i, row := range rows {
			if got := lipgloss.Width(row); got > 80 {
				t.Fatalf("height %d row %d width = %d", height, i, got)
			}
		}
	}
}

func TestTruncateColumnsUsesDisplayWidth(t *testing.T) {
	styled := "\x1b[38;2;122;162;247m你好\x1b[0m"
	got := truncateColumns(styled, 3)
	if !strings.Contains(got, "[38") {
		t.Fatalf("truncation stripped ANSI styling: %q", got)
	}
	if width := visibleMaxWidth([]string{got}); width > 3 {
		t.Fatalf("truncated width = %d, want <= 3", width)
	}
}

func TestRenderModalFrameIsBoundedOnShortTerminal(t *testing.T) {
	body := strings.Join([]string{"one", "two", "three", "four", "five", "six", "seven"}, "\n")
	bounds, frame := renderModalFrame("Sessions", body, "enter select · esc cancel", 80, 6, newTheme())
	rows := strings.Split(frame, "\n")
	if len(rows) != bounds.Height {
		t.Fatalf("modal rows = %d, bounds height = %d", len(rows), bounds.Height)
	}
	if !strings.Contains(rows[len(rows)-1], "─") {
		t.Fatalf("modal lost bottom border: %q", frame)
	}
}

func TestModalBoundsFitShortTerminal(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {20, 6}, {5, 3}} {
		r := modalBounds(size[0], size[1], 40, 20)
		if r.Width > size[0] || r.Height > size[1] || r.X < 0 || r.Y < 0 {
			t.Fatalf("modal %#v does not fit terminal %v", r, size)
		}
	}
}

func TestOverlayModalClearsUnderlyingRows(t *testing.T) {
	base := strings.Join([]string{"background one", "background two", "background three", "background four", "background five"}, "\n")
	bounds := tuiRect{X: 1, Y: 1, Width: 5, Height: 3}
	got := overlayModal(base, 20, 5, bounds, "─────\n│ok │\n─────")
	rows := strings.Split(got, "\n")
	if len(rows) != 5 {
		t.Fatalf("overlay rows = %d, want 5", len(rows))
	}
	if strings.Contains(rows[1], "background") || strings.Contains(rows[2], "background") || strings.Contains(rows[3], "background") {
		t.Fatalf("modal leaked underlying content: %q", got)
	}
	if !strings.Contains(rows[2], "ok") {
		t.Fatalf("modal frame missing: %q", got)
	}
}
