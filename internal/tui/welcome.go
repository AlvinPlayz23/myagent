package tui

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"
)

type welcomeStyle string

const (
	welcomeDefault welcomeStyle = "default"
	welcomeOrb     welcomeStyle = "orb"
	welcomeBanner  welcomeStyle = "banner"
	welcomeWave    welcomeStyle = "wave"
	welcomeRain    welcomeStyle = "rain"
	welcomeFill    welcomeStyle = "fill"
)

type welcomeChoice struct {
	style       welcomeStyle
	label       string
	description string
}

var welcomeChoices = []welcomeChoice{
	{style: welcomeDefault, label: "Default", description: "myagent text"},
	{style: welcomeOrb, label: "Orb", description: "animated dotted orb"},
	{style: welcomeBanner, label: "Banner", description: "block letters with a shimmer sweep"},
	{style: welcomeWave, label: "Wave", description: "flowing ripple under the title"},
	{style: welcomeRain, label: "Rain", description: "drifting dots behind the title"},
	{style: welcomeFill, label: "Fill", description: "block letters filling with liquid"},
}

func normalizeWelcomeStyle(style string) welcomeStyle {
	for _, choice := range welcomeChoices {
		if choice.style == welcomeStyle(style) {
			return choice.style
		}
	}
	return welcomeDefault
}

// animatedWelcome reports whether the style needs per-tick frame advancement.
func (s welcomeStyle) animated() bool { return s != welcomeDefault }

func (m *model) showWelcome() bool {
	return !m.hasSessionTitle && len(m.transcript.blocks) == 0
}

// renderWelcome returns the transient empty-session content. It deliberately
// lives outside the transcript so it is never persisted as conversation
// history and disappears as soon as the first prompt gives the session a title.
func (m *model) renderWelcome() string {
	title := centerLine(m.th.cmdPickerSel.Render("myagent"), m.width)
	if m.welcomeStyle == welcomeBanner && m.width >= bannerMinWidth {
		title = m.renderBanner()
	}
	if m.width < 24 {
		if m.welcomeStyle == welcomeOrb {
			return m.renderOrb(true) + "\n\n" + title
		}
		return title
	}

	subtitle := centerLine(m.th.muted.Render("Your terminal coding agent"), m.width)
	hint := "Type a prompt to begin · /help for commands"
	if m.width < 44 {
		hint = "Type a prompt · /help for commands"
	}
	if m.width < 34 {
		hint = "Type a prompt to begin"
	}
	hint = centerLine(m.th.muted.Render(hint), m.width)
	compact := m.viewport.Height() < 14

	switch m.welcomeStyle {
	case welcomeOrb:
		return m.renderOrb(compact) + "\n\n" + title + "\n" + subtitle + "\n\n" + hint
	case welcomeBanner:

		return title + "\n\n" + hint
	case welcomeWave:
		return title + "\n" + subtitle + "\n\n" + m.renderWave() + "\n\n" + hint
	case welcomeRain:
		return m.renderRain(compact, title, subtitle) + "\n\n" + hint
	case welcomeFill:
		if m.width < wordmarkWidth+4 {
			break
		}

		return m.renderFill() + "\n\n" + hint
	}
	if compact || m.width < 40 {
		return title + "\n" + subtitle + "\n\n" + hint
	}
	return m.renderHero(title, subtitle) + "\n\n" + hint
}

// renderHero frames the wordmark and subtitle in Grok's centered hero box and
// adds the `label … key` menu rows beneath it.
func (m *model) renderHero(title, subtitle string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#323237")).
		Padding(1, 4).
		Render(title + "\n" + subtitle)

	menu := []struct{ label, key string }{
		{"Type a prompt", "enter"},
		{"Slash commands", "/help"},
		{"Switch model", "/model"},
	}
	rows := make([]string, 0, len(menu))
	for _, item := range menu {
		rows = append(rows, "  "+m.th.userText.Render(item.label)+"   "+m.th.muted.Render(item.key)+"  ")
	}
	menuWidth := 0
	for _, item := range menu {
		menuWidth = max(menuWidth, len([]rune(item.label))+len([]rune(item.key))+5)
	}
	_ = menuWidth

	var out []string
	out = append(out, lipgloss.PlaceHorizontal(m.width, lipgloss.Center, box, lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("#323237")))), "\n")
	for _, row := range rows {
		out = append(out, centerLine(row, m.width), "\n")
	}
	out = out[:len(out)-1]
	return strings.Join(out, "")
}

// welcomeFrameCount is the shared animation cycle length for every animated
// welcome style. 96 divides evenly by the orb's 32-frame period, so the orb
// keeps its original cadence while wave/rain/banner get a longer loop.
const welcomeFrameCount = 96

