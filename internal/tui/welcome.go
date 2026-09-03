package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
)

// welcomeStyle selects the startup animation, kept from the previous UI and
// rendered through the new engine.
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

// welcomeMenuItem is one row of the welcome menu: label left, key right.
type welcomeMenuItem struct {
	label string
	key   string
	cmd   commandKind
}

var welcomeMenuItems = []welcomeMenuItem{
	{label: "Type a message", key: "enter", cmd: commandNone},
	{label: "Resume a session", key: "r", cmd: commandResume},
	{label: "Choose a model", key: "m", cmd: commandModel},
	{label: "Manage providers", key: "p", cmd: commandProviders},
	{label: "Customize", key: "c", cmd: commandCustomize},
	{label: "Help", key: "h", cmd: commandHelp},
	{label: "Quit", key: "q", cmd: commandQuit},
}

// logoGlyphs assembles the block-letter logo per frame so the shimmer can
// color individual glyphs.
var logoGlyphs = map[rune][]string{
	'M': {" __  __ ", "|  \\/  |", "| |\\/| |", "| |  | |", "|_|  |_|"},
	'Y': {"__  __ ", "\\ \\/ / ", " \\  /  ", "  \\/   ", "        "},
	'A': {"   _   ", "  / \\  ", " / _ \\ ", "/ ___ \\", "/_/   \\_\\"},
	'G': {"  ____ ", " / ___|", "| |  _ ", "| |_| |", " \\____|"},
	'E': {" _____ ", "| ____|", "|  _|  ", "| |___ ", "|_____|"},
	'N': {" _   _ ", "| \\ | |", "|  \\| |", "| |\\  |", "|_| \\_|"},
	'T': {" _____ ", "|_   _|", "  | |  ", "  | |  ", "  |_|  "},
}

// buildLogo renders "myagent" block letters.
func buildLogo() []string {
	word := "MYAGENT"
	rows := make([]string, 5)
	for i := range rows {
		var b strings.Builder
		for li, r := range []rune(word) {
			if li > 0 {
				b.WriteByte(' ')
			}
			g := logoGlyphs[r]
			b.WriteString(g[i])
		}
		rows[i] = strings.TrimRight(b.String(), " ")
	}
	return rows
}

// shimmerPhase mirrors the pager's logo shine: a raised-cosine band sweeping
// from bottom-left to top-right with a gentle global pulse.
func shimmerOpacity(diag, secs float64) float64 {
	const (
		band      = 0.38
		cycle     = 4.0
		sweepFrac = 0.32
		shine     = 0.33
		pulse     = 0.06
		pulseSecs = 5.0
	)
	p := math.Mod(secs, cycle) / cycle
	q := math.Min(p/sweepFrac, 1.0)
	bandPos := -band + q*(1.0+2.0*band)
	puls := pulse * (0.5 - 0.5*math.Cos(2*math.Pi*secs/pulseSecs))
	d := math.Abs(diag - bandPos)
	shineAmt := 0.0
	if d < band {
		shineAmt = 0.5 * (1.0 + math.Cos(math.Pi*d/band))
	}
	v := puls + shine*shineAmt
	return math.Min(math.Max(v, 0), 1)
}

// logoSpans renders one logo row with the diagonal shimmer applied.
func logoSpans(row, rowCount int, line string, secs float64, th *theme) []engine.Span {
	cols := len(line)
	var spans []engine.Span
	run := ""
	var runColor engine.Color
	haveColor := false
	base := th.Comment
	hilite := th.FG
	for col, ch := range line {
		diag := (float64(col) + float64(rowCount-1-row)) / float64(cols+rowCount)
		color := engine.Blend(base, hilite, shimmerOpacity(diag, secs))
		if !haveColor || color != runColor {
			if run != "" {
				spans = append(spans, engine.Span{Text: run, Style: engine.Style{}.WithFg(runColor).WithBg(th.BGBase)})
			}
			run = ""
			runColor = color
			haveColor = true
		}
		run += string(ch)
	}
	if run != "" {
		spans = append(spans, engine.Span{Text: run, Style: engine.Style{}.WithFg(runColor).WithBg(th.BGBase)})
	}
	return spans
}

// welcomeRender paints the welcome screen: top bar, logo animation, menu,
// and the composer placeholder. selIdx is the highlighted menu row (-1 = none).
func (a *app) welcomeRender(scr *engine.Screen, area engine.Rect, selIdx int, now time.Time) {
	th := a.th
	secs := now.Sub(a.start).Seconds()

	// Top bar: git branch + cwd, like the pager.
	loc := a.topBarSpans()
	scr.Line(area.X+2, area.Y, loc)

	body := engine.Rect{X: area.X, Y: area.Y + 2, W: area.W, H: area.H - 2}
	style := a.welcomeSty
	logo := a.welcomeLogoRows(style, secs, th)

	// Center the logo vertically in the space above the menu.
	menuH := len(welcomeMenuItems) + 1 // items + header gap
	logoH := len(logo)
	gap := (body.H - logoH - menuH - 4) / 3
	if gap < 1 {
		gap = 1
	}
	y := body.Y + gap
	for _, spans := range logo {
		x := body.X + max(2, (body.W-engine.SpansWidth(spans))/2)
		scr.Line(x, y, spans)
		y++
	}

	// Menu: label left, key right, selected row background highlight.
	menuY := body.Y + body.H - menuH - 2
	y = menuY
	menuW := 34
	mx := body.X + max(2, (body.W-menuW)/2)
	for i, item := range welcomeMenuItems {
		if i == selIdx {
			hover := engine.Style{}.WithBg(th.BGHighlight)
			for c := 0; c < menuW; c++ {
				scr.SetCell(mx+c, y, engine.Cell{Ch: ' ', Width: 1, Style: hover})
			}
		}
		labelSt := engine.Style{}.WithFg(th.FGDark).WithBg(th.BGBase)
		keySt := engine.Style{}.WithFg(th.GrayBright).WithBg(th.BGBase)
		if i == selIdx {
			labelSt = engine.Style{}.WithFg(th.FG).WithBg(th.BGHighlight).Bold()
			keySt = engine.Style{}.WithFg(th.Magenta).WithBg(th.BGHighlight).Bold()
		}
		scr.SetString(mx, y, item.label, labelSt)
		scr.SetString(mx+menuW-runewidthString(item.key), y, item.key, keySt)
		y++
	}
}

