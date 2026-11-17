package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/charmbracelet/x/ansi"
)

func TestPickerPaletteFrames(t *testing.T) {
	for _, kind := range []string{"models", "commands"} {
		for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 18}} {
			t.Run(fmt.Sprintf("%s_%dx%d", kind, size[0], size[1]), func(t *testing.T) {
				host := pickerHost()
				if kind == "commands" {
					host.picker = tui.KitPickerState{Kind: "commands", Title: "Commands", Placeholder: "Search commands…", Query: "/", Items: []tui.KitPickerItem{
						{ID: "/models", Title: "/models", Desc: "Switch model"},
						{ID: "/new", Title: "/new", Desc: "Start a conversation"},
						{ID: "/tools", Title: "/tools", Desc: "Choose enabled tools"},
					}}
				} else {
					host.picker.Placeholder = "Search models…"
					host.picker.Items[1].Footer = "✓ current · 200k"
				}
				s := newTestShell(host)
				s.Sync()
				raw := s.Render(size[0], size[1])
				assertFrame(t, raw, size[0], size[1])
				if s.pickerLayout.Footer != nil {
					t.Fatal("palette should not show confirm/cancel buttons")
				}
				plain := trimLines(ansi.Strip(raw))
				if !strings.Contains(plain, "results") && size[0] >= 80 {
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

func TestCommandPaletteStartsWithAnEmptySearch(t *testing.T) {
	host := pickerHost()
	host.picker = tui.KitPickerState{Kind: "commands", Title: "Commands", Placeholder: "Search commands…", Query: "/", Items: []tui.KitPickerItem{{ID: "/models", Title: "/models"}}}
	s := newTestShell(host)
	s.Sync()
	if s.picker.Query() != "" {
		t.Fatalf("query = %q", s.picker.Query())
	}
	if !s.Key(tea.KeyPressMsg{Code: tea.KeyEsc}) || host.cancels != 1 {
		t.Fatal("Esc should close unfiltered command palette immediately")
	}
}
