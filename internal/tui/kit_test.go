package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fakeKitUI records the model -> shell contract without importing app (which
// would create an import cycle in the tui test package).
type fakeKitUI struct {
	keys    []string
	syncs   int
	mice    int
	renders int
	resizes [][2]int
	cmd     tea.Cmd
}

func (f *fakeKitUI) Render(w, h int) string {
	f.renders++
	return ""
}

func TestUsageSidebarHiddenByDefaultAndToggled(t *testing.T) {
	m := newPanelModel()
	m.transcript.addUser("hello")
	if title, rows := m.KitSidebar(); title != "" || rows != nil {
		t.Fatalf("default sidebar = %q, %d rows; want hidden", title, len(rows))
	}
	m.runCommand("/usage")
	title, rows := m.KitSidebar()
	if title != "Usage" || len(rows) == 0 {
		t.Fatalf("after /usage = %q, %d rows; want visible", title, len(rows))
	}
	m.runCommand("/usage")
	if title, rows := m.KitSidebar(); title != "" || rows != nil {
		t.Fatalf("after second /usage = %q, %d rows; want hidden", title, len(rows))
	}
}

func (f *fakeKitUI) Key(ev tea.KeyPressMsg) bool {
	f.keys = append(f.keys, ev.Keystroke())
	return true
}

func (f *fakeKitUI) Mouse(tea.MouseMsg) bool {
	f.mice++
	return true
}

func (f *fakeKitUI) Resize(w, h int) {
	f.resizes = append(f.resizes, [2]int{w, h})
}

func (f *fakeKitUI) Sync() { f.syncs++ }

func (f *fakeKitUI) ComposeCount() int { return 0 }

func (f *fakeKitUI) TakeCmd() tea.Cmd {
	cmd := f.cmd
	f.cmd = nil
	return cmd
}

func TestUIKitEnabledLegacyEscapeHatch(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"", true},    // default: kit
		{"kit", true}, // backwards-compatible alias
		{"KIT", true}, // only the exact legacy value opts out
		{"Kit", true},
		{" kit", true},
		{"kit ", true},
		{"1", true},
		{"0", true},
		{"other", true},
		{"legacy", false}, // the one-release escape hatch
		{"LEGACY", true},
		{"Legacy", true},
		{" legacy", true},
		{"legacy ", true},
	}
	for _, tc := range cases {
		t.Setenv(uiEnvVar, tc.value)
		if got := UIKitEnabled(); got != tc.want {
			t.Errorf("UIKitEnabled(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestInitKitDefaultsToKitUnlessLegacy(t *testing.T) {
	oldFactory := kitFactory
	defer func() { kitFactory = oldFactory }()

	cases := []struct {
		name    string
		env     string
		factory bool
		want    bool
	}{
		{"unset defaults to kit", "", true, true},
		{"kit alias", "kit", true, true},
		{"other value defaults to kit", "KIT", true, true},
		{"legacy escape hatch", "legacy", true, false},
		{"no factory falls back", "", false, false},
		{"legacy without factory", "legacy", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(uiEnvVar, tc.env)
			if tc.factory {
				SetKitUIFactory(func(KitHost) KitUI { return &fakeKitUI{} })
			} else {
				SetKitUIFactory(nil)
			}
			m := &model{}
			m.initKit()
			if got := m.kit != nil; got != tc.want {
				t.Fatalf("kit active = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestModelKitIntegration drives the real model through the KitUI seam: resize,
// key, mouse, Sync-per-update, transcript revision exposure, and command
// draining all cross the interface without using the legacy path.
func TestModelKitIntegration(t *testing.T) {
	oldFactory := kitFactory
	defer func() { kitFactory = oldFactory }()

	fake := &fakeKitUI{}
	var host KitHost
	SetKitUIFactory(func(h KitHost) KitUI {
		host = h
		return fake
	})
	t.Setenv(uiEnvVar, "") // default shell: kit

	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", t.TempDir())
	if m.kit == nil || host == nil {
		t.Fatal("kit shell not activated through the factory")
	}

	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	if len(fake.resizes) != 1 {
		t.Fatalf("resize calls = %d, want 1", len(fake.resizes))
	}
	if fake.resizes[0] != [2]int{90, 30} {
		t.Fatalf("resize = %v, want [90 30]", fake.resizes[0])
	}
	if fake.syncs == 0 {
		t.Fatal("Update did not Sync the kit shell")
	}

	// A key consumed by the shell must not reach the legacy textarea.
	fake.cmd = func() tea.Msg { return nil }
	beforeSyncs := fake.syncs
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if len(fake.keys) != 1 || fake.keys[0] != "a" {
		t.Fatalf("kit keys = %v, want [a]", fake.keys)
	}
	if m.input.Value() != "" {
		t.Fatalf("legacy input received the key: %q", m.input.Value())
	}
	if fake.syncs != beforeSyncs+1 {
		t.Fatalf("syncs = %d, want %d", fake.syncs, beforeSyncs+1)
	}
	if cmd == nil {
		t.Fatal("TakeCmd command was not batched into Update's return")
	}

	// Transcript mutations are visible through the host revision.
	rev := host.KitTranscriptRev()
	m.transcript.addUser("hello")
	if host.KitTranscriptRev() == rev {
		t.Fatal("transcript mutation did not bump the kit revision")
	}

	m.Update(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if fake.mice != 1 {
		t.Fatalf("kit mouse calls = %d, want 1", fake.mice)
	}
}

func TestKitSetSizeUsesTranscriptInvalidatePath(t *testing.T) {
	tr := &transcript{
		rev:    7,
		blocks: []*block{{cacheValid: true}, {cacheValid: true}},
	}
	m := &model{transcript: tr}

	m.KitSetSize(40, 10)
	if m.kitWidth != 40 || m.kitHeight != 10 {
		t.Fatalf("size = %dx%d, want 40x10", m.kitWidth, m.kitHeight)
	}
	if tr.rev != 8 {
		t.Fatalf("rev = %d, want 8 after a width change", tr.rev)
	}
	for i, b := range tr.blocks {
		if b.cacheValid {
			t.Fatalf("block %d cache still valid after width change", i)
		}
		b.cacheValid = true
	}

	// Height-only changes do not invalidate the caches; width changes do.
	m.KitSetSize(40, 12)
	if tr.rev != 8 {
		t.Fatalf("height-only rev = %d, want 8", tr.rev)
	}
	if !tr.blocks[0].cacheValid {
		t.Fatal("height-only change invalidated the block cache")
	}
	m.KitSetSize(30, 12)
	if tr.rev != 9 {
		t.Fatalf("rev = %d, want 9 after the second width change", tr.rev)
	}
	if tr.blocks[0].cacheValid {
		t.Fatal("second width change did not invalidate the block cache")
	}
}
