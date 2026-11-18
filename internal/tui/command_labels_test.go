package tui

import "testing"

func TestCommandItemsHaveLabelsAndStableIDs(t *testing.T) {
	seen := make(map[string]bool, len(commandItems))
	for _, item := range commandItems {
		if item.name == "" {
			t.Error("command has an empty ID")
		}
		if seen[item.name] {
			t.Errorf("duplicate command ID %q", item.name)
		}
		seen[item.name] = true
		if item.label == "" {
			t.Errorf("%s has an empty display label", item.name)
		}
		if item.description == "" {
			t.Errorf("%s has an empty description", item.name)
		}
	}

	customize, ok := commandItemByName("/customize")
	if !ok {
		t.Fatal("/customize command is missing")
	}
	if customize.label != "Customize myagent" {
		t.Errorf("/customize label = %q, want %q", customize.label, "Customize myagent")
	}
}

func TestCommandPickerDisplaysCommandIDAndDescription(t *testing.T) {
	m := newPanelModel()
	m.picker.sync("/")
	state := m.kitCommandPicker()
	if !state.Inline {
		t.Fatal("typed slash projected a modal palette")
	}

	for _, item := range state.Items {
		if item.ID == "/customize" {
			if item.Title != "/customize" {
				t.Errorf("/customize title = %q", item.Title)
			}
			if item.Desc != "Customize the startup style and composer" {
				t.Errorf("/customize description = %q", item.Desc)
			}
			return
		}
	}
	t.Fatal("/customize missing from command picker")
}

// TestCtrlPCommandPaletteIsGroupedModal pins the ctrl+p popup: every command,
// grouped, with its display label, and never the inline slash menu.
func TestCtrlPCommandPaletteIsGroupedModal(t *testing.T) {
	m := newPanelModel()
	m.picker.openAll()
	state := m.kitCommandPicker()
	if state.Inline {
		t.Fatal("ctrl+p projected the inline slash menu")
	}
	if len(state.Items) != len(m.picker.items) {
		t.Fatalf("modal items = %d, want %d", len(state.Items), len(m.picker.items))
	}
	counts := map[string]int{}
	found := false
	for _, item := range state.Items {
		if item.ID == "/customize" {
			found = true
			if item.Title != "Customize myagent (/customize)" {
				t.Errorf("/customize modal title = %q", item.Title)
			}
		}
		if item.Title == "" || item.Desc == "" || item.Group == "" {
			t.Fatalf("modal row incomplete: %+v", item)
		}
		counts[item.Group]++
	}
	if !found {
		t.Fatal("/customize missing from the ctrl+p palette")
	}
	for _, group := range []string{"Suggested", "Session", "Tools & interface"} {
		if counts[group] == 0 {
			t.Errorf("missing %q group in the ctrl+p palette", group)
		}
	}
}

func commandItemByName(name string) (commandItem, bool) {
	for _, item := range commandItems {
		if item.name == name {
			return item, true
		}
	}
	return commandItem{}, false
}
