package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/AlvinPlayz23/myagent/internal/export"
	modelcatalog "github.com/AlvinPlayz23/myagent/internal/models"
)

// overlayHandler is one modal layer above the agent screen. Key actions are
// offered to the topmost active overlay first; an overlay that does not
// consume a key lets it continue toward the composer. Height and render let
// the layout and the view treat every overlay uniformly, so a new overlay is
// a new adapter here rather than another root-model conditional.
type overlayHandler interface {
	overlayActive() bool
	overlayHeight() int
	overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool)
	overlayRender() string
}

// overlayRoute returns the overlay layers in topmost-first order. Exactly one
// is active at a time today; the fixed order makes stacked overlays (a
// confirmation above a picker, a palette above a viewer) well-defined when
// they arrive, without reordering anybody's dismissal keys.
//
// Adding an overlay means writing one adapter here: declare active/height/
// key/render, and routing, layout, and the view pick it up uniformly.
func (m *model) overlayRoute() []overlayHandler {
	return []overlayHandler{
		helpOverlay{m},
		exportPickOverlay{m},
		exportOverwriteOverlay{m},
		exportNameOverlay{m},
		sessionOverlay{m},
		modelOverlay{m},
		effortOverlay{m},
		customizeOverlay{m},
		providerKeyOverlay{m},
		providerOverlay{m},
		fileOverlay{m},
		commandOverlay{m},
	}
}

// helpOverlay lists commands and keybindings above the agent screen. It is
// the template for future informational overlays.
type helpOverlay struct{ *model }

func (o helpOverlay) overlayActive() bool { return o.helpActive }
func (o helpOverlay) overlayHeight() int {
	// Measure the same framed content used by the modal: its side padding can
	// wrap the two longest help rows even when the terminal itself is wide.
	return strings.Count(o.modalWindow(o.helpContent()), "\n") + 1 - modalWindowRows
}
func (o helpOverlay) overlayRender() string { return o.renderHelpOverlay() }

func (o helpOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "esc", "enter":
		o.helpActive = false
		o.statusMsg = ""
		o.updateLayout()
		return o.model, nil, true
	case "ctrl+c":
		// Preserve the global quit behavior in the composer layer.
		return o.model, nil, false
	}
	return o.model, nil, true
}

// exportPickOverlay chooses an export format.
type exportPickOverlay struct{ *model }

func (o exportPickOverlay) overlayActive() bool { return o.exportPick.active }
func (o exportPickOverlay) overlayHeight() int  { return 3 }
func (o exportPickOverlay) overlayRender() string {
	return o.renderExportPicker()
}

func (o exportPickOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "up":
		o.exportPick.move(-1)
	case "down":
		o.exportPick.move(1)
	case "enter":
		o.exportFormat = o.exportPick.format()
		o.exportPick.close()
		o.exportName.SetValue(export.DefaultFilename(o.sessionTitle))
		o.exportName.Focus()
		o.statusMsg = "Enter a file name, then press enter to export."
		o.updateLayout()
	case "esc":
		o.exportPick.close()
		o.statusMsg = "Export cancelled."
		o.updateLayout()
	}
	return o.model, nil, true
}

// exportOverwriteOverlay confirms overwriting an existing export file.
type exportOverwriteOverlay struct{ *model }

func (o exportOverwriteOverlay) overlayActive() bool { return o.exportOverwrite }
func (o exportOverwriteOverlay) overlayHeight() int  { return 3 }
func (o exportOverwriteOverlay) overlayRender() string {
	return o.th.cmdPickerSel.MaxWidth(max(1, o.width)).Render("File exists — enter overwrite, ↑/↓ return to name, esc cancel")
}

func (o exportOverwriteOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "up", "down":
		o.exportOverwrite = false
		o.updateLayout()
	case "enter":
		view, cmd := o.writeExport(true)
		return view, cmd, true
	case "esc":
		o.exportOverwrite = false
		o.exportFormat = ""
		o.statusMsg = "Export cancelled."
		o.updateLayout()
	}
	return o.model, nil, true
}

