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
	"time"
)

const (
	apiURL       = "https://models.dev/api.json"
	cacheFile    = "models.json"
	maxBodyBytes = 8 << 20
)

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

type cache struct {
	CheckedAt time.Time `json:"checkedAt"`
	Models    []Model   `json:"models"`
}

// Catalog stores the last successful normalized catalog.
type Catalog struct {
	path string
	data cache
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
	return c.Empty() || c.data.CheckedAt.IsZero() || now.Sub(c.data.CheckedAt) >= 4*time.Hour
}

// Models returns only candidates for the configured provider names.
func (c *Catalog) Models(providers map[string]struct{}) []Model {
	out := make([]Model, 0, len(c.data.Models))
	for _, model := range c.data.Models {
		if _, ok := providers[model.Provider]; ok {
			out = append(out, model)
		}
	}
	return out
}

// Refresh downloads models.dev and persists an atomic normalized cache.
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
	models := normalize(source)
	if len(models) == 0 {
		return fmt.Errorf("models catalog contains no compatible tool-capable models")
	}
	c.data = cache{CheckedAt: time.Now(), Models: models}
	return c.save()
}

func (c *Catalog) save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".models-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, c.path)
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

func normalize(source map[string]provider) []Model {
	var out []Model
	for key, p := range source {
		providerID := p.ID
		if providerID == "" {
			providerID = key
		}
		if !compatible(providerID, p.NPM, p.API) {
			continue
		}
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
	return out
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
