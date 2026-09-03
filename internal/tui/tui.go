package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/agent/compaction"
	"github.com/AlvinPlayz23/myagent/internal/auth"
	"github.com/AlvinPlayz23/myagent/internal/config"
	"github.com/AlvinPlayz23/myagent/internal/export"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/session"
	"github.com/AlvinPlayz23/myagent/internal/terminal"
	"github.com/AlvinPlayz23/myagent/internal/titlegen"
	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// Run starts the interactive pager. It drives the agent loop over the given
// config and prior history, persisting every produced message to sess as it
// completes. It returns the active session when the user quits, which may be
// a session created through /new.
func Run(ctx context.Context, cfg agent.Config, persistedConfig *config.Config, authStore *auth.Store, catalog *modelcatalog.Catalog, sess *session.Session, history []types.Message, modelID, cwd string) (*session.Session, error) {
	term, err := engine.OpenTerminal()
	if err != nil {
		return nil, err
	}
	if err := term.Raw(); err != nil {
		term.Restore()
		return nil, err
	}
	term.Mouse = true
	term.Enter()
	defer term.Restore()

	a := newApp(ctx, cfg, history, modelID, cwd)
	a.term = term
	a.input = engine.NewDecoder(term.Input()).Events()
	a.r.bindApp(a)

	if err := setupApp(a, persistedConfig, authStore, catalog, &sess, history, modelID); err != nil {
		return sess, err
	}

	a.updateTerminalTitle()
	defer terminal.SetTitle("myagent")

	if err := a.loop(); err != nil {
		return sess, err
	}
	return sess, nil
}

