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

	"github.com/AlvinPlayz23/myagent/internal/llm"
)

const (
	apiURL       = "https://models.dev/api.json"
	cacheFile    = "models.json"
	maxBodyBytes = 8 << 20

	// discoveryTTL bounds how long a provider's live /v1/models result is
	// reused without hitting the network again, keeping repeated picker
	// opens cheap and degradation graceful offline.
	discoveryTTL = 10 * time.Minute
)

// catalogFileMu avoids redundant OS lock acquisition within this process.
var catalogFileMu sync.Mutex

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

// FindModel returns exact provider/model metadata, including custom models.
func (c *Catalog) FindModel(provider, id string) (Model, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, model := range append(append([]Model(nil), c.data.Models...), c.data.Custom...) {
		if model.Provider == provider && model.ID == id {
			return model, true
		}
	}
	return Model{}, false
}

// FindBuiltinModel returns metadata sourced from models.dev, excluding
// discovered custom IDs whose capabilities are unknown.
func (c *Catalog) FindBuiltinModel(provider, id string) (Model, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, model := range c.data.Models {
		if model.Provider == provider && model.ID == id {
			return model, true
		}
	}
	return Model{}, false
}

// Enrich copies trusted catalog capabilities onto a resolved request model.
func (c *Catalog) Enrich(model llm.Model) llm.Model {
	entry, ok := c.FindBuiltinModel(model.Provider, model.ID)
	if !ok {
		return model
	}
	model.ReasoningKnown = true
	model.Reasoning = entry.Reasoning
	model.SupportedEfforts = llm.SupportedEffortsFor(entry.Reasoning)
	return model
}

// IsBuiltinProvider reports whether a provider is present in the downloaded
// catalog. Presets are used as an offline fallback for first-run operation.
func (c *Catalog) IsBuiltinProvider(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, provider := range c.data.Providers {
		if provider.ID == id {
			return true
		}
	}
	return false
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
	mu   sync.RWMutex

	// discovered caches per-provider live /v1/models results in memory.
	// It is deliberately not persisted: discovered IDs survive restarts
	// through data.Custom instead, so this only needs to stay fresh enough
	// to avoid hammering providers on repeated UI opens.
	discovered map[string]discoveryEntry
}

type discoveryEntry struct {
	ids []string
	at  time.Time
}

func New(dir string) *Catalog { return &Catalog{path: filepath.Join(dir, cacheFile)} }

// Load restores cached choices. A missing cache is not an error.
func (c *Catalog) Load() error {
	return c.withFileLock(func() error {
		data, found, err := loadCache(c.path)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		c.mu.Lock()
		c.data = data
		c.mu.Unlock()
		return nil
	})
}

// withFileLock serializes every cache read and read-modify-write cycle across
// both Catalog instances and cooperating myagent processes.
func (c *Catalog) withFileLock(fn func() error) error {
	catalogFileMu.Lock()
	defer catalogFileMu.Unlock()
	release, err := lockCatalogFile(c.path)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

func loadCache(path string) (cache, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cache{}, false, nil
	}
	if err != nil {
		return cache{}, false, err
	}
	var data cache
	if err := json.Unmarshal(b, &data); err != nil {
		return cache{}, false, err
	}
	return data, true, nil
}

func cloneCache(data cache) cache {
	return cache{
		CheckedAt: data.CheckedAt,
		Models:    append([]Model(nil), data.Models...),
		Providers: append([]Provider(nil), data.Providers...),
		Custom:    append([]Model(nil), data.Custom...),
	}
}

// currentCacheLocked returns the newest persisted state, falling back to the
// in-memory state before the cache has been created.
func (c *Catalog) currentCacheLocked() (cache, error) {
	data, found, err := loadCache(c.path)
	if err != nil {
		return cache{}, err
	}
	if found {
		return data, nil
	}
	return cloneCache(c.data), nil
}

func (c *Catalog) Empty() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data.Models) == 0
}

// NeedsRefresh reports whether the cache should be refreshed. A stale catalog
// remains usable while a refresh is attempted.
func (c *Catalog) NeedsRefresh(now time.Time) bool {
	// Older cache versions did not persist provider metadata. Refresh them even
	// when their model entries are otherwise still fresh.
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data.Models) == 0 || len(c.data.Providers) == 0 || c.data.CheckedAt.IsZero() || now.Sub(c.data.CheckedAt) >= 4*time.Hour
}

