package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/tools"
	"github.com/AlvinPlayz23/myagent/internal/types"
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

func TestBuildSystemPromptOmitsDisabledToolsAndBashGuidance(t *testing.T) {
	reg := tools.NewRegistry(
		stubPromptTool{"read"},
		stubPromptTool{"write"},
		stubPromptTool{"edit"},
		stubPromptTool{"bash"},
	).Without([]string{"bash"})

	prompt := BuildSystemPrompt(reg, t.TempDir())
	if strings.Contains(prompt, "- bash:") {
		t.Fatalf("prompt still advertises the disabled bash tool:\n%s", prompt)
	}
	if strings.Contains(prompt, "Use bash for file operations") {
		t.Fatalf("prompt still contains bash-specific guidance:\n%s", prompt)
	}
	for _, name := range []string{"read", "write", "edit"} {
		if !strings.Contains(prompt, "- "+name+":") {
			t.Fatalf("prompt is missing enabled tool %q:\n%s", name, prompt)
		}
	}
}

func TestBuildSystemPromptIntroMatchesEnabledTools(t *testing.T) {
	reg := tools.NewRegistry(stubPromptTool{"read"})
	prompt := BuildSystemPrompt(reg, t.TempDir())
	if !strings.Contains(prompt, "You help users by reading files.") {
		t.Fatalf("intro does not match read-only registry:\n%s", prompt)
	}
	for _, leak := range []string{"executing shell commands", "editing code", "writing new files"} {
		if strings.Contains(prompt, leak) {
			t.Fatalf("intro leaks disabled capability %q:\n%s", leak, prompt)
		}
	}
}

func TestBuildSystemPromptFullRegistryKeepsAllCapabilities(t *testing.T) {
	reg := tools.NewRegistry(
		stubPromptTool{"read"},
		stubPromptTool{"write"},
		stubPromptTool{"edit"},
		stubPromptTool{"bash"},
	)
	prompt := BuildSystemPrompt(reg, t.TempDir())
	for _, cap := range []string{"reading files", "executing shell commands", "editing code", "writing new files"} {
		if !strings.Contains(prompt, cap) {
			t.Fatalf("intro is missing capability %q:\n%s", cap, prompt)
		}
	}
}

func TestBuildSystemPromptEmptyRegistryHasNoToolsMessage(t *testing.T) {
	prompt := BuildSystemPrompt(tools.NewRegistry(), t.TempDir())
	if !strings.Contains(prompt, "no tools enabled") {
		t.Fatalf("empty registry prompt lacks a no-tools message:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Available tools: (none)") {
		t.Fatalf("empty registry prompt lacks an explicit empty tool list:\n%s", prompt)
	}
}

func TestBuildSystemPromptDisabledLine(t *testing.T) {
	base := tools.NewRegistry(
		stubPromptTool{"read"},
		stubPromptTool{"write"},
		stubPromptTool{"edit"},
		stubPromptTool{"bash"},
	)
	prompt := BuildSystemPrompt(base.Without([]string{"bash"}), t.TempDir(), "bash")
	if !strings.Contains(prompt, "Disabled tools (do not use or offer these") || !strings.Contains(prompt, "bash") {
		t.Fatalf("prompt lacks the disabled override:\n%s", prompt)
	}
	// An enabled name passed in the deny list must not appear as disabled.
	prompt = BuildSystemPrompt(base.Without([]string{"bash"}), t.TempDir(), "read", "bash")
	line := prompt[strings.Index(prompt, "Disabled tools"):]
	if strings.Contains(line[:strings.Index(line, "\n")], "read") {
		t.Fatalf("disabled line leaked an enabled tool:\n%s", prompt)
	}
	if prompt := BuildSystemPrompt(base, t.TempDir()); strings.Contains(prompt, "Disabled tools") {
		t.Fatalf("nil deny list should emit no disabled line:\n%s", prompt)
	}
}

func TestBuildSystemPromptNamesPluginToolsInIntro(t *testing.T) {
	reg := tools.NewRegistry(stubPromptTool{"read"}, stubPromptTool{"my-skill"})
	prompt := BuildSystemPrompt(reg, t.TempDir())
	if !strings.Contains(prompt, "running plugin tools") || !strings.Contains(prompt, "my-skill") {
		t.Fatalf("intro does not name the plugin tool:\n%s", prompt)
	}
}

func TestBuildSystemPromptKeepsBashGuidanceWhenEnabled(t *testing.T) {
	reg := tools.NewRegistry(stubPromptTool{"bash"})
	prompt := BuildSystemPrompt(reg, t.TempDir())
	if !strings.Contains(prompt, "Use bash for file operations") {
		t.Fatalf("prompt is missing bash guidance while bash is enabled:\n%s", prompt)
	}
}

// stubPromptTool is a minimal tools.Tool for prompt tests.
type stubPromptTool struct{ name string }

func (s stubPromptTool) Name() string               { return s.name }
func (s stubPromptTool) Description() string        { return "does " + s.name }
func (s stubPromptTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (s stubPromptTool) Execute(context.Context, string, map[string]any) (*types.ToolResult, error) {
	return nil, nil
}
