package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/agent/compaction"
	"github.com/myagent/myagent/internal/config"
	"github.com/myagent/myagent/internal/llm"
	modelcatalog "github.com/myagent/myagent/internal/models"
	"github.com/myagent/myagent/internal/session"
	"github.com/myagent/myagent/internal/types"
)

// Run starts the interactive TUI. It drives the agent loop over the given
// config and prior history, persisting every produced message to sess as it
// completes. It returns the active session when the user quits, which may be a
// session created through /new.
func Run(ctx context.Context, cfg agent.Config, persistedConfig *config.Config, catalog *modelcatalog.Catalog, sess *session.Session, history []types.Message, modelID, cwd string) (*session.Session, error) {
	queue := newMsgQueue()
	r := newRunner(cfg, queue, history)

	th := newTheme()
	md := newMDRenderer()
	m := newModel(ctx, r, queue, th, md, modelID, cwd, func() error {
		newSess, err := session.Create(cwd)
		if err != nil {
			return err
		}
		if sess != nil {
			if err := sess.Close(); err != nil {
				_ = newSess.Close()
				return err
			}
		}
		sess = newSess
		r.reset()
		return nil
	})
	m.availableModels = func() []modelcatalog.Model {
		if catalog == nil || persistedConfig == nil {
			return nil
		}
		providers := make(map[string]struct{}, len(persistedConfig.Providers))
		for name := range persistedConfig.Providers {
			providers[name] = struct{}{}
		}
		return catalog.Models(providers)
	}
	m.availableProviders = func() []modelcatalog.Provider {
		if catalog == nil {
			return nil
		}
		return catalog.Providers()
	}
	m.providerConfigured = func(name string) bool {
		if persistedConfig == nil {
			return false
		}
		provider, ok := persistedConfig.Providers[name]
		return ok && provider.APIKey != ""
	}
	m.providerAPIKey = func(name string) string {
		if persistedConfig == nil {
			return ""
		}
		return persistedConfig.Providers[name].APIKey
	}
	m.configureProvider = func(provider modelcatalog.Provider, apiKey string) error {
		if persistedConfig == nil {
			return fmt.Errorf("configuration is unavailable")
		}
		if persistedConfig.Providers == nil {
			persistedConfig.Providers = make(map[string]config.ProviderConfig)
		}
		existing := persistedConfig.Providers[provider.ID]
		baseURL := provider.BaseURL
		if baseURL == "" {
			if preset, ok := config.Preset(provider.ID); ok {
				baseURL = preset.BaseURL
			}
		}
		if baseURL == "" {
			return fmt.Errorf("provider %q has no compatible endpoint metadata; refresh the catalog and try again", provider.Name)
		}
		if existing.BaseURL != "" {
			baseURL = existing.BaseURL
		}
		persistedConfig.Providers[provider.ID] = config.ProviderConfig{
			Type:    config.DefaultProviderType,
			APIKey:  apiKey,
			BaseURL: baseURL,
			Model:   existing.Model,
		}
		return config.Save(persistedConfig)
	}
	m.selectModel = func(providerName, modelID string) (llm.Provider, llm.Model, error) {
		if persistedConfig == nil {
			return nil, llm.Model{}, fmt.Errorf("configuration is unavailable")
		}
		provider, model, err := persistedConfig.Resolve(providerName, modelID, "")
		if err != nil {
			return nil, llm.Model{}, err
		}
		persistedConfig.DefaultModel = providerName + "/" + modelID
		if err := config.Save(persistedConfig); err != nil {
			return nil, llm.Model{}, err
		}
		return provider, model, nil
	}
	m.listSessions = func() ([]session.Info, error) {
		infos, err := session.List()
		if err != nil || sess == nil {
			return infos, err
		}
		available := infos[:0]
		for _, info := range infos {
			if info.ID != sess.ID() {
				available = append(available, info)
			}
		}
		return available, nil
	}
	m.resumeSession = func(id string) ([]types.Message, error) {
		resumed, err := session.ResumeByID(id)
		if err != nil {
			return nil, err
		}
		if sess != nil {
			if err := sess.Close(); err != nil {
				_ = resumed.Close()
				return nil, err
			}
		}
		sess = resumed
		history := resumed.Messages()
		return history, nil
	}

	// Seed the transcript with prior conversation so resumed sessions show
	// their history.
	seedTranscript(m.transcript, history)

	// Persist produced messages and compactions by intercepting every event
	// on the loop goroutine, before it reaches the UI. This keeps the session
	// file in sync with the loop's in-memory history so that compaction's
	// FirstKeptIndex maps correctly to session entry ids.
	r.onEvent = func(ev types.AgentEvent) error {
		if sess == nil {
			return nil
		}
		switch ev.Type {
		case types.EventMessageEnd:
			if ev.Message != nil {
				return sess.AppendMessage(*ev.Message)
			}
		case types.EventCompactionEnd:
			if ev.Compaction != nil && ev.Message != nil {
				return sess.ApplyCompaction(*ev.Compaction, *ev.Message)
			}
		}
		return nil
	}

	p := tea.NewProgram(m, tea.WithContext(ctx))
	_, err := p.Run()
	return sess, err
}

// seedTranscript renders prior history into the transcript on resume.
func seedTranscript(t *transcript, history []types.Message) {
	for _, msg := range history {
		switch msg.Role {
		case types.RoleUser:
			if compaction.IsSummaryMessage(msg) {
				t.addNotice("∼ " + textOf(msg))
				continue
			}
			t.addUser(textOf(msg))
		case types.RoleAssistant:
			if txt := textOf(msg); txt != "" {
				t.beginAssistant()
				t.appendAssistantDelta(txt)
				t.endAssistant()
			}
			for _, tc := range msg.ToolCalls() {
				t.startTool(tc.ID, tc.Name, tc.Arguments)
			}
		case types.RoleToolResult:
			t.endTool(msg.ToolCallID, &types.ToolResult{Content: msg.Content}, msg.IsError)
		}
	}
}

func textOf(m types.Message) string {
	var parts []string
	for _, c := range m.Content {
		if c.Type == types.ContentText && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += "\n" + p
	}
	return out
}
