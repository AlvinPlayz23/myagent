package plugin

import (
	"strings"
	"testing"
)

func TestLoadBytesMergeAndCollisions(t *testing.T) {
	global := []byte(`{"tools":[{"name":"echo","description":"d","parameters":{"type":"object","properties":{"msg":{"type":"string"}}},"command":"echo {{.msg}}"}],"commands":[{"name":"/hi","description":"d","prompt":"hi {{$args}}"}],"profiles":[{"name":"plan","description":"d","tools":["read"]}]}`)
	project := []byte(`{"tools":[{"name":"echo","description":"d2","parameters":{"type":"object"},"command":"echo2"}],"commands":[{"name":"/hi","description":"d2","prompt":"yo"}],"profiles":[{"name":"plan","description":"d2"}]}`)
	b := LoadBytes(global, project)
	if len(b.Tools) != 1 || b.Tools[0].Command != "echo2" {
		t.Fatalf("project tool should win: %+v", b.Tools)
	}
	if len(b.Commands) != 1 || b.Commands[0].Prompt != "yo" {
		t.Fatalf("project command should win: %+v", b.Commands)
	}
	if len(b.Profiles) != 1 || b.Profiles[0].Description != "d2" {
		t.Fatalf("project profile should win: %+v", b.Profiles)
	}
	found := false
	for _, w := range b.Warnings {
		if strings.Contains(w, "overrides global") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected override warnings: %v", b.Warnings)
	}
}

func TestLoadBytesSkipsInvalid(t *testing.T) {
	b := LoadBytes([]byte(`{"tools":[{"name":"bash","description":"x","parameters":{"type":"object"},"command":"x"}],"commands":[{"name":"/help","description":"x","prompt":"x"}],"profiles":[{"name":"bad","description":"x","effort":"nope"}]}`), nil)
	if len(b.Tools) != 0 || len(b.Commands) != 0 || len(b.Profiles) != 0 {
		t.Fatalf("invalid entries should be skipped: %+v %+v %+v", b.Tools, b.Commands, b.Profiles)
	}
	if len(b.Warnings) != 3 {
		t.Fatalf("expected 3 warnings, got %v", b.Warnings)
	}
}

func TestEnabledKillSwitch(t *testing.T) {
	b := LoadBytes([]byte(`{"enabled":false,"tools":[{"name":"a","description":"d","parameters":{"type":"object"},"command":"x"}]}`), nil)
	if !b.Disabled || len(b.Tools) != 0 {
		t.Fatalf("enabled:false should disable all: %+v", b)
	}
	b = LoadBytes(nil, nil)
	if b.Disabled {
		t.Fatal("empty should not be disabled")
	}
}

func TestShellToolRender(t *testing.T) {
	def := ToolDef{Name: "greet", Description: "d", Parameters: map[string]any{"type": "object", "properties": map[string]any{"who": map[string]any{"type": "string"}}}, Command: "echo {{.who}} {{.cwd}}"}
	st := NewShellTool(def, "/tmp", "sess1")
	out, err := st.Render(map[string]any{"who": "a b"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a b") || !strings.Contains(out, "/tmp") {
		t.Fatalf("render = %q", out)
	}
	if _, err := st.Render(map[string]any{}); err == nil {
		// missing arg renders "" (declared), template still renders
		t.Log("empty args render ok")
	}
	bad := ToolDef{Name: "bad", Description: "d", Parameters: map[string]any{"type": "object"}, Command: "echo {{.nope}}"}
	if _, err := NewShellTool(bad, "/tmp", "").Render(map[string]any{}); err == nil {
		t.Fatal("expected template error for undeclared field")
	}
}

func TestDenyBlocks(t *testing.T) {
	deny, err := CompileDeny([]string{`rm\s+-rf`})
	if err != nil {
		t.Fatal(err)
	}
	if err := deny("rm -rf /"); err == nil {
		t.Fatal("expected deny match")
	}
	if err := deny("ls -la"); err != nil {
		t.Fatalf("unexpected deny: %v", err)
	}
}