// bannerRows is a two-row half-block rendering of "myagent". Cells animate by
// color only; the silhouette never changes, so the layout cannot jitter.
var bannerRows = [2]string{
	"█▀▄▀█ █ █ ▄▀█ █▀▀ █▀▀ █▄ █ ▀█▀",
	"█ ▀ █ ▀▄█ █▀█ █▄█ ██▄ █ ▀█  █ ",
}

// bannerMinWidth is the terminal width below which the banner falls back to the
// plain centered title.
const bannerMinWidth = 34

// renderBanner draws the block-letter wordmark with a bright highlight band
// sweeping horizontally through it.
func (m *model) renderBanner() string {
	bannerWidth := len([]rune(bannerRows[0]))

	span := float64(bannerWidth) + 8
	head := span*float64(m.welcomeFrame)/welcomeFrameCount - 4

	rows := make([]string, 0, len(bannerRows))
	for _, row := range bannerRows {
		var sb strings.Builder
		for x, cell := range []rune(row) {
			if cell == ' ' {
				sb.WriteRune(' ')
				continue
			}
			switch distance := math.Abs(float64(x) - head); {
			case distance < 2:
				sb.WriteString(m.th.orbBright.Render(string(cell)))
			case distance < 5:
				sb.WriteString(m.th.orbMedium.Render(string(cell)))
			default:
				sb.WriteString(m.th.orbDim.Render(string(cell)))
			}
		}
		rows = append(rows, centerLine(sb.String(), m.width))
	}
	return strings.Join(rows, "\n")
}

// waveGlyphs ramps from an empty cell to a full one; the ripple picks a glyph
// per column from a scrolling sine so the crest appears to travel sideways.
var waveGlyphs = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▅', '▄', '▃', '▂'}

// renderWave draws a single scrolling sine ripple, brightest at its crests.
func (m *model) renderWave() string {
	width := min(max(m.width-8, 12), 48)
	phase := 2 * math.Pi * float64(m.welcomeFrame) / welcomeFrameCount

	var sb strings.Builder
	for x := range width {

		height := 0.6*math.Sin(float64(x)*0.35-phase*2) + 0.4*math.Sin(float64(x)*0.13-phase)
		glyph := waveGlyphs[int((height+1)/2*float64(len(waveGlyphs)-1)+0.5)]
		switch {
		case height > 0.55:
			sb.WriteString(m.th.orbBright.Render(string(glyph)))
		case height > -0.2:
			sb.WriteString(m.th.orbMedium.Render(string(glyph)))
		default:
			sb.WriteString(m.th.orbDim.Render(string(glyph)))
		}
	}
	return centerLine(sb.String(), m.width)
}

// renderRain lays sparse drifting dots behind the title and subtitle. Drop
// positions come from a hash of the column, so the field is deterministic and
// stable across renders at the same frame.
func (m *model) renderRain(compact bool, centeredRows ...string) string {
	height := 7
	if compact {
		height = 5
	}
	fieldWidth := min(max(m.width-4, 16), 64)
	left := max(0, (m.width-fieldWidth)/2)

	titleRow := height / 2
	occupied := map[int]string{}
	for i, row := range centeredRows {
		occupied[titleRow+i] = row
	}

	rows := make([]string, 0, height+len(centeredRows))
	for y := 0; y < height; y++ {
		if row, ok := occupied[y]; ok {
			rows = append(rows, row)
			continue
		}
		var sb strings.Builder
		sb.WriteString(strings.Repeat(" ", left))
		for x := range fieldWidth {
			cell := ' '
			var style lipgloss.Style

			period := 11 + rainHash(x)%9
			offset := rainHash(x*7+1) % period
			if (m.welcomeFrame+offset)%period == y%period {
				cell, style = '·', m.th.orbDim
			} else if (m.welcomeFrame+offset)%period == (y+1)%period {
				cell, style = '·', m.th.orbMedium
			}
			if cell == ' ' {
				sb.WriteRune(' ')
				continue
			}
			sb.WriteString(style.Render(string(cell)))
		}
		rows = append(rows, strings.TrimRight(sb.String(), " "))
	}
	return strings.Join(rows, "\n")
}

// rainHash is a small deterministic integer hash used to scatter rain columns.
func rainHash(n int) int {
	n = (n ^ 61) ^ (n >> 16)
	n *= 9
	n ^= n >> 4
	n *= 0x27d4eb2d
	n ^= n >> 15
	if n < 0 {
		n = -n
	}
	return n
}

