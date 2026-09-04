package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/AlvinPlayz23/myagent/internal/export"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/session"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

const (
	sessionPickerMaxVisible = 5
	promptHistoryLimit      = 100
)

type sessionPicker struct {
	items     []session.Info
	sel       int
	active    bool
	currentID string
}

// open pins the active session at the top of the picker. It remains selectable
// so Enter can explicitly mean "keep working in this session."
func (p *sessionPicker) open(items []session.Info, currentID string) {
	p.items = items
	p.currentID = currentID
	if currentID != "" {
		for i, info := range p.items {
			if info.ID == currentID {
				p.items[0], p.items[i] = p.items[i], p.items[0]
				break
			}
		}
	}
	p.sel = 0
	p.active = len(items) > 0
}

func (p *sessionPicker) close() {
	p.items = nil
	p.sel = 0
	p.active = false
	p.currentID = ""
}

func (p *sessionPicker) move(delta int) {
	if !p.active || len(p.items) == 0 {
		return
	}
	p.sel = (p.sel + delta + len(p.items)) % len(p.items)
}

func (p *sessionPicker) selected() (session.Info, bool) {
	if !p.active || p.sel < 0 || p.sel >= len(p.items) {
		return session.Info{}, false
	}
	return p.items[p.sel], true
}

func (p *sessionPicker) height() int {
	if !p.active {
		return 0
	}

	if len(p.items) > 0 && p.currentID != "" && p.items[0].ID == p.currentID {
		return 3 + min(sessionPickerMaxVisible, len(p.items)-1)
	}
	return 1 + min(sessionPickerMaxVisible, len(p.items))
}

// customizeSection identifies which setting a /customize row belongs to.
type customizeSection int

const (
	sectionStartup customizeSection = iota
	sectionComposer
)

// customizeRow is one line of the /customize panel. Header rows title a group
// and carry no value, so navigation skips over them.
type customizeRow struct {
	section     customizeSection
	header      bool
	label       string
	description string
	welcome     welcomeStyle
	prompt      promptStyle
}

// customizeRows flattens the grouped settings into the display order used by
// both the renderer and the picker's cursor.
var customizeRows = buildCustomizeRows()

func buildCustomizeRows() []customizeRow {
	rows := []customizeRow{{section: sectionStartup, header: true, label: "1. Startup Style", description: "empty-session welcome"}}
	for _, choice := range welcomeChoices {
		rows = append(rows, customizeRow{
			section:     sectionStartup,
			label:       choice.label,
			description: choice.description,
			welcome:     choice.style,
		})
	}
	rows = append(rows, customizeRow{section: sectionComposer, header: true, label: "2. Composer (Prompt Box)", description: "where you type"})
	for _, choice := range promptChoices {
		rows = append(rows, customizeRow{
			section:     sectionComposer,
			label:       choice.label,
			description: choice.description,
			prompt:      choice.style,
		})
	}
	return rows
}

type customizePicker struct {
	active bool
	sel    int
}

// open positions the cursor on the row matching the active startup style so the
// panel opens showing what is currently in effect.
func (p *customizePicker) open(current welcomeStyle) {
	p.active = true
	p.sel = firstSelectableRow()
	for i, row := range customizeRows {
		if !row.header && row.section == sectionStartup && row.welcome == current {
			p.sel = i
			break
		}
	}
}

func (p *customizePicker) close() { p.active = false }

// move advances the cursor by delta selectable rows, stepping over the group
// headers in either direction.
func (p *customizePicker) move(delta int) {
	n := len(customizeRows)
	if delta == 0 || n == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	for ; delta > 0; delta-- {
		for i := 0; i < n; i++ {
			p.sel = (p.sel + step + n) % n
			if !customizeRows[p.sel].header {
				break
			}
		}
	}
}

func (p *customizePicker) selected() customizeRow {
	if p.sel < 0 || p.sel >= len(customizeRows) {
		return customizeRow{}
	}
	return customizeRows[p.sel]
}

type exportPicker struct {
	active bool
	sel    int
}

type effortChoice struct {
	effort      llm.Effort
	label       string
	description string
}

