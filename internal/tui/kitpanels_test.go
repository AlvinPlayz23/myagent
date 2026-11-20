package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/agent"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/session"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

func newPanelModel() *model {
	return newModel(nil, nil, nil, newTheme(), newMDRenderer(), "model", "")
}

// TestKitPanelKindPrecedence pins the legacy key-order: export's chained
// steps first, scope before tools, provider-key before providers.
func TestKitPanelKindPrecedence(t *testing.T) {
	m := newPanelModel()
	if got := m.kitPanelKind(); got != "" {
		t.Fatalf("idle kind = %q, want empty", got)
	}

	m.exportPick.active, m.exportOverwrite, m.exportFormat = true, true, "md"
	if got := m.kitPanelKind(); got != "export-format" {
		t.Fatalf("kind = %q, want export-format", got)
	}
	m.exportPick.active = false
	if got := m.kitPanelKind(); got != "export-overwrite" {
		t.Fatalf("kind = %q, want export-overwrite", got)
	}
	m.exportOverwrite = false
	if got := m.kitPanelKind(); got != "export-name" {
		t.Fatalf("kind = %q, want export-name", got)
	}
	m.exportFormat = ""

	m.scope.active, m.tools.active = true, true
	if got := m.kitPanelKind(); got != "scope" {
		t.Fatalf("kind = %q, want scope before tools", got)
	}
	m.scope.active = false
	if got := m.kitPanelKind(); got != "tools" {
		t.Fatalf("kind = %q, want tools", got)
	}
	m.tools.active = false

	m.providers.active = true
	m.keyFor = modelcatalog.Provider{ID: "openrouter", Name: "OpenRouter"}
	if got := m.kitPanelKind(); got != "provider-key" {
		t.Fatalf("kind = %q, want provider-key before providers", got)
	}
	m.keyFor = modelcatalog.Provider{}
	if got := m.kitPanelKind(); got != "providers" {
		t.Fatalf("kind = %q, want providers", got)
	}
	m.providers.active = false

	m.files.active = true
	if got := m.kitPanelKind(); got != "files" {
		t.Fatalf("kind = %q, want files", got)
	}
	m.files.active = false
	m.picker.active = true
	if got := m.kitPanelKind(); got != "commands" {
		t.Fatalf("kind = %q, want commands", got)
	}
}

