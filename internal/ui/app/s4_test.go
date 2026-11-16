package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/AlvinPlayz23/myagent/internal/ui/kit"
)

// s4Host is a deterministic S4 chrome state: two tabs, a usage rail, todos, a
// footer with a file chip, and enough transcript to scroll.
func s4Host() *fakeHost {
	return &fakeHost{
		blocks: manyBlocks(30),
		composer: "> draft prompt\n" +
			"  enter to send",
		composerH:     2,
		composerEmpty: false,
		tabs: []tui.KitTab{
			{ID: "t1", Title: "myagent"},
			{ID: "t2", Title: "fix-tabs", Busy: true, Unread: true},
		},
		activeTab: "t2",
		barTitle:  "Usage",
		barRows: []tui.KitStat{
			{Label: "model", Value: "muse"},
			{Label: "tokens", Value: "↑1.2k ↓3.4k"},
			{Label: "context", Value: "82%", Alert: true},
		},
		todos: []tui.KitTodo{
			{Text: "wire chrome", Status: tui.KitTodoDone},
			{Text: "write tests", Status: tui.KitTodoActive},
			{Text: "ship S4", Status: tui.KitTodoPending},
		},
		footer: tui.KitFooter{Cwd: "~/src/myagent", File: "shell.go"},
	}
}

func TestShellS4Geometry(t *testing.T) {
	t.Run("120", func(t *testing.T) {
		s := newShell(s4Host())
		s.Resize(120, 40)
		s.Render(120, 40)

		if s.tabsH != 1 || s.sidebarW != 28 || s.mainW != 92 {
			t.Fatalf("rails = tabs %d, sidebar %d, main %d; want 1/28/92", s.tabsH, s.sidebarW, s.mainW)
		}
		if s.contentW != 87 {
			t.Fatalf("content width = %d, want 87 (scrollbar reserved)", s.contentW)
		}
		if s.todoH != 3 || s.footerH != 1 || s.jumpH != 0 || s.composerH != 2 {
			t.Fatalf("heights = todo %d, footer %d, jump %d, composer %d; want 3/1/0/2",
				s.todoH, s.footerH, s.jumpH, s.composerH)
		}
		if s.transcriptH != 33 {
			t.Fatalf("transcript height = %d, want 33", s.transcriptH)
		}
		if got, want := s.scroll.Bounds(), (kit.Rect{X: 2, Y: 1, W: 88, H: 33}); got != want {
			t.Fatalf("scroll rect = %+v, want %+v", got, want)
		}
		if got, want := s.todoR, (kit.Rect{X: 2, Y: 34, W: 88, H: 3}); got != want {
			t.Fatalf("todo rect = %+v, want %+v", got, want)
		}
		if got, want := s.composerR, (kit.Rect{X: 2, Y: 37, W: 88, H: 2}); got != want {
			t.Fatalf("composer rect = %+v, want %+v", got, want)
		}
		if got, want := s.footerR, (kit.Rect{X: 2, Y: 39, W: 88, H: 1}); got != want {
			t.Fatalf("footer rect = %+v, want %+v", got, want)
		}
		if got, want := s.sideR, (kit.Rect{X: 92, Y: 1, W: 28, H: 39}); got != want {
			t.Fatalf("sidebar rect = %+v, want %+v", got, want)
		}
	})

	t.Run("80", func(t *testing.T) {
		s := newShell(s4Host())
		s.Resize(80, 24)
		s.Render(80, 24)

		if s.sidebarW != 0 || s.mainW != 80 || s.contentW != 75 {
			t.Fatalf("widths = sidebar %d, main %d, content %d; want 0/80/75",
				s.sidebarW, s.mainW, s.contentW)
		}
		if s.tabsH != 1 || s.todoH != 3 || s.footerH != 1 || s.transcriptH != 17 {
			t.Fatalf("heights = tabs %d, todo %d, footer %d, transcript %d; want 1/3/1/17",
				s.tabsH, s.todoH, s.footerH, s.transcriptH)
		}
		if got, want := s.scroll.Bounds(), (kit.Rect{X: 2, Y: 1, W: 76, H: 17}); got != want {
			t.Fatalf("scroll rect = %+v, want %+v", got, want)
		}
		if got, want := s.todoR, (kit.Rect{X: 2, Y: 18, W: 76, H: 3}); got != want {
			t.Fatalf("todo rect = %+v, want %+v", got, want)
		}
		if got, want := s.footerR, (kit.Rect{X: 2, Y: 23, W: 76, H: 1}); got != want {
			t.Fatalf("footer rect = %+v, want %+v", got, want)
		}
	})
}