// Models returns unique candidates for the configured provider names. Built-in
// catalog metadata wins over an identically named discovered custom model.
func (c *Catalog) Models(providers map[string]struct{}) []Model {
	c.mu.RLock()
	all := append(append([]Model(nil), c.data.Models...), c.data.Custom...)
	c.mu.RUnlock()
	out := make([]Model, 0, len(all))
	seen := make(map[string]struct{}, len(all))
	for _, model := range all {
		if _, ok := providers[model.Provider]; ok {
			if _, duplicate := seen[model.Ref()]; !duplicate {
				seen[model.Ref()] = struct{}{}
				out = append(out, model)
			}
		}
	}
	return out
}

// CachedDiscovery returns IDs from a recent live /v1/models lookup for the
// provider, if one is still fresh. The returned slice is a copy.
func (c *Catalog) CachedDiscovery(provider string, now time.Time) ([]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.discovered[provider]
	if !ok || now.Sub(entry.at) >= discoveryTTL {
		return nil, false
	}
	return append([]string(nil), entry.ids...), true
}

// RememberDiscovery stores a successful live /v1/models result for reuse
// within the discovery TTL.
func (c *Catalog) RememberDiscovery(provider string, ids []string, now time.Time) {
	ids = append([]string(nil), ids...)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.discovered == nil {
		c.discovered = map[string]discoveryEntry{}
	}
	c.discovered[provider] = discoveryEntry{ids: ids, at: now}
}

// SetCustomModels stores model IDs discovered from a user-configured endpoint.
// They are kept independently of models.dev so refreshes cannot erase them.
func (c *Catalog) SetCustomModels(provider, providerName string, ids []string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("custom provider name is required")
	}
	if providerName == "" {
		providerName = provider
	}
	return c.withFileLock(func() error {
		c.mu.Lock()
		defer c.mu.Unlock()
		data, err := c.currentCacheLocked()
		if err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(ids))
		custom := make([]Model, 0, len(data.Custom)+len(ids))
		for _, model := range data.Custom {
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
		data.Custom = custom
		sort.Slice(data.Custom, func(i, j int) bool { return data.Custom[i].Ref() < data.Custom[j].Ref() })
		if err := saveCache(c.path, data); err != nil {
			return err
		}
		c.data = data
		return nil
	})
}

// RemoveCustomProvider removes catalog entries belonging to a deleted or
// renamed user-configured provider.
func (c *Catalog) RemoveCustomProvider(provider string) error {
	return c.withFileLock(func() error {
		c.mu.Lock()
		defer c.mu.Unlock()
		data, err := c.currentCacheLocked()
		if err != nil {
			return err
		}
		out := make([]Model, 0, len(data.Custom))
		for _, model := range data.Custom {
			if model.Provider != provider {
				out = append(out, model)
			}
		}
		data.Custom = out
		if err := saveCache(c.path, data); err != nil {
			return err
		}
		c.data = data
		return nil
	})
}

// Providers returns compatible catalog providers in stable display order.
func (c *Catalog) Providers() []Provider {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.data.Providers) == 0 {
		return providersFromModels(append([]Model(nil), c.data.Models...))
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
	return c.withFileLock(func() error {
		c.mu.Lock()
		defer c.mu.Unlock()
		data, err := c.currentCacheLocked()
		if err != nil {
			return err
		}
		data.CheckedAt = time.Now()
		data.Models = append([]Model(nil), models...)
		data.Providers = append([]Provider(nil), providers...)
		if err := saveCache(c.path, data); err != nil {
			return err
		}
		c.data = data
		return nil
	})
}

func saveCache(path string, data cache) error {
	return saveCacheWithReplace(path, data, os.Rename)
}

func saveCacheWithReplace(path string, data cache, replace func(string, string) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	f, err := os.CreateTemp(dir, cacheFile+"-*")
	if err != nil {
		return err
	}
	tempPath := f.Name()
	closed := false
	defer func() {
		if !closed {
			_ = f.Close()
		}
		_ = os.Remove(tempPath)
	}()
	if _, err := f.Write(b); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	closed = true
	if err := replace(tempPath, path); err != nil {
		// Windows can permit writes to an open cache while denying replacement
		// of its directory entry. Keep that cache usable in this rare case.
		if fallbackErr := writeCacheInPlace(path, b); fallbackErr != nil {
			return fmt.Errorf("replace models cache: %w; in-place fallback: %w", err, fallbackErr)
		}
	}
	return nil
}

func writeCacheInPlace(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := f.Truncate(0); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
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