// exportNameOverlay collects the export file name.
type exportNameOverlay struct{ *model }

func (o exportNameOverlay) overlayActive() bool { return o.exportFormat != "" }
func (o exportNameOverlay) overlayHeight() int  { return 3 }
func (o exportNameOverlay) overlayRender() string {
	return o.th.cmdPickerSel.MaxWidth(max(1, o.width)).Render("Export as " + export.Label(o.exportFormat) + " — " + o.exportName.View() + " · enter export, esc cancel")
}

func (o exportNameOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "esc":
		o.exportFormat = ""
		o.exportName.Reset()
		o.statusMsg = "Export cancelled."
		o.updateLayout()
		return o.model, nil, true
	case "enter":
		view, cmd := o.writeExport(false)
		return view, cmd, true
	}
	var cmd tea.Cmd
	o.exportName, cmd = o.exportName.Update(k)
	return o.model, cmd, true
}

// sessionOverlay resumes a persisted session.
type sessionOverlay struct{ *model }

func (o sessionOverlay) overlayActive() bool { return o.sessions.active }
func (o sessionOverlay) overlayHeight() int  { return o.sessions.height() }
func (o sessionOverlay) overlayRender() string {
	return o.renderSessionPicker()
}

func (o sessionOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "up":
		o.sessions.move(-1)
		return o.model, nil, true
	case "down":
		o.sessions.move(1)
		return o.model, nil, true
	case "enter":
		view, cmd := o.resumeSelectedSession()
		return view, cmd, true
	case "esc":
		o.sessions.close()
		o.statusMsg = "Resume cancelled."
		o.updateLayout()
		return o.model, nil, true
	case "ctrl+c":
		// Preserve the global quit behavior in the composer layer.
		return o.model, nil, false
	default:
		return o.model, nil, true
	}
}

// modelOverlay searches and selects a model.
type modelOverlay struct{ *model }

func (o modelOverlay) overlayActive() bool { return o.models.active }
func (o modelOverlay) overlayHeight() int {
	height := o.models.height()
	if o.discovering != "" {
		height++ // room for the live-discovery indicator line
	}
	return height
}
func (o modelOverlay) overlayRender() string { return o.renderModelPicker() }

func (o modelOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "up":
		o.models.move(-1)
	case "down":
		o.models.move(1)
	case "enter":
		view, cmd := o.selectPickedModel()
		return view, cmd, true
	case "esc":
		o.models.close()
		o.statusMsg = "Model selection cancelled."
		o.updateLayout()
	case "backspace":
		if len(o.models.query) > 0 {
			o.models.query = o.models.query[:len(o.models.query)-1]
			o.models.filter()
			o.updateLayout()
		}
	default:
		if k.Text != "" {
			o.models.query += k.Text
			o.models.filter()
			o.updateLayout()
		}
	}
	return o.model, nil, true
}

// effortOverlay selects reasoning effort.
type effortOverlay struct{ *model }

func (o effortOverlay) overlayActive() bool { return o.effort.active }
func (o effortOverlay) overlayHeight() int  { return len(effortChoices) + 1 }
func (o effortOverlay) overlayRender() string {
	return o.renderEffortPicker()
}

func (o effortOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "up":
		o.effort.move(-1)
	case "down":
		o.effort.move(1)
	case "enter":
		view, cmd := o.applyEffort(o.effort.selected().effort)
		return view, cmd, true
	case "esc":
		o.effort.close()
		o.statusMsg = "Effort selection cancelled."
		o.updateLayout()
	}
	return o.model, nil, true
}

// customizeOverlay picks welcome and composer styles.
type customizeOverlay struct{ *model }

