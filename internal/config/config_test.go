package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/myagent/myagent/internal/auth"
)

func useTempDir(t *testing.T) {
	t.Helper()
	t.Setenv("MYAGENT_DIR", t.TempDir())
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvBaseURL, "")
	t.Setenv(EnvModel, "")
}

func configPath(t *testing.T) string {
	t.Helper()
	p, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	return p
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func testConfig() *Config {
	return &Config{
		Providers: map[string]ProviderConfig{
			"openai": {Type: DefaultProviderType, APIKey: "sk-openai", BaseURL: DefaultBaseURL},
			"local":  {Type: DefaultProviderType, APIKey: "local-key", BaseURL: "http://localhost:11434/v1"},
		},
		DefaultModel: "openai/gpt-4o",
	}
}

func TestNeedsSetup(t *testing.T) {
	useTempDir(t)
	needs, err := NeedsSetup()
	if err != nil || !needs {
		t.Fatalf("NeedsSetup missing = %v, %v; want true, nil", needs, err)
	}
	writeFile(t, configPath(t), "  \n")
	needs, err = NeedsSetup()
	if err != nil || !needs {
		t.Fatalf("NeedsSetup blank = %v, %v; want true, nil", needs, err)
	}
	writeFile(t, configPath(t), `{"providers":{}}`)
	needs, err = NeedsSetup()
	if err != nil || needs {
		t.Fatalf("NeedsSetup non-empty = %v, %v; want false, nil", needs, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	useTempDir(t)
	want := testConfig()
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(configPath(t))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(configPath(t))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.DefaultModel != want.DefaultModel || len(got.Providers) != 2 || got.Providers["local"].BaseURL != "http://localhost:11434/v1" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatal("saved file should end with newline")
	}
}

func TestResolveDefaultAndOverrides(t *testing.T) {
	useTempDir(t)
	cfg := testConfig()
	provider, model, err := cfg.Resolve("", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if provider == nil || model.ID != "gpt-4o" || model.Provider != "openai" || model.BaseURL != DefaultBaseURL {
		t.Fatalf("default resolution = %#v", model)
	}
	_, model, err = cfg.Resolve("local", "qwen3", "http://127.0.0.1:8080/v1")
	if err != nil {
		t.Fatalf("Resolve override: %v", err)
	}
	if model.ID != "qwen3" || model.Provider != "local" || model.BaseURL != "http://127.0.0.1:8080/v1" {
		t.Fatalf("override resolution = %#v", model)
	}
}

func TestResolveAppliesEnvironmentToDefaultProvider(t *testing.T) {
	useTempDir(t)
	t.Setenv(EnvAPIKey, "sk-env")
	t.Setenv(EnvBaseURL, "https://env.example/v1")
	t.Setenv(EnvModel, "gpt-env")
	_, model, err := testConfig().Resolve("", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if model.ID != "gpt-env" || model.BaseURL != "https://env.example/v1" {
		t.Fatalf("environment resolution = %#v", model)
	}
}

func TestResolveAllowsSlashInModelID(t *testing.T) {
	useTempDir(t)
	cfg := &Config{Providers: map[string]ProviderConfig{
		"local": {Type: DefaultProviderType, BaseURL: "http://localhost:11434/v1"},
	}, DefaultModel: "local/meta-llama/Llama-3.1-8B-Instruct"}
	_, model, err := cfg.Resolve("", "", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := model.ID, "meta-llama/Llama-3.1-8B-Instruct"; got != want {
		t.Fatalf("model ID = %q, want %q", got, want)
	}
}

func TestResolveRejectsInvalidConfiguration(t *testing.T) {
	useTempDir(t)
	for _, cfg := range []*Config{
		{DefaultModel: "gpt-4o"},
		{Providers: map[string]ProviderConfig{}, DefaultModel: "missing/model"},
		{Providers: map[string]ProviderConfig{"p": {Type: "anthropic", APIKey: "x", BaseURL: "https://x"}}, DefaultModel: "p/model"},
		{Providers: map[string]ProviderConfig{"p": {Type: DefaultProviderType, APIKey: "x"}}, DefaultModel: "p/model"},
	} {
		if _, _, err := cfg.Resolve("", "", ""); err == nil {
			t.Fatalf("Resolve(%+v) succeeded; want error", cfg)
		}
	}
}

func TestResolveWithBuiltinAuth(t *testing.T) {
	useTempDir(t)
	store := &auth.Store{Providers: map[string]auth.Credentials{"openrouter": {APIKey: "key", BaseURL: "https://openrouter.ai/api/v1"}}}
	cfg := &Config{Providers: map[string]ProviderConfig{}, DefaultModel: "openrouter/openai/gpt-4.1"}
	provider, model, err := cfg.ResolveWithAuth(store, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil || model.Provider != "openrouter" || model.ID != "openai/gpt-4.1" {
		t.Fatalf("resolved model = %#v", model)
	}
}
