package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/AlvinPlayz23/myagent/internal/export"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/session"
)

// kitPanelKind names the active inline panel in legacy key-precedence order.
// Export's three chained steps come first because onKey handles them first;
// scope precedes tools because tools hands off to scope when Esc stages edits.
func (m *model) kitPanelKind() string {
	switch {
	case m.exportPick.active:
		return "export-format"
	case m.exportOverwrite:
		return "export-overwrite"
	case m.exportFormat != "":
		return "export-name"
	case m.sessions.active:
		return "sessions"
	case m.models.active:
		return "models"
	case m.effort.active:
		return "effort"
	case m.customize.active:
		return "customize"
	case m.scope.active:
		return "scope"
	case m.tools.active:
		return "tools"
	case m.keyFor.ID != "":
		return "provider-key"
	case m.providers.active:
		return "providers"
	case m.files.active:
		return "files"
	case m.picker.active:
		return "commands"
	}
	return ""
}

// KitPicker projects every remaining inline panel into a dialog state. Select
// is the zero mode so the S4 models/commands bridge keeps its path.
func (m *model) KitPicker() (KitPickerState, bool) {
	switch kind := m.kitPanelKind(); kind {
	case "models":
		return m.kitModelPicker(), true
	case "commands":
		return m.kitCommandPicker(), true
	case "export-format":
		return m.kitExportFormat(), true
	case "export-overwrite":
		return m.kitExportOverwrite(), true
	case "export-name":
		return m.kitExportName(), true
	case "sessions":
		return m.kitSessionPicker(), true
	case "effort":
		return m.kitEffortPicker(), true
	case "customize":
		return m.kitCustomizePicker(), true
	case "scope":
		return m.kitScopePicker(), true
	case "tools":
		return m.kitToolsPicker(), true
	case "provider-key":
		return m.kitProviderKey(), true
	case "providers":
		return m.kitProviderPicker(), true
	case "files":
		return m.kitFilePicker(), true
	default:
		return KitPickerState{}, false
	}
}

func (m *model) kitModelPicker() KitPickerState {
	state := KitPickerState{Kind: "models", Mode: KitPickerSelect, Title: "Select a model", Placeholder: "Search models…", Query: m.models.query}
	if item, ok := m.models.selected(); ok {
		state.SelectedID = item.Ref()
	}
	for _, item := range m.models.items {
		title := strings.TrimSpace(item.Name)
		if title == "" {
			title = item.ID
		}
		desc := item.Ref()
		group := item.ProviderName
		if group == "" {
			group = item.Provider
		}
		metadata := []string{}
		if item.Ref() == m.modelID {
			if m.models.query == "" {
				state.SelectedID = item.Ref()
			}
			metadata = append(metadata, "✓ current")
		}
		if item.ContextWindow > 0 {
			metadata = append(metadata, fmt.Sprintf("%dk", item.ContextWindow/1000))
		}
		state.Items = append(state.Items, KitPickerItem{ID: item.Ref(), Title: title, Desc: desc, Group: group, Footer: strings.Join(metadata, " · ")})
	}
	return state
}

func (m *model) kitCommandPicker() KitPickerState {
	state := KitPickerState{Kind: "commands", Mode: KitPickerSelect, Title: "Commands", Placeholder: "Search commands…"}
	if item, ok := m.picker.selected(); ok {
		state.SelectedID = item.name
	}
	if m.picker.pinned {
		// ctrl+p opens the modal palette: every command, grouped, with the
		// dialog owning its own search.
		state.Query = strings.TrimPrefix(m.picker.prefix, "/")
		for _, group := range []string{"Suggested", "Session", "Tools & interface"} {
			for _, item := range m.picker.items {
				category := "Suggested"
				switch item.kind {
				case commandCompact, commandClear, commandNew, commandResume, commandRename, commandExport, commandInit:
					category = "Session"
				case commandThinking, commandTools, commandMouse, commandPaste:
					category = "Tools & interface"
				}
				if category != group {
					continue
				}
				label := item.label
				if label == "" {
					label = item.name
				}
				state.Items = append(state.Items, KitPickerItem{ID: item.name, Title: label + " (" + item.name + ")", Desc: item.description, Group: category})
			}
		}
		return state
	}
	// A typed "/" opens the inline menu: the host owns the filter and the rows
	// are the flat matched list in picker order, so highlight and up/down agree.
	state.Inline = true
	state.Query = m.input.Value()
	for _, index := range m.picker.matched {
		item := m.picker.items[index]
		state.Items = append(state.Items, KitPickerItem{ID: item.name, Title: item.name, Desc: item.description})
	}
	return state
}

