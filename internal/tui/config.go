package tui

import (
	"github.com/AlvinPlayz23/myagent/internal/config"
	"github.com/AlvinPlayz23/myagent/internal/llm"
)

// configurePersistedSettings uses the startup snapshot only for display.
// Each save merges its setting into the current on-disk config instead.
func (m *model) configurePersistedSettings(persisted *config.Config) {
	if persisted == nil {
		return
	}
	m.welcomeStyle = normalizeWelcomeStyle(persisted.WelcomeStyle)
	m.saveWelcomeStyle = func(style welcomeStyle) error {
		return config.Update(func(cfg *config.Config) error {
			cfg.WelcomeStyle = string(style)
			return nil
		})
	}
	m.promptStyle = normalizePromptStyle(persisted.PromptStyle)
	m.savePromptStyle = func(style promptStyle) error {
		return config.Update(func(cfg *config.Config) error {
			cfg.PromptStyle = string(style)
			return nil
		})
	}
	if parsed, err := llm.ParseEffort(persisted.DefaultEffort); err == nil {
		m.defaultEffort = parsed
	}
	m.saveDefaultEffort = func(effort llm.Effort) error {
		return config.Update(func(cfg *config.Config) error {
			cfg.DefaultEffort = string(effort)
			return nil
		})
	}
	m.globalDisabledTools = append([]string(nil), persisted.DisabledTools...)
	m.disabledTools = append([]string(nil), persisted.DisabledTools...)
	m.saveDisabledTools = func(disabled []string) error {
		return config.Update(func(cfg *config.Config) error {
			cfg.DisabledTools = append([]string(nil), disabled...)
			return nil
		})
	}
}
