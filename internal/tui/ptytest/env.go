//go:build unix

package ptytest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Env is an isolated MYAGENT_DIR whose config.json routes the default model
// to the fake server, plus a fresh working directory for the child process.
type Env struct {
	Dir string // MYAGENT_DIR (also HOME for the child)
	Cwd string // working directory the child runs in
}

// NewEnv writes the temporary myagent home: a config.json resolving a custom
// provider to srv, and a models.json cache seeded fresh so the TUI does not
// attempt a network catalog refresh at startup. The auth store is left
// absent; the custom provider carries its own (fake) key in config.json.
func NewEnv(t *testing.T, srv *Server) *Env {
	t.Helper()
	base, err := os.MkdirTemp("", "myagent-ptytest-")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	if os.Getenv("PTYTEST_KEEP") == "" {
		t.Cleanup(func() { _ = os.RemoveAll(base) })
	} else {
		t.Logf("PTYTEST_KEEP set; keeping %s", base)
	}
	env := &Env{
		Dir: filepath.Join(base, "myagent"),
		Cwd: filepath.Join(base, "work"),
	}
	for _, dir := range []string{env.Dir, env.Cwd} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	writeConfig(t, env.Dir, srv)
	writeModelsCache(t, env.Dir, srv)
	return env
}

// writeConfig mirrors internal/config's Config JSON: a custom provider entry
// (type, apiKey, baseUrl, model) plus default_model as "provider/model-id".
func writeConfig(t *testing.T, dir string, srv *Server) {
	t.Helper()
	cfg := map[string]any{
		"providers": map[string]any{
			ProviderName: map[string]any{
				"type":    "openai-compatible",
				"apiKey":  "ptytest-not-a-real-credential",
				"baseUrl": srv.BaseURL() + "/v1",
				"model":   ModelID,
			},
		},
		"default_model": ModelRef,
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), append(b, '\n'), 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
}

// The cache structs below mirror internal/models' on-disk models.json shapes
// (checkedAt, models, providers) so the seeded catalog loads and reads as
// fresh, skipping the TUI's startup refresh against models.dev.

type cacheModel struct {
	Provider      string `json:"provider"`
	ProviderName  string `json:"providerName"`
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	OutputLimit   int    `json:"outputLimit,omitempty"`
}

type cacheProvider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl,omitempty"`
}

type modelsCache struct {
	CheckedAt time.Time       `json:"checkedAt"`
	Models    []cacheModel    `json:"models"`
	Providers []cacheProvider `json:"providers"`
}

func writeModelsCache(t *testing.T, dir string, srv *Server) {
	t.Helper()
	cache := modelsCache{
		CheckedAt: time.Now().Add(-time.Minute),
		Models: []cacheModel{{
			Provider:     ProviderName,
			ProviderName: ProviderName,
			ID:           ModelID,
			Name:         "PTY Test Model",
		}},
		Providers: []cacheProvider{{
			ID:      ProviderName,
			Name:    ProviderName,
			BaseURL: srv.BaseURL() + "/v1",
		}},
	}
	b, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatalf("marshal models cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "models.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatalf("write models.json: %v", err)
	}
}
