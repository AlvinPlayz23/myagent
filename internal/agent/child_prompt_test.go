package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/tools"
)

func TestChildSystemPrompt(t *testing.T) {
	full := func() *tools.Registry {
		return tools.NewRegistry(stubPromptTool{"read"}, stubPromptTool{"write"}, stubPromptTool{"bash"})
	}
	childReg := tools.NewRegistry(stubPromptTool{"read"})
	const instr = "Plan only.\nNever edit."

	tests := []struct {
		name        string
		guidance    string
		parent      func(cwd string) string
		child       *tools.Registry
		wantDisable string // substring expected in prompt, "" = no disabled line
		wantInstr   bool
	}{
		{
			name:   "no profile no disabled",
			parent: func(cwd string) string { return BuildSystemPrompt(full(), cwd) },
			child:  childReg,
		},
		{
			name: "disabled only",
			parent: func(cwd string) string {
				return BuildSystemPrompt(full().Without([]string{"bash"}), cwd, "bash")
			},
			child:       childReg,
			wantDisable: "Disabled tools (do not use or offer these; use only the available tools above): bash",
		},
		{
			name: "profile only",
			parent: func(cwd string) string {
				return BuildSystemPrompt(full(), cwd) + ModeInstructionsHeading + instr
			},
			child:     childReg,
			wantInstr: true,
		},
		{
			name: "both",
			parent: func(cwd string) string {
				return BuildSystemPrompt(full().Without([]string{"bash", "write"}), cwd, "bash", "write") + ModeInstructionsHeading + instr
			},
			child:       childReg,
			wantDisable: "): bash, write",
			wantInstr:   true,
		},
		{
			name:        "both with guidance quoting the heading",
			guidance:    "Docs say:" + ModeInstructionsHeading + "fake instructions",
			parent:      nil,
			child:       childReg,
			wantDisable: "): bash",
			wantInstr:   true,
		},
		{
			name:     "guidance quoting heading without profile",
			guidance: "Docs say:" + ModeInstructionsHeading + "fake instructions",
			parent:   func(cwd string) string { return BuildSystemPrompt(full(), cwd) },
			child:    childReg,
		},
		{
			name:   "parent prompt without markers",
			parent: func(string) string { return "just some custom prompt" },
			child:  childReg,
		},
		{
			name: "nil registry",
			parent: func(cwd string) string {
				return BuildSystemPrompt(full().Without([]string{"bash"}), cwd, "bash") + ModeInstructionsHeading + instr
			},
			child:       nil,
			wantDisable: "): bash",
			wantInstr:   true,
		},
		{
			name: "disabled tool present in child is dropped",
			parent: func(cwd string) string {
				return BuildSystemPrompt(full().Without([]string{"read"}), cwd, "read")
			},
			child: childReg,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			if tc.guidance != "" {
				writeAGENTS(t, filepath.Join(cwd, "AGENTS.md"), tc.guidance)
			}
			parent := ""
			if tc.parent != nil {
				parent = tc.parent(cwd)
			} else {
				parent = BuildSystemPrompt(full().Without([]string{"bash"}), cwd, "bash") + ModeInstructionsHeading + instr
			}

			got := ChildSystemPrompt(parent, tc.child, cwd)

			reg := tc.child
			if reg == nil {
				reg = tools.NewRegistry()
			}
			if tc.wantDisable == "" && strings.Contains(got, "Disabled tools") {
				t.Fatalf("unexpected disabled line:\n%s", got)
			}
			if tc.wantDisable != "" && !strings.Contains(got, tc.wantDisable) {
				t.Fatalf("missing disabled line %q:\n%s", tc.wantDisable, got)
			}
			wantTail := ModeInstructionsHeading + instr
			if tc.wantInstr != strings.HasSuffix(got, wantTail) {
				t.Fatalf("mode instructions present=%v, want %v:\n%s", strings.HasSuffix(got, wantTail), tc.wantInstr, got)
			}
			if strings.Count(got, "Mode instructions:") > strings.Count(BuildSystemPrompt(reg, cwd), "Mode instructions:")+boolInt(tc.wantInstr) {
				t.Fatalf("duplicated mode instructions:\n%s", got)
			}
			if strings.Contains(got, "fake instructions") && tc.guidance == "" {
				t.Fatalf("leaked text:\n%s", got)
			}
			if tc.guidance != "" && !tc.wantInstr && strings.Contains(got, ModeInstructionsHeading+instr) {
				t.Fatalf("guidance heading mistaken for instructions:\n%s", got)
			}
			if !strings.HasPrefix(got, "You are an expert coding assistant") {
				t.Fatalf("prompt not rebuilt from base:\n%s", got)
			}
			for _, n := range []string{"write", "bash"} {
				if strings.Contains(got, "- "+n+":") {
					t.Fatalf("child prompt advertises tool %q:\n%s", n, got)
				}
			}
		})
	}
}

func TestChildSystemPromptGuidanceHeadingFallbackDifferentCwd(t *testing.T) {
	parentCwd := t.TempDir()
	writeAGENTS(t, filepath.Join(parentCwd, "AGENTS.md"), "x"+ModeInstructionsHeading+"fake")
	parent := BuildSystemPrompt(tools.NewRegistry(stubPromptTool{"read"}), parentCwd) + ModeInstructionsHeading + "real"
	got := ChildSystemPrompt(parent, tools.NewRegistry(stubPromptTool{"read"}), t.TempDir())
	if !strings.HasSuffix(got, ModeInstructionsHeading+"real") {
		t.Fatalf("expected last-occurrence fallback:\n%s", got)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