// TestShellS3ChromeStaysInert pins the strangler rule: an empty host projection
// paints no S4 rails, even at the sidebar width threshold.
func TestShellS3ChromeStaysInert(t *testing.T) {
	host := &fakeHost{
		blocks:    manyBlocks(4),
		composerH: 2,
	}
	s := newShell(host)
	s.Resize(120, 40)
	s.Render(120, 40)
	if s.tabsH != 0 || s.sidebarW != 0 || s.todoH != 0 || s.footerH != 0 || s.jumpH != 0 {
		t.Fatalf("idle chrome = %d/%d/%d/%d/%d, want all zero",
			s.tabsH, s.sidebarW, s.todoH, s.footerH, s.jumpH)
	}
	if s.contentW != 115 {
		t.Fatalf("content width = %d, want 115", s.contentW)
	}
}

func TestShellFooterSpinnerAdvances(t *testing.T) {
	host := s4Host()
	host.footer = tui.KitFooter{Busy: true, Cwd: "~/src/myagent"}
	s := newShell(host)
	s.Resize(80, 24)
	s.Render(80, 24)
	if s.footerH != 1 {
		t.Fatalf("footer height = %d, want 1", s.footerH)
	}
	if got := s.ComposeCount(); got != 1 {
		t.Fatalf("initial compose count = %d, want 1", got)
	}

	s.lastTick = time.Now().Add(-time.Second)
	s.Sync()
	s.Render(80, 24)
	if got := s.ComposeCount(); got != 2 {
		t.Fatalf("spinner tick compose count = %d, want 2", got)
	}
}

func TestShellJumpResumesFollow(t *testing.T) {
	host := &fakeHost{
		blocks:        manyBlocks(40),
		composerH:     2,
		composerEmpty: true,
	}
	s := newTestShell(host)
	s.Render(80, 24)
	if s.jump.Active() {
		t.Fatal("jump row visible while tail-following")
	}

	if !s.Key(tea.KeyPressMsg{Code: tea.KeyPgUp}) {
		t.Fatal("pgup not consumed")
	}
	s.Sync()
	s.Render(80, 24)
	if !s.jump.Active() || s.jumpH != 1 {
		t.Fatalf("after pgup jump active=%v height=%d", s.jump.Active(), s.jumpH)
	}
	if s.transcriptH != 21 {
		t.Fatalf("transcript height with jump = %d, want 21", s.transcriptH)
	}
	if got, want := s.jumpR, (kit.Rect{X: 2, Y: 21, W: 76, H: 1}); got != want {
		t.Fatalf("jump rect = %+v, want %+v", got, want)
	}

	// Clicking the status row resumes sticky follow.
	if !s.Mouse(tea.MouseClickMsg{X: 4, Y: s.jumpR.Y, Button: tea.MouseLeft}) {
		t.Fatal("jump click not consumed")
	}
	s.Sync()
	s.Render(80, 24)
	if s.jump.Active() || !s.scroll.AtBottom() {
		t.Fatal("jump click did not resume tail follow")
	}

	// Enter resumes only while the composer is empty.
	s.Key(tea.KeyPressMsg{Code: tea.KeyPgUp})
	s.Sync()
	s.Render(80, 24)
	if !s.jump.Active() {
		t.Fatal("jump row did not return after pgup")
	}
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyEnter}) {
		t.Fatal("Enter on the jump row not consumed")
	}
	s.Sync()
	s.Render(80, 24)
	if s.jump.Active() || !s.scroll.AtBottom() {
		t.Fatal("Enter did not resume tail follow with an empty composer")
	}
	if len(host.keys) != 0 {
		t.Fatalf("jump Enter leaked to the composer: %v", host.keys)
	}

	// A non-empty composer keeps Enter for the legacy onKey path.
	host.composerEmpty = false
	s.Key(tea.KeyPressMsg{Code: tea.KeyPgUp})
	s.Sync()
	s.Render(80, 24)
	if !s.jump.Active() {
		t.Fatal("jump row did not return after pgup")
	}
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyEnter}) {
		t.Fatal("composer Enter not consumed")
	}
	if len(host.keys) != 1 || host.keys[0] != "enter" {
		t.Fatalf("composer keys = %v, want [enter]", host.keys)
	}
	if !s.jump.Active() || s.scroll.AtBottom() {
		t.Fatal("composer Enter resumed follow")
	}
}