var effortChoices = []effortChoice{
	{label: "Default", description: "use the provider default"},
	{effort: llm.EffortMinimal, label: "Minimal", description: "shortest reasoning pass"},
	{effort: llm.EffortLow, label: "Low", description: "fast, lightweight reasoning"},
	{effort: llm.EffortMedium, label: "Medium", description: "balanced reasoning"},
	{effort: llm.EffortHigh, label: "High", description: "deeper reasoning"},
	{effort: llm.EffortXHigh, label: "XHigh", description: "extended reasoning"},
	{effort: llm.EffortMax, label: "Max", description: "maximum reasoning"},
}

type effortPicker struct {
	active bool
	sel    int
}

func (p *effortPicker) open(current llm.Effort) {
	p.active = true
	p.sel = 0
	for i, choice := range effortChoices {
		if choice.effort == current {
			p.sel = i
			break
		}
	}
}

func (p *effortPicker) close() { p.active = false }

func (p *effortPicker) move(delta int) {
	p.sel = (p.sel + delta + len(effortChoices)) % len(effortChoices)
}

func (p *effortPicker) selected() effortChoice { return effortChoices[p.sel] }

func (p *exportPicker) open() { p.active, p.sel = true, 0 }

func (p *exportPicker) close() { p.active = false }

func (p *exportPicker) move(delta int) { p.sel = (p.sel + delta + 2) % 2 }

func (p *exportPicker) format() export.Format {
	if p.sel == 1 {
		return export.HTML
	}
	return export.Markdown
}

func (m *model) runCommand(text string) (tea.Model, tea.Cmd) {
	cmd, err := parseSlashCommand(text)
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}
	if m.working && cmd.kind != commandThinking {
		m.statusMsg = "Cancel the current run before using slash commands."
		return m, nil
	}

	switch cmd.kind {
	case commandHelp:
		m.helpActive = true
		m.updateLayout()
	case commandClear:
		m.runner.discardEvents()
		m.transcript.clear()
		m.statusMsg = "Transcript cleared; conversation context is retained."
		m.refreshViewport()
	case commandNew:
		if m.newSession == nil {
			m.statusMsg = "Unable to create a new session."
			return m, nil
		}
		if err := m.newSession(); err != nil {
			m.statusMsg = "Could not create a new session: " + err.Error()
			return m, nil
		}
		m.transcript.clear()
		m.usage = types.Usage{}
		m.sessionTitle = ""
		m.hasSessionTitle = false
		m.updateTerminalTitle()
		m.statusMsg = "Started a new conversation."
		m.refreshViewport()
	case commandInit:
		return m.startRun("/init", userMessage(initPrompt))
	case commandThinking:
		return m.applyShowThinking(cmd.arg), nil
	case commandModel:
		return m.openModelPicker(cmd.arg)
	case commandEffort:
		return m.openEffortPicker(cmd.arg)
	case commandProviders:
		return m.openProviderPicker()
	case commandCustomize:
		m.customize.open(m.welcomeStyle)
		m.statusMsg = "Choose the empty-session startup style."
		m.updateLayout()
	case commandCompact:
		runCtx, cancel := context.WithCancel(m.ctx)
		m.cancel = cancel
		m.working = true
		m.abortRequested = false
		m.startedAt = time.Now()
		m.statusMsg = "Compacting context…"
		m.lastErr = nil
		return m, m.runner.compact(runCtx)
	case commandResume:
		if m.listSessions == nil || m.resumeSession == nil {
			m.statusMsg = "Unable to resume sessions."
			return m, nil
		}
		infos, err := m.listSessions()
		if err != nil {
			m.statusMsg = "Could not list sessions: " + err.Error()
			return m, nil
		}
		if len(infos) == 0 {
			m.statusMsg = "No sessions found."
			return m, nil
		}
		currentID := ""
		if m.currentSessionID != nil {
			currentID = m.currentSessionID()
		}
		m.sessions.open(infos, currentID)
		m.statusMsg = "Select a session to resume."
		m.updateLayout()
	case commandExport:
		if m.exportSession == nil {
			m.statusMsg = "Export is unavailable."
			return m, nil
		}
		m.exportPick.open()
		m.statusMsg = "Choose an export format."
		m.updateLayout()
	case commandRename:
		if m.renameSession == nil {
			m.statusMsg = "Unable to rename the current session."
			return m, nil
		}
		if err := m.renameSession(cmd.arg); err != nil {
			m.statusMsg = "Could not rename session: " + err.Error()
			return m, nil
		}
		m.sessionTitle = cmd.arg
		m.hasSessionTitle = true
		m.updateTerminalTitle()
		m.statusMsg = "Session renamed."
	}
	return m, nil
}