func TestKitPanelProjections(t *testing.T) {
	t.Run("effort", func(t *testing.T) {
		m := newPanelModel()
		m.modelID = "test-model"
		m.runner = newRunner(agent.Config{Effort: llm.EffortHigh}, newMsgQueue(), nil)
		m.effort.open(llm.EffortHigh)
		state, ok := m.KitPicker()
		if !ok || state.Kind != "effort" || state.Mode != KitPickerSlider {
			t.Fatalf("state = %+v ok=%v, want effort/slider", state, ok)
		}
		if len(state.Items) != len(effortChoices) || state.SelectedID != "4" {
			t.Fatalf("items/selected = %d/%q, want %d/4", len(state.Items), state.SelectedID, len(effortChoices))
		}
		if !strings.Contains(state.Items[4].Desc, "(current)") {
			t.Fatalf("current marker missing from %q", state.Items[4].Desc)
		}
	})

	t.Run("tools", func(t *testing.T) {
		m := newPanelModel()
		m.tools.active = true
		m.tools.sel = 1
		m.tools.names = []string{"read", "write"}
		m.tools.disabled = map[string]bool{"write": true}
		state, _ := m.KitPicker()
		if state.Kind != "tools" || state.SelectedID != "write" {
			t.Fatalf("state = %+v, want tools/write", state)
		}
		if got := state.Items[1]; got.Desc != "disabled" || got.Footer != "[ ]" {
			t.Fatalf("disabled row = %+v", got)
		}
	})

	t.Run("export chain", func(t *testing.T) {
		m := newPanelModel()
		m.exportPick.active, m.exportPick.sel = true, 1
		state, _ := m.KitPicker()
		if state.Kind != "export-format" || state.SelectedID != "html" || state.Mode != KitPickerList {
			t.Fatalf("format state = %+v", state)
		}
		m.exportPick.active, m.exportOverwrite = false, true
		state, _ = m.KitPicker()
		if state.Kind != "export-overwrite" || state.Mode != KitPickerConfirm || state.ConfirmLabel != "overwrite" {
			t.Fatalf("overwrite state = %+v", state)
		}
		m.exportOverwrite, m.exportFormat = false, "md"
		m.exportName.SetValue("session.md")
		state, _ = m.KitPicker()
		if state.Kind != "export-name" || state.Mode != KitPickerInput || !strings.Contains(ansi.Strip(state.Input), "session.md") {
			t.Fatalf("name state = %+v", state)
		}
	})

	t.Run("provider key", func(t *testing.T) {
		m := newPanelModel()
		m.keyFor = modelcatalog.Provider{ID: "x", Name: "Example"}
		state, _ := m.KitPicker()
		if state.Kind != "provider-key" || state.Mode != KitPickerInput || state.Title != "Configure Example" || !state.Masked {
			t.Fatalf("state = %+v", state)
		}
	})

	t.Run("sessions", func(t *testing.T) {
		m := newPanelModel()
		m.sessions.open([]session.Info{
			{ID: "current-session", Title: "Current"},
			{ID: "other-session", Title: "Other"},
		}, "current-session")
		state, _ := m.KitPicker()
		if state.Kind != "sessions" || state.SelectedID != "current-session" {
			t.Fatalf("state = %+v", state)
		}
		if len(state.Items) != 3 || !state.Items[1].Header || state.Items[1].Title != "History" {
			t.Fatalf("session rows = %+v, want current + History header", state.Items)
		}
		if !strings.Contains(state.Items[0].Title, "CURRENT") || state.Items[0].Footer != "current" {
			t.Fatalf("current row = %+v", state.Items[0])
		}
	})

	t.Run("files", func(t *testing.T) {
		m := newPanelModel()
		m.files.active = true
		m.files.items = []string{"a.go", "b.go"}
		m.files.matched = []int{0, 1}
		m.files.sel = 1
		state, _ := m.KitPicker()
		if state.Kind != "files" || state.SelectedID != "b.go" || len(state.Items) != 2 {
			t.Fatalf("state = %+v", state)
		}
	})

	t.Run("providers", func(t *testing.T) {
		m := newPanelModel()
		m.providers.open([]modelcatalog.Provider{
			{ID: "openrouter", Name: "OpenRouter"},
			{ID: "zenmux", Name: "ZenMux"},
		})
		m.providers.sel = 1
		m.providerConfigured = func(id string) bool { return id == "openrouter" }
		state, _ := m.KitPicker()
		if state.Kind != "providers" || state.SelectedID != "zenmux" {
			t.Fatalf("state = %+v", state)
		}
	})
}

