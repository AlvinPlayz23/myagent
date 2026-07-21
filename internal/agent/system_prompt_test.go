package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/myagent/myagent/internal/tools"
)

func TestBuildSystemPromptLoadsAGENTSFilesFromRootToCwd(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "service", "api")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAGENTS(t, filepath.Join(root, "AGENTS.md"), "root instruction")
	writeAGENTS(t, filepath.Join(root, "service", "AGENTS.md"), "service instruction")
	writeAGENTS(t, filepath.Join(nested, "AGENTS.md"), "api instruction")

	prompt := BuildSystemPrompt(tools.NewRegistry(), nested)
	rootIndex := strings.Index(prompt, "root instruction")
	serviceIndex := strings.Index(prompt, "service instruction")
	apiIndex := strings.Index(prompt, "api instruction")
	if rootIndex == -1 || serviceIndex == -1 || apiIndex == -1 {
		t.Fatalf("prompt did not include all AGENTS.md files:\n%s", prompt)
	}
	if rootIndex > serviceIndex || serviceIndex > apiIndex {
		t.Fatalf("guidance order is not root to cwd:\n%s", prompt)
	}
}

func TestBuildSystemPromptOmitsGuidanceSectionWhenNoAGENTSFileExists(t *testing.T) {
	prompt := BuildSystemPrompt(tools.NewRegistry(), t.TempDir())
	if strings.Contains(prompt, "Repository instructions:") {
		t.Fatalf("prompt unexpectedly contains repository guidance:\n%s", prompt)
	}
}

func TestLoadRepositoryGuidanceSkipsNonRegularAGENTSFile(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "AGENTS.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	if guidance := loadRepositoryGuidance(nested); guidance != "" {
		t.Fatalf("guidance = %q, want empty", guidance)
	}
}

func TestHasRepositoryGuidance(t *testing.T) {
	root := t.TempDir()
	if HasRepositoryGuidance(root) {
		t.Fatal("HasRepositoryGuidance = true without AGENTS.md")
	}
	writeAGENTS(t, filepath.Join(root, "AGENTS.md"), "follow the rules")
	if !HasRepositoryGuidance(root) {
		t.Fatal("HasRepositoryGuidance = false with AGENTS.md")
	}
}

func writeAGENTS(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