func (o customizeOverlay) overlayActive() bool { return o.customize.active }
func (o customizeOverlay) overlayHeight() int  { return len(customizeRows) + 1 }
func (o customizeOverlay) overlayRender() string {
	return o.renderCustomizePicker()
}

func (o customizeOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "up":
		o.customize.move(-1)
	case "down":
		o.customize.move(1)
	case "enter":
		view, cmd := o.applyCustomizeSelection()
		return view, cmd, true
	case "esc":
		o.customize.close()
		o.statusMsg = "Customization cancelled."
		o.updateLayout()
	}
	return o.model, nil, true
}

// providerKeyOverlay collects a provider API key.
type providerKeyOverlay struct{ *model }

func (o providerKeyOverlay) overlayActive() bool { return o.keyFor.ID != "" }
func (o providerKeyOverlay) overlayHeight() int {
	return min(10, max(2, len(o.providers.items)+1))
}
func (o providerKeyOverlay) overlayRender() string {
	return o.renderProviderKeyEntry()
}

func (o providerKeyOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "esc":
		o.keyInput.Reset()
		o.keyFor = modelcatalog.Provider{}
		o.providers.active = true
		o.statusMsg = "Provider edit cancelled."
		o.updateLayout()
		return o.model, nil, true
	case "enter":
		view, cmd := o.saveProviderKey()
		return view, cmd, true
	}
	var cmd tea.Cmd
	o.keyInput, cmd = o.keyInput.Update(k)
	return o.model, cmd, true
}

// providerOverlay selects a provider to configure.
type providerOverlay struct{ *model }

func (o providerOverlay) overlayActive() bool { return o.providers.active }
func (o providerOverlay) overlayHeight() int {
	return min(10, max(2, len(o.providers.items)+1))
}
func (o providerOverlay) overlayRender() string {
	return o.renderProviderPicker()
}

func (o providerOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "up":
		o.providers.move(-1)
	case "down":
		o.providers.move(1)
	case "enter":
		view, cmd := o.openProviderKeyEntry()
		return view, cmd, true
	case "esc":
		o.providers.close()
		o.statusMsg = "Provider selection cancelled."
		o.updateLayout()
	}
	return o.model, nil, true
}

// fileOverlay completes an @file mention. Unhandled keys continue to the
// composer so typing keeps editing the prompt while the menu is open.
type fileOverlay struct{ *model }

func (o fileOverlay) overlayActive() bool { return o.files.active }
func (o fileOverlay) overlayHeight() int  { return o.files.height() }
func (o fileOverlay) overlayRender() string {
	return o.renderFilePicker()
}

func (o fileOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "up":
		o.files.move(-1)
		return o.model, nil, true
	case "down":
		o.files.move(1)
		return o.model, nil, true
	case "tab", "enter":
		view, cmd := o.acceptFilePicker()
		return view, cmd, true
	case "esc":
		o.files.dismiss(o.input.Value())
		o.updateLayout()
		return o.model, nil, true
	}
	return o.model, nil, false
}

// commandOverlay completes a slash command. Unhandled keys continue to the
// composer so typing keeps editing the prompt while the menu is open.
type commandOverlay struct{ *model }

func (o commandOverlay) overlayActive() bool { return o.picker.active }
func (o commandOverlay) overlayHeight() int  { return o.picker.height() }
func (o commandOverlay) overlayRender() string {
	return o.renderCommandPicker()
}

func (o commandOverlay) overlayKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	switch k.Keystroke() {
	case "up":
		o.picker.move(-1)
		return o.model, nil, true
	case "down":
		o.picker.move(1)
		return o.model, nil, true
	case "tab":
		view, cmd := o.acceptCommandPicker(false)
		return view, cmd, true
	case "enter":
		view, cmd := o.acceptCommandPicker(true)
		return view, cmd, true
	case "esc":
		o.picker.dismiss(o.input.Value())
		o.updateLayout()
		return o.model, nil, true
	}
	return o.model, nil, false
}
