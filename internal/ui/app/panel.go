package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/tui"
	"github.com/AlvinPlayz23/myagent/internal/ui/kit"
)

// panelListBody is the host-authoritative list body used by every migrated
// list panel. It forwards every key to the legacy onKey path so the panel's
// own cursor/confirm/back semantics stay in tui; mouse rows use
// KitPickerAccept with select-first-then-activate parity.
type panelListBody struct {
	kit.WidgetBase

	shell       *Shell
	list        *kit.List
	hint        string
	layoutDirty bool
	details     bool
}

func newPanelListBody(s *Shell, state tui.KitPickerState) *panelListBody {
	b := &panelListBody{
		WidgetBase: kit.NewWidgetBase("kit.panel.list"),
		shell:      s,
		list:       kit.NewList(kit.ListOpts{ID: "kit.panel.list.rows", Compact: true}),
		hint:       state.Hint,
	}
	b.list.OnChoose = func(row kit.ListRow) bool {
		s.emit(s.host.KitPickerAccept(row.ID))
		return true
	}
	b.apply(state)
	return b
}

// apply re-feeds host rows and selection without disturbing an in-flight
// legacy key sequence.
func (b *panelListBody) apply(state tui.KitPickerState) {
	if len(b.list.Rows) != len(state.Items) || b.hint != state.Hint {
		b.layoutDirty = true
	}
	oldDetails := b.details
	b.details = false
	if state.Kind != "files" {
		for _, item := range state.Items {
			if item.Desc != "" {
				b.details = true
				break
			}
		}
	}
	if oldDetails != b.details {
		b.layoutDirty = true
	}
	b.list.SetRows(panelRows(state.Items))
	b.hint = state.Hint
	b.selectID(state.SelectedID)
	b.MarkDirty()
}

// selectID moves the row marker to id when present.
func (b *panelListBody) selectID(id string) {
	if id == "" {
		return
	}
	for i, row := range b.list.Rows {
		if !row.Header && row.ID == id {
			if b.list.Selected != i {
				b.list.Selected = i
				b.list.MarkDirty()
			}
			return
		}
	}
}

// selectedID returns the current row identity, or "".
func (b *panelListBody) selectedID() string {
	if row, ok := b.list.SelectedRow(); ok {
		return row.ID
	}
	return ""
}

// listRect is the row area below the one-row hint.
func (b *panelListBody) listRect(r kit.Rect) kit.Rect {
	if b.hint != "" {
		r.Y++
		r.H = max(0, r.H-1)
	}
	if b.details {
		r.H = max(0, r.H-kit.MenuDetailsHeight)
	}
	return r
}

// PreferredHeight sizes the dialog to the hint plus visible rows, capped by
// the §3.0.D h/2 − 6 rule.
func (b *panelListBody) PreferredHeight(_, maxH int) int {
	capRows := max(1, maxH/2-6)
	rows := min(len(b.list.Rows), capRows)
	h := rows
	if b.hint != "" {
		h++
	}
	if b.details {
		h += kit.MenuDetailsHeight
	}
	return max(1, h)
}

func (b *panelListBody) NeedsLayout() bool { return b.layoutDirty }

func (b *panelListBody) Layout(r kit.Rect) {
	b.layoutDirty = false
	b.SetBounds(r)
	b.list.Layout(b.listRect(r))
}