// wordmarkFont is a 5-row bitmap of the letters in "myagent". Letters are
// deliberately hollow rather than solid: dense blocks die immediately under
// Conway's rules, which would blow the wordmark apart before it is readable.
var wordmarkFont = [][]string{
	{"#   #", "## ##", "# # #", "#   #", "#   #"},
	{"#   #", " # # ", "  #  ", "  #  ", "  #  "},
	{" ## ", "#  #", "####", "#  #", "#  #"},
	{" ###", "#   ", "# ##", "#  #", " ###"},
	{"####", "#   ", "### ", "#   ", "####"},
	{"#  #", "## #", "# ##", "#  #", "#  #"},
	{"###", " # ", " # ", " # ", " # "},
}

// wordmarkRows renders wordmarkFont into 5 equal-length rows, one column of
// blank space between letters. '#' marks a set pixel.
var wordmarkRows = func() []string {
	rows := make([]string, len(wordmarkFont[0]))
	for y := range rows {
		parts := make([]string, 0, len(wordmarkFont))
		for _, letter := range wordmarkFont {
			parts = append(parts, letter[y])
		}
		rows[y] = strings.Join(parts, " ")
	}
	return rows
}()

// wordmarkWidth is the rendered pixel width of wordmarkRows.
var wordmarkWidth = len(wordmarkRows[0])

// Terminal cells are roughly twice as tall as they are wide, so drawing one
// font pixel per column makes the letters look stretched and thin. Doubling the
// horizontal scale restores squarer, chunkier proportions; we fall back to
// single scale when the terminal is too narrow to fit the wide form.
const wordmarkScale = 2

// wordmarkScaleFor picks the largest horizontal scale that fits the width.
func wordmarkScaleFor(width int) int {
	if width >= wordmarkWidth*wordmarkScale+4 {
		return wordmarkScale
	}
	return 1
}

// Fill shades a letter pixel by how far it sits below the waterline: an unfilled
// pixel is a faint ghost, the waterline itself is a mid tone, and submerged
// pixels are solid. Only the shade changes, so the letterform always reads.
const (
	fillEmpty     = '░'
	fillWaterline = '▒'
	fillSubmerged = '█'
)

// renderFill draws the wordmark filling with a rising liquid whose surface
// ripples, then draining back down. Every letter pixel keeps its cell, so the
// silhouette — and therefore the layout — never changes.
func (m *model) renderFill() string {
	rowCount := len(wordmarkRows)

	progress := float64(m.welcomeFrame) / welcomeFrameCount
	level := 2 * progress
	if level > 1 {
		level = 2 - level
	}

	surface := float64(rowCount)*(1.25-1.5*level) - 0.5
	phase := 2 * math.Pi * float64(m.welcomeFrame) / 16
	scale := wordmarkScaleFor(m.width)

	rows := make([]string, 0, rowCount)
	for y, row := range wordmarkRows {
		var sb strings.Builder
		for x, cell := range row {
			if cell != '#' {
				sb.WriteString(strings.Repeat(" ", scale))
				continue
			}

			waterline := surface + 0.45*math.Sin(float64(x)*0.45+phase)
			var glyph string
			var style lipgloss.Style
			switch depth := float64(y) - waterline; {
			case depth > 0.5:
				glyph, style = string(fillSubmerged), m.th.orbBright
			case depth > -0.5:
				glyph, style = string(fillWaterline), m.th.orbMedium
			default:
				glyph, style = string(fillEmpty), m.th.orbDim
			}
			sb.WriteString(style.Render(strings.Repeat(glyph, scale)))
		}
		rows = append(rows, centerLine(sb.String(), m.width))
	}
	return strings.Join(rows, "\n")
}

// renderOrb draws a fixed dotted sphere while a bright, slightly curved band
// moves across it. Keeping the silhouette stable avoids layout jitter.
func (m *model) renderOrb(compact bool) string {
	halfWidths := []int{2, 4, 6, 7, 7, 6, 4, 2}
	if compact {
		halfWidths = []int{1, 3, 4, 3, 1}
	}
	phase := 2 * math.Pi * float64(m.welcomeFrame%32) / 32
	travel := float64(halfWidths[len(halfWidths)/2] - 1)
	center := travel * math.Sin(phase)

	rows := make([]string, 0, len(halfWidths))
	for y, halfWidth := range halfWidths {
		wave := center + 0.7*math.Sin(float64(y)*0.8+phase)
		var row strings.Builder
		for x := -halfWidth; x <= halfWidth; x++ {
			distance := math.Abs(float64(x) - wave)
			switch {
			case distance < 1.25:
				row.WriteString(m.th.orbBright.Render("●"))
			case distance < 3.25:
				row.WriteString(m.th.orbMedium.Render("•"))
			default:
				row.WriteString(m.th.orbDim.Render("·"))
			}
		}
		rows = append(rows, centerLine(row.String(), m.width))
	}
	return strings.Join(rows, "\n")
}
