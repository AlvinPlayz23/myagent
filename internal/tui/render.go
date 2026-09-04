package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/AlvinPlayz23/myagent/internal/export"
)

// spinnerFrames is the working-state spinner (pi uses an animated Loader).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// renderComposer draws the textarea with the chrome its prompt style calls
// for. The default style is Grok's boxed prompt: a rounded border carrying
// the session title inlined in the top rule and the model info right-aligned
// into the bottom rule, brightening while focused. The ruled style keeps its
// historical rule-above/rule-below frame.
func (m *model) renderComposer() string {
	body := m.input.View()
	if m.promptStyle == promptRuled {
		rule := m.th.composerRule.Render(strings.Repeat("─", max(1, m.width)))
		return rule + "\n" + body + "\n" + rule
	}
	w := max(1, m.width)
	inner := w - 2
	if inner < 3 {
		return body
	}
	rail := m.th.composerRule
	if m.input.Focused() {
		rail = m.th.borderOn
	}

	// Top rule with the caption inlined, ending two cells before the corner,
	// drawn as ` title ` so its padding blanks the adjacent fill cells.
	top := "╭" + strings.Repeat("─", inner) + "╮"
	if caption := m.promptCaption(); caption != "" && inner >= 6 {
		label := " " + caption + " "
		if runes := []rune(label); len(runes) > inner-4 {
			label = string(runes[:inner-4])
		}
		labelW := len([]rune(label))
		if labelW >= 3 {
			left := strings.Repeat("─", inner-2-labelW)
			top = "╭" + left + rail.Render(label) + "──╮"
		}
	}

	// Text rows between the rails, one cell of side padding.
	rows := []string{top}
	innerBody := max(1, w-4)
	for _, line := range strings.Split(body, "\n") {
		if lipgloss.Width(line) > innerBody {
			line = ansi.Truncate(line, innerBody, "")
		}
		fill := max(0, innerBody-lipgloss.Width(line))
		rows = append(rows, "│ "+line+strings.Repeat(" ", fill)+" │")
	}

	// Bottom rule doubles as the info line: the model and flags right-aligned
	// over the fill, one padding cell from the corner.
	info := ansi.Truncate(" "+m.promptInfo(), inner, "")
	infoW := lipgloss.Width(info)
	fill := max(0, inner-infoW)
	rows = append(rows, "╰"+strings.Repeat("─", fill)+rail.Render(info)+"╯")
	return strings.Join(rows, "\n")
}

// promptCaption is the title inlined in the composer's top rule.
func (m *model) promptCaption() string {
	if m.hasSessionTitle {
		return m.sessionTitle
	}
	return "new"
}

