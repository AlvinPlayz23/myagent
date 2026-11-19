package app

import (
	"strconv"
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/AlvinPlayz23/myagent/internal/ui/kit"
)

// syncPicker routes the host projection to the S4 fuzzy select bridge or the
// P6 panel bridge. Only one dialog surface is ever open: switching modes closes
// the other with its cancel suppressed (the host already changed kind).
func (s *Shell) syncPicker() {
	state, active := s.host.KitPicker()
	if !active {
		s.closePicker()
		s.closePanel()
		s.closeCommandMenu()
		return
	}
	// Slash commands and their effort selector share the composer menu;
	// the ctrl+p path stays in a modal.
	if state.Inline && (state.Kind == "commands" || state.Kind == "effort") {
		s.closePicker()
		s.closePanel()
		s.syncCommandMenu(state)
		return
	}
	s.closeCommandMenu()
	if state.Kind == "effort" && !state.Inline && s.pickerOpen && s.pickerKind == "commands" {
		s.openEffortInPalette(state)
		return
	}
	if state.Mode == tui.KitPickerSelect {
		s.closePanel()
		s.syncSelectPicker(state)
		return
	}
	s.closePicker()
	s.syncPanel(state)
}

// syncSelectPicker mirrors the legacy model/command picker into a DialogSelect.
// The host stays authoritative: item refreshes re-feed the dialog, and every
// close path runs the same cancel/accept semantics as the legacy keys.
func (s *Shell) syncSelectPicker(state tui.KitPickerState) {
	sig := pickerSignature(state)
	if !s.pickerOpen {
		s.openPicker(state, sig)
		return
	}
	if state.Kind != s.pickerKind {
		s.closePicker()
		s.openPicker(state, sig)
		return
	}
	if sig == s.pickerSig {
		return
	}
	if s.picker != nil {
		s.picker.SetItems(pickerItems(state.Items))
		if state.SelectedID != "" {
			s.picker.SelectID(state.SelectedID)
		}
	}
	if s.pickerLayout != nil && s.pickerLayout.Title != state.Title {
		s.pickerLayout.Title = state.Title
		s.pickerLayout.MarkDirty()
	}
	s.pickerSig = sig
}

// openPicker builds the dialog surface for the active legacy picker. Enter in
// the body accepts; list panels can also expose a confirm footer action.
// Esc and backdrop dismissal share KitPickerCancel through Dismissed.
func (s *Shell) openPicker(state tui.KitPickerState, sig string) {
	sel := kit.NewDialogSelect(kit.DialogSelectOpts{
		ID:              "kit.picker",
		Items:           pickerItems(state.Items),
		Placeholder:     state.Placeholder,
		Palette:         true,
		Details:         state.Kind != "files",
		SectionDividers: state.Kind == "commands",
	})
	query := state.Query
	if state.Kind == "commands" {
		query = strings.TrimPrefix(query, "/")
	}
	sel.SetQuery(query)
	if state.SelectedID != "" {
		sel.SelectID(state.SelectedID)
	}
	sel.OnSelect = func(item kit.SelectItem) bool {
		s.emit(s.host.KitPickerAccept(item.ID))
		return true
	}
	var footer *kit.DialogFooter
	if state.Kind != "commands" && state.Kind != "models" {
		footer = kit.NewDialogFooter(
			kit.DialogButton{Label: "cancel", Key: "esc", Action: func() bool {
				s.cancelPicker()
				return true
			}},
			kit.DialogButton{Label: "confirm", Key: "enter", Primary: true, Action: func() bool {
				if id := sel.SelectedID(); id != "" {
					s.emit(s.host.KitPickerAccept(id))
				}
				return true
			}},
		)
	}
	layout := kit.NewDialogLayout(kit.DialogLayoutOpts{
		ID:     "kit.picker.dialog",
		Title:  state.Title,
		Body:   sel,
		Footer: footer,
	})
	layout.OnDismiss = s.pickerDismissed

	s.picker = sel
	s.pickerLayout = layout
	s.pickerKind = state.Kind
	s.pickerSig = sig
	s.pickerOpen = true
	s.dialogs.Push(layout)
}

// closePicker closes only the picker dialog, suppressing the Dismissed cancel
// when the host already closed (accept) or the shell initiated the close.
func (s *Shell) closePicker() {
	if !s.pickerOpen {
		return
	}
	s.suppressPickerCancel = true
	if s.pickerLayout != nil && s.dialogs.Top() == s.pickerLayout {
		s.dialogs.Pop()
	}
	s.suppressPickerCancel = false
	s.picker = nil
	s.pickerLayout = nil
	s.pickerOpen = false
	s.pickerSig = ""
	s.pickerKind = ""
}

// cancelPicker runs the legacy Esc path and closes the dialog immediately.
func (s *Shell) cancelPicker() {
	if !s.pickerOpen {
		return
	}
	s.emit(s.host.KitPickerCancel())
	s.closePicker()
}

// pickerDismissed is the DialogStack pop/backdrop hook. It forwards to the
// legacy cancel exactly once; internal replacement is suppressed.
func (s *Shell) pickerDismissed() {
	if s.suppressPickerCancel {
		return
	}
	s.picker = nil
	s.pickerLayout = nil
	s.pickerOpen = false
	s.pickerSig = ""
	s.pickerKind = ""
	s.emit(s.host.KitPickerCancel())
}

// pickerItems maps the host projection onto dialog rows.
func pickerItems(items []tui.KitPickerItem) []kit.SelectItem {
	out := make([]kit.SelectItem, len(items))
	for i, item := range items {
		out[i] = kit.SelectItem{
			ID:     item.ID,
			Title:  item.Title,
			Desc:   item.Desc,
			Group:  item.Group,
			Footer: item.Footer,
		}
	}
	return out
}

// pickerSignature fingerprints every paint-relevant field so a host refresh
// re-feeds the dialog without clobbering the dialog's own search query.
func pickerSignature(state tui.KitPickerState) string {
	var b strings.Builder
	write := func(s string) {
		b.WriteString(s)
		b.WriteByte(0)
	}
	write(state.Kind)
	write(state.Title)
	write(state.Placeholder)
	write(state.Query)
	write(state.SelectedID)
	write(strconv.Itoa(int(state.Mode)))
	write(state.Body)
	write(state.Input)
	write(strconv.FormatBool(state.Masked))
	write(state.Hint)
	write(state.ConfirmLabel)
	write(state.ConfirmKey)
	write(state.CancelLabel)
	for _, item := range state.Items {
		write(item.ID)
		write(item.Title)
		write(item.Desc)
		write(item.Group)
		write(item.Footer)
		write(strconv.FormatBool(item.Header))
	}
	return b.String()
}
