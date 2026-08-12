package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/AlvinPlayz23/myagent/internal/auth"
	"github.com/AlvinPlayz23/myagent/internal/config"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/server/ws"
)

// providerService is the server-owned source of truth for provider settings.
// All mutations are serialized, and credentials are only accepted as input.
type providerService struct {
	mu      sync.Mutex
	cfg     *config.Config
	auth    *auth.Store
	catalog *modelcatalog.Catalog
}

func newProviderService(cfg *config.Config, authStore *auth.Store, catalog *modelcatalog.Catalog) *providerService {
	return &providerService{cfg: cfg, auth: authStore, catalog: catalog}
}

func (s *providerService) Resolve(provider, model, baseURL string) (llm.Provider, llm.Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, m, err := s.cfg.ResolveWithAuth(s.auth, provider, model, baseURL)
	if err != nil {
		return nil, llm.Model{}, err
	}
	if s.catalog != nil {
		m = s.catalog.Enrich(m)
	}
	m.ProviderOrigin = s.providerOrigin(m.Provider)
	return p, m, nil
}

func (s *providerService) providerOrigin(name string) string {
	if s.catalog == nil {
		return s.configuredOrigin(name)
	}
	_, configured := s.cfg.Providers[name]
	_, preset := config.Preset(name)
	if configured && (s.catalog.IsBuiltinProvider(name) || preset) {
		return "builtin_override"
	}
	if configured {
		return "custom"
	}
	if s.catalog.IsBuiltinProvider(name) {
		return "builtin"
	}
	if _, ok := config.Preset(name); ok {
		return "builtin"
	}
	return "custom"
}

func (s *providerService) configuredOrigin(name string) string {
	if _, configured := s.cfg.Providers[name]; configured {
		return "custom"
	}
	if _, ok := config.Preset(name); ok {
		return "builtin"
	}
	return "custom"
}

func (s *providerService) List() (ws.ProviderList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listLocked(), nil
}

