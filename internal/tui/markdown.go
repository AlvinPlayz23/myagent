package tui

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/x/ansi"
)

// Markdown rendering for assistant replies is glamour plus a small cleanup
// pass. glamour is a solid parser but its terminal output needs pruning for a
// chat transcript: it pads every line to the full width with ANSI-styled
// spaces, prints literal # markers on headings, and — because it word-wraps
// the flattened list block instead of each item — lets continuation lines
// snap back to the left margin. finalizeMarkdown removes that noise and hangs
// wrapped list text under its marker.

// mdRenderer caches a glamour renderer per word-wrap width, list indent and
// light/dark mode. glamour is not reactive, so a change to any of those
// rebuilds it.
type mdRenderer struct {
	mu     sync.Mutex
	width  int
	marker int
	dark   bool
	r      *glamour.TermRenderer
}

func newMDRenderer() *mdRenderer { return &mdRenderer{} }

// reset drops the cached renderer so the next render picks up a changed
// terminal background.
func (m *mdRenderer) reset() {
	m.mu.Lock()
	m.r = nil
	m.mu.Unlock()
}

// render renders markdown to ANSI wrapped at width. A width <= 0 falls back to
// returning the raw markdown so we never panic on an unsized terminal.
func (m *mdRenderer) render(md string, width int) string {
	if width <= 0 {
		return md
	}
	// List continuation lines are shifted right by up to the widest source
	// marker, so render that much narrower to keep every line inside width.
	marker := maxMarkerWidth(md)
	if limit := max(1, width/4); marker > limit {
		marker = limit
	}
	dark := darkBackground.Load()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.r == nil || m.width != width || m.marker != marker || m.dark != dark {
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(markdownStyle()),
			glamour.WithWordWrap(max(1, width-marker)),
		)
		if err != nil {
			return md
		}
		m.r = r
		m.width = width
		m.marker = marker
		m.dark = dark
	}
	out, err := m.r.Render(md)
	if err != nil {
		return md
	}
	return finalizeMarkdown(out)
}

