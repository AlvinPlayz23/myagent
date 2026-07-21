package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/myagent/myagent/internal/config"
	modelcatalog "github.com/myagent/myagent/internal/models"
)

func setTempDir(t *testing.T) {
	t.Helper()
	t.Setenv("MYAGENT_DIR", t.TempDir())
	t.Setenv(config.EnvAPIKey, "")
	t.Setenv(config.EnvBaseURL, "")
	t.Setenv(config.EnvModel, "")
}

func readyWindow() tea.WindowSizeMsg { return tea.WindowSizeMsg{Width: 80, Height: 24} }

func saveProvider(t *testing.T, m *wizardModel, name, key, baseURL, model string) {
	t.Helper()
	m.fields[0].input.SetValue(name)
	m.fields[1].input.SetValue(key)
	m.fields[2].input.SetValue(baseURL)
	m.fields[3].input.SetValue(model)
	_, _ = m.saveProvider()
}

func TestModelPickerAddsManualModelAlongsideDiscoveredModels(t *testing.T) {
	setTempDir(t)
	m := newWizardModel()
	_, _ = m.Update(readyWindow())
	m.openEditor("")
	m.models = []string{"gpt-4o", "qwen3"}
	m.screen = screenModelPicker
	m.modelSearch.SetValue("custom/model")
	_, _ = m.onModelPickerKey(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl}))
	if got, want := m.models, []string{"custom/model", "gpt-4o", "qwen3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %v, want %v", got, want)
	}
	_, _ = m.onModelPickerKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.screen != screenEditor || m.fields[3].input.Value() != "custom/model" {
		t.Fatalf("manual model was not selected: screen=%d model=%q", m.screen, m.fields[3].input.Value())
	}
}

func TestFirstRunAddsProvider(t *testing.T) {
	setTempDir(t)
	m := newWizardModel()
	_, _ = m.Update(readyWindow())
	if m.screen != screenHome {
		t.Fatal("first run should open the auth home screen")
	}
	m.openEditor("")
	saveProvider(t, m, "openai", "sk-test", config.DefaultBaseURL, "gpt-4o-mini")
	if !m.done || m.result == nil {
		t.Fatalf("first provider should finish setup: %s", m.err)
	}
	if got := m.result.DefaultModel; got != "openai/gpt-4o-mini" {
		t.Fatalf("DefaultModel = %q", got)
	}
}

func TestAuthHomeNavigatesWithArrows(t *testing.T) {
	setTempDir(t)
	m := newWizardModel()
	_, _ = m.Update(readyWindow())
	_, _ = m.onHomeKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if m.homeSelected != 1 {
		t.Fatalf("home selection = %d, want built-in", m.homeSelected)
	}
	view := m.View().Content
	if !strings.Contains(view, "> 2  Built-in provider keys") {
		t.Fatalf("home view does not highlight built-in option:\n%s", view)
	}
}