func (m *model) writeExport(overwrite bool) (tea.Model, tea.Cmd) {
	if m.exportSession == nil {
		return m, nil
	}
	path, err := m.exportSession(m.exportFormat, m.exportName.Value(), overwrite)
	if errors.Is(err, export.ErrFileExists) {
		m.exportOverwrite = true
		m.statusMsg = "File exists — enter overwrite, up/down return to name."
		m.updateLayout()
		return m, nil
	}
	if err != nil {
		m.statusMsg = "Could not export session: " + err.Error()
		return m, nil
	}
	m.exportFormat = ""
	m.exportOverwrite = false
	m.exportName.Reset()
	m.statusMsg = "Exported session to " + path
	m.updateLayout()
	return m, nil
}

// applyCustomizeSelection saves whichever grouped setting the cursor sits on.
func (m *model) applyCustomizeSelection() (tea.Model, tea.Cmd) {
	row := m.customize.selected()
	if row.header {
		return m, nil
	}
	if row.section == sectionComposer {
		return m.applyPromptStyle(row)
	}
	return m.applyWelcomeStyle(row)
}

func (m *model) applyWelcomeStyle(row customizeRow) (tea.Model, tea.Cmd) {
	if m.saveWelcomeStyle != nil {
		if err := m.saveWelcomeStyle(row.welcome); err != nil {
			m.statusMsg = "Could not save customization: " + err.Error()
			return m, nil
		}
	}
	m.welcomeStyle = row.welcome
	m.welcomeFrame = 0
	m.customize.close()
	m.statusMsg = "Startup style set to " + row.label + "."
	m.updateLayout()
	return m, nil
}

func (m *model) applyPromptStyle(row customizeRow) (tea.Model, tea.Cmd) {
	if m.savePromptStyle != nil {
		if err := m.savePromptStyle(row.prompt); err != nil {
			m.statusMsg = "Could not save customization: " + err.Error()
			return m, nil
		}
	}
	m.promptStyle = row.prompt
	m.syncComposerStyle()
	m.customize.close()
	m.statusMsg = "Composer style set to " + row.label + "."
	m.updateLayout()
	return m, nil
}

func (m *model) openProviderPicker() (tea.Model, tea.Cmd) {
	if m.availableProviders == nil || m.providerConfigured == nil || m.configureProvider == nil {
		m.statusMsg = "Provider configuration is unavailable."
		return m, nil
	}
	items := m.availableProviders()
	if len(items) == 0 {
		m.statusMsg = "No compatible providers are available in the catalog yet."
		return m, nil
	}
	m.providers.open(items)
	m.statusMsg = "Select a provider to add or replace its API key."
	m.updateLayout()
	return m, nil
}

func (m *model) openProviderKeyEntry() (tea.Model, tea.Cmd) {
	provider, ok := m.providers.selected()
	if !ok {
		return m, nil
	}
	if m.providerIsCustom != nil && m.providerIsCustom(provider.ID) {
		m.statusMsg = provider.Name + " is managed as a custom provider. Delete or rename it in `myagent auth` to use the built-in configuration."
		return m, nil
	}
	m.providers.active = false
	m.keyFor = provider
	m.keyInput.SetValue(m.providerAPIKey(provider.ID))
	m.keyInput.Placeholder = "API key for " + provider.Name
	cmd := m.keyInput.Focus()
	if m.providerConfigured(provider.ID) {
		m.statusMsg = "Replace the masked API key, then press enter to save."
	} else {
		m.statusMsg = "Enter API key, then press enter to save."
	}
	m.updateLayout()
	return m, cmd
}

func (m *model) saveProviderKey() (tea.Model, tea.Cmd) {
	key := strings.TrimSpace(m.keyInput.Value())
	if key == "" {
		m.statusMsg = "An API key is required."
		return m, nil
	}
	if err := m.configureProvider(m.keyFor, key); err != nil {
		m.statusMsg = "Could not save provider: " + err.Error()
		return m, nil
	}
	name := m.keyFor.Name
	m.keyInput.Reset()
	m.keyFor = modelcatalog.Provider{}
	m.providers.active = true
	m.statusMsg = name + " configured."
	m.updateLayout()
	return m, nil
}

