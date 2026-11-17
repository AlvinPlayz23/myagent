package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestRainDensityStaysThin guards the "make the rain thinner" tuning: the
// field must scatter drops rather than blanket the screen, and most columns
// should carry no drops at all. A future density tweak that regresses to a
// solid curtain trips this.
func TestRainDensityStaysThin(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", t.TempDir())
	m.onResize(160, 40)
	const width, height = 160, 60

	filled, total := 0, 0
	for y := 0; y < height; y++ {
		plain := ansi.Strip(m.rainRow(y, height, false, nil))
		runes := []rune(plain)
		for x := 0; x < width; x++ {
			total++
			if x < len(runes) && (runes[x] == '│' || runes[x] == '·') {
				filled++
			}
		}
	}
	density := float64(filled) / float64(total)
	if density > 0.09 {
		t.Fatalf("rain density = %.3f of cells, want <= 0.09 (field reads as a curtain)", density)
	}
	if density == 0 {
		t.Fatal("rain field is completely empty; drops are not rendering")
	}
}

// TestWelcomeHintReportsDisabledTools covers the startup notice: when the
// persisted /tools deny list is non-empty it replaces the prompt hint, listing
// the disabled tools sorted; with an empty list the normal hint returns.
func TestWelcomeHintReportsDisabledTools(t *testing.T) {
	dir := t.TempDir()
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", dir)
	m.onResize(120, 40)

	if got := ansi.Strip(m.welcomeHintPlain()); !strings.Contains(got, "Type a prompt") {
		t.Fatalf("hint with no disabled tools = %q, want the prompt hint", got)
	}

	m.disabledTools = []string{"write", "bash"}
	if got, want := ansi.Strip(m.welcomeHintPlain()), "Disabled tools: bash, write"; got != want {
		t.Fatalf("hint = %q, want %q (sorted list, prompt hint replaced)", got, want)
	}
	// Sorting must not mutate the model's slice.
	if m.disabledTools[0] != "write" {
		t.Fatalf("welcomeHintPlain reordered m.disabledTools: %v", m.disabledTools)
	}

	// The styled line carries the same text, truncated to the terminal width
	// so a long deny list cannot wrap.
	m.disabledTools = []string{"bash", "write", "edit", "read", "glob", "grep"}
	plain := ansi.Strip(m.welcomeHint())
	if w := len([]rune(strings.TrimSpace(plain))); w > m.width {
		t.Fatalf("hint width = %d, want <= terminal width %d", w, m.width)
	}
}

// TestWelcomeHintDisabledAcrossStyles asserts every welcome style renders the
// notice, not just the default one.
func TestWelcomeHintDisabledAcrossStyles(t *testing.T) {
	for _, style := range []welcomeStyle{welcomeDefault, welcomeMyagent, welcomeRain, welcomeOrb, welcomeBanner, welcomeWave, welcomeFill} {
		m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", t.TempDir())
		m.onResize(120, 40)
		m.welcomeStyle = style
		m.disabledTools = []string{"bash"}
		view := ansi.Strip(m.renderWelcome())
		if !strings.Contains(view, "Disabled tools: bash") {
			t.Fatalf("style %v did not show the disabled-tools notice:\n%s", style, view)
		}
		if strings.Contains(view, "Type a prompt") {
			t.Fatalf("style %v kept the prompt hint alongside the notice:\n%s", style, view)
		}
	}
}