// promptInfo is the info inlined in the composer's bottom rule: the active
// model plus attachment flags, separated the Grok way.
func (m *model) promptInfo() string {
	parts := []string{m.modelID, "ctrl+enter newline"}
	if n := m.attachments.len(); n > 0 {
		parts = append(parts, fmt.Sprintf("%d image%s attached", n, pluralS(n)))
	}
	return strings.Join(parts, " · ")
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (m *model) renderQueuedFollowUps() string {
	if len(m.queuedFollowUps) == 0 {
		return ""
	}
	width := max(1, m.width)
	lines := make([]string, 0, len(m.queuedFollowUps))
	for i, queued := range m.queuedFollowUps {
		label := "↳ next"
		if len(m.queuedFollowUps) > 1 {
			label = fmt.Sprintf("↳ next %d/%d", i+1, len(m.queuedFollowUps))
		}

		body := strings.Join(strings.Fields(queued.display), " ")
		bodyWidth := max(1, width-len([]rune(label))-4)
		if r := []rune(body); len(r) > bodyWidth {
			body = string(r[:bodyWidth-1]) + "…"
		}
		lines = append(lines, " "+m.th.queuedLabel.Render(label)+"  "+m.th.muted.Render(body))
	}
	return strings.Join(lines, "\n")
}

// renderPanel draws the topmost active overlay's panel. Pickers with room to
// spare render as Grok's centered bordered modal windows; on short terminals
// the border rows collapse first and the bare list is drawn bottom-attached.
func (m *model) renderPanel() string {
	for _, o := range m.overlayRoute() {
		if o.overlayActive() {
			body := o.overlayRender()
			if m.modalWindowFits(o) {
				return m.modalWindow(body)
			}
			return body
		}
	}
	return ""
}

// modalWindowRows counts the rounded window's top and bottom border rows.
const modalWindowRows = 2

// modalWindowFits reports whether the overlay can render as a centered
// bordered window: the terminal must be wide enough for the chrome and have
// two spare transcript rows for the borders.
func (m *model) modalWindowFits(o overlayHandler) bool {
	available := m.height - m.fixedHeight() - 1
	return m.width >= 40 && o.overlayHeight() <= available-modalWindowRows
}

// modalWindow centers the overlay content in a rounded Grok window.
func (m *model) modalWindow(body string) string {
	w := max(1, m.width-4)
	contentW := 0
	for _, line := range strings.Split(body, "\n") {
		contentW = max(contentW, lipgloss.Width(line))
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#323237")).
		Padding(0, 1).
		Width(min(w-4, contentW)).
		Render(body)
	return lipgloss.PlaceHorizontal(max(1, m.width), lipgloss.Center, box)
}

// renderScrollbar renders Grok's right-edge scrollbar: a dim track whose
// thumb brightens once the user scrolls away from the tail, advertising the
// non-following state. Each entry pairs with a viewport row.
func (m *model) renderScrollbar(height int) []string {
	track := m.th.border.Render("│")
	contentH := len(m.rows)
	visible := m.viewport.Height()
	thumb := m.th.footerRight.Render("▐") // following: dim
	if !m.viewport.AtBottom() {
		thumb = m.th.muted.Render("▐") // scrolled up: brighter
	}
	cells := make([]string, height)
	if contentH <= visible || visible <= 0 || contentH <= 0 {
		for i := range cells {
			cells[i] = track
		}
		return cells
	}
	size := max(1, visible*visible/contentH)
	start := m.viewport.YOffset() * visible / contentH
	for i := range cells {
		cells[i] = track
		if i >= start && i < start+size {
			cells[i] = thumb
		}
	}
	return cells
}

// renderCommandPicker draws the slash-command completion menu.
func (m *model) renderCommandPicker() string {
	count := m.panelContentHeight()
	if count == 0 {
		return ""
	}
	start, end := m.picker.visibleRange(count)
	var lines []string
	for i := start; i < end; i++ {
		item := m.picker.items[m.picker.matched[i]]
		marker := "  "
		style := m.th.cmdPickerItem
		if i == m.picker.sel {
			marker = "› "
			style = m.th.cmdPickerSel
		}
		line := fmt.Sprintf("%s%-18s %s", marker, item.usage, item.description)
		if len(m.picker.matched) > count && i == end-1 {
			line = padBetween(line, fmt.Sprintf("%d/%d", m.picker.sel+1, len(m.picker.matched)), m.width)
		}
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(line))
	}
	return strings.Join(lines, "\n")
}

// renderHelpOverlay draws the commands-and-keys panel, truncating with an
// indicator when the terminal is too short to show it all.
func (m *model) renderHelpOverlay() string {
	height := m.panelContentHeight()
	if height == 0 {
		return ""
	}
	lines := strings.Split(m.helpContent(), "\n")
	if len(lines) <= height {
		return strings.Join(lines, "\n")
	}
	if height == 1 {
		return lines[0]
	}
	return strings.Join(append(lines[:height-1], m.th.muted.Render("… (more help below)")), "\n")
}

// helpContent returns the complete styled help body before its active panel
// budget clips it. Keeping this independent from panelHeight lets the modal
// reserve enough rows for its own wrapped text.
func (m *model) helpContent() string {
	width := max(1, m.width)
	lines := []string{m.th.cmdPickerSel.MaxWidth(width).Render("Help — esc close")}
	for _, line := range strings.Split(strings.TrimSpace(helpText), "\n") {
		lines = append(lines, m.th.cmdPickerItem.MaxWidth(width).Render(line))
	}
	return strings.Join(lines, "\n")
}

// renderExportPicker draws the export format chooser.
func (m *model) renderExportPicker() string {
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Export session as — ↑/↓ select, enter continue, esc cancel")}
	for i, format := range []export.Format{export.Markdown, export.HTML} {
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.exportPick.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		lines = append(lines, style.Render(marker+export.Label(format)))
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderFilePicker() string {
	count := m.panelContentHeight()
	if count == 0 {
		return ""
	}
	start, end := m.files.visibleRange(count)
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Files — ↑/↓ select, enter or tab insert, esc cancel")}
	count = min(count-1, end-start)
	if count <= 0 {
		return strings.Join(lines, "\n")
	}
	start, end = m.files.visibleRange(count)
	for i := start; i < end; i++ {
		path := m.files.items[m.files.matched[i]]
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.files.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		line := marker + path
		if len(m.files.matched) > count && i == end-1 {
			line = padBetween(line, fmt.Sprintf("%d/%d", m.files.sel+1, len(m.files.matched)), m.width)
		}
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(line))
	}
	return strings.Join(lines, "\n")
}

// renderCustomizePicker draws the settings grouped under numbered headers. The
// window scrolls so the cursor stays visible on short terminals.
func (m *model) renderCustomizePicker() string {
	height := m.panelContentHeight()
	if height == 0 {
		return ""
	}
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Customize — ↑/↓ select, enter save, esc cancel")}
	count := min(height-1, len(customizeRows))
	if count <= 0 {
		return strings.Join(lines, "\n")
	}
	start := max(0, m.customize.sel-count+1)
	if maxStart := len(customizeRows) - count; start > maxStart {
		start = maxStart
	}
	for i := start; i < start+count; i++ {
		row := customizeRows[i]
		if row.header {
			line := fmt.Sprintf("%s  %s", row.label, m.th.muted.Render(row.description))
			lines = append(lines, m.th.pickerGroup.MaxWidth(max(1, m.width)).Render(line))
			continue
		}
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.customize.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		current := ""
		if m.rowIsCurrent(row) {
			current = "  (current)"
		}
		line := fmt.Sprintf("  %s%-10s %s%s", marker, row.label, row.description, current)
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(line))
	}
	return strings.Join(lines, "\n")
}

// rowIsCurrent reports whether a row holds the value its group is set to.
func (m *model) rowIsCurrent(row customizeRow) bool {
	if row.header {
		return false
	}
	if row.section == sectionComposer {
		return row.prompt == m.promptStyle
	}
	return row.welcome == m.welcomeStyle
}

func (m *model) renderEffortPicker() string {
	height := m.panelContentHeight()
	if height == 0 {
		return ""
	}
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Reasoning effort — ↑/↓ select, enter apply, esc cancel")}
	count := min(height-1, len(effortChoices))
	start := max(0, m.effort.sel-count+1)
	if maxStart := len(effortChoices) - count; start > maxStart {
		start = maxStart
	}
	current := m.runner.cfg.Effort
	for i := start; i < start+count; i++ {
		choice := effortChoices[i]
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.effort.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		selected := ""
		if choice.effort == current {
			selected = "  (current)"
		}
		line := fmt.Sprintf("%s%-9s %s%s", marker, choice.label, choice.description, selected)
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(line))
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderSessionPicker() string {
	height := m.panelContentHeight()
	if height == 0 {
		return ""
	}
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Resume session — ↑/↓ select, enter resume, esc cancel")}
	current := len(m.sessions.items) > 0 && m.sessions.currentID != "" && m.sessions.items[0].ID == m.sessions.currentID
	first, fixedRows := 0, 1
	if current {
		info := m.sessions.items[0]
		marker, style := "  ", m.th.cmdPickerItem
		if m.sessions.sel == 0 {
			marker, style = "› ", m.th.cmdPickerSel
		}
		id := info.ID
		if len(id) > 8 {
			id = id[:8]
		}
		title := info.Title
		if title == "" {
			title = info.Preview
		}
		if title == "" {
			title = "(no messages)"
		}
		line := fmt.Sprintf("%s● CURRENT  %s  %s  %s", marker, info.Modified.Local().Format("Jan 02 15:04"), id, title)
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(line))
		lines = append(lines, m.th.muted.MaxWidth(max(1, m.width)).Render(strings.Repeat("─", max(1, m.width))))
		first, fixedRows = 1, 3
	}
	count := min(height-fixedRows, len(m.sessions.items)-first)
	if count <= 0 {
		return strings.Join(lines, "\n")
	}
	start := m.sessions.sel - count + 1
	if start < first {
		start = first
	}
	if maxStart := len(m.sessions.items) - count; start > maxStart {
		start = maxStart
	}
	for i := start; i < start+count; i++ {
		info := m.sessions.items[i]
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.sessions.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		id := info.ID
		if len(id) > 8 {
			id = id[:8]
		}
		title := info.Title
		if title == "" {
			title = info.Preview
		}
		if title == "" {
			title = "(no messages)"
		}
		line := fmt.Sprintf("%s%s  %s  %s", marker, info.Modified.Local().Format("Jan 02 15:04"), id, title)
		if len(m.sessions.items)-first > count && i == start+count-1 {
			line = padBetween(line, fmt.Sprintf("%d/%d", m.sessions.sel+1, len(m.sessions.items)), m.width)
		}
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(line))
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderModelPicker() string {
	height := m.panelContentHeight()
	if height == 0 {
		return ""
	}

	extra := 0
	if m.discovering != "" {
		extra = 1
	}
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Model: " + m.models.query)}
	count := max(0, min(height-1-extra, len(m.models.matched)))
	if count == 0 {
		if extra > 0 {
			return strings.Join(append(lines, m.discoveryLine()), "\n")
		}
		return strings.Join(append(lines, m.th.muted.Render("  No matching configured-provider models.")), "\n")
	}
	start := max(0, m.models.sel-count+1)
	if maxStart := len(m.models.matched) - count; start > maxStart {
		start = maxStart
	}
	for i := start; i < start+count; i++ {
		item := m.models.items[m.models.matched[i]]
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.models.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		limit := ""
		if item.ContextWindow > 0 {
			limit = fmt.Sprintf("  %dk", item.ContextWindow/1000)
		}
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(marker+item.Ref()+limit))
	}
	if extra > 0 {
		lines = append(lines, m.discoveryLine())
	}
	return strings.Join(lines, "\n")
}

// discoveryLine renders the animated in-picker indicator for a running
// /v1/models lookup.
func (m *model) discoveryLine() string {
	frame := m.th.spinner.Render(spinnerFrames[m.spinnerFrame])
	return m.th.muted.Render(fmt.Sprintf("%s Checking %s for models…", frame, m.discovering))
}

func (m *model) renderProviderPicker() string {
	height := m.panelContentHeight()
	if height == 0 {
		return ""
	}
	lines := []string{m.th.cmdPickerSel.MaxWidth(max(1, m.width)).Render("Providers: [x] configured, enter edits key")}
	count := min(height-1, len(m.providers.items))
	start := max(0, m.providers.sel-count+1)
	if maxStart := len(m.providers.items) - count; start > maxStart {
		start = maxStart
	}
	for i := start; i < start+count; i++ {
		item := m.providers.items[i]
		marker, style := "  ", m.th.cmdPickerItem
		if i == m.providers.sel {
			marker, style = "› ", m.th.cmdPickerSel
		}
		locked := ""
		if m.providerIsCustom != nil && m.providerIsCustom(item.ID) {
			locked = "  managed as custom"
		} else if m.providerConfigured(item.ID) {
			locked = "  [x]"
		}
		lines = append(lines, style.MaxWidth(max(1, m.width)).Render(marker+item.Name+locked))
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderProviderKeyEntry() string {
	action := "Configure "
	if m.providerConfigured(m.keyFor.ID) {
		action = "Edit "
	}
	return m.th.cmdPickerSel.Render(action+m.keyFor.Name+"\n") + m.keyInput.View()
}

// statusLine renders Grok's turn-status row: activity on the left, the
// elapsed timer right-aligned, and a ` │ `-separated unseen-rows hint. The
// status text keeps the segment feel of the bottom status line.
func (m *model) statusLine() string {
	sep := m.th.footerRight.Render(" │ ")
	var segments []string
	if m.working {
		frame := m.th.spinner.Render(spinnerFrames[m.spinnerFrame])
		msg := "Working…"
		if m.statusMsg != "" {
			msg = m.statusMsg
		}
		segments = append(segments, frame+" "+m.th.assistantTxt.Render(msg))
	} else if m.statusMsg != "" {
		segments = append(segments, m.th.muted.Render(m.statusMsg))
	}
	if m.unseenRows > 0 {
		segments = append(segments, m.th.queuedLabel.Render(fmt.Sprintf("↓ %d below", m.unseenRows)))
	}
	if len(segments) == 0 {
		return ""
	}
	left := strings.Join(segments, sep)
	if !m.working {
		return left
	}
	timer := m.th.muted.Render(fmt.Sprintf("%.1fs · esc to cancel", time.Since(m.startedAt).Seconds()))
	if lipgloss.Width(left)+lipgloss.Width(timer)+3 <= m.width {
		return padBetween(left, timer, m.width)
	}
	return left + sep + timer
}

// topBar renders the quiet workspace/context line above scrollback. The model
// stays in the prompt caption, leaving this row a stable orientation aid.
func (m *model) topBar() string {
	if m.topBarHeight() == 0 {
		return ""
	}
	location := collapseHome(m.cwd)
	if location == "" {
		location = "new session"
	}
	left := m.th.topBar.Render(location)
	tokens := m.usage.Input + m.usage.Output + m.usage.CacheRead + m.usage.CacheWrite
	right := m.th.topBar.Render(compact(tokens) + " tokens")
	if lipgloss.Width(left)+lipgloss.Width(right)+3 > m.width {
		return ansi.Truncate(left, max(1, m.width), "")
	}
	return padBetween(left, right, m.width)
}

// shortcuts is the compact, single-row command strip beneath the composer.
// It only advertises keys that the local router actually handles.
func (m *model) shortcuts() string {
	if m.shortcutsHeight() == 0 {
		return ""
	}
	pair := func(key, action string) string {
		return m.th.shortcutKey.Render(key) + m.th.shortcut.Render(":"+action)
	}
	sep := m.th.footerRight.Render("  │  ")
	line := strings.Join([]string{
		pair("enter", "send"),
		pair("ctrl+enter", "newline"),
		pair("alt+enter", "steer"),
		pair("esc", "cancel"),
		pair("ctrl+o", "details"),
	}, sep)
	return ansi.Truncate(line, max(1, m.width), "…")
}