func (m *model) kitExportFormat() KitPickerState {
	state := KitPickerState{
		Kind:         "export-format",
		Mode:         KitPickerList,
		Title:        "Export session as",
		Hint:         "↑/↓ select, enter continue",
		ConfirmLabel: "continue",
		SelectedID:   "markdown",
	}
	formats := []export.Format{export.Markdown, export.HTML}
	ids := []string{"markdown", "html"}
	for i, format := range formats {
		if i == m.exportPick.sel {
			state.SelectedID = ids[i]
		}
		state.Items = append(state.Items, KitPickerItem{
			ID:    ids[i],
			Title: export.Label(format),
		})
	}
	return state
}

func (m *model) kitExportOverwrite() KitPickerState {
	return KitPickerState{
		Kind:         "export-overwrite",
		Mode:         KitPickerConfirm,
		Title:        "Export",
		Body:         "File exists — enter overwrite, ↑/↓ return to name",
		Hint:         "↑/↓ return to name",
		ConfirmLabel: "overwrite",
		ConfirmKey:   "enter",
		CancelLabel:  "cancel",
	}
}

func (m *model) kitExportName() KitPickerState {
	return KitPickerState{
		Kind:         "export-name",
		Mode:         KitPickerInput,
		Title:        "Export as " + export.Label(m.exportFormat),
		Input:        m.exportName.View(),
		Hint:         "enter export",
		ConfirmLabel: "export",
		ConfirmKey:   "enter",
		CancelLabel:  "cancel",
	}
}

func (m *model) kitSessionPicker() KitPickerState {
	state := KitPickerState{
		Kind:         "sessions",
		Mode:         KitPickerList,
		Title:        "Resume session",
		Hint:         "↑/↓ select, enter resume",
		ConfirmLabel: "resume",
	}
	current := len(m.sessions.items) > 0 && m.sessions.currentID != "" &&
		m.sessions.items[0].ID == m.sessions.currentID
	if current {
		state.Items = append(state.Items, kitSessionItem(m.sessions.items[0], true))
		if len(m.sessions.items) > 1 {
			state.Items = append(state.Items, KitPickerItem{Title: "History", Header: true})
		}
	}
	first := 0
	if current {
		first = 1
	}
	for i := first; i < len(m.sessions.items); i++ {
		state.Items = append(state.Items, kitSessionItem(m.sessions.items[i], false))
	}
	if info, ok := m.sessions.selected(); ok {
		state.SelectedID = info.ID
	}
	return state
}

func kitSessionItem(info session.Info, current bool) KitPickerItem {
	title := info.Title
	if title == "" {
		title = info.Preview
	}
	if title == "" {
		title = "(no messages)"
	}
	id := info.ID
	if len(id) > 8 {
		id = id[:8]
	}
	line := title
	description := fmt.Sprintf("%s · %s", info.Modified.Local().Format("Jan 02 15:04"), id)
	footer := ""
	if current {
		line = "● CURRENT  " + line
		footer = "current"
	}
	return KitPickerItem{ID: info.ID, Title: line, Desc: description, Footer: footer}
}

func (m *model) kitEffortPicker() KitPickerState {
	current := llm.Effort("")
	if m.runner != nil {
		current = m.runner.cfg.Effort
	}
	def := m.defaultEffort
	if m.saveDefaultEffort == nil {
		def = current
	}
	state := KitPickerState{
		Kind:         "effort",
		Mode:         KitPickerList,
		Inline:       !m.effort.fromPalette,
		EffortStyle:  string(normalizeEffortSelectorStyle(string(m.effortSelectorStyle))),
		Title:        "Reasoning effort",
		Hint:         "↑/↓ select, enter saves as myagent default",
		SelectedID:   strconv.Itoa(m.effort.sel),
		ConfirmLabel: "save",
	}
	for i, choice := range effortChoices {
		isCurrent := choice.effort == current
		isDefault := choice.effort == def
		desc := choice.description
		switch {
		case isCurrent && isDefault:
			desc += "  (current) · myagent default"
		case isCurrent:
			desc += "  (current)"
		case isDefault:
			desc += "  (myagent default)"
		}
		state.Items = append(state.Items, KitPickerItem{
			ID:    strconv.Itoa(i),
			Title: choice.label,
			Desc:  desc,
		})
	}
	return state
}

