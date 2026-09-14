package tui

// Mouse interaction core (Bubble Tea native).
//
// Coordinate systems:
//   - View rows: the composed frame (viewport, status, panel, queued,
//     attachments, composer, footer). Clicks and hover arrive in these
//     coordinates and are routed by routeViewY.
//   - Transcript content rows: viewport.YOffset + y. Welcome menu hits and
//     text selection live here; the welcome renderers record their menu
//     range in content coordinates on every render.
//
// The inline panel renderers record hit layouts as a side effect so clicks
// can never desync from what's drawn: inlineRowItems holds one entry per
// emitted panel line (item index or -1 for headers/dividers), and View
// records the panel slot's view-Y bounds (panelStartY/panelEndY).

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

// hitRegion identifies which View section a mouse Y coordinate falls in.
type hitRegion int

const (
	hitViewport hitRegion = iota
	hitPanel
	hitBelow // composer, shortcuts, footer: no mouse actions
)

// Hover kinds for repaint-on-flip hover highlights.
const (
	hoverNone = iota
	hoverInline
	hoverWelcome
)

// multiClickTimeout matches grok's MULTI_CLICK_TIMEOUT_MS: presses on the
// same transcript row within this window cycle single → word → paragraph.
const multiClickTimeout = 300 * time.Millisecond

// pendingTTL is how long a "press again to confirm" arm stays live.
const pendingTTL = 3 * time.Second

// pendingTimeoutMsg expires a pending-confirm arm.
type pendingTimeoutMsg struct{ key string }

// routeViewY maps a View Y coordinate to viewport, panel slot, or below.
// Panel bounds are recorded by View on every frame.
func (m *model) routeViewY(y int) hitRegion {
	if y < m.viewport.Height() {
		return hitViewport
	}
	if y >= m.panelStartY && y < m.panelEndY {
		return hitPanel
	}
	return hitBelow
}

// setHover records hover state, reporting whether anything flipped so callers
// repaint only on change (grok's Moved → Changed contract).
func (m *model) setHover(kind, idx int, hint string) bool {
	if m.hoverKind == kind && m.hoverIdx == idx && m.hoverHint == hint {
		return false
	}
	m.hoverKind, m.hoverIdx, m.hoverHint = kind, idx, hint
	return true
}

func (m *model) clearHover() bool { return m.setHover(hoverNone, -1, "") }

// updateHover recomputes hover for a no-button motion event. Welcome menu and
// inline picker rows highlight; transcript links surface a hint in the
// status line (the transcript itself is not re-rendered for hover).
func (m *model) updateHover(x, y int) {
	kind, idx, hint := hoverNone, -1, ""
	switch m.routeViewY(y) {
	case hitViewport:
		point, ok := m.transcriptPoint(x, y)
		if !ok {
			break
		}
		if m.showWelcome() {
			if point.row >= m.welcomeMenu[0] && point.row < m.welcomeMenu[1] {
				kind, idx = hoverWelcome, point.row-m.welcomeMenu[0]
			}
			break
		}
		if url := m.hoverLinkAt(point); url != "" {
			hint = "ctrl+click to open " + url
		}
	case hitPanel:
		if i := y - m.panelStartY; i >= 0 && i < len(m.inlineRowItems) && m.inlineRowItems[i] >= 0 {
			kind, idx = hoverInline, i
		}
	case hitBelow:
	}
	if m.setHover(kind, idx, hint) {
		if kind == hoverWelcome {
			m.refreshViewport()
		}
		// Panel hovers repaint via the next View; viewport hovers need a
		// content refresh, and hint-only flips need nothing at all.
	}
}

// hoverLinkAt returns the assistant-block URL under a transcript point, or "".
// Same rule as Ctrl+click (assistant blocks only); plain hover never opens.
func (m *model) hoverLinkAt(point textPoint) string {
	b, line, ok := m.transcript.blockLineAt(point.row, m.width)
	if !ok || b.kind != blockAssistant {
		return ""
	}
	return urlAtDisplayColumn(line, point.col)
}

// trackClick advances the multi-click counter for transcript presses on the
// same content row within the timeout window.
func (m *model) trackClick(row int) {
	now := time.Now()
	if now.Sub(m.lastClickAt) < multiClickTimeout && row == m.lastClickRow {
		m.clickCount++
	} else {
		m.clickCount = 1
	}
	m.lastClickAt, m.lastClickRow = now, row
}

// --- pending-confirm ("press again") ---

// armPending shows "Press <key> again to <label>." and expires after
// pendingTTL, mirroring grok's PendingHint.
func (m *model) armPending(key, label string) tea.Cmd {
	m.pendingKey, m.pendingLabel = key, label
	m.pendingUntil = time.Now().Add(pendingTTL)
	m.pendingStatus = fmt.Sprintf("Press %s again to %s.", key, label)
	m.statusMsg = m.pendingStatus
	return tea.Tick(pendingTTL, func(time.Time) tea.Msg { return pendingTimeoutMsg{key: key} })
}

func (m *model) pendingArmed(key string) bool {
	return m.pendingKey == key && time.Now().Before(m.pendingUntil)
}

func (m *model) disarmPending() {
	m.pendingKey, m.pendingLabel, m.pendingStatus = "", "", ""
}

// requestQuit routes idle Ctrl+C / welcome-Quit clicks through confirm.
func (m *model) requestQuit() (tea.Model, tea.Cmd) {
	if m.pendingArmed("ctrl+c") {
		return m, tea.Quit
	}
	return m, m.armPending("ctrl+c", "quit")
}

// --- toast queue ---

// pushToast shows a transient message now, or queues it behind the current
// one. Direct statusMsg sets keep their exact clear semantics; toasts are
// for fire-and-forget confirmations (copied, opened, toggled).
func (m *model) pushToast(text string) tea.Cmd {
	if m.statusMsg == "" {
		m.statusMsg = text
		return clearStatusCmd(text)
	}
	m.toastQueue = append(m.toastQueue, text)
	return nil
}

// nextToast pops the queued toast after the current one clears.
func (m *model) nextToast() tea.Cmd {
	if len(m.toastQueue) == 0 {
		return nil
	}
	next := m.toastQueue[0]
	m.toastQueue = m.toastQueue[1:]
	m.statusMsg = next
	return clearStatusCmd(next)
}
