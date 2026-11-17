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

func TestCommandPickerDisplaysLabelAndCommandID(t *testing.T) {
	m := newPanelModel()
	m.picker.sync("/")
	state := m.kitCommandPicker()

	for _, item := range state.Items {
		if item.ID == "/customize" {
			if item.Title != "Customize myagent (/customize)" {
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

func commandItemByName(name string) (commandItem, bool) {
	for _, item := range commandItems {
		if item.name == name {
			return item, true
		}
	}
	return commandItem{}, false
}
