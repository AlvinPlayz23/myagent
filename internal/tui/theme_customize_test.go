package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/config"
)

func TestCustomizeOffersAllNamedThemes(t *testing.T) {
	SetThemeName("default")
	t.Cleanup(func() { SetThemeName("default") })
	m := newPanelModel()
	if len(customizeGroups) != 3 || customizeGroups[2].section != sectionTheme {
		t.Fatal("theme group missing")
	}
	for i, choice := range themeChoices {
		m.setTheme(choice.id)
		m.customize.open()
		m.customize.sel = 2
		mustNotPanic(t, choice.id, func() { m.confirmCustomize() })
		if m.customize.sel != i || m.customize.selected().themeName != choice.id {
			t.Fatalf("%s highlighted %+v at %d", choice.id, m.customize.selected(), m.customize.sel)
		}
		state, _ := m.KitPicker()
		if len(state.Items) != len(themeChoices) || state.Items[i].Footer != "(current)" {
			t.Fatalf("theme picker projection: %+v", state)
		}
	}
}

func TestThemeSavesAndRestoresWithoutClobberingOtherSettings(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	t.Cleanup(func() { SetThemeName("default") })
	initial := &config.Config{DefaultModel: "local/model", Theme: "tokyonight", WelcomeStyle: "orb", PromptStyle: "ruled"}
	if err := config.Save(initial); err != nil {
		t.Fatal(err)
	}
	m := newPanelModel()
	m.configurePersistedSettings(initial)
	if m.themeName != "tokyonight" {
		t.Fatal("persisted theme was not restored")
	}
	if err := config.Update(func(c *config.Config) error { c.DefaultModel = "local/externally-changed"; return nil }); err != nil {
		t.Fatal(err)
	}
	m.customize.openGroup(2, 0)
	m.applyTheme(customizeRow{themeName: "tokyo-mocha", label: "Tokyo Mocha"})
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Theme != "tokyo-mocha" || saved.DefaultModel != "local/externally-changed" || saved.WelcomeStyle != "orb" || saved.PromptStyle != "ruled" {
		t.Fatalf("save clobbered configuration: %+v", saved)
	}
	if initial.Theme != "tokyonight" {
		t.Fatal("startup snapshot mutated")
	}
	restored := newPanelModel()
	restored.configurePersistedSettings(saved)
	if restored.themeName != "tokyo-mocha" || restored.th.TokenSet().Background.Base != CurrentThemeTokens().Background.Base {
		t.Fatal("theme did not survive restart")
	}
}

func TestThemeSaveFailureLeavesCurrentThemeUnchanged(t *testing.T) {
	SetThemeName("default")
	t.Cleanup(func() { SetThemeName("default") })
	m := newPanelModel()
	m.customize.open()
	m.customize.openGroup(2, 1)
	m.saveTheme = func(string) error { return errors.New("disk unavailable") }
	before := ThemeRevision()
	m.applyTheme(customizeRow{themeName: "tokyonight", label: "Tokyo Night"})
	if CurrentThemeName() != "default" || ThemeRevision() != before || !m.customize.active || !strings.Contains(m.statusMsg, "disk unavailable") {
		t.Fatal("failed save changed active theme or hid error")
	}
}

func TestLiveThemeSwitchInvalidatesTranscriptAndMarkdown(t *testing.T) {
	SetThemeName("default")
	t.Cleanup(func() { SetThemeName("default") })
	m := newPanelModel()
	m.transcript.addUser("hello")
	m.transcript.render(70)
	m.md.render("```go\nfunc main() { println(\"hello\") }\n```", 70)
	beforeRenderer := m.md.r
	beforeColor := rgbaOf(m.th.tokens.Background.Raised.Base.Color())
	ptr := m.th
	rev := m.transcript.rev
	m.setTheme("catppuccin")
	if m.transcript.blocks[0].cacheValid {
		t.Fatal("theme left transcript cache valid")
	}
	if beforeColor == rgbaOf(m.th.tokens.Background.Raised.Base.Color()) {
		t.Fatal("theme left old surface color")
	}
	m.md.render("```go\nfunc main() { println(\"hello\") }\n```", 70)
	if beforeRenderer == m.md.r || m.md.themeRevision != ThemeRevision() {
		t.Fatal("theme left cached markdown renderer")
	}

	if m.th != ptr || m.transcript.th != ptr || m.transcript.rev <= rev {
		t.Fatal("theme switch replaced shared pointers or did not invalidate transcript")
	}
}

func TestUnknownSavedThemeUsesAdaptiveDefault(t *testing.T) {
	SetThemeName("vesper")
	t.Cleanup(func() { SetThemeName("default") })
	m := newPanelModel()
	m.configurePersistedSettings(&config.Config{Theme: "removed-theme"})
	if CurrentThemeName() != "default" || m.themeName != "default" {
		t.Fatal("unknown saved theme did not fall back")
	}
}