func (m *model) kitCustomizePicker() KitPickerState {
	state := KitPickerState{
		Kind:         "customize",
		Mode:         KitPickerList,
		Title:        "Customize",
		Hint:         "↑/↓ select, enter open",
		SelectedID:   "group:" + strconv.Itoa(m.customize.sel),
		ConfirmLabel: "open",
	}
	if m.customize.level == 0 {
		for i, group := range customizeGroups {
			state.Items = append(state.Items, KitPickerItem{
				ID:    "group:" + strconv.Itoa(i),
				Title: group.label,
				Desc:  group.description,
			})
		}
		return state
	}
	group := customizeGroups[m.customize.group]
	state.Title = "Customize › " + group.label
	state.Hint = "↑/↓ select, enter save, ← back"
	state.ConfirmLabel = "save"
	state.SelectedID = "choice:" + strconv.Itoa(m.customize.sel)
	for i, row := range m.customize.choices() {
		footer := ""
		if m.rowIsCurrent(row) {
			footer = "(current)"
		}
		state.Items = append(state.Items, KitPickerItem{
			ID:     "choice:" + strconv.Itoa(i),
			Title:  row.label,
			Desc:   row.description,
			Footer: footer,
		})
	}
	return state
}

func (m *model) kitScopePicker() KitPickerState {
	state := KitPickerState{
		Kind:         "scope",
		Mode:         KitPickerList,
		Title:        "Save tool changes",
		Hint:         m.scope.summary(),
		SelectedID:   scopeChoiceID(m.scope.choice().scope),
		ConfirmLabel: "save",
	}
	for _, choice := range scopeChoices {
		desc := choice.description
		if choice.scope == scopeSession && m.saveSessionTools == nil {
			desc = "unavailable: no session store"
		}
		state.Items = append(state.Items, KitPickerItem{
			ID:    scopeChoiceID(choice.scope),
			Title: choice.label,
			Desc:  desc,
		})
	}
	return state
}

func (m *model) kitToolsPicker() KitPickerState {
	state := KitPickerState{
		Kind:         "tools",
		Mode:         KitPickerList,
		Title:        "Tools",
		Hint:         m.tools.disabledSummary(),
		ConfirmLabel: "save",
	}
	if m.tools.sel >= 0 && m.tools.sel < len(m.tools.names) {
		state.SelectedID = m.tools.names[m.tools.sel]
	}
	for _, name := range m.tools.names {
		desc := "enabled"
		footer := "[x]"
		if m.tools.isDisabled(name) {
			desc = "disabled"
			footer = "[ ]"
		}
		state.Items = append(state.Items, KitPickerItem{
			ID:     name,
			Title:  name,
			Desc:   desc,
			Footer: footer,
		})
	}
	return state
}

func (m *model) kitProviderKey() KitPickerState {
	action := "Configure "
	if m.providerConfigured != nil && m.providerConfigured(m.keyFor.ID) {
		action = "Edit "
	}
	return KitPickerState{
		Kind:         "provider-key",
		Mode:         KitPickerInput,
		Title:        action + m.keyFor.Name,
		Placeholder:  m.keyInput.Placeholder,
		Input:        m.keyInput.View(),
		Masked:       true,
		Hint:         "enter saves",
		ConfirmLabel: "save",
		ConfirmKey:   "enter",
		CancelLabel:  "cancel",
	}
}

func (m *model) kitProviderPicker() KitPickerState {
	state := KitPickerState{
		Kind:         "providers",
		Mode:         KitPickerList,
		Title:        "Providers",
		Hint:         "[x] configured, enter edits key",
		ConfirmLabel: "edit",
	}
	if item, ok := m.providers.selected(); ok {
		state.SelectedID = item.ID
	}
	for _, item := range m.providers.items {
		desc := ""
		if m.providerIsCustom != nil && m.providerIsCustom(item.ID) {
			desc = "managed as custom"
		} else if m.providerConfigured != nil && m.providerConfigured(item.ID) {
			desc = "[x] configured"
		}
		state.Items = append(state.Items, KitPickerItem{
			ID:    item.ID,
			Title: item.Name,
			Desc:  desc,
		})
	}
	return state
}

