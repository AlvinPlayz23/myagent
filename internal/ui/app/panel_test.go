package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/AlvinPlayz23/myagent/internal/ui/kit"
)

// panelTestHost is a mutable panel projection. It overrides the fakeHost
// picker methods so accept/cancel can drive state transitions in tests.
type panelTestHost struct {
	*fakeHost

	state    tui.KitPickerState
	active   bool
	onAccept func(id string)
	onCancel func()
}

func (h *panelTestHost) KitPicker() (tui.KitPickerState, bool) {
	return h.state, h.active
}

func (h *panelTestHost) KitPickerAccept(id string) tea.Cmd {
	h.accepted = id
	if h.onAccept != nil {
		h.onAccept(id)
	}
	return nil
}

func (h *panelTestHost) KitPickerCancel() tea.Cmd {
	h.cancels++
	if h.onCancel != nil {
		h.onCancel()
	}
	h.active = false
	return nil
}

func panelState() tui.KitPickerState {
	return tui.KitPickerState{
		Kind:       "effort",
		Mode:       tui.KitPickerList,
		Title:      "Reasoning effort",
		Hint:       "↑/↓ select, enter saves as myagent default, esc cancel",
		SelectedID: "1",
		Items: []tui.KitPickerItem{
			{ID: "0", Title: "Low", Desc: "fast, lightweight reasoning"},
			{ID: "1", Title: "Medium", Desc: "balanced reasoning  (current)"},
			{ID: "2", Title: "High", Desc: "deeper reasoning"},
			{ID: "3", Title: "Max", Desc: "maximum reasoning"},
		},
		ConfirmLabel: "save",
	}
}

func newPanelHost(state tui.KitPickerState) *panelTestHost {
	return &panelTestHost{
		fakeHost: &fakeHost{
			blocks:    manyBlocks(4),
			composer:  "> draft prompt\n  enter to send",
			composerH: 2,
		},
		state:  state,
		active: true,
	}
}

func TestShellPanelListSelectionAndKeys(t *testing.T) {
	host := newPanelHost(panelState())
	host.onKey = func(ev tea.KeyPressMsg) tea.Cmd {
		if ev.String() == "down" {
			host.state.SelectedID = "2"
		}
		if ev.String() == "enter" {
			host.active = false
		}
		return nil
	}
	s := newShell(host)
	s.Resize(80, 24)
	s.Render(80, 24)

	if !s.panelOpen || s.dialogs.Len() != 1 {
		t.Fatalf("panel open = %v, stack len = %d; want true/1", s.panelOpen, s.dialogs.Len())
	}
	if s.panelList == nil || s.panelList.selectedID() != "1" {
		t.Fatalf("panel list selection = %q, want 1", s.panelList.selectedID())
	}

	if !s.Key(tea.KeyPressMsg{Code: tea.KeyDown}) {
		t.Fatal("down not consumed by the panel body")
	}
	if got := host.keys; len(got) != 1 || got[0] != "down" {
		t.Fatalf("forwarded keys = %v, want [down]", got)
	}
	s.Sync()
	if got := s.panelList.selectedID(); got != "2" {
		t.Fatalf("dialog selection after host move = %q, want 2", got)
	}

	if !s.Key(tea.KeyPressMsg{Code: tea.KeyEnter}) {
		t.Fatal("enter not consumed by the panel body")
	}
	s.Sync()
	if s.panelOpen || s.dialogs.Modal() {
		t.Fatal("panel stayed open after the host accepted")
	}
	if host.cancels != 0 {
		t.Fatalf("accept ran the cancel path %d times", host.cancels)
	}
}

func TestShellPanelClickSelectsThenActivates(t *testing.T) {
	host := newPanelHost(panelState())
	s := newShell(host)
	s.Resize(80, 24)
	s.Render(80, 24)

	bounds := s.panelList.Bounds()
	x := bounds.X + 4
	y := bounds.Y + 1 // first row sits below the hint row
	if !s.Mouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}) {
		t.Fatal("panel row click not consumed")
	}
	if host.accepted != "0" {
		t.Fatalf("first click accepted %q, want 0 (select)", host.accepted)
	}
	host.state.SelectedID = "0"
	s.Sync()

	host.onAccept = func(string) { host.active = false }
	if !s.Mouse(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}) {
		t.Fatal("second row click not consumed")
	}
	s.Sync()
	if s.panelOpen || host.cancels != 0 {
		t.Fatalf("second click did not activate cleanly (open=%v cancels=%d)", s.panelOpen, host.cancels)
	}
}