func (s *providerService) listLocked() ws.ProviderList {
	models := map[string]map[string]struct{}{}
	if s.catalog != nil {
		configured := map[string]struct{}{}
		for name := range s.cfg.Providers {
			configured[name] = struct{}{}
		}
		for name := range s.auth.Providers {
			configured[name] = struct{}{}
		}
		for _, entry := range s.catalog.Models(configured) {
			if models[entry.Provider] == nil {
				models[entry.Provider] = map[string]struct{}{}
			}
			models[entry.Provider][entry.ID] = struct{}{}
		}
	}
	for name, p := range s.cfg.Providers {
		if p.Model != "" {
			if models[name] == nil {
				models[name] = map[string]struct{}{}
			}
			models[name][p.Model] = struct{}{}
		}
	}
	if name, model, ok := providerRef(s.cfg.DefaultModel); ok {
		if models[name] == nil {
			models[name] = map[string]struct{}{}
		}
		models[name][model] = struct{}{}
	}

	entries := make([]ws.ProviderRecord, 0, len(s.cfg.Providers)+len(s.auth.Providers))
	seen := map[string]bool{}
	for name, p := range s.cfg.Providers {
		ids := sortedModels(models[name])
		entries = append(entries, ws.ProviderRecord{Name: name, Models: ids, ModelDetails: s.modelDetails(name, ids), Source: "config", Origin: s.providerOrigin(name), BaseURL: p.BaseURL, ReasoningDialect: p.ReasoningDialect, HasAPIKey: p.APIKey != ""})
		seen[name] = true
	}
	for name, credential := range s.auth.Providers {
		if seen[name] {
			continue
		}
		ids := sortedModels(models[name])
		entries = append(entries, ws.ProviderRecord{Name: name, Models: ids, ModelDetails: s.modelDetails(name, ids), Source: "auth", Origin: s.providerOrigin(name), BaseURL: credential.BaseURL, HasAPIKey: credential.APIKey != ""})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	available := []ws.ProviderOption{}
	if s.catalog != nil {
		for _, p := range s.catalog.Providers() {
			available = append(available, ws.ProviderOption{Name: p.ID, Label: p.Name, BaseURL: p.BaseURL})
		}
	}
	return ws.ProviderList{Providers: entries, Available: available, DefaultModel: s.cfg.DefaultModel}
}

func (s *providerService) modelDetails(provider string, ids []string) []ws.ModelRecord {
	details := make([]ws.ModelRecord, 0, len(ids))
	for _, id := range ids {
		record := ws.ModelRecord{ID: id}
		if s.catalog != nil {
			if model, ok := s.catalog.FindBuiltinModel(provider, id); ok {
				record.ReasoningKnown, record.Reasoning = true, model.Reasoning
				if model.Reasoning {
					record.SupportedEfforts = []string{"low", "medium", "high", "xhigh", "max"}
				} else {
					record.SupportedEfforts = nil
				}
			}
		}
		details = append(details, record)
	}
	return details
}

func sortedModels(set map[string]struct{}) []string {
	items := make([]string, 0, len(set))
	for id := range set {
		items = append(items, id)
	}
	sort.Strings(items)
	return items
}

func providerRef(ref string) (string, string, bool) {
	name, model, ok := strings.Cut(strings.TrimSpace(ref), "/")
	return name, model, ok && name != "" && model != ""
}

func validateProviderInput(p ws.ProviderInput) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || strings.Contains(p.Name, "/") || strings.ContainsAny(p.Name, " \t\r\n") {
		return fmt.Errorf("provider name must be non-empty and cannot contain spaces or '/'")
	}
	if strings.TrimSpace(p.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if _, err := llm.ParseReasoningDialect(p.ReasoningDialect); err != nil {
		return err
	}
	return nil
}

func (s *providerService) Save(in ws.ProviderInput) (ws.ProviderList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateProviderInput(in); err != nil {
		return ws.ProviderList{}, err
	}
	name, model := strings.TrimSpace(in.Name), strings.TrimSpace(in.Model)
	if in.Builtin {
		known := s.catalog != nil && s.catalog.IsBuiltinProvider(name)
		if !known {
			if _, ok := config.Preset(name); !ok {
				return ws.ProviderList{}, fmt.Errorf("provider %q is not a known built-in provider", name)
			}
		}
		baseURL := strings.TrimSpace(in.BaseURL)
		if baseURL == "" {
			if preset, ok := config.Preset(name); ok {
				baseURL = preset.BaseURL
			}
		}
		if baseURL == "" {
			return ws.ProviderList{}, fmt.Errorf("base URL is required")
		}
		existing, _ := s.auth.Get(name)
		key := strings.TrimSpace(in.APIKey)
		if key == "" {
			key = existing.APIKey
		}
		if key == "" {
			return ws.ProviderList{}, fmt.Errorf("API key is required")
		}
		if _, custom := s.cfg.Providers[name]; custom {
			return ws.ProviderList{}, fmt.Errorf("provider %q is managed as a custom provider", name)
		}
		if err := s.auth.Set(name, auth.Credentials{APIKey: key, BaseURL: baseURL}); err != nil {
			return ws.ProviderList{}, err
		}
	} else {
		baseURL := strings.TrimSpace(in.BaseURL)
		if baseURL == "" {
			return ws.ProviderList{}, fmt.Errorf("base URL is required")
		}
		if s.cfg.Providers == nil {
			s.cfg.Providers = map[string]config.ProviderConfig{}
		}
		existing := s.cfg.Providers[name]
		key := strings.TrimSpace(in.APIKey)
		if key == "" {
			key = existing.APIKey
		}
		dialect := strings.TrimSpace(in.ReasoningDialect)
		if dialect == "" {
			dialect = existing.ReasoningDialect
		}
		s.cfg.Providers[name] = config.ProviderConfig{Type: config.DefaultProviderType, APIKey: key, BaseURL: baseURL, Model: model, ReasoningDialect: dialect}
	}
	s.cfg.DefaultModel = name + "/" + model
	if err := config.Save(s.cfg); err != nil {
		return ws.ProviderList{}, err
	}
	return s.listLocked(), nil
}

func (s *providerService) Delete(name string) (ws.ProviderList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return ws.ProviderList{}, fmt.Errorf("provider name is required")
	}
	if defaultName, _, ok := providerRef(s.cfg.DefaultModel); ok && defaultName == name {
		return ws.ProviderList{}, fmt.Errorf("choose a different default model before deleting %q", name)
	}
	if _, custom := s.cfg.Providers[name]; custom {
		delete(s.cfg.Providers, name)
		if err := config.Save(s.cfg); err != nil {
			return ws.ProviderList{}, err
		}
		if s.catalog != nil {
			_ = s.catalog.RemoveCustomProvider(name)
		}
	} else if _, configured := s.auth.Get(name); configured {
		if err := s.auth.Delete(name); err != nil {
			return ws.ProviderList{}, err
		}
	} else {
		return ws.ProviderList{}, fmt.Errorf("provider %q is not configured", name)
	}
	return s.listLocked(), nil
}

func (s *providerService) SetDefault(name, model string) (ws.ProviderList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name, model = strings.TrimSpace(name), strings.TrimSpace(model)
	if name == "" || model == "" {
		return ws.ProviderList{}, fmt.Errorf("provider and model are required")
	}
	if _, _, err := s.cfg.ResolveWithAuth(s.auth, name, model, ""); err != nil {
		return ws.ProviderList{}, err
	}
	s.cfg.DefaultModel = name + "/" + model
	if err := config.Save(s.cfg); err != nil {
		return ws.ProviderList{}, err
	}
	return s.listLocked(), nil
}

func (s *providerService) Discover(ctx context.Context, name, apiKey string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("provider name is required")
	}
	var baseURL string
	if p, ok := s.cfg.Providers[name]; ok {
		baseURL, apiKey = p.BaseURL, firstNonEmpty(apiKey, p.APIKey)
	} else if credential, ok := s.auth.Get(name); ok {
		baseURL, apiKey = credential.BaseURL, firstNonEmpty(apiKey, credential.APIKey)
	} else {
		return nil, fmt.Errorf("provider %q is not configured", name)
	}
	ids, err := llm.ListOpenAIModels(ctx, nil, apiKey, baseURL)
	if err != nil {
		return nil, err
	}
	if s.catalog != nil {
		if err := s.catalog.SetCustomModels(name, name, ids); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
