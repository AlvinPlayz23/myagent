package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestUnionToolsKeepsDisabledFromEitherScope asserts the deny list is a union:
// a tool disabled globally stays disabled even when the session list omits it.
// Intersecting would let a session silently re-enable a globally disabled tool.
func TestUnionToolsKeepsDisabledFromEitherScope(t *testing.T) {
	got := unionTools([]string{"bash", "write"}, []string{"write", "edit"})
	if want := []string{"bash", "edit", "write"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("union = %v, want %v", got, want)
	}
	if unionTools(nil, nil) != nil {
		t.Fatal("union of two empty lists should be nil")
	}
	// Blank names are dropped rather than becoming an empty entry.
	if got := unionTools([]string{"", "bash"}, nil); !reflect.DeepEqual(got, []string{"bash"}) {
		t.Fatalf("union with blank = %v, want [bash]", got)
	}
}

// TestScopedSaveWritesOnlyChosenScope checks that saving to one scope leaves the
// other scope's stored list alone.
func TestScopedSaveWritesOnlyChosenScope(t *testing.T) {
	m := newToolsTestModel(t)
	var globalSaved, sessionSaved [][]string
	m.saveDisabledTools = func(d []string) error {
		globalSaved = append(globalSaved, append([]string(nil), d...))
		return nil
	}
	m.saveSessionTools = func(d []string) error {
		sessionSaved = append(sessionSaved, append([]string(nil), d...))
		return nil
	}

	m.applyToolTogglesScoped([]string{"bash"}, scopeSession)
	if len(globalSaved) != 0 {
		t.Fatalf("session scope wrote the global list: %v", globalSaved)
	}
	if len(sessionSaved) != 1 {
		t.Fatalf("session saves = %d, want 1", len(sessionSaved))
	}
	if want := []string{"bash"}; !reflect.DeepEqual(sessionSaved[0], want) {
		t.Fatalf("session saved %v, want %v", sessionSaved[0], want)
	}
	if want := []string{"bash"}; !reflect.DeepEqual(m.sessionDisabledTools, want) {
		t.Fatalf("sessionDisabledTools = %v, want %v", m.sessionDisabledTools, want)
	}

	// Now save globally; the session list must survive.
	m.applyToolTogglesScoped([]string{"write"}, scopeGlobal)
	if want := []string{"bash"}; !reflect.DeepEqual(m.sessionDisabledTools, want) {
		t.Fatalf("global save clobbered the session list: %v", m.sessionDisabledTools)
	}
	if want := []string{"write"}; !reflect.DeepEqual(m.globalDisabledTools, want) {
		t.Fatalf("globalDisabledTools = %v, want %v", m.globalDisabledTools, want)
	}
	// The effective list is the union of both scopes.
	if want := []string{"bash", "write"}; !reflect.DeepEqual(m.disabledTools, want) {
		t.Fatalf("effective = %v, want %v", m.disabledTools, want)
	}
	if got, want := m.runner.cfg.Registry.Names(), []string{"read", "edit"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("registry = %v, want %v", got, want)
	}
}

// TestScopePanelBackOutPreservesToggles verifies esc from the scope panel
// returns to the toggles with the staged list intact, so no work is lost.
func TestScopePanelBackOutPreservesToggles(t *testing.T) {
	m := newToolsTestModel(t)
	saved := false
	m.saveDisabledTools = func([]string) error { saved = true; return nil }

	m.tools.open(m.baseRegistry, nil)
	m.tools.toggleEnabled(0) // stage one toggle
	staged := m.tools.pending()
	if len(staged) != 1 {
		t.Fatalf("staged = %v, want one entry", staged)
	}

	// Stage into the scope panel, then back out.
	m.scope.open(staged)
	m.tools.close()
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.scope.active {
		t.Fatal("scope panel still active after esc")
	}
	if !m.tools.active {
		t.Fatal("tools panel was not reopened")
	}
	if got := m.tools.pending(); !reflect.DeepEqual(got, staged) {
		t.Fatalf("toggles after back-out = %v, want %v", got, staged)
	}
	if saved {
		t.Fatal("backing out wrote to disk")
	}
	if len(m.disabledTools) != 0 {
		t.Fatalf("disabledTools = %v, want unchanged after back-out", m.disabledTools)
	}
}

// TestScopePanelRendersBothChoices confirms the panel offers both scopes and
// marks session scope unavailable when no store is wired.
func TestScopePanelRendersBothChoices(t *testing.T) {
	m := newToolsTestModel(t)
	m.onResize(100, 30)
	m.saveSessionTools = func([]string) error { return nil }
	m.scope.open([]string{"bash", "write"})

	view := m.renderScopePicker()
	for _, want := range []string{"This session only", "Global", "config.json"} {
		if !strings.Contains(view, want) {
			t.Fatalf("scope panel missing %q:\n%s", want, view)
		}
	}

	m.saveSessionTools = nil
	if !strings.Contains(m.renderScopePicker(), "unavailable") {
		t.Fatal("session scope not marked unavailable when no store is wired")
	}
}

// TestWelcomeHintLabelsScope checks the startup notice distinguishes a
// session-scoped deny list from a global one.
func TestWelcomeHintLabelsScope(t *testing.T) {
	m := newToolsTestModel(t)
	m.onResize(120, 40)

	m.globalDisabledTools = []string{"bash"}
	m.disabledTools = []string{"bash"}
	if got := m.welcomeHintPlain(); !strings.Contains(got, "(global)") {
		t.Fatalf("global hint = %q, want a (global) label", got)
	}

	m.sessionDisabledTools = []string{"bash"}
	if got := m.welcomeHintPlain(); !strings.Contains(got, "(session)") {
		t.Fatalf("session hint = %q, want a (session) label", got)
	}
}

// TestSessionScopeUnavailableRefusesSave keeps the panel honest: with no
// session store wired, a session-scoped save must fail loudly and change
// nothing rather than silently pretending to persist.
func TestSessionScopeUnavailableRefusesSave(t *testing.T) {
	m := newToolsTestModel(t)
	m.saveSessionTools = nil

	m.applyToolTogglesScoped([]string{"bash"}, scopeSession)
	if len(m.disabledTools) != 0 {
		t.Fatalf("disabledTools = %v, want unchanged", m.disabledTools)
	}
	if len(m.runner.cfg.Registry.Names()) != 4 {
		t.Fatalf("registry changed despite no session store: %v", m.runner.cfg.Registry.Names())
	}
}