// markdownStyle builds the glamour style from the active palette so markdown
// follows the same light/dark colors as the rest of the transcript.
func markdownStyle() glamouransi.StyleConfig {
	p := newPalette()
	hex := func(c color.Color) *string { return strPtr(hexColor(c)) }
	chroma := &glamouransi.Chroma{
		Text:                primitive(hex(p.textPrimary.resolve())),
		Comment:             primitive(hex(p.textDim.resolve())),
		CommentPreproc:      primitive(hex(p.pending.resolve())),
		Keyword:             primitive(hex(p.primary.resolve())),
		KeywordReserved:     primitive(hex(p.primary.resolve())),
		KeywordNamespace:    primitive(hex(p.primary.resolve())),
		KeywordType:         primitive(hex(p.secondary.resolve())),
		Operator:            primitive(hex(p.textPrimary.resolve())),
		Punctuation:         primitive(hex(p.textDim.resolve())),
		Name:                primitive(hex(p.textPrimary.resolve())),
		NameBuiltin:         primitive(hex(p.success.resolve())),
		NameFunction:        primitive(hex(p.success.resolve())),
		NameClass:           primitive(hex(p.textBright.resolve())),
		NameTag:             primitive(hex(p.secondary.resolve())),
		NameAttribute:       primitive(hex(p.secondary.resolve())),
		LiteralString:       primitive(hex(p.pending.resolve())),
		LiteralStringEscape: primitive(hex(p.success.resolve())),
		LiteralNumber:       primitive(hex(p.pending.resolve())),
		GenericInserted:     primitive(hex(p.success.resolve())),
		GenericDeleted:      primitive(hex(p.failure.resolve())),
		Background:          primitive(hex(p.rule.resolve())),
	}
	chroma.Comment.Italic = boolPtr(true)
	chroma.NameClass.Bold = boolPtr(true)
	chroma.Background.BackgroundColor = hex(p.rule.resolve())

	return glamouransi.StyleConfig{
		Document: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       hex(p.textPrimary.resolve()),
			},
			Margin: uintPtr(2),
		},
		BlockQuote: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{Color: hex(p.textDim.resolve())},
			Indent:         uintPtr(1),
			IndentToken:    strPtr("▏ "),
		},
		Paragraph: glamouransi.StyleBlock{},
		List:      glamouransi.StyleList{LevelIndent: 2},
		Heading: glamouransi.StyleBlock{
			StylePrimitive: glamouransi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       hex(p.textBright.resolve()),
				Bold:        boolPtr(true),
			},
		},
		H1:            glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Color: hex(p.primary.resolve()), Bold: boolPtr(true)}},
		H2:            glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Color: hex(p.primary.resolve()), Bold: boolPtr(true)}},
		H3:            glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Color: hex(p.textBright.resolve()), Bold: boolPtr(true)}},
		H4:            glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Color: hex(p.textDim.resolve()), Bold: boolPtr(true)}},
		H5:            glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Color: hex(p.textDim.resolve()), Bold: boolPtr(true)}},
		H6:            glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Color: hex(p.textDim.resolve()), Bold: boolPtr(false)}},
		Text:          glamouransi.StylePrimitive{},
		Strikethrough: glamouransi.StylePrimitive{CrossedOut: boolPtr(true)},
		Emph:          glamouransi.StylePrimitive{Italic: boolPtr(true)},
		Strong:        glamouransi.StylePrimitive{Bold: boolPtr(true)},
		HorizontalRule: glamouransi.StylePrimitive{
			Color:  hex(p.rule.resolve()),
			Format: "\n────────────────\n",
		},
		Item:        glamouransi.StylePrimitive{BlockPrefix: "• ", Color: hex(p.textDim.resolve())},
		Enumeration: glamouransi.StylePrimitive{BlockPrefix: ". ", Color: hex(p.textDim.resolve())},
		Task:        glamouransi.StyleTask{Ticked: "[✓] ", Unticked: "[ ] "},
		Link:        glamouransi.StylePrimitive{Color: hex(p.diffHunk.resolve())},
		LinkText:    glamouransi.StylePrimitive{Color: hex(p.diffHunk.resolve())},
		Image:       glamouransi.StylePrimitive{Color: hex(p.diffHunk.resolve())},
		ImageText:   glamouransi.StylePrimitive{Color: hex(p.textDim.resolve()), Format: "Image: {{.text}}"},
		// Inline code is color-only: glamour's default padding plus a
		// background block turns every identifier into a heavy chip that
		// breaks the reading rhythm of a paragraph.
		Code: glamouransi.StyleBlock{StylePrimitive: glamouransi.StylePrimitive{Color: hex(p.secondary.resolve())}},
		CodeBlock: glamouransi.StyleCodeBlock{
			StyleBlock: glamouransi.StyleBlock{
				StylePrimitive: glamouransi.StylePrimitive{Color: hex(p.textPrimary.resolve())},
				Margin:         uintPtr(0),
			},
			Chroma: chroma,
		},
		Table: glamouransi.StyleTable{
			CenterSeparator: strPtr("┼"),
			ColumnSeparator: strPtr("│"),
			RowSeparator:    strPtr("─"),
		},
		DefinitionList:        glamouransi.StyleBlock{},
		DefinitionTerm:        glamouransi.StylePrimitive{},
		DefinitionDescription: glamouransi.StylePrimitive{BlockPrefix: "\n▪ "},
		HTMLBlock:             glamouransi.StyleBlock{},
		HTMLSpan:              glamouransi.StyleBlock{},
	}
}

func primitive(c *string) glamouransi.StylePrimitive { return glamouransi.StylePrimitive{Color: c} }

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }
func uintPtr(u uint) *uint    { return &u }

// resolve returns the variant for the terminal background reported by the
// running program.
func (c adaptiveColor) resolve() color.Color {
	if darkBackground.Load() {
		return c.Dark
	}
	return c.Light
}