func TestBuiltinProviderCollisionIsManagedAsCustom(t *testing.T) {
	setTempDir(t)
	m := newWizardModel()
	m.cfg.Providers = map[string]config.ProviderConfig{
		"openrouter": {Type: config.DefaultProviderType, BaseURL: "https://custom.example/v1"},
	}
	m.builtinProviders = []modelcatalog.Provider{{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1"}}
	m.screen = screenBuiltinList
	_, _ = m.Update(readyWindow())

	_, _ = m.onBuiltinListKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.screen != screenBuiltinList {
		t.Fatalf("screen = %d, want built-in list", m.screen)
	}
	if !strings.Contains(m.err, "managed as a custom provider") {
		t.Fatalf("error = %q", m.err)
	}
	if !strings.Contains(m.View().Content, "managed as custom") {
		t.Fatalf("collision is not marked in view:\n%s", m.View().Content)
	}
}

func TestManagerAddsEditsAndSelectsProvider(t *testing.T) {
	setTempDir(t)
	if err := config.Save(&config.Config{Providers: map[string]config.ProviderConfig{
		"openai": {Type: config.DefaultProviderType, APIKey: "sk-old", BaseURL: config.DefaultBaseURL},
	}, DefaultModel: "openai/gpt-4o"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	m := newWizardModel()
	_, _ = m.Update(readyWindow())
	m.openEditor("")
	saveProvider(t, m, "local", "", "http://localhost:11434/v1", "qwen3")
	if m.screen != screenList || !m.isDefault("local") {
		t.Fatalf("add should return to list and select local default: screen=%d default=%q", m.screen, m.cfg.DefaultModel)
	}
	m.openEditor("openai")
	saveProvider(t, m, "openai", "sk-new", "https://api.openai.com/v1", "gpt-4.1")
	if got := m.cfg.Providers["openai"].APIKey; got != "sk-new" {
		t.Fatalf("edited API key = %q", got)
	}
	m.selected = m.indexOf("local")
	m.makeDefault("local")
	if got := m.cfg.DefaultModel; got != "local/qwen3" {
		t.Fatalf("selected default model = %q", got)
	}
}

func TestManagerDeletesNonDefaultProvider(t *testing.T) {
	setTempDir(t)
	m := newWizardModel()
	m.cfg = &config.Config{Providers: map[string]config.ProviderConfig{
		"openai": {Type: config.DefaultProviderType, BaseURL: config.DefaultBaseURL},
		"local":  {Type: config.DefaultProviderType, BaseURL: "http://localhost:11434/v1"},
	}, DefaultModel: "openai/gpt-4o"}
	if err := config.Save(m.cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	m.refreshProviders()
	m.selected = m.indexOf("local")
	m.screen = screenDelete
	_, _ = m.onDeleteKey(tea.KeyPressMsg(tea.Key{Text: "y", Code: 'y'}))
	if _, exists := m.cfg.Providers["local"]; exists {
		t.Fatal("local provider was not deleted")
	}
	if m.result == nil {
		t.Fatal("delete should retain a usable manager result")
	}
}

func TestManagerRefusesDefaultProviderDeletion(t *testing.T) {
	setTempDir(t)
	m := newWizardModel()
	m.cfg = &config.Config{Providers: map[string]config.ProviderConfig{
		"openai": {Type: config.DefaultProviderType, BaseURL: config.DefaultBaseURL},
		"local":  {Type: config.DefaultProviderType, BaseURL: "http://localhost:11434/v1"},
	}, DefaultModel: "openai/gpt-4o"}
	m.refreshProviders()
	m.selected = m.indexOf("openai")
	m.screen = screenList
	_, _ = m.onListKey(tea.KeyPressMsg(tea.Key{Text: "d", Code: 'd'}))
	if m.screen != screenList || m.err == "" {
		t.Fatal("deleting the default provider should be refused")
	}
}

func TestManagerQuitsSuccessfullyWithExistingConfig(t *testing.T) {
	setTempDir(t)
	if err := config.Save(&config.Config{Providers: map[string]config.ProviderConfig{
		"openai": {Type: config.DefaultProviderType, BaseURL: config.DefaultBaseURL, Model: "gpt-4o"},
	}, DefaultModel: "openai/gpt-4o"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	m := newWizardModel()
	_, _ = m.onListKey(tea.KeyPressMsg(tea.Key{Text: "q", Code: 'q'}))
	if !m.quit || m.result == nil {
		t.Fatal("quitting an existing manager should return its unchanged configuration")
	}
}

func TestManagerSaveErrorIsShown(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("MYAGENT_DIR", filepath.Join(blocker, "config"))
	m := newWizardModel()
	_, _ = m.Update(readyWindow())
	m.openEditor("")
	saveProvider(t, m, "openai", "key", config.DefaultBaseURL, "gpt-4o")
	if m.done || m.err == "" {
		t.Fatal("save failure should keep the manager open with an error")
	}
}

func TestManagerSavesProviderWhenCustomModelCacheFails(t *testing.T) {
	setTempDir(t)
	m := newWizardModel()
	_, _ = m.Update(readyWindow())
	blocker := filepath.Join(t.TempDir(), "catalog-blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.catalog = modelcatalog.New(blocker)
	m.openEditor("")
	saveProvider(t, m, "openai", "sk-test", config.DefaultBaseURL, "gpt-4o-mini")
	if !m.done || m.result == nil {
		t.Fatalf("provider was not saved: %s", m.err)
	}
	if !strings.Contains(m.err, "Provider saved, but the custom model cache") {
		t.Fatalf("cache failure warning = %q", m.err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.DefaultModel; got != "openai/gpt-4o-mini" {
		t.Fatalf("DefaultModel = %q", got)
	}
}

func TestManagerDoesNotOverwriteMalformedConfig(t *testing.T) {
	setTempDir(t)
	path, err := config.Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m := newWizardModel()
	if !m.loadErr {
		t.Fatal("malformed config should leave the manager read-only")
	}
	_, _ = m.onKey(tea.KeyPressMsg(tea.Key{Text: "a", Code: 'a'}))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "{invalid" {
		t.Fatalf("malformed config changed to %q", data)
	}
}

func TestRunWizardNonInteractiveReturnsErrNoTty(t *testing.T) {
	setTempDir(t)
	cfg, err := RunWizard(nil)
	if err != ErrNoTty || cfg != nil {
		t.Fatalf("RunWizard = %v, %v; want nil, ErrNoTty", cfg, err)
	}
}
