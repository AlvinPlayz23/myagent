package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/auth"
	"github.com/AlvinPlayz23/myagent/internal/config"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/server/ws"
)

func TestProviderServiceSavesCustomProvidersWithoutLeakingKeys(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	store, err := auth.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := newProviderService(&config.Config{Providers: map[string]config.ProviderConfig{}}, store, modelcatalog.New(dir))

	list, err := service.Save(ws.ProviderInput{Name: "local", BaseURL: "http://localhost:11434/v1", Model: "qwen3", APIKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if list.DefaultModel != "local/qwen3" || len(list.Providers) != 1 {
		t.Fatalf("unexpected provider list: %#v", list)
	}
	if list.Providers[0].HasAPIKey != true || list.Providers[0].Name != "local" {
		t.Fatalf("unexpected record: %#v", list.Providers[0])
	}
	if list.Providers[0].BaseURL == "secret" {
		t.Fatal("provider record leaked API key")
	}

	if _, err := service.Delete("local"); err == nil {
		t.Fatal("expected deleting the default provider to fail")
	}
}

func TestProviderServiceSetsDefaultForConfiguredBuiltin(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	store, err := auth.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := newProviderService(&config.Config{Providers: map[string]config.ProviderConfig{}}, store, modelcatalog.New(dir))
	if _, err := service.Save(ws.ProviderInput{Name: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-test", APIKey: "secret", Builtin: true}); err != nil {
		t.Fatal(err)
	}
	list, err := service.SetDefault("openai", "gpt-test")
	if err != nil {
		t.Fatal(err)
	}
	if list.DefaultModel != "openai/gpt-test" {
		t.Fatalf("default = %q", list.DefaultModel)
	}
}

func TestProviderServiceDerivesProviderOriginAndRejectsFakeBuiltin(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	dir, _ := config.Dir()
	store, err := auth.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	catalog := modelcatalog.New(dir)
	service := newProviderService(&config.Config{Providers: map[string]config.ProviderConfig{}}, store, catalog)
	if _, err := service.Save(ws.ProviderInput{Name: "fake", BaseURL: "https://fake.example/v1", Model: "model", APIKey: "secret", Builtin: true}); err == nil {
		t.Fatal("fake builtin should be rejected")
	}
	if _, err := service.Save(ws.ProviderInput{Name: "local", BaseURL: "http://localhost:8000/v1", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	list, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Providers) != 1 || list.Providers[0].Origin != "custom" {
		t.Fatalf("providers = %#v", list.Providers)
	}
}

func newPersistedProviderServiceForTest(t *testing.T) *providerService {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MYAGENT_DIR", dir)
	t.Setenv(config.EnvAPIKey, "")
	t.Setenv(config.EnvBaseURL, "")
	t.Setenv(config.EnvModel, "")
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{
			"local":   {Type: config.DefaultProviderType, APIKey: "old-test-key", BaseURL: "http://localhost:8000/v1", Model: "old-model", ReasoningDialect: "openai", Transport: "chat-completions"},
			"other":   {Type: config.DefaultProviderType, BaseURL: "http://localhost:9000/v1", Model: "other-model"},
			"removed": {Type: config.DefaultProviderType, BaseURL: "http://localhost:11000/v1", Model: "removed-model"},
		},
		DefaultModel: "other/other-model",
		WelcomeStyle: "orb",
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	store, err := auth.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set("openai", auth.Credentials{APIKey: "builtin-test-key", BaseURL: config.DefaultBaseURL}); err != nil {
		t.Fatal(err)
	}
	return newProviderService(cfg, store, nil)
}

func TestProviderServiceWritersPreserveFreshConfig(t *testing.T) {
	for _, tt := range []struct {
		name         string
		staleDefault string
		deleteAuth   bool
		write        func(*providerService) (ws.ProviderList, error)
		want         func(*config.Config)
	}{
		{
			name: "Save custom",
			write: func(s *providerService) (ws.ProviderList, error) {
				return s.Save(ws.ProviderInput{Name: " local ", BaseURL: " http://localhost:8001/v1 ", Model: " new-model ", APIKey: " ", ReasoningDialect: " ", Transport: " "})
			},
			want: func(cfg *config.Config) {
				p := cfg.Providers["local"]
				p.BaseURL, p.Model = "http://localhost:8001/v1", "new-model"
				cfg.Providers["local"] = p
				cfg.DefaultModel = "local/new-model"
			},
		},
		{
			name: "Save builtin",
			write: func(s *providerService) (ws.ProviderList, error) {
				return s.Save(ws.ProviderInput{Name: "openai", Model: "new-model", Builtin: true})
			},
			want: func(cfg *config.Config) { cfg.DefaultModel = "openai/new-model" },
		},
		{
			name:         "Delete custom after default moved away",
			staleDefault: "local/old-model",
			write: func(s *providerService) (ws.ProviderList, error) {
				return s.Delete(" local ")
			},
			want: func(cfg *config.Config) { delete(cfg.Providers, "local") },
		},
		{
			name:         "Delete builtin after default moved away",
			staleDefault: "openai/old-model",
			deleteAuth:   true,
			write: func(s *providerService) (ws.ProviderList, error) {
				return s.Delete("openai")
			},
			want: func(cfg *config.Config) {},
		},
		{
			name: "SetDefault existing provider",
			write: func(s *providerService) (ws.ProviderList, error) {
				return s.SetDefault(" local ", " new-model ")
			},
			want: func(cfg *config.Config) { cfg.DefaultModel = "local/new-model" },
		},
		{
			name: "SetDefault newly configured provider",
			write: func(s *providerService) (ws.ProviderList, error) {
				return s.SetDefault(" external ", " new-model ")
			},
			want: func(cfg *config.Config) { cfg.DefaultModel = "external/new-model" },
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := newPersistedProviderServiceForTest(t)
			if tt.staleDefault != "" {
				service.cfg.DefaultModel = tt.staleDefault
				if err := config.Save(service.cfg); err != nil {
					t.Fatal(err)
				}
			}
			// Simulate another writer after the service retained its snapshot.
			if err := config.Update(func(cfg *config.Config) error {
				cfg.WelcomeStyle, cfg.PromptStyle, cfg.DefaultEffort = "compact", "plain", "high"
				cfg.DisabledTools = []string{"bash", "write"}
				cfg.Retry = &config.RetryConfig{MaxAttempts: 1, BaseDelayMs: 250, MaxDelayMs: 1000}
				cfg.Providers["external"] = config.ProviderConfig{Type: config.DefaultProviderType, BaseURL: "http://localhost:10000/v1", Model: "external-model"}
				delete(cfg.Providers, "removed")
				other := cfg.Providers["other"]
				other.BaseURL = "http://localhost:9001/v1"
				cfg.Providers["other"] = other
				local := cfg.Providers["local"]
				local.APIKey, local.ReasoningDialect, local.Transport = "updated-test-key", "openrouter", "responses"
				cfg.Providers["local"] = local
				cfg.DefaultModel = "external/external-model"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			want, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			tt.want(want)
			list, err := tt.write(service)
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(persisted, want) {
				t.Fatal("writer changed unrelated fresh config or failed to apply its focused mutation")
			}
			if !reflect.DeepEqual(service.cfg, persisted) || list.DefaultModel != persisted.DefaultModel {
				t.Fatal("service snapshot/list did not synchronize with the successful save")
			}
			_, model, err := service.Resolve("external", "external-model", "")
			if err != nil || model.BaseURL != persisted.Providers["external"].BaseURL {
				t.Fatal("service cannot resolve the externally configured provider after saving")
			}
			dir, err := config.Dir()
			if err != nil {
				t.Fatal(err)
			}
			storedAuth, err := auth.Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(storedAuth.Providers, service.auth.Providers) {
				t.Fatal("auth persistence does not match the service credentials")
			}
			_, configured := storedAuth.Get("openai")
			if configured == tt.deleteAuth {
				t.Fatal("built-in credentials were not kept in the auth store as expected")
			}
		})
	}
}

func TestProviderServiceFailuresKeepSnapshot(t *testing.T) {
	for _, tt := range []struct {
		name      string
		external  func(*config.Config)
		write     func(*providerService) (ws.ProviderList, error)
		wantError string
	}{
		{
			name:      "Delete current custom default",
			external:  func(cfg *config.Config) { cfg.DefaultModel = "local/new-model" },
			write:     func(s *providerService) (ws.ProviderList, error) { return s.Delete("local") },
			wantError: "choose a different default model",
		},
		{
			name:      "Delete current builtin default",
			external:  func(cfg *config.Config) { cfg.DefaultModel = "openai/new-model" },
			write:     func(s *providerService) (ws.ProviderList, error) { return s.Delete("openai") },
			wantError: "choose a different default model",
		},
		{
			name:      "Delete removed provider",
			external:  func(cfg *config.Config) { delete(cfg.Providers, "local") },
			write:     func(s *providerService) (ws.ProviderList, error) { return s.Delete("local") },
			wantError: "not configured",
		},
		{
			name:      "SetDefault removed provider",
			external:  func(cfg *config.Config) { delete(cfg.Providers, "local") },
			write:     func(s *providerService) (ws.ProviderList, error) { return s.SetDefault("local", "new-model") },
			wantError: "not configured",
		},
		{
			name: "SetDefault invalid fresh provider",
			external: func(cfg *config.Config) {
				p := cfg.Providers["local"]
				p.Transport = "invalid"
				cfg.Providers["local"] = p
			},
			write:     func(s *providerService) (ws.ProviderList, error) { return s.SetDefault("local", "new-model") },
			wantError: "invalid transport",
		},
		{
			name: "Save builtin conflicts with fresh custom provider",
			external: func(cfg *config.Config) {
				cfg.Providers["openai"] = config.ProviderConfig{Type: config.DefaultProviderType, BaseURL: "http://localhost:8001/v1"}
			},
			write: func(s *providerService) (ws.ProviderList, error) {
				return s.Save(ws.ProviderInput{Name: "openai", Model: "new-model", Builtin: true})
			},
			wantError: "managed as a custom provider",
		},
		{
			name:     "Save missing base URL",
			external: func(cfg *config.Config) {},
			write: func(s *providerService) (ws.ProviderList, error) {
				return s.Save(ws.ProviderInput{Name: "local", Model: "new-model"})
			},
			wantError: "base URL is required",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := newPersistedProviderServiceForTest(t)
			before := service.cfg
			wantSnapshot, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if err := config.Update(func(cfg *config.Config) error {
				cfg.WelcomeStyle = "compact"
				tt.external(cfg)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			wantPersisted, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			credential, _ := service.auth.Get("openai")
			if _, err := tt.write(service); err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected failure containing %q, got %v", tt.wantError, err)
			}
			if service.cfg != before || !reflect.DeepEqual(service.cfg, wantSnapshot) {
				t.Fatal("failed writer mutated or replaced the service snapshot")
			}
			persisted, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(persisted, wantPersisted) {
				t.Fatal("failed writer mutated the persisted config")
			}
			if got, configured := service.auth.Get("openai"); !configured || got != credential {
				t.Fatal("failed writer mutated built-in credentials")
			}
		})
	}
}

func TestProviderServiceLoadFailuresKeepSnapshot(t *testing.T) {
	for _, tt := range []struct {
		name  string
		write func(*providerService) (ws.ProviderList, error)
	}{
		{"Save", func(s *providerService) (ws.ProviderList, error) {
			return s.Save(ws.ProviderInput{Name: "local", BaseURL: "http://localhost:8001/v1", Model: "new-model"})
		}},
		{"Delete", func(s *providerService) (ws.ProviderList, error) { return s.Delete("local") }},
		{"SetDefault", func(s *providerService) (ws.ProviderList, error) { return s.SetDefault("local", "new-model") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := newPersistedProviderServiceForTest(t)
			before := service.cfg
			want, err := config.Load()
			if err != nil {
				t.Fatal(err)
			}
			brokenDir := t.TempDir()
			if err := os.Mkdir(filepath.Join(brokenDir, "config.json"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MYAGENT_DIR", brokenDir)
			if _, err := tt.write(service); err == nil {
				t.Fatal("expected config load failure")
			}
			if service.cfg != before || !reflect.DeepEqual(service.cfg, want) {
				t.Fatal("config load failure mutated or replaced the service snapshot")
			}
		})
	}
}

func TestProviderServiceSaveFailureKeepsSnapshot(t *testing.T) {
	service := newPersistedProviderServiceForTest(t)
	before := service.cfg
	want, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	blockedDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if err := service.updateConfigLocked(func(cfg *config.Config) error {
		cfg.DefaultModel = "local/new-model"
		delete(cfg.Providers, "other")
		// Load has succeeded and mutation has run; now force Save to fail on
		// every platform without relying on directory permission semantics.
		t.Setenv("MYAGENT_DIR", blockedDir)
		return nil
	}); err == nil {
		t.Fatal("expected config save failure")
	}
	if service.cfg != before || !reflect.DeepEqual(service.cfg, want) {
		t.Fatal("config save failure mutated or replaced the service snapshot")
	}
}
