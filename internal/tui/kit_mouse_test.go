package tui

import (
	"strings"
	"testing"

	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
)

// clickKit drives n kit-mode presses on one content cell.
func clickKit(m *model, n, row, col int) {
	for range n {
		m.KitMouseClick(row, col, 0)
	}
}

func TestKitDoubleClickSelectsWordAndKeeps(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.hasSessionTitle = true
	m.transcript.addUser("hello world")
	m.onResize(40, 20)
	m.kitWidth = 40
	var copied string
	m.clipboardWrite = func(text string) error {
		copied = text
		return nil
	}

	clickKit(m, 2, 0, 2)
	m.KitMouseRelease(0, 2)
	if copied != "hello" {
		t.Fatalf("clipboard = %q, want hello", copied)
	}
	if m.selection == nil || !m.keepSelection {
		t.Fatal("word selection was not kept")
	}
	if m.clickCount != 2 {
		t.Fatalf("click count = %d, want 2", m.clickCount)
	}

	// The next press clears the sticky highlight, matching the legacy path.
	m.KitMouseClick(0, 2, 0)
	if m.keepSelection {
		t.Fatal("press did not clear the sticky selection")
	}
}

func TestKitTripleClickSelectsParagraph(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.hasSessionTitle = true
	m.transcript.addUser("hello world")
	m.onResize(40, 20)
	m.kitWidth = 40
	var copied string
	m.clipboardWrite = func(text string) error {
		copied = text
		return nil
	}

	clickKit(m, 3, 0, 2)
	m.KitMouseRelease(0, 2)
	if !strings.Contains(copied, "hello world") {
		t.Fatalf("clipboard = %q, want the whole rendered paragraph", copied)
	}
	if strings.TrimSpace(copied) == "hello" {
		t.Fatalf("clipboard = %q, triple-click collapsed to a word", copied)
	}
	if m.selection == nil || !m.keepSelection {
		t.Fatal("paragraph selection was not kept")
	}
}

func TestKitPickerProjectsModelsAndCommands(t *testing.T) {
	m := newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
	m.onResize(80, 24)

	m.models.open([]modelcatalog.Model{
		{Provider: "p", ProviderName: "P", ID: "a", Name: "Alpha", ContextWindow: 128000},
		{Provider: "p", ProviderName: "P", ID: "b", Name: "Beta"},
	}, "al")
	state, active := m.KitPicker()
	if !active || state.Kind != "models" {
		t.Fatalf("model picker = %+v active=%v", state, active)
	}
	if state.Query != "al" || state.SelectedID != "p/a" {
		t.Fatalf("query/selection = %q/%q, want al/p/a", state.Query, state.SelectedID)
	}
	if len(state.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(state.Items))
	}
	if state.Items[0].ID != "p/a" || state.Items[0].Title != "Alpha" ||
		state.Items[0].Group != "P" || state.Items[0].Footer != "128k" {
		t.Fatalf("first model projection = %+v", state.Items[0])
	}
	m.models.close()

	m.input.SetValue("/")
	m.syncPickers()
	state, active = m.KitPicker()
	if !active || state.Kind != "commands" {
		t.Fatalf("command picker = %+v active=%v", state, active)
	}
	if state.Title != "Commands" || len(state.Items) == 0 {
		t.Fatalf("command projection = %+v", state)
	}
	found := false
	for _, item := range state.Items {
		if item.ID == "/help" {
			found = strings.TrimSpace(item.Desc) != ""
			break
		}
	}
	if !found {
		t.Fatal("/help projection missing a description")
	}
}
