package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/export"
	"github.com/AlvinPlayz23/myagent/internal/llm"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
	"github.com/AlvinPlayz23/myagent/internal/types"
)

// runCommand executes a parsed slash command. Local commands never become
// conversation messages or reach the model.
func (a *app) runCommand(text string) {
	cmd, err := parseSlashCommand(text)
	if err != nil {
		a.statusMsg = err.Error()
		return
	}
	if a.working && cmd.kind != commandThinking {
		a.statusMsg = "Cancel the current run before using slash commands."
		return
	}

	switch cmd.kind {
	case commandHelp:
		a.sb.addNotice(helpText)
	case commandClear:
		a.generation = a.r.discardEvents()
		a.sb.clear()
		a.statusMsg = "Transcript cleared; conversation context is retained."
	case commandNew:
		if a.newSession == nil {
			a.statusMsg = "Unable to create a new session."
			return
		}
		if err := a.newSession(); err != nil {
			a.statusMsg = "Could not create a new session: " + err.Error()
			return
		}
		a.generation = a.r.reset()
		a.sb.clear()
		a.usage = types.Usage{}
		a.sessionTitle = ""
		a.hasSessionTitle = false
		a.updateTerminalTitle()
		a.statusMsg = "Started a new conversation."
	case commandInit:
		a.startRun("/init", userMessage(initPrompt))
	case commandThinking:
		a.applyShowThinking(cmd.arg)
	case commandModel:
		a.openModelPicker(cmd.arg)
	case commandEffort:
		a.openEffortPicker(cmd.arg)
	case commandProviders:
		a.openProviderPicker()
	case commandCustomize:
		a.modalKind = modalCustomize
		a.customizeSel = a.firstSelectableCustomize()
		a.statusMsg = "Choose the empty-session startup style."
	case commandCompact:
		runCtx, cancel := context.WithCancel(a.ctx)
		a.cancel = cancel
		a.working = true
		a.abortRequested = false
		a.startedAt = time.Now()
		a.statusMsg = "Compacting context…"
		a.lastErr = nil
		a.generation = a.r.compact(runCtx)
	case commandResume:
		a.openSessions()
	case commandExport:
		if a.exportSession == nil {
			a.statusMsg = "Export is unavailable."
			return
		}
		a.exportPick.open()
		a.modalKind = modalExportFormat
		a.statusMsg = "Choose an export format."
	case commandRename:
		if a.renameSession == nil {
			a.statusMsg = "Unable to rename the current session."
			return
		}
		if err := a.renameSession(cmd.arg); err != nil {
			a.statusMsg = "Could not rename session: " + err.Error()
			return
		}
		a.sessionTitle = cmd.arg
		a.hasSessionTitle = true
		a.updateTerminalTitle()
		a.statusMsg = "Session renamed."
	}
}

// firstSelectableCustomize positions the cursor on the first non-header row.
func (a *app) firstSelectableCustomize() int {
	for i, r := range customizeRows {
		if !r.header {
			return i
		}
	}
	return 0
}

// openSessions lists persisted sessions in the resume modal.
func (a *app) openSessions() {
	if a.listSessions == nil || a.resumeSession == nil {
		a.statusMsg = "Unable to resume sessions."
		return
	}
	infos, err := a.listSessions()
	if err != nil {
		a.statusMsg = "Could not list sessions: " + err.Error()
		return
	}
	if len(infos) == 0 {
		a.statusMsg = "No sessions found."
		return
	}
	currentID := ""
	if a.currentSessionID != nil {
		currentID = a.currentSessionID()
	}
	a.sessions.open(infos, currentID)
	a.modalKind = modalSessions
	a.statusMsg = "Select a session to resume."
}

