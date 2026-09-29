package tui

// Regression coverage for the /customize crash reported in 3131333.txt:
//
//	runtime error: index out of range [6] with length 2
//	tui.(*model).confirmCustomize  model.go:1845
//	tui.(*model).onKey             model.go:1025
//
// confirmCustomize opens the highlighted group and *then* formats the status
// line from m.customize.sel:
//
//	m.customize.openGroup(m.customize.sel, m.currentCustomizeChoice(m.customize.sel))
//	m.statusMsg = "Choose a " + customizeGroups[m.customize.sel].label + " option."
//
// openGroup assigns sel = current, where current indexes the opened group's
// *choices* (7 startup styles, 2 composer styles), so the second line indexes
// customizeGroups (len 2) with a choice index. Any startup style past the
// second one (index >= 2) panics: Banner=2, Wave=3, Rain=4, Fill=5, Myagent=6.
// The reported [6] is the "Myagent" startup style.
//
// These tests assert the intended behavior, so they fail (panic) until
// confirmCustomize keeps the group index separate from the choice index.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// mustNotPanic runs f and fails the test with the recovered value instead of
// taking the whole test binary down, so one run reports every affected style.
func mustNotPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("%s panicked: %v", what, r)
		}
	}()
	f()
}

// openCustomize builds a model whose startup style is style, opens
// /customize, and leaves the cursor on the first option group.
func openCustomize(t *testing.T, style welcomeStyle, prompt promptStyle) *model {
	t.Helper()
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(100, 30)
	m.welcomeStyle = style
	m.promptStyle = prompt
	m.syncComposerStyle()
	m.runCommand("/customize")
	if !m.customize.active || m.customize.level != 0 {
		t.Fatalf("customize panel not open at the group list: %+v", m.customize)
	}
	if m.customize.sel != 0 {
		t.Fatalf("group cursor = %d, want 0", m.customize.sel)
	}
	return m
}

func TestCustomizeEnterOpensStartupGroupForEveryStyle(t *testing.T) {
	for i, choice := range welcomeChoices {
		style := choice.style
		t.Run(string(style), func(t *testing.T) {
			m := openCustomize(t, style, promptDefault)

			// The keyboard path: enter at the group list.
			mustNotPanic(t, "enter on the startup group", func() {
				m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
			})

			if m.customize.level != 1 || m.customize.group != 0 {
				t.Fatalf("enter did not open the startup group: level=%d group=%d sel=%d",
					m.customize.level, m.customize.group, m.customize.sel)
			}
			if m.customize.sel != i {
				t.Fatalf("cursor = %d, want the current style %d (%q)", m.customize.sel, i, choice.label)
			}
			if got := m.customize.selected(); got.section != sectionStartup || got.welcome != style {
				t.Fatalf("selected row = %+v, want startup/%q", got, style)
			}
			if msg := m.statusMsg; !strings.Contains(msg, customizeGroups[0].label) {
				t.Fatalf("status = %q, want it to name %q", msg, customizeGroups[0].label)
			}
		})
	}
}

func TestCustomizeEnterOpensComposerGroupForEveryStyle(t *testing.T) {
	for i, choice := range promptChoices {
		style := choice.style
		t.Run(string(style), func(t *testing.T) {
			m := openCustomize(t, welcomeDefault, style)
			m.customize.move(1) // cursor on "Composer (Prompt Box)"
			if m.customize.sel != 1 {
				t.Fatalf("group cursor = %d, want 1", m.customize.sel)
			}

			mustNotPanic(t, "enter on the composer group", func() {
				m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
			})

			if m.customize.level != 1 || m.customize.group != 1 {
				t.Fatalf("enter did not open the composer group: level=%d group=%d sel=%d",
					m.customize.level, m.customize.group, m.customize.sel)
			}
			if m.customize.sel != i {
				t.Fatalf("cursor = %d, want the current composer style %d (%q)", m.customize.sel, i, choice.label)
			}
			if got := m.customize.selected(); got.section != sectionComposer || got.prompt != style {
				t.Fatalf("selected row = %+v, want composer/%q", got, style)
			}
			if msg := m.statusMsg; !strings.Contains(msg, customizeGroups[1].label) {
				t.Fatalf("status = %q, want it to name %q", msg, customizeGroups[1].label)
			}
		})
	}
}

// TestCustomizeClickConfirmOpensStartupGroup is the mouse equivalent: a click
// previews the row, a second click on the same row confirms it. Both clicks
// land in confirmCustomize, so both used to panic the same way.
func TestCustomizeClickConfirmOpensStartupGroup(t *testing.T) {
	m := openCustomize(t, welcomeMyagent, promptRuled)
	m.View() // records the panel row map the clicks resolve against

	startupRow, composerRow := -1, -1
	for line, item := range m.inlineRowItems {
		switch item {
		case 0: // "Startup Style"
			startupRow = m.panelStartY + line
		case 1: // "Composer (Prompt Box)"
			composerRow = m.panelStartY + line
		}
	}
	if startupRow < 0 || composerRow < 0 {
		t.Fatalf("missing group rows: %v", m.inlineRowItems)
	}

	click := func(row int, what string) {
		mustNotPanic(t, what, func() {
			m.onMouseClick(tea.MouseClickMsg{X: 2, Y: row, Button: tea.MouseLeft})
		})
	}
	// The cursor opens on the startup group, so clicking it would confirm on the
	// very first click. Move it to the other group first, making the next pair a
	// genuine preview-then-confirm gesture.
	click(composerRow, "click on the composer group")
	click(startupRow, "preview click on the startup group")
	click(startupRow, "confirm click on the startup group")

	if m.customize.level != 1 || m.customize.group != 0 {
		t.Fatalf("double click did not open the startup group: level=%d group=%d sel=%d",
			m.customize.level, m.customize.group, m.customize.sel)
	}
	if got := m.customize.selected(); got.welcome != welcomeMyagent {
		t.Fatalf("selected row = %+v, want the current style %q", got, welcomeMyagent)
	}
}

