package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/myagent/myagent/internal/session"
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
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"providers":{"openai":{"type":"openai-compatible"}},"default_model":"openai/gpt-4o"}`), 0o600); err != nil {
		t.Fatalf("write config.json: %v", err)
	}

	err := run([]string{"-p", "hello"})
	if err == nil {
		t.Fatal("incomplete config should fail API-key validation")
	}
	if !strings.Contains(err.Error(), "has no baseUrl") {
		t.Fatalf("expected API-key validation error, got: %v", err)
	}
}

func TestRunAuthOpensSetup(t *testing.T) {
	t.Setenv("MYAGENT_DIR", t.TempDir())
	err := run([]string{"auth"})
	if err == nil || !strings.Contains(err.Error(), "requires an interactive terminal") {
		t.Fatalf("auth should launch the setup wizard, got: %v", err)
	}
}

func TestRunAuthRejectsArguments(t *testing.T) {
	err := run([]string{"auth", "openai"})
	if err == nil || !strings.Contains(err.Error(), "does not accept arguments") {
		t.Fatalf("auth with arguments should fail, got: %v", err)
	}
}

func TestCollapseHomePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	inside := filepath.Join(home, ".myagent", "sessions", "session.jsonl")
	if got, want := collapseHomePath(inside), "~"+string(filepath.Separator)+filepath.Join(".myagent", "sessions", "session.jsonl"); got != want {
		t.Errorf("collapseHomePath(%q) = %q, want %q", inside, got, want)
	}

	outside := filepath.Join(t.TempDir(), "session.jsonl")
	if got := collapseHomePath(outside); got != outside {
		t.Errorf("collapseHomePath(%q) = %q, want unchanged path", outside, got)
	}
}

func TestResumeInstructions(t *testing.T) {
	// Construct a real session so the test covers its generated id and path.
	t.Setenv("MYAGENT_DIR", t.TempDir())
	sess, err := session.Create("/work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer sess.Close()

	got := resumeInstructions(sess)
	if !strings.Contains(got, "Resume this session:\n") {
		t.Errorf("instructions missing heading: %q", got)
	}
	if !strings.Contains(got, "myagent --resume-id "+sess.ID()) {
		t.Errorf("instructions missing resume-id command: %q", got)
	}
	if !strings.Contains(got, "myagent --resume "+collapseHomePath(sess.Path())) {
		t.Errorf("instructions missing resume-path command: %q", got)
	}
}
