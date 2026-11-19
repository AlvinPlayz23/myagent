package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/ui/kit"
)

func TestCommandMenuUsesEveryRowWhenScrollbarIsVisible(t *testing.T) {
	m := newCommandMenu(nil)
	m.list.SetRows([]kit.ListRow{
		{ID: "one", Title: "/one"},
		{ID: "two", Title: "/two"},
		{ID: "three", Title: "/three"},
	})
	r := kit.Rect{X: 2, Y: 3, W: 20, H: 2}
	m.Layout(r)
	check := func(want string) {
		t.Helper()
		line := strings.Split(ansi.Strip(m.Render(r.W, r.H)), "\n")[1]
		if !strings.Contains(line, want) {
			t.Fatalf("last visible row = %q, want %q", line, want)
		}
	}
	check("/two")
	m.list.Move(2)
	check("/three")

	var chosen string
	m.list.OnChoose = func(row kit.ListRow) bool {
		chosen = row.ID
		return true
	}
	if !m.Mouse(tea.MouseClickMsg{X: r.X + 2, Y: r.Y + 1, Button: tea.MouseLeft}, r) || chosen != "three" {
		t.Fatalf("click on last visible row chose %q, want three", chosen)
	}
}
