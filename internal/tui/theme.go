package tui

import (
	"os"
	"strings"
	"sync"

	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
)

// theme is the GrokNight port: a neutral gray base with TokyoNight accents,
// defined in truecolor and quantized to the terminal's capability at startup.
// All UI colors come from here — nothing else hardcodes colors.
type theme struct {
	BGBase        engine.Color // #141414 main background
	BGDark        engine.Color // #1c1c1c code blocks
	BGHighlight   engine.Color // #242424 selection / hover
	BGTerminal    engine.Color // #0a0a0a outer background
	FG            engine.Color // #e1e1e1 primary text
	FGDark        engine.Color // #c8c8c8 secondary text
	Comment       engine.Color // #6c6c6c muted
	GrayDim       engine.Color // #585858
	GrayBright    engine.Color // #787878
	Blue          engine.Color // #7aa2f7
	Blue1         engine.Color // #3A95AB inline code
	Cyan          engine.Color // #7dcfff
	Green         engine.Color // #9ece6a
	Magenta       engine.Color // #bb9af7
	Orange        engine.Color // #ff9e64
	Red           engine.Color // #f7768e
	Yellow        engine.Color // #e0af68
	Teal          engine.Color // #1abc9c
	Purple        engine.Color // #9d7cd8
	RedDark       engine.Color // diff delete bg
	GreenDark     engine.Color // diff insert bg
	PromptBorder  engine.Color // #323237
	PromptActive  engine.Color // #505058
	SelectionBord engine.Color // #3c3c41
	LinkFG        engine.Color // #7aa6da

	AccentUser      engine.Color // user prompt rail
	AccentAssistant engine.Color // assistant rail
	AccentThinking  engine.Color
	AccentTool      engine.Color
	AccentSystem    engine.Color
	AccentError     engine.Color
	AccentSuccess   engine.Color
	AccentRunning   engine.Color
}

// groknight returns the default GrokNight theme.
func groknight() *theme { return newTheme(true) }

func newTheme(truecolor bool) *theme {
	q := func(c engine.Color) engine.Color {
		if truecolor || c.Default || c.IsIdx {
			return c
		}
		return c.Quantize()
	}
	return &theme{
		BGBase:        q(engine.RGB(20, 20, 20)),
		BGDark:        q(engine.RGB(28, 28, 28)),
		BGHighlight:   q(engine.RGB(36, 36, 36)),
		BGTerminal:    q(engine.RGB(10, 10, 10)),
		FG:            q(engine.RGB(225, 225, 225)),
		FGDark:        q(engine.RGB(200, 200, 200)),
		Comment:       q(engine.RGB(108, 108, 108)),
		GrayDim:       q(engine.RGB(88, 88, 88)),
		GrayBright:    q(engine.RGB(120, 120, 120)),
		Blue:          q(engine.RGB(122, 162, 247)),
		Blue1:         q(engine.RGB(58, 149, 171)),
		Cyan:          q(engine.RGB(125, 207, 255)),
		Green:         q(engine.RGB(158, 206, 106)),
		Magenta:       q(engine.RGB(187, 154, 247)),
		Orange:        q(engine.RGB(255, 158, 100)),
		Red:           q(engine.RGB(247, 118, 142)),
		Yellow:        q(engine.RGB(224, 175, 104)),
		Teal:          q(engine.RGB(26, 188, 156)),
		Purple:        q(engine.RGB(157, 124, 216)),
		RedDark:       q(engine.RGB(66, 14, 20)),
		GreenDark:     q(engine.RGB(6, 56, 6)),
		PromptBorder:  q(engine.RGB(50, 50, 55)),
		PromptActive:  q(engine.RGB(80, 80, 88)),
		SelectionBord: q(engine.RGB(60, 60, 65)),
		LinkFG:        q(engine.RGB(122, 166, 218)),

		AccentUser:      q(engine.RGB(200, 200, 200)),
		AccentAssistant: q(engine.RGB(187, 154, 247)),
		AccentThinking:  q(engine.RGB(187, 154, 247)),
		AccentTool:      q(engine.RGB(120, 120, 120)),
		AccentSystem:    q(engine.RGB(122, 162, 247)),
		AccentError:     q(engine.RGB(247, 118, 142)),
		AccentSuccess:   q(engine.RGB(158, 206, 106)),
		AccentRunning:   q(engine.RGB(187, 154, 247)),
	}
}

// St convenience styles used across the UI.

func (t *theme) style(fg engine.Color) engine.Style {
	return engine.Style{}.WithFg(fg).WithBg(t.BGBase)
}

func (t *theme) text() engine.Style    { return t.style(t.FG) }
func (t *theme) textDim() engine.Style { return t.style(t.FGDark) }
func (t *theme) muted() engine.Style   { return t.style(t.Comment) }

func (t *theme) codeStyle() engine.Style {
	return engine.Style{}.WithFg(t.Blue1).WithBg(t.BGDark)
}

// detectTruecolor honors COLORTERM and TERM the way the pager does.
func detectTruecolor() bool {
	if c := os.Getenv("COLORTERM"); c != "" {
		return c == "truecolor" || c == "24bit" || c == "yes"
	}
	if os.Getenv("NO_COLOR") != "" {
		return true // styling is dropped later; quantization is irrelevant
	}
	term := os.Getenv("TERM")
	return strings.Contains(term, "truecolor") || strings.Contains(term, "direct")
}

// themeOnce caches the detected theme for the process.
var (
	themeOnce   sync.Once
	themeCached *theme
)

// currentTheme lazily builds the active theme.
func currentTheme() *theme {
	themeOnce.Do(func() { themeCached = newTheme(detectTruecolor()) })
	return themeCached
}
