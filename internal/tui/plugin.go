package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/AlvinPlayz23/myagent/internal/llm"
	"github.com/AlvinPlayz23/myagent/internal/plugin"
	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// extractPluginArgs pulls (*plugin.Bundle, profileName) out of Run's variadic
// tail. main.go passes (bundle, profileFlag).
func extractPluginArgs(args []any) (*plugin.Bundle, string) {
	var b *plugin.Bundle
	var profile string
	for _, a := range args {
		switch v := a.(type) {
		case *plugin.Bundle:
			b = v
		case string:
			profile = v
		}
	}
	return b, profile
}

// refreshPluginCommands rebuilds the command picker items and help text from
// built-ins + plugin bundle commands (+ /profile when profiles exist,
// + /plan sugar when a profile literally named "plan" exists).
func (m *model) refreshPluginCommands() {
	items := append([]commandItem(nil), commandItems...)
	if m.pluginBundle != nil && !m.pluginBundle.Disabled {
		for _, c := range m.pluginBundle.Commands {
			kind := commandPluginPrompt
			usage := c.Name + " [<args>]"
			if strings.TrimSpace(c.Run) != "" {
				kind = commandPluginRun
			}
			items = append(items, commandItem{
				name:        c.Name,
				usage:       usage,
				description: c.Description,
				kind:        kind,
			})
		}
		if len(m.pluginBundle.Profiles) > 0 {
			items = append(items, commandItem{
				name:        "/profile",
				usage:       "/profile [name|reset]",
				description: "Switch plugin profile (tool allowlist + instructions)",
				kind:        commandProfile,
				requiresArg: true,
			})
			if m.pluginBundle.Profile("plan") != nil {
				items = append(items, commandItem{
					name:        "/plan",
					usage:       "/plan [on|off]",
					description: "Toggle plan profile (sugar for /profile plan|reset)",
					kind:        commandProfile,
				})
			}
		}
	}
	m.picker.items = items
	m.pluginHelpExtra = buildPluginHelpExtra(items)
}

// dispatchPluginCommand handles plugin prompt/run commands and profile
// switching. Returns handled=true when the command was a plugin command.
func (m *model) dispatchPluginCommand(cmd slashCommand) (tea.Model, tea.Cmd, bool) {
	switch cmd.kind {
	case commandPluginPrompt:
		if m.pluginBundle == nil {
			return m, nil, true
		}
		def := m.pluginBundle.Command(cmd.name)
		if def == nil {
			m.statusMsg = "unknown command: " + cmd.name + " (try /help)"
			return m, nil, true
		}
		rendered, err := plugin.RenderCommand(def.Prompt, cmd.arg, m.cwd, false)
		if err != nil {
			m.statusMsg = "plugin " + def.Name + ": " + err.Error()
			return m, nil, true
		}
		mod, cmd2 := m.startRun(cmd.name+" "+cmd.arg, userMessage(rendered))
		return mod, cmd2, true
	case commandPluginRun:
		if m.pluginBundle == nil {
			return m, nil, true
		}
		def := m.pluginBundle.Command(cmd.name)
		if def == nil {
			m.statusMsg = "unknown command: " + cmd.name + " (try /help)"
			return m, nil, true
		}
		return m, m.runPluginRun(def, cmd.arg), true
	case commandProfile:
		if cmd.name == "/plan" {
			switch strings.ToLower(strings.TrimSpace(cmd.arg)) {
			case "", "on", "plan":
				if err := m.applyProfile("plan"); err != nil {
					m.statusMsg = err.Error()
				}
			case "off", "reset":
				if err := m.applyProfile("reset"); err != nil {
					m.statusMsg = err.Error()
				}
			default:
				m.statusMsg = "usage: /plan [on|off]"
			}
			return m, nil, true
		}
		arg := strings.TrimSpace(cmd.arg)
		if arg == "" {
			// Bare /profile: list available profiles.
			if m.pluginBundle == nil || len(m.pluginBundle.Profiles) == 0 {
				m.statusMsg = "no profiles available"
				return m, nil, true
			}
			names := m.pluginBundle.ProfileNames()
			cur := m.activeProfile
			if cur == "" {
				cur = "(none)"
			}
			m.transcript.addNotice("Profiles (active: " + cur + "):\n  " + strings.Join(names, "\n  "))
			m.refreshViewport()
			return m, nil, true
		}
		if err := m.applyProfile(arg); err != nil {
			m.statusMsg = err.Error()
		}
		return m, nil, true
	}
	return m, nil, false
}