// TestKitPickerAcceptSelectsThenActivates covers the click contract: a click
// that lands on a non-selected row only moves the cursor; clicking the already
// selected row runs the legacy Enter action.
func TestKitPickerAcceptSelectsThenActivates(t *testing.T) {
	t.Run("effort", func(t *testing.T) {
		m := newPanelModel()
		m.effort.open(llm.EffortLow)
		if cmd := m.KitPickerAccept("4"); cmd != nil {
			t.Fatal("selection move returned a command")
		}
		if !m.effort.active || m.effort.sel != 4 {
			t.Fatalf("selection = active %v sel %d, want true/4", m.effort.active, m.effort.sel)
		}
		m.KitPickerCancel()
		if m.effort.active || !strings.Contains(m.statusMsg, "cancelled") {
			t.Fatalf("cancel = active %v status %q", m.effort.active, m.statusMsg)
		}
	})

	t.Run("tools", func(t *testing.T) {
		m := newPanelModel()
		m.tools.open(nil, nil)
		m.tools.names = []string{"read", "write"}
		m.tools.disabled = map[string]bool{}
		m.KitPickerAccept("write")
		if m.tools.sel != 1 || m.tools.isDisabled("write") {
			t.Fatalf("select-only = sel %d disabled %v", m.tools.sel, m.tools.isDisabled("write"))
		}
		m.KitPickerAccept("write")
		if !m.tools.isDisabled("write") {
			t.Fatal("second accept did not toggle the selected tool")
		}
	})

	t.Run("sessions", func(t *testing.T) {
		m := newPanelModel()
		m.sessions.open([]session.Info{
			{ID: "current-session", Title: "Current"},
			{ID: "other-session", Title: "Other"},
		}, "current-session")
		m.resumeSession = func(string) ([]types.Message, error) { return nil, nil }
		m.KitPickerAccept("other-session")
		if m.sessions.sel != 1 {
			t.Fatalf("select-only sel = %d, want 1", m.sessions.sel)
		}
		m.KitPickerAccept("current-session")
		if m.sessions.sel != 0 {
			t.Fatalf("second move sel = %d, want 0", m.sessions.sel)
		}
		m.KitPickerAccept("current-session")
		if m.sessions.active || !strings.Contains(m.statusMsg, "Continuing current session") {
			t.Fatalf("activate = active %v status %q", m.sessions.active, m.statusMsg)
		}
	})

	t.Run("files", func(t *testing.T) {
		m := newPanelModel()
		m.input.SetValue("@b")
		m.files.active = true
		m.files.items = []string{"a.go", "b.go"}
		m.files.matched = []int{0, 1}
		m.files.sel = 1
		m.files.start, m.files.end = 0, 2

		m.KitPickerAccept("a.go")
		if m.files.sel != 0 || m.input.Value() != "@b" {
			t.Fatalf("select-only = sel %d input %q", m.files.sel, m.input.Value())
		}
		m.KitPickerAccept("a.go")
		if m.files.active || m.input.Value() != "@a.go" {
			t.Fatalf("activate = active %v input %q, want false/@a.go", m.files.active, m.input.Value())
		}
	})
}