func (m *model) kitFilePicker() KitPickerState {
	state := KitPickerState{
		Kind:         "files",
		Mode:         KitPickerList,
		Title:        "Files",
		Hint:         "↑/↓ select, enter or tab insert",
		ConfirmLabel: "insert",
	}
	if path, ok := m.files.selected(); ok {
		state.SelectedID = path
	}
	for _, index := range m.files.matched {
		path := m.files.items[index]
		state.Items = append(state.Items, KitPickerItem{ID: path, Title: path})
	}
	return state
}

// KitPickerAccept applies a row click/footer action. A row that is not the
// current legacy selection only moves the cursor; activating the selected row
// runs the same action as that panel's Enter key.
func (m *model) KitPickerAccept(id string) tea.Cmd {
	switch m.kitPanelKind() {
	case "models":
		for _, item := range m.models.items {
			if item.Ref() != id {
				continue
			}
			_, cmd := m.applyModel(item)
			return cmd
		}
	case "commands":
		for _, item := range m.picker.items {
			if item.name != id {
				continue
			}
			if item.kind == commandEffort && m.picker.pinned {
				m.picker.close()
				_, cmd := m.openEffortPicker("")
				m.effort.fromPalette = true
				return cmd
			}
			needsInput := item.requiresArg && item.kind != commandModel && item.kind != commandEffort
			value := item.name
			if needsInput {
				value += " "
			}
			m.input.SetValue(value)
			m.picker.dismiss(value)
			m.updateLayout()
			if !needsInput {
				_, cmd := m.submit(submitFollowUp)
				return cmd
			}
			return nil
		}
	case "export-format":
		index := kitFormatIndex(id)
		if index < 0 {
			return nil
		}
		if m.exportPick.sel == index {
			_, cmd := m.confirmExportPick()
			return cmd
		}
		m.exportPick.sel = index
	case "export-overwrite":
		_, cmd := m.writeExport(true)
		return cmd
	case "export-name":
		_, cmd := m.writeExport(false)
		return cmd
	case "sessions":
		index := kitSessionIndex(m.sessions.items, id)
		if index < 0 {
			return nil
		}
		if m.sessions.sel == index {
			_, cmd := m.resumeSelectedSession()
			return cmd
		}
		m.sessions.sel = index
	case "effort":
		index, err := strconv.Atoi(id)
		if err != nil || index < 0 || index >= len(effortChoices) {
			return nil
		}
		if m.effort.sel == index || !m.effort.fromPalette {
			_, cmd := m.applyEffort(effortChoices[index].effort)
			return cmd
		}
		m.effort.sel = index
	case "customize":
		level, index, ok := kitCustomizeIndex(id)
		if !ok || level != m.customize.level {
			return nil
		}
		if index < 0 || index >= m.customize.height() {
			return nil
		}
		if m.customize.sel == index {
			_, cmd := m.confirmCustomize()
			return cmd
		}
		m.customize.sel = index
	case "scope":
		index, ok := kitScopeIndex(id)
		if !ok {
			return nil
		}
		if m.scope.sel == index {
			pending := m.scope.pending
			choice := scopeChoices[index]
			m.scope.close()
			m.applyToolTogglesScoped(pending, choice.scope)
			return nil
		}
		m.scope.sel = index
	case "tools":
		index := kitNameIndex(m.tools.names, id)
		if index < 0 {
			return nil
		}
		if m.tools.sel == index {
			m.tools.toggle()
			return nil
		}
		m.tools.sel = index
	case "provider-key":
		_, cmd := m.saveProviderKey()
		return cmd
	case "providers":
		index := kitProviderIndex(m.providers.items, id)
		if index < 0 {
			return nil
		}
		if m.providers.sel == index {
			_, cmd := m.openProviderKeyEntry()
			return cmd
		}
		m.providers.sel = index
	case "files":
		index := kitFileIndex(m.files.items, m.files.matched, id)
		if index < 0 {
			return nil
		}
		if m.files.sel == index {
			_, cmd := m.acceptFilePicker()
			return cmd
		}
		m.files.sel = index
	}
	return nil
}

