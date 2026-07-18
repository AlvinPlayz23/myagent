package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPrintModeRequiresFirstRunSetup(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	t.Setenv("OPENAI_API_KEY", "")

	err := run([]string{"-p", "hello"})
	if err == nil {
		t.Fatal("print mode should require setup when config.json is missing")
	}
	if !strings.Contains(err.Error(), "run `myagent` once to complete setup") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPrintModeSkipsSetupForNonEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MYAGENT_DIR", dir)
	t.Setenv("OPENAI_API_KEY", "")
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"model":"gpt-4o"}`), 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	err := run([]string{"-p", "hello"})
	if err == nil {
		t.Fatal("incomplete config should fail API-key validation")
	}
	if !strings.Contains(err.Error(), "no API key:") {
		t.Fatalf("expected API-key validation error, got: %v", err)
	}
}
