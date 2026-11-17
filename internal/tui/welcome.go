package tui

// Welcome screen v2 (Go-native).
//
// Layout (top to bottom): a centered logo and menu, a responsive tip, and the
// working directory at the bottom edge.
//
// Two surfaces share this file:
//   - welcomeDefault: stacked text layout (title + menu + version badge).
//     No braille art, so it works on every terminal.
//   - welcomeMyagent: the same centered hierarchy with a tiered myagent mark.
// The animated orb/banner/wave/rain/fill styles keep their old renderer.

import (
	"context"
	_ "embed"
	"os/exec"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

//go:embed assets/logo07.txt
var myagentLogoFull string

//go:embed assets/logo05.txt
var myagentLogoSmall string

// Height at or above which the small logo is shown (below it, no logo).
const myagentSmallMinHeight = 22

// Height at or above which the full logo is shown.
const myagentFullMinHeight = 26

// myagentLogoTier picks the diamond-mark art by terminal height, stepping down
// while the stacked column would overflow.
type myagentLogoTier int

const (
	logoHidden myagentLogoTier = iota
	logoCompact
	logoFull
)

func myagentTierForHeight(h int) myagentLogoTier {
	if h < myagentSmallMinHeight {
		return logoHidden
	}
	if h < myagentFullMinHeight {
		return logoCompact
	}
	return logoFull
}

func (t myagentLogoTier) art() string {
	switch t {
	case logoFull:
		return myagentLogoFull
	case logoCompact:
		return myagentLogoSmall
	default:
		return ""
	}
}

func (t myagentLogoTier) stepDown() (myagentLogoTier, bool) {
	switch t {
	case logoFull:
		return logoCompact, true
	case logoCompact:
		return logoHidden, true
	default:
		return logoHidden, false
	}
}

// myagentArtLines splits embedded logo art into lines, normalizing the CRLF
// line endings the asset files ship with so stray \r never reaches the
// terminal (it would rewind the cursor mid-row and garble bordered layouts).
func myagentArtLines(art string) []string {
	if art == "" {
		return nil
	}
	return strings.Split(strings.Trim(strings.ReplaceAll(art, "\r\n", "\n"), "\n"), "\n")
}

func logoArtWidth(art string) int {
	w := 0
	for _, l := range myagentArtLines(art) {
		if n := lipgloss.Width(l); n > w {
			w = n
		}
	}
	return w
}

func logoArtRows(art string) int { return len(myagentArtLines(art)) }

// shimmerLogoLines colors the braille art with a bright band sweeping
// horizontally (same cadence as the banner: welcomeFrame/96). Only the color
// animates; the silhouette never changes, so layout is stable. Plain ASCII
// spaces stay unstyled; every art cell keeps its column.
func (m *model) shimmerLogoLines(art string) []string {
	lines := myagentArtLines(art)
	span := float64(logoArtWidth(art)) + 8
	head := span*float64(m.welcomeFrame)/welcomeFrameCount - 4
	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		var sb strings.Builder
		x := 0
		for _, r := range line {
			if r == ' ' {
				sb.WriteRune(' ')
				x++
				continue
			}
			d := float64(x) - head
			if d < 0 {
				d = -d
			}
			switch {
			case d < 2:
				sb.WriteString(m.th.cmdPickerSel.Render(string(r)))
			case d < 5:
				sb.WriteString(m.th.textPrimary.Render(string(r)))
			default:
				sb.WriteString(m.th.muted.Render(string(r)))
			}
			x++
		}
		rows = append(rows, sb.String())
	}
	return rows
}

// renderMyagentLogo draws the myagent mark centered with a neutral shimmer.
// Every row is padded to the full art width before centering so the block
// stays aligned: centering each ragged line individually would shift narrow
// rows right and leave the widest row's edge sticking out (the stray pixel).
func (m *model) renderMyagentLogo(tier myagentLogoTier) string {
	art := tier.art()
	if art == "" || m.width < 24 {
		return ""
	}
	rows := m.shimmerLogoLines(art)
	logoW := logoArtWidth(art)
	for i, r := range rows {
		if w := lipgloss.Width(r); w < logoW {
			r += strings.Repeat(" ", logoW-w)
		}
		rows[i] = centerLine(r, m.width)
	}
	return strings.Join(rows, "\n")
}

// --- git branch (cached, best-effort) ---

var welcomeGitCache struct {
	sync.Mutex
	cwd      string
	branch   string
	at       time.Time
	inflight bool
}

