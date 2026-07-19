package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/myagent/myagent/internal/config"
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
	if m.screen != screenEditor {
		t.Fatal("first run should open the provider editor")
	}
	saveProvider(t, m, "openai", "sk-test", config.DefaultBaseURL, "gpt-4o-mini")
	if !m.done || m.result == nil {
		t.Fatalf("first provider should finish setup: %s", m.err)
	}
	if got := m.result.DefaultModel; got != "openai/gpt-4o-mini" {
		t.Fatalf("DefaultModel = %q", got)
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
	saveProvider(t, m, "openai", "key", config.DefaultBaseURL, "gpt-4o")
	if m.done || m.err == "" {
		t.Fatal("save failure should keep the manager open with an error")
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