func (m *model) openModelPicker(query string) (tea.Model, tea.Cmd) {
	if m.availableModels == nil || m.selectModel == nil {
		m.statusMsg = "Model selection is unavailable."
		return m, nil
	}
	items := m.availableModels()
	if len(items) == 0 && m.discoverModels != nil {

		if cmd := m.discoverActiveProviderModels(); cmd != nil {
			m.models.open(items, query)
			m.statusMsg = "Catalog unavailable; discovering models from the provider…"
			m.updateLayout()
			return m, cmd
		}
	}
	if len(items) == 0 {
		m.statusMsg = "No catalog models are available for configured providers."
		return m, nil
	}
	for _, item := range items {
		if strings.EqualFold(item.Ref(), strings.TrimSpace(query)) {
			return m.applyModel(item)
		}
	}
	cmd := m.discoverActiveProviderModels()
	m.models.open(items, query)
	m.statusMsg = "Search models, use up/down, enter selects, esc cancels."
	m.updateLayout()
	return m, cmd
}

// discoverActiveProviderModels live-refreshes the picker with the active
// provider's own /v1/models listing in the background. The catalog-backed
// list opens the picker immediately; discovered IDs merge in when the
// lookup returns. Failures are silent because the catalog remains usable.
func (m *model) discoverActiveProviderModels() tea.Cmd {
	if m.discoverModels == nil {
		return nil
	}
	provider, _, ok := strings.Cut(strings.TrimSpace(m.modelID), "/")
	provider = strings.TrimSpace(provider)
	if !ok || provider == "" || m.discovering == provider {
		return nil
	}
	m.discovering = provider
	target := provider
	return func() tea.Msg {
		ids, err := m.discoverModels(m.ctx, target)
		return modelsDiscoveredMsg{provider: target, models: ids, err: err}
	}
}

func (m *model) selectPickedModel() (tea.Model, tea.Cmd) {
	item, ok := m.models.selected()
	if !ok {
		return m, nil
	}
	return m.applyModel(item)
}

func (m *model) applyModel(item modelcatalog.Model) (tea.Model, tea.Cmd) {
	provider, selected, err := m.selectModel(item.Provider, item.ID)
	if err != nil {
		m.statusMsg = "Could not select model: " + err.Error()
		return m, nil
	}
	m.runner.setModel(provider, selected)
	if effort, err := llm.NormalizeEffort(selected, m.runner.cfg.Effort); err == nil {
		m.runner.setEffort(effort)
	} else {
		m.runner.setEffort("")
	}
	m.modelID = item.Ref()
	m.models.close()
	m.statusMsg = "Model set to " + item.Ref() + "."
	m.updateLayout()
	return m, nil
}

func (m *model) openEffortPicker(value string) (tea.Model, tea.Cmd) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "default") {
		return m.applyEffort("")
	}
	if value != "" {
		effort, err := llm.ParseEffort(value)
		if err != nil {
			m.statusMsg = err.Error()
			return m, nil
		}
		return m.applyEffort(effort)
	}
	m.effort.open(m.runner.cfg.Effort)
	m.statusMsg = "Choose reasoning effort."
	m.updateLayout()
	return m, nil
}

// applyShowThinking flips (no arg) or sets ("on"/"off") the transcript's
// thinking visibility. It is a pure display toggle, so it is also allowed
// while a run is streaming.
func (m *model) applyShowThinking(arg string) tea.Model {
	var show bool
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "":
		show = !m.transcript.showThinking
	case "on", "true", "yes", "1":
		show = true
	case "off", "false", "no", "0":
		show = false
	default:
		m.statusMsg = "usage: /thinking [on|off]"
		return m
	}
	m.transcript.setShowThinking(show)
	if show {
		m.statusMsg = "Thinking shown."
	} else {
		m.statusMsg = "Thinking hidden."
	}
	m.refreshViewport()
	return m
}

func (m *model) applyEffort(effort llm.Effort) (tea.Model, tea.Cmd) {
	effort, err := llm.NormalizeEffort(m.runner.cfg.Model, effort)
	if err != nil {
		m.statusMsg = err.Error()
		return m, nil
	}
	m.runner.setEffort(effort)
	m.effort.close()
	if effort == "" {
		m.statusMsg = "Reasoning effort reset to provider default."
	} else {
		m.statusMsg = "Reasoning effort set to " + string(effort) + "."
	}
	m.updateLayout()
	return m, nil
}