// KitPickerCancel reruns the exact legacy Esc path for the active panel.
func (m *model) KitPickerCancel() tea.Cmd {
	switch m.kitPanelKind() {
	case "export-format":
		m.exportPick.close()
		m.statusMsg = "Export cancelled."
		m.updateLayout()
	case "export-overwrite":
		m.exportOverwrite = false
		m.exportFormat = ""
		m.statusMsg = "Export cancelled."
		m.updateLayout()
	case "export-name":
		m.exportFormat = ""
		m.exportName.Reset()
		m.statusMsg = "Export cancelled."
		m.updateLayout()
	case "sessions":
		m.sessions.close()
		m.statusMsg = "Resume cancelled."
		m.updateLayout()
	case "models":
		m.models.close()
		m.statusMsg = "Model selection cancelled."
		m.updateLayout()
	case "effort":
		m.effort.close()
		m.statusMsg = "Effort selection cancelled."
		m.updateLayout()
	case "customize":
		if m.customize.level == 1 {
			m.customize.backToGroups()
			return nil
		}
		m.customize.close()
		m.statusMsg = "Customization cancelled."
		m.updateLayout()
	case "scope":
		pending := m.scope.pending
		m.scope.close()
		m.tools.reopen(pending)
	case "tools":
		m.scope.open(m.tools.pending())
		m.tools.close()
		m.statusMsg = "Choose where to save the tool changes."
		m.updateLayout()
	case "provider-key":
		m.keyInput.Reset()
		m.keyFor = modelcatalog.Provider{}
		m.providers.active = true
		m.statusMsg = "Provider edit cancelled."
		m.updateLayout()
	case "providers":
		m.providers.close()
		m.statusMsg = "Provider selection cancelled."
		m.updateLayout()
	case "files":
		m.files.dismiss(m.input.Value())
		m.updateLayout()
	case "commands":
		m.picker.dismiss(m.input.Value())
		m.updateLayout()
	}
	return nil
}

// KitDock projects the bottom-stack rows above the composer. The legacy render
// tails remain authoritative for MYAGENT_UI=legacy.
func (m *model) KitDock() KitDock {
	var dock KitDock
	if m.attachments.len() > 0 {
		total := 0
		for _, item := range m.attachments.items {
			total += item.bytes
		}
		dock.Rows = append(dock.Rows, KitDockRow{
			Label: "[image]",
			Text:  fmt.Sprintf("%d attached (%s)", m.attachments.len(), formatBytes(total)),
			Hint:  "backspace on an empty prompt removes the last",
		})
	}
	for i, queued := range m.queuedFollowUps {
		label := "next"
		if len(m.queuedFollowUps) > 1 {
			label = fmt.Sprintf("next %d/%d", i+1, len(m.queuedFollowUps))
		}
		dock.Rows = append(dock.Rows, KitDockRow{
			Label: label,
			Text:  strings.Join(strings.Fields(queued.display), " "),
		})
	}
	return dock
}

func kitFormatIndex(id string) int {
	switch id {
	case "markdown":
		return 0
	case "html":
		return 1
	}
	return -1
}

func kitSessionIndex(items []session.Info, id string) int {
	for i, info := range items {
		if info.ID == id {
			return i
		}
	}
	return -1
}

func kitCustomizeIndex(id string) (level, index int, ok bool) {
	kind, raw, found := strings.Cut(id, ":")
	if !found {
		return 0, 0, false
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 {
		return 0, 0, false
	}
	switch kind {
	case "group":
		return 0, index, true
	case "choice":
		return 1, index, true
	}
	return 0, 0, false
}

func scopeChoiceID(scope toolScope) string {
	switch scope {
	case scopeSession:
		return "session"
	case scopeGlobal:
		return "global"
	case scopeExit:
		return "exit"
	}
	return ""
}

func kitScopeIndex(id string) (int, bool) {
	for i, choice := range scopeChoices {
		if scopeChoiceID(choice.scope) == id {
			return i, true
		}
	}
	return 0, false
}

func kitNameIndex(names []string, name string) int {
	for i, candidate := range names {
		if candidate == name {
			return i
		}
	}
	return -1
}

func kitProviderIndex(items []modelcatalog.Provider, id string) int {
	for i, item := range items {
		if item.ID == id {
			return i
		}
	}
	return -1
}

func kitFileIndex(items []string, matched []int, path string) int {
	for i, index := range matched {
		if index >= 0 && index < len(items) && items[index] == path {
			return i
		}
	}
	return -1
}
