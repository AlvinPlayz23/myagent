package app

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/AlvinPlayz23/myagent/internal/ui/kit"
)

func bgHex(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8)
}

// TestCommandMenuUsesComposerSurface pins the slash menu to the composer's
// raised surface for every theme: each non-selected row cell, including the
// scrollbar column, carries Background.Raised.Base. The selected row keeps its
// selection fill instead.
func TestCommandMenuUsesComposerSurface(t *testing.T) {
	host := pickerHost()
	host.picker = tui.KitPickerState{
		Kind:       "commands",
		Inline:     true,
		SelectedID: "/models",
		Items: []tui.KitPickerItem{
			{ID: "/help", Title: "/help", Desc: "Help & shortcuts"},
			{ID: "/models", Title: "/models", Desc: "Switch model"},
		},
	}
	s := newTestShell(host)
	s.Sync()
	raw := s.Render(80, 24)
	if !s.menuOpen || s.menuR.Empty() {
		t.Fatal("command menu did not open")
	}

	cv := lipgloss.NewCanvas(80, 24)
	uv.NewStyledString(raw).Draw(cv, uv.Rect(0, 0, 80, 24))
	th := kit.DefaultTheme()
	wantSurface := bgHex(th.Background.Raised.Base.Color())
	wantSelection := bgHex(th.Background.Selection.Color())
	for y := s.menuR.Y; y < s.menuR.Y+s.menuR.H; y++ {
		selectedRow := y == s.menuR.Y+1
		for x := s.menuR.X; x < s.menuR.X+s.menuR.W; x++ {
			cell := cv.CellAt(x, y)
			if cell == nil {
				t.Fatalf("menu cell %d,%d missing", x, y)
			}
			want := wantSurface
			if selectedRow && x < s.menuR.X+s.menuR.W-1 {
				want = wantSelection
			}
			if got := bgHex(cell.Style.Bg); got != want {
				t.Fatalf("menu cell %d,%d bg = %q, want %q", x, y, got, want)
			}
		}
	}
}

func TestPickerPaletteFrames(t *testing.T) {
	for _, kind := range []string{"models", "commands"} {
		for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 18}} {
			t.Run(fmt.Sprintf("%s_%dx%d", kind, size[0], size[1]), func(t *testing.T) {
				host := pickerHost()
				if kind == "commands" {
					host.picker = tui.KitPickerState{Kind: "commands", Title: "Commands", Placeholder: "Search commands…", Query: "/", Items: []tui.KitPickerItem{
						{ID: "/models", Title: "/models", Desc: "Switch model", Group: "Suggested"},
						{ID: "/new", Title: "/new", Desc: "Start a conversation", Group: "Session"},
						{ID: "/tools", Title: "/tools", Desc: "Choose enabled tools", Group: "Tools & interface"},
					}}
				} else {
					host.picker.Placeholder = "Search models…"
					host.picker.Items[1].Footer = "✓ current · 200k"
				}
				s := newTestShell(host)
				s.Sync()
				raw := s.Render(size[0], size[1])
				assertFrame(t, raw, size[0], size[1])
				if kind == "commands" {
					if !s.pickerOpen || !s.dialogs.Modal() {
						t.Fatal("ctrl+p command palette did not open a modal")
					}
				} else if s.pickerLayout == nil || s.pickerLayout.Footer != nil {
					t.Fatal("palette should not show confirm/cancel buttons")
				}
				plain := trimLines(ansi.Strip(raw))
				if kind == "commands" {
					for _, want := range []string{"Commands", "/models", "Switch model"} {
						if !strings.Contains(plain, want) {
							t.Errorf("command palette missing %q:\n%s", want, plain)
						}
					}
				} else if !strings.Contains(plain, "results") && size[0] >= 80 {
					t.Fatal("missing results count")
				}
				path := filepath.Join("testdata", fmt.Sprintf("picker_%s_%dx%d.txt", kind, size[0], size[1]))
				if os.Getenv("UPDATE_GOLDENS") == "1" {
					if err := os.WriteFile(path, []byte(plain+"\n"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if plain != strings.TrimSuffix(string(want), "\n") {
					t.Fatalf("palette golden mismatch:\n%s", plain)
				}
			})
		}
	}
}

// TestWelcomeCommandMenuOverlaysWithoutMovingComposer pins the home-screen
// slash menu to an overlay: opening it paints over the startup art and leaves
// the centered composer exactly where it was.
func TestWelcomeCommandMenuOverlaysWithoutMovingComposer(t *testing.T) {
	host := &fakeHost{
		welcome:     true,
		welcomeText: "myagent\n───────\nkit shell",
		composer:    "/",
		composerH:   2,
	}
	s := newTestShell(host)
	s.Render(80, 24)
	closedComposer := s.composerR
	closedScroll := kit.WidgetBounds(s.scroll)

	host.picker = tui.KitPickerState{
		Kind:   "commands",
		Inline: true,
		Query:  "/",
		Items: []tui.KitPickerItem{
			{ID: "/help", Title: "/help", Desc: "Help & shortcuts"},
			{ID: "/models", Title: "/models", Desc: "Switch model"},
		},
	}
	host.pickerActive = true
	s.Sync()
	frame := ansi.Strip(s.Render(80, 24))

	if !s.menuOpen || s.menuR.Empty() {
		t.Fatal("command menu did not open")
	}
	if s.composerR != closedComposer {
		t.Fatalf("composer rect = %+v, want unchanged %+v", s.composerR, closedComposer)
	}
	if got := kit.WidgetBounds(s.scroll); got != closedScroll {
		t.Fatalf("welcome art rect = %+v, want unchanged %+v", got, closedScroll)
	}
	if s.menuR.Y+s.menuR.H != s.composerR.Y {
		t.Fatalf("menu bottom = %d, want composer top %d", s.menuR.Y+s.menuR.H, s.composerR.Y)
	}
	if s.menuR.Y >= closedScroll.Y+closedScroll.H || s.menuR.Y+s.menuR.H <= closedScroll.Y {
		t.Fatalf("menu rect %+v does not overlay the welcome art %+v", s.menuR, closedScroll)
	}
	for _, want := range []string{"/help", "/models"} {
		if !strings.Contains(frame, want) {
			t.Errorf("welcome menu missing %q:\n%s", want, frame)
		}
	}
}

func TestCommandMenuStartsInlineWithoutDialog(t *testing.T) {
	host := pickerHost()
	host.picker = tui.KitPickerState{Kind: "commands", Inline: true, Title: "Commands", Placeholder: "Search commands…", Query: "/", Items: []tui.KitPickerItem{{ID: "/models", Title: "/models"}}}
	s := newTestShell(host)
	s.Sync()
	if s.pickerOpen || s.dialogs.Modal() || !s.menuOpen {
		t.Fatalf("commands = picker %v modal %v menu %v; want inline menu", s.pickerOpen, s.dialogs.Modal(), s.menuOpen)
	}
	// First render applies the queued composer focus; without it the shell has
	// no focused key target and cannot forward Esc.
	s.Render(80, 24)
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyEsc}) {
		t.Fatal("Esc not forwarded to the command menu host")
	}
	if len(host.keys) == 0 || host.keys[len(host.keys)-1] != "esc" {
		t.Fatalf("forwarded keys = %v, want trailing esc", host.keys)
	}
}