// welcomeGitBranch returns the cached short branch for cwd, or "" outside a
// repo / before the first lookup lands. The render path never blocks on
// fork+exec: misses and stale entries refresh on a background goroutine
// (single-flighted) while the frame keeps the stale value or "".
func welcomeGitBranch(cwd string) string {
	welcomeGitCache.Lock()
	if welcomeGitCache.cwd == cwd {
		branch, fresh := welcomeGitCache.branch, time.Since(welcomeGitCache.at) < 5*time.Second
		if fresh || welcomeGitCache.inflight {
			welcomeGitCache.Unlock()
			return branch
		}
		welcomeGitCache.inflight = true
		welcomeGitCache.Unlock()
		go refreshGitBranch(cwd)
		return branch
	}
	if welcomeGitCache.inflight {
		welcomeGitCache.Unlock()
		return ""
	}
	welcomeGitCache.inflight = true
	welcomeGitCache.Unlock()
	go refreshGitBranch(cwd)
	return ""
}

// refreshGitBranch resolves the branch off-frame and publishes it. The
// timeout backstops hung git invocations; failures publish "" (not a repo).
func refreshGitBranch(cwd string) {
	branch := ""
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "git", "-C", cwd, "symbolic-ref", "--short", "HEAD").Output(); err == nil {
		branch = strings.TrimSpace(string(out))
	}
	welcomeGitCache.Lock()
	welcomeGitCache.cwd, welcomeGitCache.branch, welcomeGitCache.at, welcomeGitCache.inflight = cwd, branch, time.Now(), false
	welcomeGitCache.Unlock()
}

// --- location line (bottom-right, above the prompt box) ---

// locationText builds the `{branch} {cwd}` line: accent branch plus the
// home-collapsed working directory.
func (m *model) locationText() string {
	var sb strings.Builder
	if branch := welcomeGitBranch(m.cwd); branch != "" {
		sb.WriteString(m.th.accentUser.Render("⎇ " + branch))
		sb.WriteString(" ")
	}
	sb.WriteString(m.th.muted.Render(collapseHome(m.cwd)))
	return sb.String()
}

// renderWelcomeLocation right-aligns the location line on the bottom row of
// the welcome area, directly above the prompt box. Overlong paths truncate
// from the left (ANSI-aware) so the significant tail stays visible.
func (m *model) renderWelcomeLocation() string {
	line := m.locationText()
	if lipgloss.Width(line) > m.width && m.width > 1 {
		line = ansi.TruncateLeft(line, m.width, "…")
	}
	if w := lipgloss.Width(line); w < m.width {
		line = strings.Repeat(" ", m.width-w) + line
	}
	return line
}

// --- menu ---

// --- hint ---

// welcomeHintPlain returns the unstyled hint text: the disabled-tools notice
// when a deny list is in effect, otherwise the prompt hint narrowed to fit the
// terminal. Shared by every welcome style so the rain, hero box, and stacked
// layouts report the same thing.
func (m *model) welcomeHintPlain() string {
	names := unionTools(m.disabledTools, nil)
	if len(names) == 0 {
		if m.width < 34 {
			return "Type a prompt to begin"
		}
		if m.width < 44 {
			return "Type a prompt · /help for commands"
		}
		return "Type a prompt to begin · /help for commands"
	}
	// Sorted by unionTools so the line reads the same on every restart,
	// whatever order the toggles were applied in.
	scope := ""
	if len(m.sessionDisabledTools) > 0 {
		scope = " (session)"
	} else if len(m.globalDisabledTools) > 0 {
		scope = " (global)"
	}
	return "Disabled tools: " + strings.Join(names, ", ") + scope
}

// welcomeHint returns the styled, centered hint line. Truncates from the right
// so a long deny list cannot wrap and push the layout down a row.
func (m *model) welcomeHint() string {
	plain := m.welcomeHintPlain()
	if m.width > 1 && lipgloss.Width(plain) > m.width {
		plain = ansi.Truncate(plain, m.width, "…")
	}
	return centerLine(m.th.muted.Render(plain), m.width)
}

// welcomeMenuItems lists the `label … key` rows: action left (bold),
// key right (gray). Keys shown are all real (typed text or real binds).
// welcomeMenuActions runs parallel: the click/hover target per row.
var welcomeMenuItems = [][2]string{
	{"Start typing", "↵"},
	{"Commands & keys", "/help"},
	{"Resume a session", "/resume"},
	{"Switch model", "/models"},
	{"Quit", "ctrl+c"},
}

