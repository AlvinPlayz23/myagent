package tui

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func visibleComposer(m *model) []string {
	return strings.Split(ansi.Strip(m.renderComposer()), "\n")
}

func assertComposerWidths(t *testing.T, rows []string, width int) {
	t.Helper()
	for i, row := range rows {
		if got := lipgloss.Width(row); got > width {
			t.Fatalf("composer row %d width = %d, want <= %d: %q", i, got, width, row)
		}
	}
}

func TestDefaultComposerRendersBoxedChromeAndInfoDivider(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "grok-3", "")
	m.onResize(72, 24)
	m.input.SetValue("draft")

	rows := visibleComposer(m)
	if got, want := len(rows), m.layout.composer.Height; got != want {
		t.Fatalf("composer rows = %d, want %d", got, want)
	}
	top := []rune(rows[0])
	bottom := []rune(rows[len(rows)-1])
	content := []rune(rows[1])
	if top[0] != '╭' || top[len(top)-1] != '╮' {
		t.Fatalf("top divider = %q", rows[0])
	}
	if bottom[0] != '╰' || bottom[len(bottom)-1] != '╯' {
		t.Fatalf("bottom divider = %q", rows[len(rows)-1])
	}
	if content[0] != '┃' || content[len(content)-1] != '│' {
		t.Fatalf("content chrome = %q", rows[1])
	}
	if !strings.Contains(rows[1], promptGlyph()) {
		t.Fatalf("content row omitted prompt glyph: %q", rows[1])
	}
	if !strings.Contains(rows[len(rows)-1], "grok-3") {
		t.Fatalf("bottom divider omitted model metadata: %q", rows[len(rows)-1])
	}

	m.input.SetValue("first\nsecond")
	rows = visibleComposer(m)
	if !strings.Contains(rows[len(rows)-1], "multiline") {
		t.Fatalf("multiline draft omitted info flag: %q", rows[len(rows)-1])
	}
	assertComposerWidths(t, rows, m.layout.composer.Width)
}

func TestComposerWrapUsesInnerWidthAndPrefixOnlyOnFirstVisualRow(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.promptStyle = promptRuled
	m.onResize(30, 24)
	draft := strings.Repeat("wrap ", 40)
	m.input.SetValue(draft)
	m.updateLayout()

	rows := visibleComposer(m)
	if len(rows) < 4 {
		t.Fatalf("wrapped composer has %d rows, want multiple text rows: %q", len(rows), rows)
	}
	content := rows[1 : len(rows)-1]
	if got := strings.Count(strings.Join(content, "\n"), promptGlyph()); got != 1 {
		t.Fatalf("prompt glyph count = %d, want one first-row prefix: %q", got, content)
	}
	if got := m.input.Value(); got != draft {
		t.Fatalf("textarea draft changed during layout: %q", got)
	}
	assertComposerWidths(t, rows, m.layout.composer.Width)
}

func TestRuledComposerRetainsDynamicHeightAndBottomDivider(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.promptStyle = promptRuled
	m.onResize(64, 24)
	shortHeight := m.composerHeight()
	m.input.SetValue(strings.Repeat("line\n", 4))
	m.updateLayout()

	if got := m.composerHeight(); got != m.input.Height()+composerChromeRows {
		t.Fatalf("ruled composer height = %d, want input %d + chrome %d", got, m.input.Height(), composerChromeRows)
	}
	if got := m.composerHeight(); got <= shortHeight {
		t.Fatalf("ruled composer did not grow: short=%d long=%d", shortHeight, got)
	}
	rows := visibleComposer(m)
	if []rune(rows[0])[0] != '╭' || []rune(rows[len(rows)-1])[0] != '╰' {
		t.Fatalf("ruled frame lost corners: first=%q last=%q", rows[0], rows[len(rows)-1])
	}
	if !strings.Contains(rows[len(rows)-1], "model") {
		t.Fatalf("ruled bottom divider omitted model metadata: %q", rows[len(rows)-1])
	}
	assertComposerWidths(t, rows, m.layout.composer.Width)
}

func TestComposerFocusTintTracksModalOwnershipWithoutChangingDraft(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(64, 20)
	m.input.SetValue("keep this draft")

	focusedBorder := m.promptBorderStyle(true).GetForeground()
	unfocusedBorder := m.promptBorderStyle(false).GetForeground()
	if reflect.DeepEqual(focusedBorder, unfocusedBorder) {
		t.Fatal("focused and unfocused prompt borders use the same color")
	}

	m.customize.active = true
	m.updateLayout()
	if m.input.Focused() {
		t.Fatal("composer stayed focused while a modal owns input")
	}
	if got := m.input.Value(); got != "keep this draft" {
		t.Fatalf("modal focus sync changed draft: %q", got)
	}
	m.customize.active = false
	m.updateLayout()
	if !m.input.Focused() {
		t.Fatal("composer did not regain focus after modal closed")
	}
}

func TestComposerFrameNeverOverflowsNarrowOrShortLayouts(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	for _, size := range []struct{ width, height int }{
		{1, 1}, {2, 3}, {3, 6}, {8, 10}, {20, 12},
	} {
		m.onResize(size.width, size.height)
		rows := visibleComposer(m)
		if len(rows) == 0 {
			t.Fatalf("size %dx%d produced no composer rows", size.width, size.height)
		}
		assertComposerWidths(t, rows, size.width)
	}
}