// applyProfile activates (or resets with "" / "reset") a plugin profile.
// It filters the registry, rebuilds the system prompt, applies effort, and
// wraps bash with bashDeny. Refuses while a run is active.
func (m *model) applyProfile(name string) error {
	if m.working {
		return fmt.Errorf("Cancel the current run before switching profiles.")
	}
	if name == "" || name == "reset" {
		m.runner.setTools(m.baseRegistry, m.basePrompt)
		m.runner.setEffort(m.baseEffort)
		m.activeProfile = ""
		m.statusMsg = "Profile reset."
		m.refreshViewport()
		return nil
	}
	if m.pluginBundle == nil {
		return fmt.Errorf("no profiles available")
	}
	applied, err := plugin.Apply(m.pluginBundle, m.baseRegistry, m.basePrompt, m.cwd, name)
	if err != nil {
		return err
	}
	m.runner.setTools(applied.Registry, applied.SystemPrompt)
	if applied.Effort != "" {
		if eff, nerr := llm.NormalizeEffort(m.runner.cfg.Model, applied.Effort); nerr == nil {
			m.runner.setEffort(eff)
		} else {
			// Unsupported effort for this model: fall back to the provider
			// default, mirroring applyModel/SetModel. Never apply the raw
			// value past per-model validation.
			m.runner.setEffort("")
		}
	}
	m.activeProfile = name
	m.statusMsg = "Profile: " + name
	m.refreshViewport()
	return nil
}

// pluginRunResultMsg carries the outcome of an async `run` command back to
// the Update loop so long-running shell commands never block the UI.
type pluginRunResultMsg struct {
	text  string
	isErr bool
}

// runPluginRun executes a `run` command template locally with timeout. The
// shell execution runs as a tea.Cmd so the TUI stays responsive (spinner,
// esc/abort of agent runs); the result renders as a transcript block when it
// completes. Template/deny failures are reported synchronously with a nil Cmd.
func (m *model) runPluginRun(def *plugin.CommandDef, rawArg string) tea.Cmd {
	rendered, err := plugin.RenderCommand(def.Run, rawArg, m.cwd, false)
	if err != nil {
		m.statusMsg = "plugin " + def.Name + ": " + err.Error()
		return nil
	}
	if deny := m.activeDeny(); deny != nil {
		if err := deny(rendered); err != nil {
			m.transcript.addErrorText("Error: " + err.Error())
			m.refreshViewport()
			return nil
		}
	}
	timeoutMs := def.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}
	ctx, cancel := context.WithTimeout(m.ctx, time.Duration(timeoutMs)*time.Millisecond)
	cwd := m.cwd
	return func() tea.Msg {
		defer cancel()
		inner := &tools.BashTool{Cwd: cwd}
		res, err := inner.Execute(ctx, "", map[string]any{
			"command": rendered,
			"timeout": float64(timeoutMs) / 1000.0,
		})
		if err != nil {
			return pluginRunResultMsg{text: "Error: " + err.Error(), isErr: true}
		}
		text := ""
		if res != nil && len(res.Content) > 0 {
			for _, b := range res.Content {
				if b.Type == types.ContentText {
					text += b.Text
				}
			}
		}
		if strings.TrimSpace(text) == "" {
			text = "(no output)"
		}
		return pluginRunResultMsg{text: text}
	}
}

// activeDeny returns the compiled deny check for the active profile, if any.
func (m *model) activeDeny() func(string) error {
	if m.activeProfile == "" || m.pluginBundle == nil {
		return nil
	}
	def := m.pluginBundle.Profile(m.activeProfile)
	if def == nil || len(def.BashDeny) == 0 {
		return nil
	}
	deny, err := plugin.CompileDeny(def.BashDeny)
	if err != nil {
		return nil
	}
	return deny
}

func buildPluginHelpExtra(items []commandItem) string {
	var b strings.Builder
	for _, item := range items {
		if item.kind != commandPluginPrompt && item.kind != commandPluginRun && item.kind != commandProfile {
			continue
		}
		fmt.Fprintf(&b, "  %-21s %s\n", item.usage, item.description)
	}
	return b.String()
}