// welcomeMenuActions maps each menu row to its click behavior: focus the
// composer, submit a slash command, or request quit (via pending-confirm).
var welcomeMenuActions = []string{"focus", "/help", "/resume", "/models", "quit"}

// renderWelcomeMenu centers the menu block; block width fits the widest row
// with a 4-col gap, minimum 30. The hovered row gets the
// hover background (repainted via refreshViewport on hover flip).
func (m *model) renderWelcomeMenu() string {
	contentMin := 0
	for _, item := range welcomeMenuItems {
		if n := len([]rune(item[0])) + len([]rune(item[1])) + 4; n > contentMin {
			contentMin = n
		}
	}
	menuWidth := max(30, contentMin)
	if menuWidth > m.width-4 && m.width-4 > 10 {
		menuWidth = m.width - 4
	}
	var rows []string
	for i, item := range welcomeMenuItems {
		gap := menuWidth - len([]rune(item[0])) - len([]rune(item[1]))
		if gap < 2 {
			gap = 2
		}
		row := item[0] + strings.Repeat(" ", gap) + item[1]
		if m.hoverKind == hoverWelcome && m.hoverIdx == i {
			rows = append(rows, centerLine(m.th.rowHover.Render(row), m.width))
			continue
		}
		rows = append(rows, centerLine(
			m.th.textPrimary.Render(item[0])+
				strings.Repeat(" ", gap)+
				m.th.muted.Render(item[1]), m.width))
	}
	return strings.Join(rows, "\n")
}

// welcomeLayout accumulates content lines while recording the menu's
// [start, end) row range for click/hover hit-testing.
type welcomeLayout struct {
	lines        []string
	menuStart    int
	menuEnd      int
	menuRecorded bool
}

func (l *welcomeLayout) add(s string) {
	l.lines = append(l.lines, strings.Split(s, "\n")...)
}

func (l *welcomeLayout) blanks(n int) {
	for range n {
		l.lines = append(l.lines, "")
	}
}

func (l *welcomeLayout) addMenu(m *model) {
	l.menuStart = len(l.lines)
	l.add(m.renderWelcomeMenu())
	l.menuEnd = len(l.lines)
	l.menuRecorded = true
}

func (l *welcomeLayout) record(m *model) {
	if l.menuRecorded {
		m.welcomeMenu[0], m.welcomeMenu[1] = l.menuStart, l.menuEnd
	} else {
		m.welcomeMenu[0], m.welcomeMenu[1] = -1, -1
	}
}

// welcomeViewportHeight is the content budget for the welcome layout. The kit
// shell publishes its own viewport height through kitViewportH so the welcome
// shares one layout implementation without borrowing the legacy viewport.
func (m *model) welcomeViewportHeight() int {
	if m.kitViewportH > 0 {
		return m.kitViewportH
	}
	if h := m.viewport.Height(); h > 0 {
		return h
	}
	return max(10, m.height-10)
}

// --- stacked default layout (text title, no braille) ---

func (m *model) renderDefaultWelcome() string {
	if m.width < 24 {
		return centerLine(m.th.cmdPickerSel.Render("myagent"), m.width)
	}
	vh := m.welcomeViewportHeight()
	title := centerLine(m.th.cmdPickerSel.Render("myagent"), m.width)
	subtitle := centerLine(m.th.muted.Render("Your terminal coding agent"), m.width)
	hint := m.welcomeHint()
	location := m.renderWelcomeLocation()

	// Measure the middle so centering weights toward the top (/3).
	menuRows := len(welcomeMenuItems)
	middleRows := 1 + 1 + 1 + menuRows + 1 + 1 // title, subtitle, gap, menu, gap, hint
	topPad := (vh - 2 - middleRows) / 3
	if topPad < 0 {
		topPad = 0
	}
	var l welcomeLayout
	l.blanks(1) // top margin row (the location line moved to the bottom)
	l.blanks(topPad)
	l.add(title)
	l.add(subtitle)
	l.blanks(1)
	l.addMenu(m)
	l.blanks(1)
	l.add(hint)
	// Fill so the location line sits at the bottom of the viewport.
	for len(l.lines) < vh-1 {
		l.lines = append(l.lines, "")
	}
	l.add(location)
	l.record(m)
	return strings.Join(l.lines, "\n")
}

// --- myagent layout (braille tiers + hero box when wide) ---

func (m *model) renderMyagentWelcome() string {
	if m.width < 24 {
		return centerLine(m.th.cmdPickerSel.Render("myagent"), m.width)
	}
	vh := m.welcomeViewportHeight()
	compact := vh < 14
	return m.renderMyagentStacked(vh, compact)
}