func TestKitPickerCustomizeIDsNavigateSaveAndBack(t *testing.T) {
	m := openCustomize(t, welcomeDefault, promptDefault)

	groups, ok := m.KitPicker()
	if !ok || groups.Kind != "customize" || len(groups.Items) != len(customizeGroups) {
		t.Fatalf("group picker = %+v, ok %v", groups, ok)
	}
	groupID := groups.Items[1].ID
	if groupID != "group:1" {
		t.Fatalf("second group ID = %q, want group:1", groupID)
	}
	m.KitPickerAccept(groupID)
	if got, _ := m.KitPicker(); got.SelectedID != groupID || m.customize.level != 0 {
		t.Fatalf("first group accept = state %+v, customize %+v; want select only", got, m.customize)
	}
	m.KitPickerAccept(groupID)
	choices, ok := m.KitPicker()
	if !ok || choices.Title != "Customize › "+customizeGroups[1].label || m.customize.group != 1 {
		t.Fatalf("second group accept = state %+v, customize %+v", choices, m.customize)
	}

	// Back from the choice list should restore the group cursor, even though
	// the cursor in the nested list is a different legacy selection index.
	m.KitPickerCancel()
	groups, ok = m.KitPicker()
	if !ok || groups.SelectedID != groupID || m.customize.level != 0 {
		t.Fatalf("cancel from choices = state %+v, customize %+v", groups, m.customize)
	}
	m.KitPickerAccept(groupID)

	choices, ok = m.KitPicker()
	if !ok || len(choices.Items) == 0 {
		t.Fatalf("choice picker = %+v, ok %v", choices, ok)
	}
	target := -1
	for i, row := range m.customize.choices() {
		if row.prompt != m.promptStyle {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatal("no alternate composer style to select")
	}
	item := choices.Items[target]
	if item.ID != "choice:"+strconv.Itoa(target) {
		t.Fatalf("choice ID = %q, want choice:%d", item.ID, target)
	}
	row := m.customize.choices()[target]
	var persisted promptStyle
	persistCalls := 0
	m.savePromptStyle = func(style promptStyle) error {
		persisted = style
		persistCalls++
		return nil
	}
	m.KitPickerAccept(item.ID)
	if m.customize.sel != target || m.promptStyle == row.prompt || persistCalls != 0 {
		t.Fatalf("first choice accept = sel %d, style %q, save calls %d; want select only", m.customize.sel, m.promptStyle, persistCalls)
	}
	m.KitPickerAccept(item.ID)
	if m.customize.active || m.promptStyle != row.prompt || persisted != row.prompt || persistCalls != 1 {
		t.Fatalf("confirmed style = active %v, style %q, persisted %q, save calls %d; want %q saved once",
			m.customize.active, m.promptStyle, persisted, persistCalls, row.prompt)
	}
}

func TestKitPickerCustomizeRejectsInvalidIndexes(t *testing.T) {
	m := openCustomize(t, welcomeDefault, promptDefault)
	for _, id := range []string{"group:-1", "group:" + strconv.Itoa(len(customizeGroups)), "choice:0", "other:0", "group:nope"} {
		m.KitPickerAccept(id)
		if m.customize.level != 0 || m.customize.sel != 0 {
			t.Fatalf("accepting %q changed customize selection: %+v", id, m.customize)
		}
	}
}

func TestKitPickerCancelProviderKeyReturnsToProviders(t *testing.T) {
	m := newPanelModel()
	m.keyFor = modelcatalog.Provider{ID: "x", Name: "Example"}
	m.providers.active = true
	m.keyInput.SetValue("secret")
	m.KitPickerCancel()
	if m.keyFor.ID != "" || m.keyInput.Value() != "" {
		t.Fatalf("key state = %+v value %q", m.keyFor, m.keyInput.Value())
	}
	if !m.providers.active || !strings.Contains(m.statusMsg, "cancelled") {
		t.Fatalf("providers active = %v status %q", m.providers.active, m.statusMsg)
	}
}

func TestKitDockProjectsAttachmentsAndQueuedFollowUps(t *testing.T) {
	m := newPanelModel()
	if dock := m.KitDock(); len(dock.Rows) != 0 {
		t.Fatalf("idle dock rows = %+v", dock.Rows)
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	if err := m.attachments.add(png); err != nil {
		t.Fatal(err)
	}
	m.queuedFollowUps = []queuedMessage{
		{display: "run the   tests", message: types.Message{}},
		{display: "then ship\n", message: types.Message{}},
	}
	dock := m.KitDock()
	if len(dock.Rows) != 3 {
		t.Fatalf("dock rows = %+v, want 3", dock.Rows)
	}
	if got := dock.Rows[0]; got.Label != "[image]" || got.Text != "1 attached (40 B)" || got.Hint == "" {
		t.Fatalf("image row = %+v", got)
	}
	if got := dock.Rows[1]; got.Label != "next 1/2" || got.Text != "run the tests" {
		t.Fatalf("first queue row = %+v", got)
	}
	if got := dock.Rows[2]; got.Label != "next 2/2" || got.Text != "then ship" {
		t.Fatalf("second queue row = %+v", got)
	}
}

// TestKitKeyStillReachesLegacyPanels is the P6 base contract: the shell forwards
// every panel key to the host, and the legacy onKey path is unchanged.
func TestKitKeyStillReachesLegacyPanels(t *testing.T) {
	m := newPanelModel()
	m.effort.open(llm.EffortHigh)
	if cmd := m.KitKey(tea.KeyPressMsg{Code: tea.KeyDown}); cmd != nil {
		t.Fatal("down on effort returned a command")
	}
	if m.effort.sel != 5 {
		t.Fatalf("effort sel = %d, want 5 after down", m.effort.sel)
	}
	if cmd := m.KitKey(tea.KeyPressMsg{Code: tea.KeyEsc}); cmd != nil {
		t.Fatal("esc on effort returned a command")
	}
	if m.effort.active {
		t.Fatal("esc did not close the effort panel")
	}
}