// topBarSpans builds the "branch cwd" welcome line.
func (a *app) topBarSpans() []engine.Span {
	th := a.th
	gray := engine.Style{}.WithFg(th.Comment).WithBg(th.BGBase)
	dim := engine.Style{}.WithFg(th.FG).WithBg(th.BGBase).Dim()
	cwdSt := engine.Style{}.WithFg(th.GrayDim).WithBg(th.BGBase)
	var spans []engine.Span
	if a.gitBranch != "" {
		spans = append(spans, engine.Span{Text: branchIcon() + " " + a.gitBranch, Style: dim})
		spans = append(spans, engine.Span{Text: " ", Style: gray})
	}
	spans = append(spans, engine.Span{Text: collapseHome(a.cwd), Style: cwdSt})
	return spans
}

func branchIcon() string { return "" }

// welcomeLogoRows renders the styled logo/title rows for the active style.
func (a *app) welcomeLogoRows(style welcomeStyle, secs float64, th *theme) [][]engine.Span {
	switch style {
	case welcomeBanner:
		logo := buildLogo()
		out := make([][]engine.Span, 0, len(logo))
		for i, l := range logo {
			out = append(out, logoSpans(i, len(logo), l, secs, th))
		}
		return out
	case welcomeFill:
		logo := buildLogo()
		cycle := math.Mod(secs, 6) / 6
		frontier := int(cycle * float64(len(logo)))
		out := make([][]engine.Span, 0, len(logo))
		fill := engine.Style{}.WithFg(th.Magenta).WithBg(th.BGBase)
		for i, l := range logo {
			if i < frontier {
				out = append(out, []engine.Span{{Text: l, Style: fill}})
			} else {
				out = append(out, logoSpans(i, len(logo), l, secs, th))
			}
		}
		return out
	case welcomeOrb:
		return orbRows(secs, th)
	case welcomeWave:
		return waveRows(secs, th)
	case welcomeRain:
		return rainRows(secs, th)
	default:
		title := engine.Style{}.WithFg(th.FG).WithBg(th.BGBase).Bold()
		tag := engine.Style{}.WithFg(th.Comment).WithBg(th.BGBase)
		return [][]engine.Span{
			{{Text: "myagent", Style: title}},
			{{Text: "a terminal coding agent", Style: tag}},
		}
	}
}

// orbRows draws a rotating dotted orb with a bright leading dot.
func orbRows(secs float64, th *theme) [][]engine.Span {
	const rows, cols = 7, 15
	grid := make([][]rune, rows)
	for i := range grid {
		grid[i] = []rune(strings.Repeat("·", cols))
	}
	phase := secs * 1.2
	headR := rows/2 + int(math.Sin(phase)*2.5)
	headC := cols/2 + int(math.Cos(phase)*6)
	if headR >= 0 && headR < rows && headC >= 0 && headC < cols {
		grid[headR][headC] = '●'
	}
	st := engine.Style{}.WithFg(th.Blue).WithBg(th.BGBase)
	bright := engine.Style{}.WithFg(th.FG).WithBg(th.BGBase).Bold()
	out := make([][]engine.Span, 0, rows)
	for r := range grid {
		text := string(grid[r])
		s := st
		if strings.ContainsRune(text, '●') {
			s = bright
		}
		out = append(out, []engine.Span{{Text: text, Style: s}})
	}
	return out
}

// waveRows draws a sine ripple under the title.
func waveRows(secs float64, th *theme) [][]engine.Span {
	const width = 44
	row := make([]rune, width)
	for c := range row {
		h := math.Sin(float64(c)*0.35 - secs*2.2)
		switch {
		case h > 0.55:
			row[c] = '▔'
		case h > -0.2:
			row[c] = '─'
		default:
			row[c] = '▁'
		}
	}
	st := engine.Style{}.WithFg(th.Cyan).WithBg(th.BGBase)
	return [][]engine.Span{{{Text: string(row), Style: st}}}
}

// rainRows draws drifting dots behind the title.
func rainRows(secs float64, th *theme) [][]engine.Span {
	const rows, cols = 7, 40
	st := engine.Style{}.WithFg(th.GrayDim).WithBg(th.BGBase)
	bright := engine.Style{}.WithFg(th.Blue).WithBg(th.BGBase)
	out := make([][]engine.Span, 0, rows)
	for r := 0; r < rows; r++ {
		buf := []rune(strings.Repeat(" ", cols))
		for c := 0; c < cols; c++ {
			// Deterministic drift: each column has a phase; dots fall.
			seed := float64(c*31+r*17) * 0.13
			fall := math.Mod(secs*0.6+seed, float64(rows))
			if float64(r) >= fall-0.4 && float64(r) < fall {
				buf[c] = '·'
			}
		}
		s := st
		if r%3 == 0 {
			s = bright
		}
		out = append(out, []engine.Span{{Text: strings.TrimRight(string(buf), " "), Style: s}})
	}
	return out
}

// runewidthString measures display width of a plain string.
func runewidthString(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

var _ = fmt.Sprintf