// renderMyagentStacked is the narrow fallback: tiered logo, title, menu, hint,
// and the location line at the bottom. The logo steps down while the column
// overflows.
func (m *model) renderMyagentStacked(vh int, compact bool) string {
	title := centerLine(m.th.cmdPickerSel.Render("myagent"), m.width)
	hint := m.welcomeHint()
	location := m.renderWelcomeLocation()

	tier := myagentTierForHeight(vh)
	if m.width < 44 && tier == logoFull {
		tier = logoCompact
	}
	if compact {
		tier = logoHidden
	}
	menuRows := len(welcomeMenuItems)
	// fixed rows besides the logo: margin + title + gaps + menu + gaps + hint + location
	fixed := 1 + 1 + 1 + menuRows + 1 + 1 + 1
	for tier != logoHidden {
		if fixed+logoArtRows(tier.art())+1 <= vh {
			break
		}
		next, ok := tier.stepDown()
		if !ok {
			break
		}
		tier = next
	}

	logo := m.renderMyagentLogo(tier)
	logoRows := 0
	if logo != "" {
		logoRows = strings.Count(logo, "\n") + 2 // art + trailing gap
	}
	topPad := (vh - 1 - (logoRows + 1 + 1 + menuRows + 1 + 1) - 1) / 3
	if topPad < 0 {
		topPad = 0
	}
	var l welcomeLayout
	l.blanks(1) // top margin row (the location line moved to the bottom)
	l.blanks(topPad)
	if logo != "" {
		l.add(logo)
		l.blanks(1)
	}
	l.add(title)
	l.blanks(1)
	l.addMenu(m)
	l.blanks(1)
	l.add(hint)
	for len(l.lines) < vh-1 {
		l.lines = append(l.lines, "")
	}
	l.add(location)
	l.record(m)
	return strings.Join(l.lines, "\n")
}

// renderHomeWordmark shares the prompt's centered column without session chrome.
func (m *model) renderHomeWordmark(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := []string{}
	if width >= 41 && height >= 4 {
		for _, line := range m.pixelWordmark() {
			rows = append(rows, centerLine(line, width))
		}
	} else if width >= 31 && height >= 2 {
		for _, line := range bannerRows {
			rows = append(rows, centerLine(m.th.textPrimary.Render(line), width))
		}
	} else {
		rows = append(rows, centerLine(m.th.textPrimary.Bold(true).Render("myagent"), width))
	}

	if len(m.disabledTools) > 0 && len(rows) < height {
		rows = append(rows, "", centerLine(ansi.Truncate(m.welcomeHintPlain(), width, "…"), width))
	}
	top := max(0, (height-len(rows))/2)
	for i := 0; i < top; i++ {
		rows = append([]string{""}, rows...)
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	return strings.Join(rows[:min(height, len(rows))], "\n")
}

// Pack square pixels into terminal half-blocks instead of stretching thin
// one-cell strokes across tall character rows.
func (m *model) pixelWordmark() []string {
	glyphs := [7][7]string{
		{"10001", "11011", "11111", "10101", "10001", "10001", "10001"},
		{"11011", "11011", "11011", "01110", "00100", "00100", "00100"},
		{"01110", "11011", "11011", "11111", "11011", "11011", "11011"},
		{"01111", "11000", "11000", "11011", "11011", "11011", "01110"},
		{"11111", "11000", "11000", "11110", "11000", "11000", "11111"},
		{"11001", "11001", "11101", "11111", "11011", "11011", "11001"},
		{"11111", "11111", "00100", "00100", "00100", "00100", "00100"},
	}
	rows := make([]string, 0, 4)
	for y := 0; y < 7; y += 2 {
		var line strings.Builder
		for letter, glyph := range glyphs {
			if letter > 0 {
				line.WriteByte(' ')
			}
			var pixels strings.Builder
			for x := 0; x < 5; x++ {
				top := glyph[y][x] == '1'
				bottom := y+1 < 7 && glyph[y+1][x] == '1'
				switch {
				case top && bottom:
					pixels.WriteRune('█')
				case top:
					pixels.WriteRune('▀')
				case bottom:
					pixels.WriteRune('▄')
				default:
					pixels.WriteByte(' ')
				}
			}
			style := m.th.textPrimary
			if letter < 2 {
				style = m.th.muted
			}
			line.WriteString(style.Render(pixels.String()))
		}
		rows = append(rows, line.String())
	}
	return rows
}