func TestShellPanelWheelMovesSelection(t *testing.T) {
	host := newPanelHost(panelState())
	host.onAccept = func(id string) { host.state.SelectedID = id }
	s := newShell(host)
	s.Resize(80, 24)
	s.Render(80, 24)

	bounds := s.panelList.Bounds()
	x := bounds.X + 4
	y := bounds.Y + 1 // first row sits below the hint row
	if !s.Mouse(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown}) {
		t.Fatal("panel wheel not consumed")
	}
	if host.accepted != "2" {
		t.Fatalf("wheel accepted %q, want 2", host.accepted)
	}
	s.Sync()
	if got := s.panelList.selectedID(); got != "2" {
		t.Fatalf("dialog selection after wheel = %q, want 2", got)
	}
}

func TestShellPanelBackdropCancel(t *testing.T) {
	host := newPanelHost(panelState())
	s := newShell(host)
	s.Resize(80, 24)
	s.Render(80, 24)

	if !s.Mouse(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft}) {
		t.Fatal("backdrop click not consumed")
	}
	if host.cancels != 1 {
		t.Fatalf("cancels = %d, want 1", host.cancels)
	}
	if s.panelOpen {
		t.Fatal("panel still open after backdrop cancel")
	}
}

func TestShellPanelInputForwardsKeysAndHidesText(t *testing.T) {
	host := newPanelHost(tui.KitPickerState{
		Kind:         "provider-key",
		Mode:         tui.KitPickerInput,
		Title:        "Configure Example",
		Input:        "••••",
		Masked:       true,
		Hint:         "enter saves, esc cancels",
		ConfirmLabel: "save",
	})
	s := newShell(host)
	s.Resize(80, 24)
	s.Render(80, 24)

	if s.panelInput == nil {
		t.Fatal("input panel body not built")
	}
	if got := s.panelFooter.Buttons[1].Label; got != "save" {
		t.Fatalf("confirm label = %q, want save", got)
	}
	frame := ansi.Strip(s.Render(80, 24))
	if !strings.Contains(frame, "••••") {
		t.Fatal("masked input missing from the frame")
	}
	if strings.Contains(frame, "secret") {
		t.Fatal("masked input leaked cleartext")
	}

	if !s.Key(keyRune('x')) {
		t.Fatal("input key not consumed")
	}
	if got := host.keys; len(got) != 1 || got[0] != "x" {
		t.Fatalf("forwarded keys = %v, want [x]", got)
	}

	// Esc belongs to the legacy input, not the stack.
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyEsc}) {
		t.Fatal("esc not consumed by the input body")
	}
	if got := host.keys; len(got) != 2 || got[1] != "esc" {
		t.Fatalf("forwarded keys = %v, want [x esc]", got)
	}
	if !s.panelOpen {
		t.Fatal("panel closed without the host handling esc")
	}
}