// resumeSelected loads the highlighted session.
func (a *app) resumeSelected() {
	info, ok := a.sessions.selected()
	if !ok {
		return
	}
	history, err := a.resumeSession(info.ID)
	if err != nil {
		a.statusMsg = "Could not resume session: " + err.Error()
		return
	}
	a.sessions.close()
	a.modalKind = modalNone
	a.sb.clear()
	seedScrollback(a.sb, history)
	a.generation = a.r.resume(history)
	a.sessionTitle = info.Title
	a.hasSessionTitle = info.Title != "" && info.Title != "new"
	a.usage = types.Usage{}
	a.updateTerminalTitle()
	a.statusMsg = "Resumed session."
}

// openModelPicker opens the model modal, optionally applying an exact ref.
func (a *app) openModelPicker(query string) {
	if a.availableModels == nil || a.selectModel == nil {
		a.statusMsg = "Model selection is unavailable."
		return
	}
	a.invalidateModelDiscovery()
	provider := a.activeModelProvider()
	items := a.availableModels()
	if len(items) == 0 && a.discoverModels != nil {
		// Nothing in the catalog for the configured providers (offline first
		// run, or an endpoint models.dev does not know). Open the picker
		// anyway and populate it from the provider's live /v1/models listing.
		a.models.open(items, query, provider)
		a.modalKind = modalModels
		a.modalInput = query
		a.statusMsg = "Catalog unavailable; discovering models from the provider…"
		a.discoverActiveProviderModels()
		return
	}
	if len(items) == 0 {
		a.statusMsg = "No catalog models are available for configured providers."
		return
	}
	for _, item := range items {
		if strings.EqualFold(item.Ref(), strings.TrimSpace(query)) {
			a.applyModel(item)
			return
		}
	}
	a.models.open(items, query, provider)
	a.modalKind = modalModels
	a.modalInput = query
	a.statusMsg = "Search models, use up/down, enter selects, esc cancels."
	a.discoverActiveProviderModels()
}

// discoverActiveProviderModels live-refreshes the picker with the active
// provider's own /v1/models listing in the background.
func (a *app) discoverActiveProviderModels() {
	if a.discoverModels == nil {
		return
	}
	provider := a.models.provider
	if provider == "" || a.discovering == provider {
		return
	}
	a.modelDiscovery++
	request := a.modelDiscovery
	a.discovering = provider
	target := provider
	discover := a.discoverModels
	ctx, cancel := context.WithCancel(a.ctx)
	a.modelDiscoverCancel = cancel
	go func() {
		ids, err := discover(ctx, target)
		if ctx.Err() != nil {
			return
		}
		select {
		case a.loopCh <- loopEvent{discovery: &discoveryResult{provider: target, request: request, models: ids, err: err}}:
		case <-ctx.Done():
		}
	}()
}

// activeModelProvider extracts the active provider without depending on the
// catalog order used by the picker.
func (a *app) activeModelProvider() string {
	provider, _, ok := strings.Cut(strings.TrimSpace(a.modelID), "/")
	if ok && provider != "" {
		return provider
	}
	return strings.TrimSpace(a.r.cfg.Model.Provider)
}

// invalidateModelDiscovery prevents a late response from a closed picker from
// mutating the next picker session.
func (a *app) invalidateModelDiscovery() {
	if a.modelDiscoverCancel != nil {
		a.modelDiscoverCancel()
		a.modelDiscoverCancel = nil
	}
	a.modelDiscovery++
	a.discovering = ""
}

// closeModelPicker drops picker state and ignores any in-flight discovery.
func (a *app) closeModelPicker(status string) {
	a.models.close()
	a.invalidateModelDiscovery()
	a.modalKind = modalNone
	a.modalInput = ""
	a.statusMsg = status
}

// onDiscovered merges the response only when it belongs to the active picker.
func (a *app) onDiscovered(result *discoveryResult) {
	if result == nil || result.request != a.modelDiscovery || !strings.EqualFold(result.provider, a.discovering) {
		return
	}
	a.modelDiscoverCancel = nil
	a.discovering = ""
	if a.modalKind != modalModels || !a.models.active || !strings.EqualFold(result.provider, a.models.provider) {
		return
	}
	if result.err != nil {
		a.statusMsg = fmt.Sprintf("Could not discover models from %s: %v", result.provider, result.err)
		return
	}
	a.models.mergeDiscovered(result.provider, result.models)
	if a.rememberDiscoveredModels != nil {
		a.rememberDiscoveredModels(result.provider, result.models)
	}
	a.statusMsg = "Models refreshed from " + result.provider + "."
}