// hexColor renders any color as #RRGGBB, the form glamour/termenv accept for
// both text and chroma styles.
func hexColor(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02X%02X%02X", r>>8, g>>8, b>>8)
}

var (
	// mdListMarker matches a markdown list marker at the start of a source
	// line: bullets, plus ordered items ("1.", "1)").
	mdListMarker = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+`)
	// mdRenderedMarker matches the marker glamour prints: a bullet, an
	// ordered number, or a task checkbox.
	mdRenderedMarker = regexp.MustCompile(`^(\s*)(•|\d+\.|\[[^\]]*\])\s`)
	// mdSGRSuffix matches a trailing ANSI SGR sequence.
	mdSGRSuffix = regexp.MustCompile("\x1b\\[[0-9;]*m$")
)

// maxMarkerWidth returns the widest list marker prefix in the markdown source,
// i.e. how far wrapped list text has to hang. Fenced code is skipped so its
// contents never widen the margin.
func maxMarkerWidth(md string) int {
	widest := 0
	fence := ""
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			switch {
			case fence == "":
				fence = marker
			case fence == marker:
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		if m := mdListMarker.FindStringSubmatch(line); m != nil {
			if w := ansi.StringWidth(m[1]) + ansi.StringWidth(m[2]) + 1; w > widest {
				widest = w
			}
		}
	}
	return widest
}

// finalizeMarkdown cleans up glamour's terminal output: trailing styled
// padding is removed, runs of blank lines collapse to one, and wrapped list
// items are indented under their marker text.
func finalizeMarkdown(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = trimTrailingPadding(line)
	}

	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(ansi.Strip(line)) == "" {
			if len(out) == 0 || out[len(out)-1] == "" {
				continue
			}
			out = append(out, "")
			continue
		}
		out = append(out, line)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}

	return hangListContinuations(out)
}

// trimTrailingPadding removes the ANSI-styled space runs glamour's margin
// writer appends to pad every line to the full width. A trailing reset is kept
// so the style state never leaks into the next line's indentation.
func trimTrailingPadding(line string) string {
	trimmed := line
	reset := false
	for {
		if strings.HasSuffix(trimmed, " ") || strings.HasSuffix(trimmed, "\t") {
			trimmed = trimmed[:len(trimmed)-1]
			continue
		}
		loc := mdSGRSuffix.FindStringIndex(trimmed)
		if loc == nil || loc[1] != len(trimmed) {
			break
		}
		if seq := trimmed[loc[0]:loc[1]]; seq == "\x1b[0m" || seq == "\x1b[m" {
			reset = true
		}
		trimmed = trimmed[:loc[0]]
	}
	if reset && trimmed != "" {
		trimmed += "\x1b[0m"
	}
	return trimmed
}

// markerPos tracks one nesting level of a rendered list: where its marker
// starts and where its text starts in plain columns.
type markerPos struct{ indent, text int }

// hangListContinuations indents continuation lines so wrapped list items stay
// under their text instead of snapping back to the left margin. The stack
// follows nesting depth: a marker at a shallower indent closes deeper levels.
// Blank lines end the list, which matches how glamour separates blocks.
func hangListContinuations(lines []string) string {
	var stack []markerPos
	for i, line := range lines {
		plain := ansi.Strip(line)
		if strings.TrimSpace(plain) == "" {
			stack = nil
			continue
		}
		if m := mdRenderedMarker.FindStringSubmatch(plain); m != nil {
			indent := ansi.StringWidth(m[1])
			// A marker at or left of the open level closes it: siblings share
			// a level, deeper markers open a new one.
			for len(stack) > 0 && indent <= stack[len(stack)-1].indent {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, markerPos{indent: indent, text: indent + ansi.StringWidth(m[2]) + 1})
			continue
		}
		if len(stack) == 0 {
			continue
		}
		cur := stack[len(stack)-1]
		lead := ansi.StringWidth(plain) - ansi.StringWidth(strings.TrimLeft(plain, " "))
		if lead < cur.text {
			lines[i] = strings.Repeat(" ", cur.text-lead) + line
		}
	}
	return strings.Join(lines, "\n")
}
