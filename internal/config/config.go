// Package config loads myagent configuration from JSON with environment
// overrides.
//
// Matches pi's JSON-everywhere approach. Config lives at
// ~/.myagent/config.json; environment variables take precedence over file
// values so a user can override per-invocation without editing the file.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Env var names (see myagent-plan.md, Phase 0).
const (
	EnvAPIKey  = "OPENAI_API_KEY"
	EnvBaseURL = "OPENAI_BASE_URL"
	EnvModel   = "MYAGENT_MODEL"
)

// Default values used when neither the config file nor the environment supply
// one.
const (
	DefaultBaseURL = "https://api.openai.com/v1"
	DefaultModel   = "gpt-4o"
)

// Config is the resolved runtime configuration.
type Config struct {
	// APIKey for the OpenAI-compatible provider. Never persisted back to disk
	// from env.
	APIKey string `json:"apiKey,omitempty"`
	// BaseURL of the OpenAI-compatible endpoint (base_url override enables
	// Ollama, LM Studio, vLLM, etc.).
	BaseURL string `json:"baseUrl,omitempty"`
	// Model id to use for requests.
	Model string `json:"model,omitempty"`
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

// Load reads config.json (if present) and applies environment overrides and
// defaults. A missing file is not an error: env + defaults still produce a
// usable Config.
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
		// no file: fall through to env + defaults
	default:
		return nil, err
	}

	// Environment overrides win over file values.
	if v := os.Getenv(EnvAPIKey); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv(EnvBaseURL); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv(EnvModel); v != "" {
		cfg.Model = v
	}

	// Defaults.
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}

	return cfg, nil
}