// selectPickedModel applies the highlighted model.
func (a *app) selectPickedModel() {
	item, ok := a.models.selected()
	if !ok {
		return
	}
	a.applyModel(item)
}

// applyModel switches the runner to the chosen model.
func (a *app) applyModel(item modelcatalog.Model) {
	provider, selected, err := a.selectModel(item.Provider, item.ID)
	if err != nil {
		a.statusMsg = "Could not select model: " + err.Error()
		return
	}
	a.r.setModel(provider, selected)
	if effort, err := llm.NormalizeEffort(selected, a.r.cfg.Effort); err == nil {
		a.r.setEffort(effort)
	} else {
		a.r.setEffort("")
	}
	a.modelID = item.Ref()
	a.effort.value = a.r.cfg.Effort
	a.models.close()
	a.invalidateModelDiscovery()
	a.modalKind = modalNone
	a.modalInput = ""
	a.statusMsg = "Model set to " + item.Ref() + "."
	a.updateTerminalTitle()
}

// openEffortPicker opens the effort modal, or applies an inline value.
func (a *app) openEffortPicker(value string) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "default") {
		a.applyEffort("")
		return
	}
	if value != "" {
		effort, err := llm.ParseEffort(value)
		if err != nil {
			a.statusMsg = err.Error()
			return
		}
		a.applyEffort(effort)
		return
	}
	a.effort.open(a.r.cfg.Effort)
	a.modalKind = modalEffort
	a.statusMsg = "Choose reasoning effort."
}

// applyEffort validates and stores the reasoning effort.
func (a *app) applyEffort(effort llm.Effort) {
	effort, err := llm.NormalizeEffort(a.r.cfg.Model, effort)
	if err != nil {
		a.statusMsg = err.Error()
		return
	}
	a.r.setEffort(effort)
	a.effort.value = effort
	a.effort.close()
	a.modalKind = modalNone
	if effort == "" {
		a.statusMsg = "Reasoning effort reset to provider default."
	} else {
		a.statusMsg = "Reasoning effort set to " + string(effort) + "."
	}
}

// applyShowThinking flips (no arg) or sets ("on"/"off") thinking visibility.
// It is a pure display toggle, so it is also allowed while a run streams.
func (a *app) applyShowThinking(arg string) {
	var show bool
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "":
		show = !a.sb.showThinking
	case "on", "true", "yes", "1":
		show = true
	case "off", "false", "no", "0":
		show = false
	default:
		a.statusMsg = "usage: /thinking [on|off]"
		return
	}
	a.sb.setShowThinking(show)
	if show {
		a.statusMsg = "Thinking shown."
	} else {
		a.statusMsg = "Thinking hidden."
	}
}

// openProviderPicker opens the provider modal.
func (a *app) openProviderPicker() {
	if a.availableProviders == nil || a.providerConfigured == nil || a.configureProvider == nil {
		a.statusMsg = "Provider configuration is unavailable."
		return
	}
	items := a.availableProviders()
	if len(items) == 0 {
		a.statusMsg = "No compatible providers are available."
		return
	}
	a.providers.open(items)
	a.modalKind = modalProviders
	a.statusMsg = "Choose a provider to configure."
}

// openProviderKeyEntry prompts for the highlighted provider's API key.
func (a *app) openProviderKeyEntry() {
	if len(a.providers.items) == 0 {
		return
	}
	provider := a.providers.items[a.providers.sel]
	if a.providerIsCustom != nil && a.providerIsCustom(provider.ID) {
		a.statusMsg = fmt.Sprintf("provider %q is managed as a custom provider", provider.Name)
		return
	}
	a.keyFor = provider
	a.modalInput = a.providerAPIKey(provider.ID)
	a.modalKind = modalProviderKey
}

