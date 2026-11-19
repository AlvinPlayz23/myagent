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

func TestInlineEffortUsesComposerMenuAndRoutesKeys(t *testing.T) {
	state := panelState()
	state.Inline = true
	host := newPanelHost(state)
	s := newShell(host)
	s.Resize(80, 24)
	s.Sync()
	s.Render(80, 24)
	if !s.menuOpen || s.menuKind != "effort" || s.dialogs.Modal() || s.panelOpen {
		t.Fatal("effort should open in the inline menu, not a dialog")
	}
	if s.menuR.Y+s.menuR.H != s.composerR.Y {
		t.Fatalf("effort menu %#v is not attached to composer %#v", s.menuR, s.composerR)
	}
	for _, k := range []tea.KeyPressMsg{{Code: tea.KeyDown}, {Code: tea.KeyEnter}, {Code: tea.KeyEsc}} {
		if !s.Key(k) {
			t.Fatalf("key %s not consumed", k.String())
		}
	}
	if got := strings.Join(host.keys, ","); got != "down,enter,esc" {
		t.Fatalf("host keys = %q", got)
	}
}

func TestPaletteEffortReusesCommandDialog(t *testing.T) {
	host := newPanelHost(tui.KitPickerState{
		Kind: "commands", Mode: tui.KitPickerSelect, Title: "Commands",
		Items: []tui.KitPickerItem{{ID: "/effort", Title: "Reasoning effort"}},
	})
	s := newShell(host)
	s.Resize(80, 24)
	s.Render(80, 24)
	palette := s.pickerLayout
	host.state = panelState()
	s.Sync()
	s.Render(80, 24)
	if !s.panelOpen || s.panelLayout != palette || s.dialogs.Top() != palette || s.dialogs.Len() != 1 {
		t.Fatal("effort must replace the content of the existing command palette")
	}
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyDown}) || len(host.keys) != 1 || host.keys[0] != "down" {
		t.Fatalf("effort keys did not reach the host: %v", host.keys)
	}
}

func TestCustomEffortSelectorFitsAndRespondsToClicks(t *testing.T) {
	state := panelState()
	state.EffortStyle = "custom"
	state.Inline = true
	host := newPanelHost(state)
	host.welcome = true
	host.welcomeText = "Welcome"
	host.blocks = nil
	s := newShell(host)
	s.Resize(80, 24)
	s.Render(80, 24)
	if !s.effort.Visible || s.menu.Visible || s.menuH != 3 {
		t.Fatal("default effort style should show the compact custom selector")
	}
	out := ansi.Strip(s.effort.Render(s.menuR.W, s.menuR.H))
	for _, want := range []string{"Low", "Medium", "High", "←/→ change", "enter save"} {
		if !strings.Contains(out, want) {
			t.Fatalf("selector missing %q: %q", want, out)
		}
	}
	target := s.effort.hits[2].rect
	if !s.Mouse(tea.MouseClickMsg{X: target.X, Y: target.Y, Button: tea.MouseLeft}) || host.accepted != "2" {
		t.Fatalf("click selected %q, want high", host.accepted)
	}
}

func TestEffortListStyleAndNarrowCustomSelector(t *testing.T) {
	state := panelState()
	state.Inline = true
	state.EffortStyle = "list"
	host := newPanelHost(state)
	s := newShell(host)
	s.Resize(80, 24)
	s.Render(80, 24)
	if !s.menu.Visible || s.effort.Visible || s.menuH != len(state.Items) {
		t.Fatal("list style should retain the vertical list")
	}
	host.state.EffortStyle = "custom"
	s.Sync()
	s.Resize(20, 12)
	s.Render(20, 12)
	if !s.effort.Visible || len(s.effort.hits) == 0 {
		t.Fatal("narrow custom selector lost its choices")
	}
	for _, hit := range s.effort.hits {
		if hit.rect.Intersect(s.menuR) != hit.rect {
			t.Fatalf("choice hit %#v outside menu %#v", hit.rect, s.menuR)
		}
	}
	host.state.SelectedID = "3"
	s.Sync()
	s.Render(20, 12)
	found := false
	for _, hit := range s.effort.hits {
		found = found || hit.id == "3"
	}
	if !found {
		t.Fatal("selected choice must remain visible while navigating a narrow selector")
	}
}

func TestPaletteEffortRespectsSelectorStyle(t *testing.T) {
	for _, style := range []string{"custom", "list"} {
		t.Run(style, func(t *testing.T) {
			host := newPanelHost(tui.KitPickerState{
				Kind: "commands", Mode: tui.KitPickerSelect, Title: "Commands",
				Items: []tui.KitPickerItem{{ID: "/effort", Title: "Reasoning effort"}},
			})
			s := newShell(host)
			s.Resize(80, 24)
			s.Render(80, 24)
			palette := s.pickerLayout
			host.state = panelState()
			host.state.EffortStyle = style
			s.Sync()
			frame := ansi.Strip(s.Render(80, 24))
			if s.panelLayout != palette || s.dialogs.Top() != palette {
				t.Fatal("effort should stay in the command palette")
			}
			if strings.Contains(frame, "↑↓ navigate") {
				t.Fatal("old command palette content remains visible after switching to effort")
			}
			if style == "custom" {
				if s.panelEffort == nil || s.panelList != nil || !strings.Contains(frame, "←/→ change") {
					t.Fatalf("interactive selector missing: %s", frame)
				}
				if !s.Key(tea.KeyPressMsg{Code: tea.KeyRight}) || len(host.keys) != 1 || host.keys[0] != "right" {
					t.Fatalf("right key not forwarded: %v", host.keys)
				}
				hit := s.panelEffort.hits[2].rect
				if !s.Mouse(tea.MouseClickMsg{X: hit.X, Y: hit.Y, Button: tea.MouseLeft}) || host.accepted != "2" {
					t.Fatalf("click selected %q, want 2", host.accepted)
				}
			} else if s.panelList == nil || s.panelEffort != nil || strings.Contains(frame, "←/→ change") {
				t.Fatalf("list selector missing: %s", frame)
			}
		})
	}
}
