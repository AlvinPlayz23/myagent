package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// useTempDir points MYAGENT_DIR at a per-test temp dir so config files do not
// collide with the developer's real ~/.myagent.
func useTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MYAGENT_DIR", dir)
	// Make sure stale OPENAI_* env from the developer machine does not leak
	// into expectations. Load() re-reads these, but NeedsSetup's env path
	// must behave the way the tests assert.
	t.Setenv("OPENAI_API_KEY", "")
}

// configPath returns the resolved config.json path for the active MYAGENT_DIR.
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

func TestNeedsSetup_MissingFile_NoEnv(t *testing.T) {
	useTempDir(t)
	needs, err := NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if !needs {
		t.Fatal("expected setup to be required when no config.json and no env API key")
	}
}

func TestNeedsSetup_MissingFile_EnvApiKey(t *testing.T) {
	useTempDir(t)
	t.Setenv("OPENAI_API_KEY", "sk-from-env")
	needs, err := NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if !needs {
		t.Fatal("missing config.json must require setup even with an env API key")
	}
}

func TestNeedsSetup_EmptyFile(t *testing.T) {
	useTempDir(t)
	writeFile(t, configPath(t), "")
	needs, err := NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if !needs {
		t.Fatal("empty/blank config.json should require setup")
	}
}

func TestNeedsSetup_WhitespaceOnlyFile(t *testing.T) {
	useTempDir(t)
	writeFile(t, configPath(t), "   \n\t\n ")
	needs, err := NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if !needs {
		t.Fatal("whitespace-only config.json should require setup")
	}
}

func TestNeedsSetup_NonEmptyConfig(t *testing.T) {
	useTempDir(t)
	writeFile(t, configPath(t), `{"apiKey":"sk-real","model":"gpt-4o"}`)
	needs, err := NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if needs {
		t.Fatal("a non-empty config.json must not trigger setup")
	}
}

func TestNeedsSetup_FileWithoutApiKey(t *testing.T) {
	useTempDir(t)
	writeFile(t, configPath(t), `{"model":"gpt-4o"}`)
	needs, err := NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if needs {
		t.Fatal("a non-empty config.json should skip setup even without apiKey")
	}
}

func TestNeedsSetup_InvalidJson(t *testing.T) {
	useTempDir(t)
	writeFile(t, configPath(t), `{"apiKey": "broken"`) // malformed
	needs, err := NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if needs {
		t.Fatal("a non-empty malformed config.json should skip setup and be reported by Load")
	}
}

func TestExists_Missing(t *testing.T) {
	useTempDir(t)
	got, err := Exists()
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if got {
		t.Fatal("missing file should not exist")
	}
}

func TestExists_PresentAndNonEmpty(t *testing.T) {
	useTempDir(t)
	writeFile(t, configPath(t), `{"apiKey":"x"}`)
	got, err := Exists()
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !got {
		t.Fatal("non-empty file should exist")
	}
}

func TestSave_WritesFileWithPermissions(t *testing.T) {
	useTempDir(t)
	cfg := &Config{
		APIKey:  "sk-test",
		BaseURL: "https://example.test/v1",
		Model:   "gpt-4o-mini",
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(configPath(t))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	perm := info.Mode().Perm()
	if runtime.GOOS != "windows" && perm != 0o600 {
		t.Fatalf("config.json should be 0600, got %o", perm)
	}

	data, err := os.ReadFile(configPath(t))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.APIKey != "sk-test" || got.BaseURL != "https://example.test/v1" || got.Model != "gpt-4o-mini" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatal("saved file should end with a trailing newline")
	}
}

func TestSave_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MYAGENT_DIR", filepath.Join(dir, "nested", "config-dir"))
	t.Setenv("OPENAI_API_KEY", "")
	if err := Save(&Config{APIKey: "sk-x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Join(dir, "nested", "config-dir"), "config.json")); err != nil {
		t.Fatalf("Stat nested config.json: %v", err)
	}
}

func TestSave_ReplacesEmptyExistingFile(t *testing.T) {
	useTempDir(t)
	writeFile(t, configPath(t), "\n\t")

	if err := Save(&Config{APIKey: "sk-replaced"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIKey != "sk-replaced" {
		t.Fatalf("APIKey = %q, want sk-replaced", cfg.APIKey)
	}
}

func TestSave_ThenNeedsSetupFalse(t *testing.T) {
	useTempDir(t)
	if err := Save(&Config{APIKey: "sk-after-save"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	needs, err := NeedsSetup()
	if err != nil {
		t.Fatalf("NeedsSetup: %v", err)
	}
	if needs {
		t.Fatal("after Save with an apiKey, NeedsSetup should be false")
	}
}

func TestLoad_AppliesDefaultsAfterSave(t *testing.T) {
	useTempDir(t)
	if err := Save(&Config{APIKey: "sk-only"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIKey != "sk-only" {
		t.Fatalf("APIKey = %q, want sk-only", cfg.APIKey)
	}
	if cfg.BaseURL != DefaultBaseURL {
		t.Fatalf("BaseURL = %q, want default %q", cfg.BaseURL, DefaultBaseURL)
	}
	if cfg.Model != DefaultModel {
		t.Fatalf("Model = %q, want default %q", cfg.Model, DefaultModel)
	}
}

func TestSave_DoesNotOverwriteEnvOnLoad(t *testing.T) {
	useTempDir(t)
	if err := Save(&Config{APIKey: "sk-file"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv("OPENAI_API_KEY", "sk-env-override")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIKey != "sk-env-override" {
		t.Fatalf("env must override file; got %q", cfg.APIKey)
	}
}
