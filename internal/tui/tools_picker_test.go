package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/tools"
)

func newToolsTestModel(t *testing.T) *model {
	t.Helper()
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, nil)
	r.cfg.Registry = tools.DefaultRegistry("/tmp")
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "/tmp")
	m.baseRegistry = r.cfg.Registry
	m.basePrompt = r.cfg.SystemPrompt
	m.baseEffort = r.cfg.Effort
	return m
}

func TestToolsPickerToggleAndPending(t *testing.T) {
	p := &toolsPicker{}
	reg := tools.DefaultRegistry("/tmp")
	p.open(reg, []string{"write"})

	if len(p.names) != 4 {
		t.Fatalf("names = %v, want 4 tools", p.names)
	}
	if !p.isDisabled("write") {
		t.Fatal("write should start disabled")
	}
	if p.isDisabled("bash") {
		t.Fatal("bash should start enabled")
	}

	// Toggle bash off, then write back on.
	p.sel = 3 // bash is last in DefaultRegistry order
	p.toggle()
	if !p.isDisabled("bash") {
		t.Fatal("toggle did not disable bash")
	}
	p.toggle()
	if p.isDisabled("bash") {
		t.Fatal("second toggle did not re-enable bash")
	}

	p.sel = 1 // write
	p.toggle()
	if p.isDisabled("write") {
		t.Fatalf("pending = %v, want write re-enabled", p.pending())
	}
	if got := p.pending(); len(got) != 0 {
		t.Fatalf("pending = %v, want empty", got)
	}
}

func TestToolsPickerPendingIsSorted(t *testing.T) {
	p := &toolsPicker{}
	p.open(tools.DefaultRegistry("/tmp"), nil)
	p.disabled["write"] = true
	p.disabled["bash"] = true
	p.disabled["edit"] = true
	want := []string{"bash", "edit", "write"}
	if got := p.pending(); !reflect.DeepEqual(got, want) {
		t.Fatalf("pending = %v, want %v", got, want)
	}
}

func TestToolsPickerMoveWraps(t *testing.T) {
	p := &toolsPicker{}
	p.open(tools.DefaultRegistry("/tmp"), nil)
	p.move(-1)
	if p.sel != len(p.names)-1 {
		t.Fatalf("sel = %d, want %d", p.sel, len(p.names)-1)
	}
	p.move(1)
	if p.sel != 0 {
		t.Fatalf("sel = %d, want 0", p.sel)
	}
}

func TestApplyToolTogglesRebuildsRegistryAndPersists(t *testing.T) {
	m := newToolsTestModel(t)

	var saved []string
	m.saveDisabledTools = func(disabled []string) error {
		saved = append([]string(nil), disabled...)
		return nil
	}

	m.applyToolToggles([]string{"write", "bash"})

	if !reflect.DeepEqual(saved, []string{"write", "bash"}) {
		t.Fatalf("saved = %v, want [write bash]", saved)
	}
	// disabledTools is the effective union, which is sorted for stability.
	if want := []string{"bash", "write"}; !reflect.DeepEqual(m.disabledTools, want) {
		t.Fatalf("disabledTools = %v, want %v", m.disabledTools, want)
	}
	if want := []string{"write", "bash"}; !reflect.DeepEqual(m.globalDisabledTools, want) {
		t.Fatalf("globalDisabledTools = %v, want %v", m.globalDisabledTools, want)
	}
	names := m.runner.cfg.Registry.Names()
	want := []string{"read", "edit"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("effective registry = %v, want %v", names, want)
	}
	if strings.Contains(m.runner.cfg.SystemPrompt, "- bash:") {
		t.Fatalf("system prompt still advertises bash:\n%s", m.runner.cfg.SystemPrompt)
	}
}

func TestApplyToolTogglesEmptyRestoresAllTools(t *testing.T) {
	m := newToolsTestModel(t)
	m.saveDisabledTools = func([]string) error { return nil }
	m.applyToolToggles([]string{"write"})
	m.applyToolToggles(nil)

	if len(m.disabledTools) != 0 {
		t.Fatalf("disabledTools = %v, want empty", m.disabledTools)
	}
	if got, want := len(m.runner.cfg.Registry.Names()), 4; got != want {
		t.Fatalf("registry size = %d, want %d", got, want)
	}
}

