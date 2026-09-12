package tui

import (
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestUrlAtDisplayColumn(t *testing.T) {
	line := "see https://example.com now"
	start := strings.Index(line, "https")
	cases := []struct {
		name string
		line string
		col  int
		want string
	}{
		{"inside", line, start + 2, "https://example.com"},
		{"first char", line, start, "https://example.com"},
		{"before", line, start - 1, ""},
		{"after", line, start + len("https://example.com"), ""},
		{"negative", line, -1, ""},
		{"styled", "\033[32msee https://example.com\033[0m now", start + 2, "https://example.com"},
		{"trailing period excluded", "visit https://example.com.", 7, "https://example.com"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := urlAtDisplayColumn(c.line, c.col); got != c.want {
				t.Errorf("urlAtDisplayColumn(col=%d) = %q, want %q", c.col, got, c.want)
			}
		})
	}
}

func TestBlockLineAtMapsRowsToBlocks(t *testing.T) {
	tr := newTranscript(newTheme(), newMDRenderer())
	tr.addUser("hello")
	tr.beginAssistant()
	tr.appendAssistantDelta("see https://example.com now")
	tr.endAssistant()

	const width = 80
	rendered := tr.render(width)
	lines := strings.Split(rendered, "\n")
	urlRow := -1
	for i, line := range lines {
		if strings.Contains(ansi.Strip(line), "https://example.com") {
			urlRow = i
			break
		}
	}
	if urlRow < 0 {
		t.Fatalf("assistant URL missing from render:\n%s", rendered)
	}
	b, _, ok := tr.blockLineAt(urlRow, width)
	if !ok || b.kind != blockAssistant {
		t.Fatalf("blockLineAt(%d) kind = %v, ok = %v; want assistant block", urlRow, b, ok)
	}
	if _, _, ok := tr.blockLineAt(len(lines)+5, width); ok {
		t.Fatalf("blockLineAt past end returned ok")
	}
	// The user block occupies the first rows.
	if b, _, ok := tr.blockLineAt(0, width); !ok || b.kind != blockUser {
		t.Fatalf("blockLineAt(0) kind = %v, ok = %v; want user block", b, ok)
	}
}

// findURLCell locates the first rendered cell inside target within the model's
// transcript content space.
func findURLCell(t *testing.T, m *model, target string) (x, y int) {
	t.Helper()
	lines := strings.Split(m.transcript.render(m.width), "\n")
	for row, line := range lines {
		plain := ansi.Strip(line)
		i := strings.Index(plain, target)
		if i < 0 {
			continue
		}
		y = row - m.viewport.YOffset()
		if y < 0 || y >= m.viewport.Height() {
			t.Fatalf("URL row %d outside viewport (offset %d)", row, m.viewport.YOffset())
		}
		return i + 1, y
	}
	t.Fatalf("URL %q missing from transcript", target)
	return 0, 0
}

func stubOpenURL(t *testing.T, stub func(string) *exec.Cmd) {
	t.Helper()
	prev := openURLCmd
	openURLCmd = stub
	t.Cleanup(func() { openURLCmd = prev })
}

func TestCtrlClickOpensAssistantURL(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 24)
	m.transcript.addUser("hello")
	m.transcript.beginAssistant()
	m.transcript.appendAssistantDelta("see https://example.com now")
	m.transcript.endAssistant()
	m.refreshViewport()

	var opened string
	stubOpenURL(t, func(url string) *exec.Cmd { opened = url; return nil })

	x, y := findURLCell(t, m, "https://example.com")
	_, cmd := m.onMouseClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})
	_ = cmd
	if opened != "https://example.com" {
		t.Fatalf("opened = %q, want https://example.com", opened)
	}
	if m.selection != nil {
		t.Fatalf("Ctrl+click started a selection")
	}
	if !strings.Contains(m.statusMsg, "Opened https://example.com") {
		t.Fatalf("status = %q, want opened message", m.statusMsg)
	}
}

func TestCtrlClickIgnoresNonAssistantURL(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 24)
	m.transcript.addUser("visit https://other.example/page today")
	m.transcript.beginAssistant()
	m.transcript.appendAssistantDelta("plain reply without links")
	m.transcript.endAssistant()
	m.refreshViewport()

	var opened string
	stubOpenURL(t, func(url string) *exec.Cmd { opened = url; return nil })

	x, y := findURLCell(t, m, "https://other.example/page")
	_, _ = m.onMouseClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: tea.ModCtrl})
	if opened != "" {
		t.Fatalf("opened = %q, want no open for user-block URL", opened)
	}
	if m.selection != nil {
		t.Fatalf("Ctrl+miss started a selection")
	}
}

func TestPlainClickStillStartsSelection(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 24)
	m.transcript.beginAssistant()
	m.transcript.appendAssistantDelta("see https://example.com now")
	m.transcript.endAssistant()
	m.refreshViewport()

	x, y := findURLCell(t, m, "https://example.com")
	_, _ = m.onMouseClick(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if m.selection == nil {
		t.Fatalf("plain click did not start a selection")
	}
}
