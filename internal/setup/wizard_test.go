package setup

import (
	"os"
	"path/filepath"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/myagent/myagent/internal/config"
)

// setTempDir points MYAGENT_DIR at a per-test temp dir and clears stray
// OPENAI_* env so the wizard's env-derived placeholders are predictable.
func setTempDir(t *testing.T) {
	t.Helper()
	t.Setenv("MYAGENT_DIR", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("MYAGENT_MODEL", "")
}

func configPath(t *testing.T) string {
	t.Helper()
	p, err := config.Path()
	if err != nil {
		t.Fatalf("config.Path: %v", err)
	}
	return p
}

// readyWindow is the smallest tea.WindowSizeMsg the wizard needs to render so
// View() returns a non-empty string and resizeInputs sets field widths.
func readyWindow() tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: 80, Height: 24}
}

// TestWizardModel_RejectsBlankApiKey ensures submitField keeps the user on
// the API key field with an error when they try to advance without a key.
func TestWizardModel_RejectsBlankApiKey(t *testing.T) {
	setTempDir(t)
	m := newWizardModel()
	_, _ = m.Update(readyWindow())

	if m.current != 0 {
		t.Fatalf("initial field = %d, want 0", m.current)
	}
	_, _ = m.submitField()
	if m.current != 0 {
		t.Fatalf("blank API key advanced to %d, want 0", m.current)
	}
	if m.err == "" {
		t.Fatal("expected an error after submitting a blank API key")
	}
}

func TestWizardModel_CancelsOnEscAtFirstField(t *testing.T) {
	setTempDir(t)
	m := newWizardModel()
	_, _ = m.Update(readyWindow())

	_, _ = m.onKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if !m.quit {
		t.Fatal("Esc on blank first field should cancel the wizard")
	}
}

func TestWizardModel_FullFlowWritesConfig(t *testing.T) {
	setTempDir(t)
	m := newWizardModel()
	_, _ = m.Update(readyWindow())

	// API key field, then advance to Base URL, then Model, then finish.
	m.fields[0].input.SetValue("sk-wizard")
	_, _ = m.nextField(1)
	if m.current != 1 {
		t.Fatalf("after API key, current = %d, want 1", m.current)
	}

	_, _ = m.nextField(1)
	if m.current != 2 {
		t.Fatalf("after Base URL, current = %d, want 2", m.current)
	}

	m.fields[2].input.SetValue("gpt-4o-mini")
	_, _ = m.nextField(1) // advancing past the last field finalizes

	if !m.done {
		t.Fatal("wizard should be done after advancing past the last field")
	}
	if m.result == nil {
		t.Fatalf("expected a non-nil resolved config after finalize")
	}
	provider := m.result.Providers[config.DefaultProviderName]
	if provider.APIKey != "sk-wizard" {
		t.Fatalf("APIKey = %q, want sk-wizard", provider.APIKey)
	}
	if m.result.DefaultModel != "openai/gpt-4o-mini" {
		t.Fatalf("DefaultModel = %q, want openai/gpt-4o-mini", m.result.DefaultModel)
	}
	if provider.BaseURL != config.DefaultBaseURL {
		t.Fatalf("BaseURL = %q, want default %q", provider.BaseURL, config.DefaultBaseURL)
	}

	if _, err := os.Stat(configPath(t)); err != nil {
		t.Fatalf("config.json not written: %v", err)
	}

	// After save, NeedsSetup must be false (file has an apiKey).
	needs, err := config.NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if needs {
		t.Fatal("NeedsSetup should be false after the wizard writes a config")
	}
}

func TestWizardModel_PreservesOtherProviders(t *testing.T) {
	setTempDir(t)
	if err := config.Save(&config.Config{
		Providers: map[string]config.ProviderConfig{
			"local": {Type: config.DefaultProviderType, BaseURL: "http://localhost:11434/v1"},
		},
		DefaultModel: "local/qwen3",
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	m := newWizardModel()
	_, _ = m.Update(readyWindow())
	m.fields[0].input.SetValue("sk-new")
	m.fields[1].input.SetValue("https://api.openai.com/v1")
	m.fields[2].input.SetValue("gpt-4o-mini")
	_, _ = m.nextField(1)
	_, _ = m.nextField(1)
	_, _ = m.nextField(1)
	if !m.done {
		t.Fatalf("wizard should finish: %s", m.err)
	}
	if _, ok := m.result.Providers["local"]; !ok {
		t.Fatal("wizard should preserve existing providers")
	}
}

func TestWizardModel_FinalizeSurfacesSaveError(t *testing.T) {
	// Point MYAGENT_DIR at a path that cannot be created (parent is a regular
	// file) so Save fails and finalize leaves err set without finishing.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file-blocks-mkdir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	t.Setenv("MYAGENT_DIR", filepath.Join(blocker, "config-dir"))
	t.Setenv("OPENAI_API_KEY", "")

	m := newWizardModel()
	_, _ = m.Update(readyWindow())
	m.fields[0].input.SetValue("sk-never-saved")
	m.fields[1].input.SetValue("https://x/v1")
	m.fields[2].input.SetValue("m")

	_, _ = m.nextField(1)
	_, _ = m.nextField(1)
	_, _ = m.nextField(1)

	if m.done {
		t.Fatal("finalize should not mark done when Save fails")
	}
	if m.err == "" {
		t.Fatal("expected an error message when Save fails")
	}
	if _, err := os.Stat(filepath.Join(filepath.Join(blocker, "config-dir"), "config.json")); !os.IsNotExist(err) {
		t.Fatalf("config.json should not exist after Save failed; stat err = %v", err)
	}
}

func TestRunWizard_NonInteractiveReturnsErrNoTty(t *testing.T) {
	setTempDir(t)
	// When this test runs under `go test`, stdin/stdout are not a tty, so
	// isInteractive() returns false and RunWizard refuses to run.
	cfg, err := RunWizard(nil)
	if err != ErrNoTty {
		t.Fatalf("err = %v, want ErrNoTty", err)
	}
	if cfg != nil {
		t.Fatalf("cfg = %v, want nil", cfg)
	}
}