func (b *panelListBody) Render(w, h int) string {
	if !b.Visible || w <= 0 || h <= 0 {
		return ""
	}
	th := kit.DefaultTheme()
	var lines []string
	listH := h
	if b.details {
		listH = max(0, listH-kit.MenuDetailsHeight)
	}
	if b.hint != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(th.Text.Muted.Color()).
			Render(ansi.Truncate(b.hint, w, "…")))
		listH = max(0, listH-1)
	}
	if listH > 0 {
		lines = append(lines, strings.Split(b.list.Render(w, listH), "\n")...)
	}
	if b.details {
		if row, ok := b.list.SelectedRow(); ok {
			lines = append(lines, kit.RenderMenuDetails(row.Desc, w, min(kit.MenuDetailsHeight, h)))
		}
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

func (b *panelListBody) Key(ev tea.KeyPressMsg) bool {
	b.shell.emit(b.shell.host.KitKey(ev))
	return true
}

// Mouse selects first and activates on the already-selected row, matching the
// legacy confirmInlineRow path. A selection move is sent through
// KitPickerAccept so the host cursor tracks the dialog.
func (b *panelListBody) Mouse(ev tea.MouseMsg, r kit.Rect) bool {
	before := b.selectedID()
	rect := b.listRect(r)
	if !b.list.Mouse(ev, rect) {
		return false
	}
	if id := b.selectedID(); id != "" && id != before && b.shell.panelOpen {
		b.shell.emit(b.shell.host.KitPickerAccept(id))
	}
	return true
}

func (b *panelListBody) FocusTarget() *kit.FocusTarget { return nil }

// panelInputBody is the host-authoritative input body. The legacy textinput
// keeps the cursor and masking; the dialog only paints its View().
type panelInputBody struct {
	kit.WidgetBase

	shell *Shell
	input string
	ph    string
	hint  string
	mask  bool
}

func newPanelInputBody(s *Shell, state tui.KitPickerState) *panelInputBody {
	b := &panelInputBody{
		WidgetBase: kit.NewWidgetBase("kit.panel.input"),
		shell:      s,
	}
	b.apply(state)
	return b
}

func (b *panelInputBody) apply(state tui.KitPickerState) {
	b.input = state.Input
	b.ph = state.Placeholder
	b.hint = state.Hint
	b.mask = state.Masked
	b.MarkDirty()
}

func (b *panelInputBody) PreferredHeight(_, _ int) int {
	if b.hint != "" {
		return 2
	}
	return 1
}

func (b *panelInputBody) Layout(r kit.Rect) { b.SetBounds(r) }

func (b *panelInputBody) Render(w, h int) string {
	if !b.Visible || w <= 0 || h <= 0 {
		return ""
	}
	th := kit.DefaultTheme()
	prompt := "› "
	if b.mask {
		prompt = "• "
	}
	field := b.input
	fg := th.Text.Base
	if field == "" {
		field = b.ph
		fg = th.Text.Muted
	}
	line := lipgloss.NewStyle().
		Foreground(fg.Color()).
		Background(th.Background.Raised.High.Color()).
		Width(w).
		MaxHeight(1).
		Render(prompt + field)
	lines := []string{line}
	if h > 1 && b.hint != "" {
		lines = append(lines, lipgloss.NewStyle().
			Foreground(th.Text.Muted.Color()).
			Render(ansi.Truncate(b.hint, w, "…")))
	}
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

func (b *panelInputBody) Key(ev tea.KeyPressMsg) bool {
	b.shell.emit(b.shell.host.KitKey(ev))
	return true
}

func (b *panelInputBody) Mouse(tea.MouseMsg, kit.Rect) bool { return false }

func (b *panelInputBody) FocusTarget() *kit.FocusTarget { return nil }

// confirmPanelBody forwards only up/down to the legacy confirm handler. Enter
// and Esc stay with kit.NewConfirm so the standard dialog action grammar owns
// them; up/down keep the export-overwrite "return to name" path.
type confirmPanelBody struct {
	kit.Widget
	shell *Shell
}

func (b *confirmPanelBody) Key(ev tea.KeyPressMsg) bool {
	switch ev.String() {
	case "up", "down":
		b.shell.emit(b.shell.host.KitKey(ev))
		return true
	}
	return false
}

// syncPanel mirrors the active non-select panel into a DialogStack body.
func (s *Shell) syncPanel(state tui.KitPickerState) {
	sig := pickerSignature(state)
	if !s.panelOpen {
		s.openPanel(state, sig)
		return
	}
	if state.Kind != s.panelKind || state.Mode != s.panelMode {
		s.closePanel()
		s.openPanel(state, sig)
		return
	}
	if sig == s.panelSig {
		return
	}
	if state.Mode == tui.KitPickerConfirm {
		// Confirm bodies are built by NewConfirm and are cheap to recreate;
		// replacement also refreshes the label/body pair.
		s.closePanel()
		s.openPanel(state, sig)
		return
	}
	if s.panelLayout != nil {
		s.panelLayout.SetTitle(state.Title)
	}
	if s.panelList != nil {
		s.panelList.apply(state)
	}
	if s.panelInput != nil {
		s.panelInput.apply(state)
	}
	s.setPanelButtons(state)
	s.panelSig = sig
}

// openPanel builds the dialog surface for the active non-select panel.
func (s *Shell) openPanel(state tui.KitPickerState, sig string) {
	s.panelKind = state.Kind
	s.panelMode = state.Mode
	s.panelSig = sig
	s.panelOpen = true

	if state.Mode == tui.KitPickerConfirm {
		s.openConfirmPanel(state)
		return
	}

	var body kit.Widget
	switch state.Mode {
	case tui.KitPickerList:
		s.panelList = newPanelListBody(s, state)
		s.panelInput = nil
		body = s.panelList
	case tui.KitPickerInput:
		s.panelInput = newPanelInputBody(s, state)
		s.panelList = nil
		body = s.panelInput
	default:
		s.panelOpen = false
		return
	}

	footer := kit.NewDialogFooter(
		kit.DialogButton{Label: panelCancelLabel(state), Key: "esc", Action: func() bool {
			s.cancelPanel()
			return true
		}},
		kit.DialogButton{Label: panelConfirmLabel(state), Key: "enter", Primary: true, Action: func() bool {
			s.emit(s.host.KitKey(tea.KeyPressMsg{Code: tea.KeyEnter}))
			return true
		}},
	)
	layout := kit.NewDialogLayout(kit.DialogLayoutOpts{
		ID:     "kit.panel.dialog",
		Title:  state.Title,
		Body:   body,
		Footer: footer,
	})
	layout.OnDismiss = s.panelDismissed

	s.panelLayout = layout
	s.panelFooter = footer
	s.dialogs.Push(layout)
}

// openConfirmPanel uses kit.NewConfirm for the standard title/body/footer
// grammar and chains the shell's dismissal cleanup after NewConfirm's own.
func (s *Shell) openConfirmPanel(state tui.KitPickerState) {
	s.panelList = nil
	s.panelInput = nil
	layout := kit.NewConfirm(kit.ConfirmOpts{
		Title:        state.Title,
		Body:         state.Body,
		ConfirmLabel: panelConfirmLabel(state),
		CancelLabel:  panelCancelLabel(state),
		OnConfirm: func() bool {
			s.emit(s.host.KitPickerAccept(state.Kind))
			// A save failure leaves the host in the same panel; veto the close
			// so the dialog stays open for a retry.
			if next, active := s.host.KitPicker(); active && next.Kind == state.Kind {
				return false
			}
			return true
		},
	})
	if layout.Body != nil {
		layout.Body = &confirmPanelBody{Widget: layout.Body, shell: s}
	}
	inner := layout.OnDismiss
	layout.OnDismiss = func() {
		if inner != nil {
			inner()
		}
		s.panelDismissed()
	}
	// Push wires a bare Pop to OnClose. Own it here so a host Sync that already
	// replaced this dialog can never pop the next one.
	layout.OnClose = func() bool {
		if s.dialogs.Top() == layout {
			s.dialogs.Pop()
		}
		return true
	}

	s.panelLayout = layout
	s.panelFooter = layout.Footer
	s.dialogs.Push(layout)
}

// setPanelButtons refreshes the footer labels after a same-kind host update
// (customize's group→choice transition changes "open" to "save").
func (s *Shell) setPanelButtons(state tui.KitPickerState) {
	if s.panelFooter == nil || len(s.panelFooter.Buttons) != 2 {
		return
	}
	cancel := panelCancelLabel(state)
	confirm := panelConfirmLabel(state)
	changed := false
	if s.panelFooter.Buttons[0].Label != cancel {
		s.panelFooter.Buttons[0].Label = cancel
		changed = true
	}
	if s.panelFooter.Buttons[1].Label != confirm {
		s.panelFooter.Buttons[1].Label = confirm
		changed = true
	}
	if changed {
		s.panelFooter.MarkDirty()
	}
}

// closePanel closes only the panel dialog, suppressing the Dismissed cancel
// when the host already closed or the shell is replacing the dialog.
func (s *Shell) closePanel() {
	if !s.panelOpen {
		return
	}
	s.suppressPanelCancel = true
	if s.panelLayout != nil && s.dialogs.Top() == s.panelLayout {
		s.dialogs.Pop()
	}
	s.suppressPanelCancel = false
	s.panelList = nil
	s.panelInput = nil
	s.panelLayout = nil
	s.panelFooter = nil
	s.panelOpen = false
	s.panelSig = ""
	s.panelKind = ""
	s.panelMode = tui.KitPickerSelect
}

// cancelPanel runs the legacy Esc path and closes the dialog immediately.
func (s *Shell) cancelPanel() {
	if !s.panelOpen {
		return
	}
	s.emit(s.host.KitPickerCancel())
	s.closePanel()
}

// panelDismissed is the DialogStack pop/backdrop hook. It runs legacy cancel
// only when the host still reports the same panel.
func (s *Shell) panelDismissed() {
	if s.suppressPanelCancel {
		return
	}
	kind := s.panelKind
	s.panelList = nil
	s.panelInput = nil
	s.panelLayout = nil
	s.panelFooter = nil
	s.panelOpen = false
	s.panelSig = ""
	s.panelKind = ""
	s.panelMode = tui.KitPickerSelect
	if kind == "" {
		return
	}
	if state, active := s.host.KitPicker(); active && state.Kind == kind {
		s.emit(s.host.KitPickerCancel())
	}
}

// panelRows maps the host projection onto raw list rows.
func panelRows(items []tui.KitPickerItem) []kit.ListRow {
	out := make([]kit.ListRow, len(items))
	for i, item := range items {
		out[i] = kit.ListRow{
			ID:     item.ID,
			Title:  item.Title,
			Desc:   item.Desc,
			Footer: item.Footer,
			Header: item.Header,
		}
	}
	return out
}

func panelCancelLabel(state tui.KitPickerState) string {
	if state.CancelLabel != "" {
		return state.CancelLabel
	}
	return "cancel"
}

func panelConfirmLabel(state tui.KitPickerState) string {
	if state.ConfirmLabel != "" {
		return state.ConfirmLabel
	}
	return "confirm"
}