func TestShellPanelConfirmUsesDialogActions(t *testing.T) {
	confirmState := func() tui.KitPickerState {
		return tui.KitPickerState{
			Kind:         "export-overwrite",
			Mode:         tui.KitPickerConfirm,
			Title:        "Export",
			Body:         "File exists — enter overwrite, ↑/↓ return to name, esc cancel",
			Hint:         "↑/↓ return to name",
			ConfirmLabel: "overwrite",
			CancelLabel:  "cancel",
		}
	}

	t.Run("confirm closes", func(t *testing.T) {
		host := newPanelHost(confirmState())
		host.onAccept = func(string) { host.active = false }
		s := newShell(host)
		s.Resize(80, 24)
		s.Render(80, 24)

		if s.panelLayout == nil || s.panelLayout.OnConfirm == nil {
			t.Fatal("confirm dialog not built through kit.NewConfirm")
		}
		if !s.Key(tea.KeyPressMsg{Code: tea.KeyEnter}) {
			t.Fatal("enter not consumed by the confirm dialog")
		}
		s.Sync()
		if host.accepted != "export-overwrite" || s.panelOpen {
			t.Fatalf("accept = %q open = %v, want export-overwrite/false", host.accepted, s.panelOpen)
		}
		if host.cancels != 0 {
			t.Fatalf("confirm ran the cancel path %d times", host.cancels)
		}
	})

	t.Run("failure keeps dialog open", func(t *testing.T) {
		host := newPanelHost(confirmState())
		s := newShell(host)
		s.Resize(80, 24)
		s.Render(80, 24)

		if !s.Key(tea.KeyPressMsg{Code: tea.KeyEnter}) {
			t.Fatal("enter not consumed by the confirm dialog")
		}
		if !s.panelOpen || s.dialogs.Len() != 1 {
			t.Fatalf("failed overwrite closed the dialog (open=%v len=%d)", s.panelOpen, s.dialogs.Len())
		}
		host.active = false
		s.Sync()
		if s.panelOpen {
			t.Fatal("panel stayed open after the host cleared the failure")
		}
	})

	t.Run("up returns to name", func(t *testing.T) {
		host := newPanelHost(confirmState())
		s := newShell(host)
		s.Resize(80, 24)
		s.Render(80, 24)

		if !s.Key(tea.KeyPressMsg{Code: tea.KeyUp}) {
			t.Fatal("up not consumed by the confirm body")
		}
		if got := host.keys; len(got) != 1 || got[0] != "up" {
			t.Fatalf("forwarded keys = %v, want [up]", got)
		}
		host.state = tui.KitPickerState{
			Kind:         "export-name",
			Mode:         tui.KitPickerInput,
			Title:        "Export as Markdown",
			Input:        "session.md",
			Hint:         "enter export, esc cancel",
			ConfirmLabel: "export",
		}
		s.Sync()
		if s.panelMode != tui.KitPickerInput || s.panelInput == nil {
			t.Fatalf("kind transition = mode %v input %v, want input body", s.panelMode, s.panelInput != nil)
		}
		if host.cancels != 0 {
			t.Fatalf("kind replacement cancelled the confirm %d times", host.cancels)
		}
	})

	t.Run("esc cancels", func(t *testing.T) {
		host := newPanelHost(confirmState())
		s := newShell(host)
		s.Resize(80, 24)
		s.Render(80, 24)

		if !s.Key(tea.KeyPressMsg{Code: tea.KeyEsc}) {
			t.Fatal("esc not consumed by the dialog stack")
		}
		if host.cancels != 1 || s.panelOpen {
			t.Fatalf("esc = cancels %d open %v, want 1/false", host.cancels, s.panelOpen)
		}
	})
}

// dockTestHost adds the optional KitDockHost projection to fakeHost.
type dockTestHost struct {
	*fakeHost
	dock tui.KitDock
}

func (h *dockTestHost) KitDock() tui.KitDock { return h.dock }

func TestShellDockRowsAboveComposer(t *testing.T) {
	host := &dockTestHost{
		fakeHost: &fakeHost{
			blocks:    manyBlocks(4),
			composer:  "> ready",
			composerH: 2,
		},
		dock: tui.KitDock{Rows: []tui.KitDockRow{
			{Label: "next", Text: "run the tests after this"},
			{Label: "[image]", Text: "1 attached (12 B)"},
		}},
	}
	s := newShell(host)
	s.Resize(80, 24)
	s.Render(80, 24)

	if s.dockH != 2 {
		t.Fatalf("dock height = %d, want 2", s.dockH)
	}
	if got, want := s.dockR, (kit.Rect{X: 0, Y: 20, W: 80, H: 2}); got != want {
		t.Fatalf("dock rect = %+v, want %+v", got, want)
	}
	frame := ansi.Strip(s.Render(80, 24))
	if !strings.Contains(frame, "run the tests after this") || !strings.Contains(frame, "1 attached") {
		t.Fatal("dock rows missing from the frame")
	}

	host.dock = tui.KitDock{}
	s.Sync()
	s.Render(80, 24)
	if s.dockH != 0 || s.dockR.H != 0 {
		t.Fatalf("cleared dock height/rect = %d/%+v, want 0/zero", s.dockH, s.dockR)
	}
}

func TestShellS6PanelGoldens(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"s6_panel_80x24", 80, 24},
		{"s6_panel_120x40", 120, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := newPanelHost(panelState())
			s := newShell(host)
			s.Resize(tc.w, tc.h)
			raw := s.Render(tc.w, tc.h)
			assertFrame(t, raw, tc.w, tc.h)
			plain := trimLines(ansi.Strip(raw))

			path := filepath.Join("testdata", tc.name+".txt")
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
				t.Fatalf("read golden: %v (run UPDATE_GOLDENS=1 to create)", err)
			}
			wantStr := strings.TrimSuffix(strings.TrimSuffix(string(want), "\n"), "\r")
			if plain != wantStr {
				t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, plain, wantStr)
			}
		})
	}
}
