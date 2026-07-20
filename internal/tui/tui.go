package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/myagent/myagent/internal/agent"
	"github.com/myagent/myagent/internal/agent/compaction"
	"github.com/myagent/myagent/internal/auth"
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
func Run(ctx context.Context, cfg agent.Config, persistedConfig *config.Config, authStore *auth.Store, catalog *modelcatalog.Catalog, sess *session.Session, history []types.Message, modelID, cwd string) (*session.Session, error) {
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
		return availableModelCandidates(catalog, persistedConfig, authStore)
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
		if authStore == nil {
			return false
		}
		_, ok := authStore.Get(name)
		return ok
	}
	m.providerAPIKey = func(name string) string {
		if persistedConfig == nil {
			return ""
		}
		if authStore == nil {
			return ""
		}
		credentials, _ := authStore.Get(name)
		return credentials.APIKey
	}
	m.configureProvider = func(provider modelcatalog.Provider, apiKey string) error {
		if persistedConfig == nil || authStore == nil {
			return fmt.Errorf("configuration is unavailable")
		}
		existing, _ := authStore.Get(provider.ID)
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
		return authStore.Set(provider.ID, auth.Credentials{APIKey: apiKey, BaseURL: baseURL})
	}
	m.selectModel = func(providerName, modelID string) (llm.Provider, llm.Model, error) {
		if persistedConfig == nil {
			return nil, llm.Model{}, fmt.Errorf("configuration is unavailable")
		}
		provider, model, err := persistedConfig.ResolveWithAuth(authStore, providerName, modelID, "")
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

func availableModelCandidates(catalog *modelcatalog.Catalog, cfg *config.Config, authStore *auth.Store) []modelcatalog.Model {
	if catalog == nil || cfg == nil {
		return nil
	}
	providers := make(map[string]struct{}, len(cfg.Providers))
	for name := range cfg.Providers {
		providers[name] = struct{}{}
	}
	if authStore != nil {
		for name := range authStore.Providers {
			providers[name] = struct{}{}
		}
	}

	models := catalog.Models(providers)
	seen := make(map[string]struct{}, len(models)+len(cfg.Providers))
	for _, model := range models {
		seen[model.Ref()] = struct{}{}
	}
	addCustom := func(provider, modelID string) {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			return
		}
		model := modelcatalog.Model{Provider: provider, ProviderName: provider, ID: modelID}
		if _, exists := seen[model.Ref()]; exists {
			return
		}
		seen[model.Ref()] = struct{}{}
		models = append(models, model)
	}
	for name, provider := range cfg.Providers {
		addCustom(name, provider.Model)
	}
	if provider, modelID, ok := strings.Cut(strings.TrimSpace(cfg.DefaultModel), "/"); ok {
		if _, custom := cfg.Providers[provider]; custom {
			addCustom(provider, modelID)
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Ref() < models[j].Ref() })
	return models
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