func TestShellNoticeToast(t *testing.T) {
	host := s4Host()
	host.notice = tui.KitNotice{Text: "Saved settings.", Severity: tui.KitNoticeSuccess}
	s := newShell(host)
	s.Resize(80, 24)
	s.Sync()

	toast, ok := s.toasts.Current()
	if !ok {
		t.Fatal("success notice did not reach the toast host")
	}
	if toast.Variant != kit.ToastSuccess || toast.Message != "Saved settings." {
		t.Fatalf("toast = %+v, want success/Saved settings.", toast)
	}
	frame := ansi.Strip(s.Render(80, 24))
	if !strings.Contains(frame, "Saved settings.") {
		t.Fatal("toast text missing from the frame")
	}

	// The same status line does not re-toast every sync.
	before := s.toasts.Len()
	s.Sync()
	if got := s.toasts.Len(); got != before {
		t.Fatalf("duplicate notice queued %d toasts, want %d", got-before, 0)
	}

	// Clearing the status arms the emitter for the identical message.
	host.notice = tui.KitNotice{}
	s.Sync()
	host.notice = tui.KitNotice{Text: "Saved settings.", Severity: tui.KitNoticeSuccess}
	s.Sync()
	if got := s.toasts.Len(); got != before+1 {
		t.Fatalf("repeated status queued %d toasts, want 1", got-before)
	}
}

func TestShellNoticeSuppressedWhilePickerOpen(t *testing.T) {
	host := &fakeHost{
		composerH: 2,
		picker: tui.KitPickerState{
			Kind:  "models",
			Title: "Select a model",
			Items: []tui.KitPickerItem{{ID: "p/a", Title: "a"}},
		},
		pickerActive: true,
		notice:       tui.KitNotice{Text: "Type to filter.", Severity: tui.KitNoticeInfo},
	}
	s := newShell(host)
	s.Resize(80, 24)
	s.Sync()
	if !s.pickerOpen {
		t.Fatal("picker did not open")
	}
	if s.toasts.Active() || s.toasts.Len() != 0 {
		t.Fatal("info notice surfaced while the picker dialog is open")
	}

	// Errors still surface over a dialog.
	host.notice = tui.KitNotice{Text: "Could not load models.", Severity: tui.KitNoticeError}
	s.Sync()
	toast, ok := s.toasts.Current()
	if !ok || toast.Variant != kit.ToastError {
		t.Fatalf("error toast = %+v, %v; want error variant", toast, ok)
	}
}

func pickerHost() *fakeHost {
	return &fakeHost{
		composerH: 2,
		picker: tui.KitPickerState{
			Kind:        "models",
			Title:       "Select a model",
			Placeholder: "search models",
			SelectedID:  "p/b",
			Items: []tui.KitPickerItem{
				{ID: "p/a", Title: "alpha", Desc: "p/a", Group: "P", Footer: "8k"},
				{ID: "p/b", Title: "beta", Desc: "p/b", Group: "P", Footer: "16k"},
				{ID: "q/c", Title: "gamma", Desc: "q/c", Group: "Q"},
			},
		},
		pickerActive: true,
	}
}

