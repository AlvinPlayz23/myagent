// Package config loads myagent configuration from JSON with environment
// overrides.
//
// Matches pi's JSON-everywhere approach. Config lives at
// ~/.myagent/config.json; environment variables take precedence over file
// values so a user can override per-invocation without editing the file.
//
// On first run (no config.json, or an empty/blank one), the interactive setup
// wizard collects the required fields and writes the file via Save. Once a
// non-empty config file exists, setup is skipped.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Env var names (see myagent-plan.md, Phase 0).
const (
	EnvAPIKey   = "OPENAI_API_KEY"
	EnvBaseURL  = "OPENAI_BASE_URL"
	EnvModel    = "MYAGENT_MODEL"
	EnvVTMyAgent = "VT_MYAGENT"
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

// Exists reports whether a config.json file is present at Path() and is
// non-empty (i.e. contains at least one non-whitespace byte). A missing or
// whitespace-only file is treated as not existing, so the setup wizard runs
// again until real values have been written.
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

// NeedsSetup reports whether the interactive setup wizard should run before
// myagent proceeds with an interactive session. Setup is required when the
// config file is missing or empty/blank. A non-empty file is left intact; if
// it is malformed or incomplete, Load() / normal credential validation reports
// the configuration error rather than overwriting user data.
func NeedsSetup() (bool, error) {
	exists, err := Exists()
	if err != nil {
		return false, err
	}
	return !exists, nil
}

// Save writes cfg to config.json under Dir(), creating the directory if
// needed. The file is written with 0600 permissions so it is not
// world-readable (it contains the API key). tmp+rename is used so the
// config.json is never left half-written. Save does NOT round-trip the
// environment: only the explicit cfg fields are persisted, mirroring the
// documented "env wins" semantics; call Load() afterwards to reapply env
// overrides and defaults.
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
	// Rename on Windows can leave the moved file with the temp file's mode;
	// re-assert 0600 on the final path.
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return nil
}