// TestCurrentCustomizeChoiceIndexesTheGroupsChoices pins the helper that feeds
// openGroup: it must return an index into the opened group's choices, never
// into customizeGroups.
func TestCurrentCustomizeChoiceIndexesTheGroupsChoices(t *testing.T) {
	for i, choice := range welcomeChoices {
		m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
		m.onResize(100, 30)
		m.welcomeStyle = choice.style
		m.promptStyle = promptDefault
		if got := m.currentCustomizeChoice(0); got != i {
			t.Errorf("currentCustomizeChoice(startup) = %d, want %d for %q", got, i, choice.style)
		}
	}
	for i, choice := range promptChoices {
		m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
		m.onResize(100, 30)
		m.welcomeStyle = welcomeDefault
		m.promptStyle = choice.style
		if got := m.currentCustomizeChoice(1); got != i {
			t.Errorf("currentCustomizeChoice(composer) = %d, want %d for %q", got, i, choice.style)
		}
	}
	// Out-of-range groups must stay harmless.
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(100, 30)
	for _, bad := range []int{-1, 2, 6, 1 << 20} {
		if got := m.currentCustomizeChoice(bad); got != 0 {
			t.Errorf("currentCustomizeChoice(%d) = %d, want 0", bad, got)
		}
	}
}

// TestCustomizeEnterOnOutOfRangeGroupDoesNotPanic forces the same out-of-range
// cursor the click path can produce and asserts Enter closes the panel with a
// status message instead of indexing customizeGroups out of range.
func TestCustomizeEnterOnOutOfRangeGroupDoesNotPanic(t *testing.T) {
	m := openCustomize(t, welcomeDefault, promptDefault)
	m.customize.sel = 6 // past the two groups

	mustNotPanic(t, "enter on an out-of-range group", func() {
		m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	})

	if m.customize.active {
		t.Fatalf("panel still active after out-of-range enter: %+v", m.customize)
	}
	if m.statusMsg == "" {
		t.Fatal("no status message after out-of-range enter")
	}
}

// TestCustomizeOutOfRangeClickIsIgnored clicks a row index that belonged to a
// taller level after returning to the group list, and asserts the cursor is not
// moved past the current level's height.
func TestCustomizeOutOfRangeClickIsIgnored(t *testing.T) {
	m := openCustomize(t, welcomeDefault, promptDefault)
	m.View()

	// Enter the startup group (7 rows), then go back to the group list (2 rows).
	m.confirmCustomize()
	m.customize.backToGroups()
	if m.customize.level != 0 {
		t.Fatalf("level = %d, want 0", m.customize.level)
	}

	// A click on what used to be the 7th startup row must not land on sel.
	m.onMouseClick(tea.MouseClickMsg{X: 2, Y: m.panelStartY + 3, Button: tea.MouseLeft})
	if m.customize.sel < 0 || m.customize.sel >= m.customize.height() {
		t.Fatalf("sel = %d escaped the level-0 height %d", m.customize.sel, m.customize.height())
	}
}

// TestCustomizeAppliesNoRowWhenCursorOutOfRange guards config.json: an
// out-of-range cursor must not resolve to the zero row and persist an empty
// style.
func TestCustomizeAppliesNoRowWhenCursorOutOfRange(t *testing.T) {
	m := openCustomize(t, welcomeDefault, promptDefault)
	m.confirmCustomize() // descend so level == 1
	m.customize.sel = 99 // past the startup choices

	savedWelcome, savedPrompt := false, false
	m.saveWelcomeStyle = func(welcomeStyle) error { savedWelcome = true; return nil }
	m.savePromptStyle = func(promptStyle) error { savedPrompt = true; return nil }

	m.applyCustomizeSelection()

	if savedWelcome || savedPrompt {
		t.Fatalf("saved a row for an out-of-range cursor: welcome=%v prompt=%v", savedWelcome, savedPrompt)
	}
}

// TestCustomizeOpenGroupClampsCurrentChoice pins openGroup's clamp so a bogus
// current index can never leave sel out of range for the level it switches to.
func TestCustomizeOpenGroupClampsCurrentChoice(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(100, 30)

	m.customize.openGroup(0, 99)
	if got, want := m.customize.sel, len(m.customize.choices())-1; got != want {
		t.Fatalf("openGroup(0, 99) sel = %d, want %d", got, want)
	}
	m.customize.openGroup(1, -5)
	if m.customize.sel != 0 {
		t.Fatalf("openGroup(1, -5) sel = %d, want 0", m.customize.sel)
	}
}
