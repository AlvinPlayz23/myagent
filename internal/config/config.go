// Package config loads myagent configuration from JSON with environment
// overrides. Configuration lives at ~/.myagent/config.json.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/myagent/myagent/internal/llm"
)

// Env var names. OPENAI_* are temporary overrides for a selected provider;
// MYAGENT_MODEL overrides its model ID.
const (
	EnvAPIKey  = "OPENAI_API_KEY"
	EnvBaseURL = "OPENAI_BASE_URL"
	EnvModel   = "MYAGENT_MODEL"
)

const (
	DefaultProviderName = "openai"
	DefaultProviderType = "openai-compatible"
	DefaultBaseURL      = "https://api.openai.com/v1"
	DefaultModel        = "gpt-4o"
)

// ProviderConfig describes one named endpoint. Only openai-compatible is
// currently supported; it covers OpenAI, Ollama, LM Studio, vLLM, and similar
// chat-completions endpoints.
type ProviderConfig struct {
	Type    string `json:"type"`
	APIKey  string `json:"apiKey,omitempty"`
	BaseURL string `json:"baseUrl"`
}

// Config is the persisted configuration. DefaultModel must use
// "provider-name/model-id" so model selection remains unambiguous.
type Config struct {
	Providers    map[string]ProviderConfig `json:"providers"`
	DefaultModel string                    `json:"default_model"`
}

// Dir returns the myagent config/data directory (~/.myagent), honoring
// MYAGENT_DIR if set.
func Dir() (string, error) {
	if d := os.Getenv("MYAGENT_DIR"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".myagent"), nil
}

// Path returns the path to config.json within Dir().
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads config.json. A missing file is not an error; first-run setup
// determines whether a usable configuration must be created.
func Load() (*Config, error) {
	cfg := &Config{}
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, err
		}
	case errors.Is(err, fs.ErrNotExist):
		// no file
	default:
		return nil, err
	}
	return cfg, nil
}

// Resolve selects a configured provider and model. providerName, modelID, and
// baseURL override the corresponding configured values when non-empty.
func (c *Config) Resolve(providerName, modelID, baseURL string) (llm.Provider, llm.Model, error) {
	if c == nil {
		return nil, llm.Model{}, errors.New("config is nil")
	}
	defaultProvider, defaultModel, err := splitModelRef(c.DefaultModel)
	if err != nil {
		return nil, llm.Model{}, fmt.Errorf("default_model: %w", err)
	}
	if providerName == "" {
		providerName = defaultProvider
	}
	if modelID == "" {
		modelID = defaultModel
		if v := os.Getenv(EnvModel); v != "" {
			modelID = v
		}
	}

	providerCfg, ok := c.Providers[providerName]
	if !ok {
		return nil, llm.Model{}, fmt.Errorf("provider %q is not configured", providerName)
	}
	if providerCfg.Type != DefaultProviderType {
		return nil, llm.Model{}, fmt.Errorf("provider %q has unsupported type %q", providerName, providerCfg.Type)
	}
	if baseURL != "" {
		providerCfg.BaseURL = baseURL
	} else if v := os.Getenv(EnvBaseURL); v != "" && providerName == defaultProvider {
		providerCfg.BaseURL = v
	}
	if providerCfg.BaseURL == "" {
		return nil, llm.Model{}, fmt.Errorf("provider %q has no baseUrl", providerName)
	}
	if v := os.Getenv(EnvAPIKey); v != "" && providerName == defaultProvider {
		providerCfg.APIKey = v
	}
	return llm.NewOpenAIProvider(providerCfg.APIKey), llm.Model{
		ID:       modelID,
		Provider: providerName,
		BaseURL:  providerCfg.BaseURL,
	}, nil
}

func splitModelRef(ref string) (string, string, error) {
	provider, model, found := strings.Cut(strings.TrimSpace(ref), "/")
	if !found || provider == "" || model == "" || strings.Contains(model, "/") {
		return "", "", fmt.Errorf("must be provider/model-id")
	}
	return provider, model, nil
}

// Exists reports whether a config.json file is present at Path() and is
// non-empty (i.e. contains at least one non-whitespace byte).
func Exists() (bool, error) {
	path, err := Path()
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(data)) != "", nil
}

// NeedsSetup reports whether the interactive setup wizard should run.
func NeedsSetup() (bool, error) {
	exists, err := Exists()
	if err != nil {
		return false, err
	}
	return !exists, nil
}

// Save writes cfg to config.json under Dir(), creating the directory if
// needed. The file is atomically replaced and stored with 0600 permissions.
func Save(cfg *Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".config-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