func TestRainFillsViewportAndKeepsMenu(t *testing.T) {
	dir := t.TempDir()
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", dir)
	m.onResize(80, 30)
	m.welcomeStyle = welcomeRain
	view := m.renderWelcome()
	lines := strings.Split(view, "\n")
	if len(lines) != m.welcomeViewportHeight() {
		t.Fatalf("rain rows = %d, want viewport height %d", len(lines), m.welcomeViewportHeight())
	}
	// The same overlay keeps working on the next frame.
	m.welcomeFrame++
	m.welcomeFrame++
	if again := strings.Count(m.renderWelcome(), "\n") + 1; again != m.welcomeViewportHeight() {
		t.Fatalf("rain rows on later frame = %d, want %d", again, m.welcomeViewportHeight())
	}
	for _, want := range []string{"myagent", "Start typing", "/resume", "/models"} {
		if !strings.Contains(view, want) {
			t.Fatalf("rain welcome missing %q:\n%s", want, view)
		}
	}
	// Menu rows are recorded for clicks and span the full menu.
	rows := m.welcomeMenu[1] - m.welcomeMenu[0]
	if rows != len(welcomeMenuItems) {
		t.Fatalf("rain menu rows = %d, want %d", rows, len(welcomeMenuItems))
	}
	// Full-screen rain reaches the edges within the first few rows.
	edge := false
	for _, line := range lines[:min(6, len(lines))] {
		plain := ansi.Strip(line)
		if strings.HasPrefix(plain, "│") || strings.HasSuffix(plain, "│") {
			edge = true
			break
		}
	}
	if !edge {
		t.Fatalf("no rain row touches the viewport edge:\n%s", view)
	}

	// The menu row still sits ON falling rain: stripping the text leaves
	// drops on both sides instead of a blank gap.
	menuLine := lines[m.welcomeMenu[0]+2]
	withoutText := menuLine
	for _, item := range welcomeMenuItems {
		withoutText = strings.ReplaceAll(withoutText, item[0], "")
		withoutText = strings.ReplaceAll(withoutText, item[1], "")
	}
	if plain := ansi.Strip(withoutText); !strings.Contains(plain, "│") && !strings.Contains(plain, "·") {
		t.Fatalf("menu row has no rain behind it:\n%s", menuLine)
	}
}

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

func TestMyagentWelcomeWideCentered(t *testing.T) {
	dir := t.TempDir()
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", dir)
	m.onResize(120, 40)
	m.welcomeStyle = welcomeMyagent
	view := m.renderWelcome()
	logoAt, brandAt, menuAt := strings.Index(view, "█"), strings.Index(view, "myagent"), strings.Index(view, "/resume")
	if logoAt < 0 || brandAt <= logoAt || menuAt <= brandAt {
		t.Fatalf("wide welcome should stack the myagent mark above its menu:\n%s", view)
	}
	if strings.Contains(view, "╭") || strings.Contains(view, "│") {
		t.Fatalf("wide myagent welcome should remain borderless:\n%s", view)
	}
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[len(lines)-1], collapseHome(dir)) {
		t.Fatalf("location should remain on the bottom row:\n%s", view)
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

func TestKitHomeWordmarkFitsPromptRegion(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", t.TempDir())
	for _, width := range []int{20, 40, 75} {
		view := ansi.Strip(m.KitWelcome(width, 8))
		lines := strings.Split(view, "\n")
		if len(lines) != 8 {
			t.Fatalf("home wordmark height = %d", len(lines))
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("wordmark overflow at %d: %q", width, line)
			}
		}
		if strings.Contains(view, "Start typing") || strings.Contains(view, "/resume") {
			t.Fatal("home logo region includes obsolete menu")
		}
	}
	m.disabledTools = []string{"bash"}
	if view := ansi.Strip(m.KitWelcome(75, 8)); !strings.Contains(view, "Disabled tools: bash") {
		t.Fatal("home must keep disabled-tools notice")
	}
}

func TestHomeWordmarkUsesCompactPixels(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	rows := m.pixelWordmark()
	if len(rows) != 4 {
		t.Fatalf("pixel wordmark height = %d", len(rows))
	}
	text := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(text, "▀") || !strings.Contains(text, "▄") || !strings.Contains(text, "█") {
		t.Fatal("wordmark should use packed pixel blocks")
	}
	for _, row := range rows {
		if ansi.StringWidth(row) != 41 {
			t.Fatalf("wordmark row width = %d", ansi.StringWidth(row))
		}
	}
	for _, choice := range welcomeChoices {
		m.welcomeStyle = choice.style
		before := m.KitWelcomeRev()
		m.onResize(80, 30)
		m.update(tickMsg{})
		if choice.style.animated() && m.KitWelcomeRev() == before {
			t.Fatalf("%s did not advance", choice.style)
		}
	}
}
