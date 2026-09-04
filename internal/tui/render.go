package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/export"
)

// spinnerFrames is the working-state spinner (pi uses an animated Loader).
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// renderComposer draws the textarea with the chrome its prompt style calls for.
func (m *model) renderComposer() string {
	if m.promptStyle != promptRuled {
		return m.input.View()
	}
	rule := m.th.composerRule.Render(strings.Repeat("─", max(1, m.width)))
	return rule + "\n" + m.input.View() + "\n" + rule
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

// renderPanel draws the topmost active overlay's panel.
func (m *model) renderPanel() string {
	for _, o := range m.overlayRoute() {
		if o.overlayActive() {
			return o.overlayRender()
		}
	}
	return ""
}

// renderCommandPicker draws the slash-command completion menu.
func (m *model) renderCommandPicker() string {
	count := m.panelHeight()
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
	count := m.panelHeight()
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
	height := m.panelHeight()
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
			marker, style = "> ", m.th.cmdPickerSel
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
	height := m.panelHeight()
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
			marker, style = "> ", m.th.cmdPickerSel
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
	height := m.panelHeight()
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
	height := m.panelHeight()
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
	height := m.panelHeight()
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

// statusLine shows the working spinner + elapsed time, a transient status,
// and — when the user has scrolled away from the tail — how many rows of
// output wait below.
func (m *model) statusLine() string {
	indicator := ""
	if m.unseenRows > 0 {
		indicator = "  " + m.th.accent.Render(fmt.Sprintf("↓ %d below", m.unseenRows))
	}
	if m.working {
		frame := m.th.spinner.Render(spinnerFrames[m.spinnerFrame])
		elapsed := time.Since(m.startedAt).Seconds()
		msg := "Working…"
		if m.statusMsg != "" {
			msg = m.statusMsg
		}
		return fmt.Sprintf("%s %s%s", frame,
			m.th.muted.Render(fmt.Sprintf("%s (%.1fs, esc to cancel)", msg, elapsed)), indicator)
	}
	if m.statusMsg != "" {
		return m.th.muted.Render(m.statusMsg) + indicator
	}
	return strings.TrimLeft(indicator, " ")
}

// footer renders the cwd/model line and the token/cost stats line.
func (m *model) footer() string {
	left := m.th.footer.Render(collapseHome(m.cwd))
	right := m.th.footerRight.Render(m.modelID)
	line1 := padBetween(left, right, m.width)

	stats := fmt.Sprintf("↑%s ↓%s R%s W%s $%.4f",
		compact(m.usage.Input), compact(m.usage.Output),
		compact(m.usage.CacheRead), compact(m.usage.CacheWrite),
		m.usage.Cost.Total)
	line2 := m.th.footer.Render(stats)
	return line1 + "\n" + line2
}