// setupApp injects every environment seam into the app, mirroring the
// previous model's wiring one-to-one.
func setupApp(a *app, persistedConfig *config.Config, authStore *auth.Store, catalog *modelcatalog.Catalog, sess **session.Session, history []types.Message, modelID string) error {
	a.setTerminalTitle = terminal.SetTitle
	if *sess != nil {
		a.sessionTitle = (*sess).Title()
	}
	a.hasSessionTitle = a.sessionTitle != "" && a.sessionTitle != "new"
	if persistedConfig != nil {
		a.welcomeSty = normalizeWelcomeStyle(persistedConfig.WelcomeStyle)
		a.saveWelcomeStyle = func(style welcomeStyle) error {
			previous := persistedConfig.WelcomeStyle
			persistedConfig.WelcomeStyle = string(style)
			if err := config.Save(persistedConfig); err != nil {
				persistedConfig.WelcomeStyle = previous
				return err
			}
			return nil
		}
		a.promptSty = normalizePromptStyle(persistedConfig.PromptStyle)
		a.savePromptStyle = func(style promptStyle) error {
			previous := persistedConfig.PromptStyle
			persistedConfig.PromptStyle = string(style)
			if err := config.Save(persistedConfig); err != nil {
				persistedConfig.PromptStyle = previous
				return err
			}
			return nil
		}
	}
	if agent.HasRepositoryGuidance(a.cwd) {
		a.statusMsg = "Loaded AGENTS.md"
	}
	a.availableModels = func() []modelcatalog.Model {
		return availableModelCandidates(catalog, persistedConfig, authStore)
	}
	a.discoverModels = func(ctx context.Context, provider string) ([]string, error) {
		return discoverProviderModels(ctx, catalog, persistedConfig, authStore, provider)
	}
	a.rememberDiscoveredModels = func(provider string, ids []string) {
		if catalog == nil {
			return
		}
		catalog.RememberDiscovery(provider, ids, time.Now())
		ids = append([]string(nil), ids...)
		go func() {
			// Persistence is deliberately off the render loop.
			_ = catalog.SetCustomModels(provider, provider, ids)
		}()
	}
	a.availableProviders = func() []modelcatalog.Provider {
		if catalog == nil {
			return nil
		}
		return catalog.Providers()
	}
	a.providerConfigured = func(name string) bool {
		if persistedConfig == nil || authStore == nil {
			return false
		}
		_, ok := authStore.Get(name)
		return ok
	}
	a.providerIsCustom = func(name string) bool {
		if persistedConfig == nil {
			return false
		}
		_, ok := persistedConfig.Providers[name]
		return ok
	}
	a.providerAPIKey = func(name string) string {
		if persistedConfig == nil || authStore == nil {
			return ""
		}
		credentials, _ := authStore.Get(name)
		return credentials.APIKey
	}
	a.configureProvider = func(provider modelcatalog.Provider, apiKey string) error {
		if persistedConfig == nil || authStore == nil {
			return fmt.Errorf("configuration is unavailable")
		}
		if _, custom := persistedConfig.Providers[provider.ID]; custom {
			return fmt.Errorf("provider %q is managed as a custom provider", provider.Name)
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
	a.selectModel = func(providerName, modelID string) (llm.Provider, llm.Model, error) {
		if persistedConfig == nil {
			return nil, llm.Model{}, fmt.Errorf("configuration is unavailable")
		}
		provider, model, err := persistedConfig.ResolveWithAuth(authStore, providerName, modelID, "")
		if err != nil {
			return nil, llm.Model{}, err
		}
		if catalog != nil {
			model = catalog.Enrich(model)
		}
		persistedConfig.DefaultModel = providerName + "/" + modelID
		if err := config.Save(persistedConfig); err != nil {
			return nil, llm.Model{}, err
		}
		return provider, model, nil
	}
	a.listSessions = func() ([]session.Info, error) {
		return session.List()
	}
	a.currentSessionID = func() string {
		if *sess == nil {
			return ""
		}
		return (*sess).ID()
	}
	a.resumeSession = func(id string) ([]types.Message, error) {
		resumed, err := session.ResumeByID(id)
		if err != nil {
			return nil, err
		}
		if *sess != nil {
			if err := (*sess).Close(); err != nil {
				_ = resumed.Close()
				return nil, err
			}
		}
		*sess = resumed
		history := resumed.Messages()
		return history, nil
	}
	a.exportSession = func(format export.Format, name string, overwrite bool) (string, error) {
		return export.Write(*sess, format, name, overwrite)
	}
	a.renameSession = func(title string) error {
		if *sess == nil {
			return fmt.Errorf("no active session")
		}
		return (*sess).SetTitle(title)
	}
	a.newSession = func() error {
		newSess, err := session.Create(a.cwd)
		if err != nil {
			return err
		}
		if *sess != nil {
			if err := (*sess).Close(); err != nil {
				_ = newSess.Close()
				return err
			}
		}
		*sess = newSess
		return nil
	}

	a.r.generateTitle = func(parent context.Context, prompt string) (string, error) {
		if *sess == nil || (*sess).Title() != "new" {
			return "", nil
		}
		titleCtx, cancel := context.WithTimeout(parent, 4*time.Second)
		defer cancel()
		title, err := titlegen.Generate(titleCtx, a.r.cfg.Provider, a.r.cfg.Model, prompt)
		if err != nil {
			return "", err
		}
		if err := (*sess).SetGeneratedTitle(title); err != nil {
			return "", err
		}
		return title, nil
	}

	a.clipboardRead = readNativeClipboard

	// Persist produced messages and compactions by intercepting every event
	// on the loop goroutine, before it reaches the UI.
	a.r.onEvent = func(ev types.AgentEvent) error {
		if *sess == nil {
			return nil
		}
		switch ev.Type {
		case types.EventMessageEnd:
			if ev.Message != nil {
				return (*sess).AppendMessage(*ev.Message)
			}
		case types.EventCompactionEnd:
			if ev.Compaction != nil && ev.Message != nil {
				return (*sess).ApplyCompaction(*ev.Compaction, *ev.Message)
			}
		}
		return nil
	}

	seedScrollback(a.sb, history)
	a.gitBranch = readGitBranch(a.cwd)
	return nil
}

// seedScrollback renders prior history into the scrollback on resume.
func seedScrollback(t *scrollback, history []types.Message) {
	for _, msg := range history {
		switch msg.Role {
		case types.RoleUser:
			if compaction.IsSummaryMessage(msg) {
				t.addNotice("∼ " + textOf(msg))
				continue
			}
			t.addUser(textOf(msg))
		case types.RoleAssistant:
			// Walk content blocks so thinking and text interleave in their
			// original order rather than collapsing into one text block.
			var pending strings.Builder
			flushText := func() {
				if pending.Len() == 0 {
					return
				}
				t.beginAssistant()
				t.appendAssistantDelta(pending.String())
				t.endAssistant()
				pending.Reset()
			}
			for _, c := range msg.Content {
				switch c.Type {
				case types.ContentThinking:
					flushText()
					if strings.TrimSpace(c.Thinking) == "" {
						continue
					}
					t.beginThinking()
					t.appendThinkingDelta(c.Thinking)
					t.endThinking()
				case types.ContentText:
					if c.Text == "" {
						continue
					}
					if pending.Len() > 0 {
						pending.WriteByte('\n')
					}
					pending.WriteString(c.Text)
				}
			}
			flushText()
			for _, tc := range msg.ToolCalls() {
				t.startTool(tc.ID, tc.Name, tc.Arguments)
			}
		case types.RoleToolResult:
			t.endTool(msg.ToolCallID, &types.ToolResult{Content: msg.Content}, msg.IsError)
		}
	}
}

// discoverProviderModels queries the provider's own /v1/models endpoint so
// the picker can offer models that ship before the catalog knows about them.
// The active picker owns accepting, caching, and persisting a response.
func discoverProviderModels(ctx context.Context, catalog *modelcatalog.Catalog, cfg *config.Config, authStore *auth.Store, provider string) ([]string, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil, fmt.Errorf("no active provider")
	}
	if catalog != nil {
		if ids, ok := catalog.CachedDiscovery(provider, time.Now()); ok {
			return ids, nil
		}
	}
	baseURL, apiKey, err := config.ResolveProviderEndpoint(cfg, authStore, provider)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ids, err := llm.ListOpenAIModels(ctx, nil, apiKey, baseURL)
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// readGitBranch returns the checked-out branch name, or "" when the repo
// state cannot be read cheaply.
func readGitBranch(cwd string) string {
	head := findGitHead(cwd)
	if head == "" {
		return ""
	}
	data, err := os.ReadFile(head)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if ref, ok := strings.CutPrefix(line, "ref: refs/heads/"); ok {
		return ref
	}
	return ""
}

// findGitHead walks up to the nearest .git/HEAD.
func findGitHead(cwd string) string {
	dir := cwd
	for {
		head := dir + "/.git/HEAD"
		if _, err := os.Stat(head); err == nil {
			return head
		}
		parent := parentDir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func parentDir(dir string) string {
	for i := len(dir) - 1; i > 0; i-- {
		if dir[i] == '/' || dir[i] == '\\' {
			return dir[:i]
		}
	}
	return dir
}

// setSessionTitle applies a generated title.
func (a *app) setSessionTitle(title string) {
	a.sessionTitle = title
	a.hasSessionTitle = title != "" && title != "new"
	a.updateTerminalTitle()
}

// textOf flattens a message's text blocks.
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

// availableModelCandidates unions catalog models with custom provider models.
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
