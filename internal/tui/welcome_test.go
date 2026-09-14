package tui

import (
	"strings"
	"testing"
)

func TestMyagentTierThresholds(t *testing.T) {
	if got := myagentTierForHeight(21); got != logoHidden {
		t.Fatalf("height 21 = %v, want hidden", got)
	}
	if got := myagentTierForHeight(22); got != logoCompact {
		t.Fatalf("height 22 = %v, want compact", got)
	}
	if got := myagentTierForHeight(26); got != logoFull {
		t.Fatalf("height 26 = %v, want full", got)
	}
}

func TestLogoAssetsEmbedded(t *testing.T) {
	if logoArtRows(myagentLogoFull) != 9 {
		t.Fatalf("full logo rows = %d, want 9", logoArtRows(myagentLogoFull))
	}
	if logoArtRows(myagentLogoSmall) != 7 {
		t.Fatalf("small logo rows = %d, want 7", logoArtRows(myagentLogoSmall))
	}
	if logoArtWidth(myagentLogoFull) <= 0 {
		t.Fatal("full logo has no width")
	}
}

func TestDefaultWelcomeV2(t *testing.T) {
	dir := t.TempDir()
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", dir)
	m.onResize(80, 30)
	m.welcomeStyle = welcomeDefault
	view := m.renderWelcome()
	for _, want := range []string{"myagent", "/help", "/resume", "/models", collapseHome(dir)} {
		if !strings.Contains(view, want) {
			t.Fatalf("default welcome missing %q:\n%s", want, view)
		}
	}
	// The location line sits on the last row, right-aligned above the box.
	lines := strings.Split(view, "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, collapseHome(dir)) {
		t.Fatalf("location line is not the bottom row:\n%s", view)
	}
}

func TestMyagentWelcomeStacked(t *testing.T) {
	dir := t.TempDir()
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", dir)
	m.onResize(80, 30)
	m.welcomeStyle = welcomeMyagent
	view := m.renderWelcome()
	for _, want := range []string{"myagent", "/help", collapseHome(dir)} {
		if !strings.Contains(view, want) {
			t.Fatalf("myagent welcome missing %q:\n%s", want, view)
		}
	}
}

func TestMyagentWelcomeHeroWide(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", t.TempDir())
	m.onResize(120, 40)
	m.welcomeStyle = welcomeMyagent
	view := m.renderWelcome()
	// Borderless hero: logo and menu side by side, no box drawing.
	if strings.Contains(view, "╭") || strings.Contains(view, "╰") || strings.Contains(view, "│") {
		t.Fatalf("wide myagent welcome still draws a hero box:\n%s", view)
	}
	if !strings.Contains(view, "█") || !strings.Contains(view, "/resume") {
		t.Fatalf("wide myagent welcome missing logo or menu:\n%s", view)
	}
}

func TestMyagentChoiceRegistered(t *testing.T) {
	if got := normalizeWelcomeStyle("myagent"); got != welcomeMyagent {
		t.Fatalf("normalize myagent = %q, want myagent", got)
	}
	// Legacy alias: persisted "grok" configs still resolve to myagent.
	if got := normalizeWelcomeStyle("grok"); got != welcomeMyagent {
		t.Fatalf("normalize grok = %q, want myagent", got)
	}
	if got := normalizeWelcomeStyle("bogus"); got != welcomeDefault {
		t.Fatalf("normalize bogus = %q, want default", got)
	}
}
