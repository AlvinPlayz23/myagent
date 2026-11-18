package tui

import (
	"context"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/plugin"
	"github.com/AlvinPlayz23/myagent/internal/tools"
)

func testBundle() *plugin.Bundle {
	return plugin.LoadBytes(
		[]byte(`{"commands":[{"name":"/poem","description":"poem","prompt":"poem about {{$args}}"}],"profiles":[{"name":"plan","description":"plan mode","tools":["read","bash"],"instructions":"PLAN MODE"}]}`),
		nil,
	)
}

func TestPluginPromptCommandStartsRun(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, nil)
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "/tmp")
	m.pluginBundle = testBundle()
	if r.cfg.Registry == nil {
		r.cfg.Registry = tools.DefaultRegistry("/tmp")
	}
	m.baseRegistry = r.cfg.Registry
	m.basePrompt = r.cfg.SystemPrompt
	m.baseEffort = r.cfg.Effort
	m.refreshPluginCommands()

	cmd, err := parseSlashCommandWithPlugins("/poem the sea", m.pluginBundle)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.kind != commandPluginPrompt || cmd.arg != "the sea" {
		t.Fatalf("cmd = %+v", cmd)
	}
	mod, c, handled := m.dispatchPluginCommand(cmd)
	if !handled || c == nil {
		t.Fatalf("handled=%v cmd=%v", handled, c)
	}
	_ = mod
	if m.activePrompt == nil {
		t.Fatal("expected active prompt")
	}
}

func TestProfileCommandAndPlanSugar(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, nil)
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "/tmp")
	m.pluginBundle = testBundle()
	if r.cfg.Registry == nil {
		r.cfg.Registry = tools.DefaultRegistry("/tmp")
	}
	m.baseRegistry = r.cfg.Registry
	m.basePrompt = "base"
	r.cfg.SystemPrompt = "base"
	m.baseEffort = r.cfg.Effort
	m.refreshPluginCommands()

	cmd, err := parseSlashCommandWithPlugins("/plan", m.pluginBundle)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.kind != commandProfile {
		t.Fatalf("cmd = %+v", cmd)
	}
	if _, _, handled := m.dispatchPluginCommand(cmd); !handled {
		t.Fatal("plan not handled")
	}
	if m.activeProfile != "plan" {
		t.Fatalf("active = %q", m.activeProfile)
	}
	// Re-entering bare /plan toggles plan off.
	if _, _, handled := m.dispatchPluginCommand(cmd); !handled {
		t.Fatal("second /plan not handled")
	}
	if m.activeProfile != "" {
		t.Fatalf("after toggle active = %q, want none", m.activeProfile)
	}
	// /profile <name> toggles the same way when that profile is active.
	m.runCommand("/profile plan")
	if m.activeProfile != "plan" {
		t.Fatalf("after /profile plan active = %q", m.activeProfile)
	}
	m.runCommand("/profile plan")
	if m.activeProfile != "" {
		t.Fatalf("after /profile plan re-entry active = %q, want none", m.activeProfile)
	}
	// Explicit reset still works from either state.
	m.runCommand("/plan on")
	if m.activeProfile != "plan" {
		t.Fatalf("after /plan on active = %q", m.activeProfile)
	}
	m.runCommand("/profile reset")
	if m.activeProfile != "" {
		t.Fatalf("after reset active = %q", m.activeProfile)
	}
}

func TestPlanOnOff(t *testing.T) {
	cases := []struct {
		input string
		pre   string
		want  string
		usage bool
	}{
		{"/plan", "", "plan", false},
		{"/plan", "plan", "", false},
		{"/plan on", "", "plan", false},
		{"/plan on", "plan", "plan", false},
		{"/plan off", "plan", "", false},
		{"/plan off", "", "", false},
		{"/plan bogus", "", "", true},
	}
	for _, tc := range cases {
		q := newMsgQueue()
		r := newRunner(agent.Config{}, q, nil)
		m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "/tmp")
		m.pluginBundle = testBundle()
		if r.cfg.Registry == nil {
			r.cfg.Registry = tools.DefaultRegistry("/tmp")
		}
		m.baseRegistry = r.cfg.Registry
		m.basePrompt = r.cfg.SystemPrompt
		m.baseEffort = r.cfg.Effort
		m.refreshPluginCommands()
		if tc.pre != "" {
			if err := m.applyProfile(tc.pre); err != nil {
				t.Fatal(err)
			}
		}
		cmd, err := parseSlashCommandWithPlugins(tc.input, m.pluginBundle)
		if err != nil {
			t.Fatalf("%s: %v", tc.input, err)
		}
		if _, _, handled := m.dispatchPluginCommand(cmd); !handled {
			t.Fatalf("%s not handled", tc.input)
		}
		if tc.usage {
			if m.statusMsg != "usage: /plan [on|off]" {
				t.Fatalf("%s: status = %q", tc.input, m.statusMsg)
			}
			continue
		}
		if m.activeProfile != tc.want {
			t.Fatalf("%s (pre %q): active = %q, want %q", tc.input, tc.pre, m.activeProfile, tc.want)
		}
	}
}

func TestUnknownProfileError(t *testing.T) {
	q := newMsgQueue()
	r := newRunner(agent.Config{}, q, nil)
	m := newModel(context.Background(), r, q, newTheme(), newMDRenderer(), "model", "/tmp")
	m.pluginBundle = testBundle()
	if r.cfg.Registry == nil {
		r.cfg.Registry = tools.DefaultRegistry("/tmp")
	}
	m.baseRegistry = r.cfg.Registry
	m.basePrompt = r.cfg.SystemPrompt
	m.refreshPluginCommands()
	m.runCommand("/profile nope")
	if m.statusMsg == "" {
		t.Fatal("expected error status")
	}
}