func TestApplyToolTogglesRefusedWhileWorking(t *testing.T) {
	m := newToolsTestModel(t)
	m.saveDisabledTools = func([]string) error { return nil }
	m.working = true

	m.applyToolToggles([]string{"bash"})

	if len(m.disabledTools) != 0 {
		t.Fatalf("disabledTools = %v, want unchanged while working", m.disabledTools)
	}
	if !strings.Contains(m.statusMsg, "Cancel the current run") {
		t.Fatalf("status = %q, want a refusal message", m.statusMsg)
	}
}

func TestApplyToolTogglesRollsBackOnSaveError(t *testing.T) {
	m := newToolsTestModel(t)
	m.saveDisabledTools = func([]string) error { return context.DeadlineExceeded }
	m.applyToolToggles([]string{"bash"})
	if m.disabledTools != nil {
		t.Fatalf("disabledTools = %v, want unchanged after save error", m.disabledTools)
	}
	if !strings.Contains(m.statusMsg, "Could not save tools") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestToolsCommandOpensPanelAndEscApplies(t *testing.T) {
	m := newToolsTestModel(t)
	var saved []string
	m.saveDisabledTools = func(disabled []string) error {
		saved = append([]string(nil), disabled...)
		return nil
	}

	// /tools opens the panel.
	if _, _ = m.runCommand("/tools"); !m.tools.active {
		t.Fatal("expected the tools panel to be active after /tools")
	}
	if len(m.tools.names) == 0 {
		t.Fatal("expected the panel to list tools")
	}

	// Disable bash, then esc stages the change and opens the scope panel.
	for i, n := range m.tools.names {
		if n == "bash" {
			m.tools.sel = i
		}
	}
	m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeySpace}))
	if !m.tools.isDisabled("bash") {
		t.Fatal("space did not toggle bash")
	}
	if _, _ = m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})); m.tools.active {
		t.Fatal("esc did not close the tools panel")
	}
	if !m.scope.active {
		t.Fatal("esc did not open the scope panel")
	}
	if saved != nil {
		t.Fatalf("staging wrote %v before a scope was chosen", saved)
	}

	// Choose Global (move down from the session default) and save.
	if _, _ = m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})); m.scope.choice().scope != scopeGlobal {
		t.Fatalf("down selected %v, want scopeGlobal", m.scope.choice().scope)
	}
	if _, _ = m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})); m.scope.active {
		t.Fatal("enter did not close the scope panel")
	}
	if !reflect.DeepEqual(saved, []string{"bash"}) {
		t.Fatalf("saved = %v, want [bash]", saved)
	}
	if got := m.runner.cfg.Registry.Names(); !reflect.DeepEqual(got, []string{"read", "write", "edit"}) {
		t.Fatalf("registry = %v, want [read write edit]", got)
	}
}

func TestToolsPickerEnabledCountIgnoresStaleEntries(t *testing.T) {
	p := &toolsPicker{}
	p.open(tools.DefaultRegistry("/tmp"), []string{"write", "stale-plugin-tool"})
	// Only "write" is in the registry; the stale name must not affect the count.
	if got, want := p.enabledCount(), 3; got != want {
		t.Fatalf("enabledCount = %d, want %d", got, want)
	}
	if s := p.disabledSummary(); !strings.Contains(s, "(3/4 enabled)") {
		t.Fatalf("summary = %q, want 3/4 enabled", s)
	}
}

func TestToolsPickerEnabledCountIgnoresProfileExcludedTools(t *testing.T) {
	// Simulates the picker opened over a profile-filtered registry (read+bash)
	// while the deny list still holds a tool the profile excludes (write).
	p := &toolsPicker{}
	p.open(tools.DefaultRegistry("/tmp").Without([]string{"write", "edit"}), []string{"write", "bash"})
	if got, want := p.enabledCount(), 1; got != want {
		t.Fatalf("enabledCount = %d, want %d", got, want)
	}
	if s := p.disabledSummary(); !strings.Contains(s, "(1/2 enabled)") {
		t.Fatalf("summary = %q, want 1/2 enabled", s)
	}
}
