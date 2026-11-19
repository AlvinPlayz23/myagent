package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
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

func TestCommandMenuWheelKeepsHostSelectionInSync(t *testing.T) {
	host := pickerHost()
	host.picker = tui.KitPickerState{
		Kind: "commands", Inline: true, SelectedID: "/one",
		Items: []tui.KitPickerItem{{ID: "/one", Title: "/one"}, {ID: "/two", Title: "/two"}},
	}
	host.onKey = func(key tea.KeyPressMsg) tea.Cmd {
		switch key.Code {
		case tea.KeyDown:
			host.picker.SelectedID = "/two"
		case tea.KeyUp:
			host.picker.SelectedID = "/one"
		}
		return nil
	}
	s := newTestShell(host)
	s.Sync()
	s.Render(80, 24)
	r := s.menuR
	if !s.Mouse(tea.MouseWheelMsg{X: r.X + 1, Y: r.Y, Button: tea.MouseWheelDown}) {
		t.Fatal("wheel over menu not consumed")
	}
	s.Sync()
	row, ok := s.menu.list.SelectedRow()
	if !ok || row.ID != "/two" || host.picker.SelectedID != "/two" {
		t.Fatalf("selected row = %#v, host = %q", row, host.picker.SelectedID)
	}
	for i := 0; i < 2; i++ {
		if !s.Mouse(tea.MouseWheelMsg{X: r.X + 1, Y: r.Y, Button: tea.MouseWheelUp}) {
			t.Fatal("wheel up over menu not consumed")
		}
		s.Sync()
		row, ok = s.menu.list.SelectedRow()
		if !ok || row.ID != "/one" || host.picker.SelectedID != "/one" {
			t.Fatalf("selected row = %#v, host = %q after wheel up %d", row, host.picker.SelectedID, i+1)
		}
	}
	if got := strings.Join(host.keys, ","); got != "down,up,up" {
		t.Fatalf("host keys = %q, want down,up,up", got)
	}
}