func TestShellPickerOpensSelected(t *testing.T) {
	host := pickerHost()
	s := newTestShell(host)
	s.Sync()
	s.Render(80, 24)

	if !s.pickerOpen || !s.dialogs.Modal() {
		t.Fatal("active host picker did not open a modal dialog")
	}
	if s.dialogs.Len() != 1 || s.pickerKind != "models" {
		t.Fatalf("stack len/kind = %d/%q, want 1/models", s.dialogs.Len(), s.pickerKind)
	}
	if got := s.picker.SelectedID(); got != "p/b" {
		t.Fatalf("selected row = %q, want p/b", got)
	}
	frame := ansi.Strip(s.Render(80, 24))
	if !strings.Contains(frame, "Select a model") || !strings.Contains(frame, "beta") {
		t.Fatal("dialog frame missing title/selected row")
	}
}

func TestShellPickerOwnsQueryAfterOpen(t *testing.T) {
	host := pickerHost()
	s := newTestShell(host)
	s.Sync()
	s.Render(80, 24)

	if !s.Key(keyRune('b')) {
		t.Fatal("typing into the picker not consumed")
	}
	if got := s.picker.Query(); got != "b" {
		t.Fatalf("dialog query = %q, want b", got)
	}

	// A host sync with no change must not clobber the dialog's own query.
	s.Sync()
	if got := s.picker.Query(); got != "b" {
		t.Fatalf("query after sync = %q, want b", got)
	}
}

func TestShellPickerRefreshAndReplace(t *testing.T) {
	host := pickerHost()
	s := newTestShell(host)
	s.Sync()
	s.Render(80, 24)

	// A host refresh re-feeds rows and the owner selection.
	host.picker.Items = append(host.picker.Items, tui.KitPickerItem{ID: "q/d", Title: "delta", Group: "Q"})
	host.picker.SelectedID = "q/d"
	s.Sync()
	if got := s.picker.SelectedID(); got != "q/d" {
		t.Fatalf("refreshed selection = %q, want q/d", got)
	}

	// A different picker kind replaces the dialog instead of stacking.
	host.picker = tui.KitPickerState{
		Kind:  "commands",
		Title: "Commands",
		Items: []tui.KitPickerItem{{ID: "/help", Title: "/help", Group: "Suggested"}},
	}
	s.Sync()
	if s.dialogs.Len() != 1 || s.pickerKind != "commands" {
		t.Fatalf("replaced stack len/kind = %d/%q, want 1/commands", s.dialogs.Len(), s.pickerKind)
	}
	if got := s.host.(*fakeHost).cancels; got != 0 {
		t.Fatalf("kind replacement cancelled the legacy picker %d times", got)
	}
}

func TestShellCommandPickerUsesPaletteChrome(t *testing.T) {
	host := pickerHost()
	host.picker.Kind = "commands"
	host.picker.Title = "Commands"
	host.picker.Placeholder = "search commands"
	host.picker.Items = []tui.KitPickerItem{{ID: "/help", Title: "Help", Footer: "F1"}}
	s := newTestShell(host)
	s.Sync()

	frame := ansi.Strip(s.Render(80, 24))
	for _, want := range []string{"Commands", "esc", "search commands", "Help", "F1"} {
		if !strings.Contains(frame, want) {
			t.Errorf("command palette missing %q:\n%s", want, frame)
		}
	}
	for _, absent := range []string{"cancel", "confirm"} {
		if strings.Contains(frame, absent) {
			t.Errorf("command palette has redundant %q footer control:\n%s", absent, frame)
		}
	}
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyEnter}) || host.accepted != "/help" {
		t.Fatalf("Enter without a footer accepted %q, want /help", host.accepted)
	}

	cancelHost := pickerHost()
	cancelHost.picker.Kind = "commands"
	cancelShell := newTestShell(cancelHost)
	cancelShell.Sync()
	if !cancelShell.Key(tea.KeyPressMsg{Code: tea.KeyEsc}) || cancelHost.cancels != 1 {
		t.Fatalf("Esc without a footer canceled %d times, want 1", cancelHost.cancels)
	}
}

