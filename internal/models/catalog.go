// Package models provides a cached, normalized view of models.dev provider
// catalogs. It deliberately keeps source-specific JSON out of the UI and
// request path.
package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	apiURL       = "https://models.dev/api.json"
	cacheFile    = "models.json"
	maxBodyBytes = 8 << 20
)

var catalogWriteMu sync.Mutex

// Model is a provider-qualified, selectable model.
type Model struct {
	Provider      string `json:"provider"`
	ProviderName  string `json:"providerName"`
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	OutputLimit   int    `json:"outputLimit,omitempty"`
	Reasoning     bool   `json:"reasoning,omitempty"`
}

func (m Model) Ref() string { return m.Provider + "/" + m.ID }

// Provider is a catalog provider that this build can route through its
// OpenAI-compatible transport.
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl,omitempty"`
}

type cache struct {
	CheckedAt time.Time  `json:"checkedAt"`
	Models    []Model    `json:"models"`
	Providers []Provider `json:"providers"`
	Custom    []Model    `json:"custom,omitempty"`
}

// Catalog stores the last successful normalized catalog.
type Catalog struct {
	path string
	data cache
	mu   sync.Mutex
}

func New(dir string) *Catalog { return &Catalog{path: filepath.Join(dir, cacheFile)} }

// Load restores cached choices. A missing cache is not an error.
func (c *Catalog) Load() error {
	b, err := os.ReadFile(c.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &c.data)
}

func (c *Catalog) Empty() bool { return len(c.data.Models) == 0 }

// NeedsRefresh reports whether the cache should be refreshed. A stale catalog
// remains usable while a refresh is attempted.
func (c *Catalog) NeedsRefresh(now time.Time) bool {
	// Older cache versions did not persist provider metadata. Refresh them even
	// when their model entries are otherwise still fresh.
	return c.Empty() || len(c.data.Providers) == 0 || c.data.CheckedAt.IsZero() || now.Sub(c.data.CheckedAt) >= 4*time.Hour
}

// Models returns only candidates for the configured provider names.
func (c *Catalog) Models(providers map[string]struct{}) []Model {
	all := append(append([]Model(nil), c.data.Models...), c.data.Custom...)
	out := make([]Model, 0, len(all))
	for _, model := range all {
		if _, ok := providers[model.Provider]; ok {
			out = append(out, model)
		}
	}
	return out
}

// SetCustomModels stores model IDs discovered from a user-configured endpoint.
// They are kept independently of models.dev so refreshes cannot erase them.
func (c *Catalog) SetCustomModels(provider, providerName string, ids []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("custom provider name is required")
	}
	if providerName == "" {
		providerName = provider
	}
	seen := make(map[string]struct{}, len(ids))
	custom := c.data.Custom[:0]
	for _, model := range c.data.Custom {
		if model.Provider != provider {
			custom = append(custom, model)
		}
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		custom = append(custom, Model{Provider: provider, ProviderName: providerName, ID: id})
	}
	c.data.Custom = custom
	sort.Slice(c.data.Custom, func(i, j int) bool { return c.data.Custom[i].Ref() < c.data.Custom[j].Ref() })
	return c.save()
}

// RemoveCustomProvider removes catalog entries belonging to a deleted or
// renamed user-configured provider.
func (c *Catalog) RemoveCustomProvider(provider string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.data.Custom[:0]
	for _, model := range c.data.Custom {
		if model.Provider != provider {
			out = append(out, model)
		}
	}
	c.data.Custom = out
	return c.save()
}

// Providers returns compatible catalog providers in stable display order.
func (c *Catalog) Providers() []Provider {
	if len(c.data.Providers) == 0 {
		return providersFromModels(c.data.Models)
	}
	out := make([]Provider, len(c.data.Providers))
	copy(out, c.data.Providers)
	return out
}

// providersFromModels keeps /providers usable when an older cache cannot be
// refreshed (for example while offline). A successful refresh replaces these
// derived entries with provider records that include endpoints.
func providersFromModels(models []Model) []Provider {
	seen := make(map[string]Provider, len(models))
	for _, model := range models {
		if _, ok := seen[model.Provider]; !ok {
			seen[model.Provider] = Provider{ID: model.Provider, Name: model.ProviderName}
		}
	}
	out := make([]Provider, 0, len(seen))
	for _, provider := range seen {
		if provider.Name == "" {
			provider.Name = provider.ID
		}
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Refresh downloads models.dev and persists the normalized cache.
func (c *Catalog) Refresh(ctx context.Context, client *http.Client) error {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch models catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("models catalog returned %s", resp.Status)
	}

	var source map[string]provider
	dec := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes))
	if err := dec.Decode(&source); err != nil {
		return fmt.Errorf("decode models catalog: %w", err)
	}
	models, providers := normalize(source)
	if len(models) == 0 {
		return fmt.Errorf("models catalog contains no compatible tool-capable models")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = cache{CheckedAt: time.Now(), Models: models, Providers: providers, Custom: c.data.Custom}
	return c.save()
}

func (c *Catalog) save() error {
	// Catalogs are constructed in multiple UI flows, so serialize writes across
	// instances as well as individual Catalog mutations.
	catalogWriteMu.Lock()
	defer catalogWriteMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	// Update the existing cache entry directly rather than replacing it with a
	// rename. Windows can allow writes to a file while denying replacement of
	// its directory entry. This cache is rebuildable, so direct persistence is
	// preferable to blocking provider configuration on that restriction.
	f, err := os.OpenFile(c.path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		return err
	}
	return f.Sync()
}

type provider struct {
	ID     string                 `json:"id"`
	Name   string                 `json:"name"`
	NPM    string                 `json:"npm"`
	API    string                 `json:"api"`
	Models map[string]sourceModel `json:"models"`
}

type sourceModel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ToolCall  bool   `json:"tool_call"`
	Reasoning bool   `json:"reasoning"`
	Limit     struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	Provider *struct {
		NPM string `json:"npm"`
		API string `json:"api"`
	} `json:"provider"`
}

func normalize(source map[string]provider) ([]Model, []Provider) {
	var out []Model
	var providers []Provider
	for key, p := range source {
		providerID := p.ID
		if providerID == "" {
			providerID = key
		}
		if !compatible(providerID, p.NPM, p.API) {
			continue
		}
		providers = append(providers, Provider{ID: providerID, Name: p.Name, BaseURL: p.API})
		for key, model := range p.Models {
			if !model.ToolCall || (model.Provider != nil && !strings.Contains(model.Provider.NPM, "openai-compatible")) {
				continue
			}
			id := model.ID
			if id == "" {
				id = key
			}
			out = append(out, Model{Provider: providerID, ProviderName: p.Name, ID: id, Name: model.Name, ContextWindow: model.Limit.Context, OutputLimit: model.Limit.Output, Reasoning: model.Reasoning})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref() < out[j].Ref() })
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })
	return out, providers
}

func compatible(providerID, npm, api string) bool {
	switch providerID {
	case "openai", "openrouter", "aihubmix", "zenmux", "ollama", "lmstudio", "vllm":
		return true
	}
	if strings.Contains(npm, "openai-compatible") {
		return true
	}
	return false
}