// saveProviderKey stores the entered API key and returns to the list.
func (a *app) saveProviderKey() {
	if a.configureProvider == nil {
		return
	}
	if err := a.configureProvider(a.keyFor, a.modalInput); err != nil {
		a.statusMsg = "Could not save provider: " + err.Error()
		return
	}
	a.modalInput = ""
	a.keyFor = modelcatalog.Provider{}
	a.modalKind = modalProviders
	a.statusMsg = "Provider key saved."
}

// applyCustomizeSelection applies the highlighted /customize row.
func (a *app) applyCustomizeSelection() {
	row := customizeRows[a.customizeSel]
	if row.section == sectionStartup {
		a.applyWelcomeStyle(row)
		return
	}
	a.applyPromptStyle(row)
}

// applyWelcomeStyle stores the chosen startup style.
func (a *app) applyWelcomeStyle(row customizeRow) {
	if a.welcomeSty == row.welcome {
		a.modalKind = modalNone
		return
	}
	if a.saveWelcomeStyle != nil {
		if err := a.saveWelcomeStyle(row.welcome); err != nil {
			a.statusMsg = "Could not save startup style: " + err.Error()
			return
		}
	}
	a.welcomeSty = row.welcome
	a.modalKind = modalNone
	a.statusMsg = "Startup style set to " + row.label + "."
}

// applyPromptStyle stores the chosen composer chrome.
func (a *app) applyPromptStyle(row customizeRow) {
	if a.promptSty != row.prompt && a.savePromptStyle != nil {
		if err := a.savePromptStyle(row.prompt); err != nil {
			a.statusMsg = "Could not save composer style: " + err.Error()
			return
		}
	}
	a.promptSty = row.prompt
	a.modalKind = modalNone
	a.statusMsg = "Composer style set to " + row.label + "."
}

// acceptFile completes the highlighted @mention.
func (a *app) acceptFile() {
	path, ok := a.files.selected()
	if !ok {
		return
	}
	text := a.prompt.value()
	if a.files.end > a.files.start && a.files.end <= len(text) {
		text = text[:a.files.start] + path + text[a.files.end:]
	}
	a.prompt.setValue(text)
	a.files.close()
}

// acceptCommand completes the highlighted slash command. Enter runs commands
// whose arguments are optional; Tab always leaves room for a direct argument.
func (a *app) acceptCommand(submit bool) {
	item, ok := a.picker.selected()
	if !ok {
		return
	}
	if submit && (!item.requiresArg || item.allowsOmittedArg()) {
		a.prompt.setValue("")
		a.picker.close()
		a.runCommand(item.name)
		return
	}
	a.prompt.setValue(item.name + " ")
	a.picker.close()
}

// writeExport writes the session export, honoring overwrite confirmation.
func (a *app) writeExport(overwrite bool) {
	if a.exportSession == nil {
		return
	}
	path, err := a.exportSession(export.Format(a.exportFormat), a.modalInput, overwrite)
	if errors.Is(err, export.ErrFileExists) {
		a.modalOverwrite = true
		a.statusMsg = "File exists — enter overwrite, esc cancels."
		return
	}
	if err != nil {
		a.statusMsg = "Export failed: " + err.Error()
		return
	}
	a.modalKind = modalNone
	a.modalOverwrite = false
	a.exportFormat = ""
	a.modalInput = ""
	a.statusMsg = "Exported to " + path
}

// onClipboard consumes a clipboard read result.
func (a *app) onClipboard(payload clipboardPayload, err error) {
	if err != nil {
		a.statusMsg = "Clipboard: " + err.Error()
		return
	}
	if len(payload.image) > 0 {
		if addErr := a.att.add(payload.image); addErr != nil {
			a.statusMsg = "Clipboard image: " + addErr.Error()
			return
		}
		a.statusMsg = "Image attached from clipboard."
		return
	}
	if payload.text != "" {
		a.prompt.insertString(payload.text)
		a.syncPickers()
		a.statusMsg = "Pasted clipboard text."
	}
}

var _ = fmt.Sprintf
var _ = engine.Style{}