func TestShellPickerAcceptAndCancel(t *testing.T) {
	t.Run("accept", func(t *testing.T) {
		host := pickerHost()
		s := newTestShell(host)
		s.Sync()
		s.Render(80, 24)

		if !s.Key(tea.KeyPressMsg{Code: tea.KeyEnter}) {
			t.Fatal("Enter in the picker not consumed")
		}
		if host.accepted != "p/b" {
			t.Fatalf("accepted = %q, want p/b", host.accepted)
		}
		s.Sync()
		if s.pickerOpen || s.dialogs.Modal() {
			t.Fatal("dialog stayed open after accept")
		}
		if host.cancels != 0 {
			t.Fatalf("accept ran the cancel path %d times", host.cancels)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		host := pickerHost()
		s := newTestShell(host)
		s.Sync()
		s.Render(80, 24)

		if !s.Key(tea.KeyPressMsg{Code: tea.KeyEsc}) {
			t.Fatal("Esc in the picker not consumed")
		}
		if host.cancels != 1 {
			t.Fatalf("cancels = %d, want 1", host.cancels)
		}
		if s.pickerOpen {
			t.Fatal("picker bridge still open after Esc")
		}
	})
}

func TestShellToolBlockHover(t *testing.T) {
	host := &fakeHost{
		blocks: []*fakeBlock{
			textBlock("assistant › ready"),
			{lines: []string{"tool › read GOTUI.md", "section 3.0.D"}, kind: "tool"},
			textBlock("assistant › done"),
		},
		composerH: 2,
	}
	s := newTestShell(host)
	s.Render(80, 24)

	if _, ok := s.scroll.Items[0].W.(*kit.Hoverable); ok {
		t.Fatal("assistant block was wrapped as a tool card")
	}
	wrapped, ok := s.scroll.Items[2].W.(*kit.Hoverable)
	if !ok {
		t.Fatalf("tool block widget = %T, want *kit.Hoverable", s.scroll.Items[2].W)
	}
	bounds := wrapped.Bounds()
	if !s.Mouse(tea.MouseMotionMsg{X: bounds.X + 2, Y: bounds.Y}) {
		t.Fatal("motion over the tool block not consumed")
	}
	if !wrapped.Hovered() {
		t.Fatal("tool block did not enter hover")
	}

	// Moving onto the composer clears the hover even though Root does not route
	// motion to the wrapper once the pointer leaves its bounds.
	s.Mouse(tea.MouseMotionMsg{X: s.composerR.X + 1, Y: s.composerR.Y})
	if wrapped.Hovered() {
		t.Fatal("tool block stayed hovered after the pointer left")
	}
}

// TestShellTabClickSelectsSession pins the Tabs rail's host wiring: a click on
// a tab id calls KitSelectTab and survives the next projection sync.
func TestShellTabClickSelectsSession(t *testing.T) {
	host := s4Host()
	s := newTestShell(host)
	s.Render(80, 24)

	if !s.Mouse(tea.MouseClickMsg{X: 4, Y: 0, Button: tea.MouseLeft}) {
		t.Fatal("tab click not consumed")
	}
	if host.tabSelectCalls != 1 || host.selectedTab != "t1" {
		t.Fatalf("tab select = %d/%q, want 1/t1", host.tabSelectCalls, host.selectedTab)
	}
	if got := s.tabs.ActiveID(); got != "t1" {
		t.Fatalf("rail active = %q, want t1", got)
	}

	s.Sync()
	s.Render(80, 24)
	if got := s.tabs.ActiveID(); got != "t1" {
		t.Fatalf("rail active after sync = %q, want t1", got)
	}
}

func TestShellS4ChromeGoldens(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"s4_chrome_80x24", 80, 24},
		{"s4_chrome_120x40", 120, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newShell(s4Host())
			s.Resize(tc.w, tc.h)
			s.Render(tc.w, tc.h)
			if !s.Key(tea.KeyPressMsg{Code: tea.KeyPgUp}) {
				t.Fatal("pgup not consumed")
			}
			s.Sync()
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
