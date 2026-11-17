package tui

import (
	"strings"
	"testing"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
)

func TestModelPickerPresentation(t *testing.T) {
	m := newPanelModel()
	m.modelID = "local/sonnet"
	m.models.open([]modelcatalog.Model{
		{Provider: "local", ID: "sonnet", ContextWindow: 200000},
		{Provider: "studio", ProviderName: "Studio", ID: "gemini-pro", Name: "Gemini Pro", ContextWindow: 1000000},
	}, "")
	state := m.kitModelPicker()
	if state.SelectedID != "local/sonnet" {
		t.Fatalf("initial selection = %q", state.SelectedID)
	}
	if len(state.Items) != 2 {
		t.Fatalf("items = %d", len(state.Items))
	}
	current := state.Items[0]
	if current.Title != "sonnet" || current.Desc != "" || current.Group != "local" || current.Footer != "✓ current · 200k" {
		t.Fatalf("current row = %+v", current)
	}
	if other := state.Items[1]; other.Title != "Gemini Pro" || other.Desc != "gemini-pro" || other.Group != "Studio" || strings.Contains(other.Footer, "current") {
		t.Fatalf("other row = %+v", other)
	}
}

func TestCommandPickerUsesContiguousCategories(t *testing.T) {
	m := newPanelModel()
	m.picker.sync("/")
	state := m.kitCommandPicker()
	seen := map[string]bool{}
	last := ""
	for _, item := range state.Items {
		if item.Group != last {
			if seen[item.Group] {
				t.Fatalf("repeated category %q", item.Group)
			}
			seen[item.Group] = true
			last = item.Group
		}
	}
	for _, group := range []string{"Suggested", "Session", "Tools & interface"} {
		if !seen[group] {
			t.Errorf("missing category %q", group)
		}
	}
}

func TestCommandPaletteOpensOptionalArgumentPickersImmediately(t *testing.T) {
	for command, wantKind := range map[string]string{"/models": "models", "/effort": "effort", "/tools": "tools", "/customize": "customize"} {
		t.Run(command, func(t *testing.T) {
			m := newPanelModel()
			m.runner = newRunner(agent.Config{}, newMsgQueue(), nil)
			m.selectModel = func(string, string) (llm.Provider, llm.Model, error) { return nil, llm.Model{}, nil }
			m.availableModels = func() []modelcatalog.Model {
				return []modelcatalog.Model{{Provider: "local", ID: "sonnet"}, {Provider: "local", ID: "gemini"}}
			}
			m.input.SetValue("/")
			m.picker.sync("/")
			m.KitPickerAccept(command)
			if got := m.kitPanelKind(); got != wantKind {
				t.Fatalf("accepted %s opened %q, want %q", command, got, wantKind)
			}
			if m.input.Value() != "" {
				t.Fatalf("command left draft %q", m.input.Value())
			}
		})
	}
	m := newPanelModel()
	m.picker.sync("/")
	m.KitPickerAccept("/rename")
	if got := m.input.Value(); got != "/rename " {
		t.Fatalf("required argument draft = %q", got)
	}
}
