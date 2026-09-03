package tui

import (
	"strings"

	"github.com/AlvinPlayz23/myagent/internal/tui/engine"
)

// md renders markdown into styled span rows with the GrokNight text roles:
// colored bold headings, dim-italic quote bars, code blocks on bg_dark, and
// task boxes. It replaces glamour, which cannot feed a cell buffer.

type mdParser struct {
	rows [][]engine.Span
}

var mdBoldMarkers = []string{"**", "__"}
var mdItalicMarkers = []string{"*", "_"}

// renderMarkdown parses markdown text and wraps it to width.
func renderMarkdown(src string, width int, th *theme) [][]engine.Span {
	p := &mdParser{}
	p.parse(src, th)
	rows := make([][]engine.Span, 0, len(p.rows))
	for _, r := range p.rows {
		rows = append(rows, engine.WrapSpans(r, max(width, 1))...)
	}
	return rows
}

func (p *mdParser) line(spans []engine.Span) { p.rows = append(p.rows, spans) }

func (p *mdParser) blank() { p.rows = append(p.rows, nil) }

func (p *mdParser) parse(src string, th *theme) {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	i := 0
	inCode := false
	var codeLang string
	var codeBuf []string

	flushCode := func() {
		if len(codeBuf) == 0 {
			codeBuf = nil
			inCode = false
			return
		}
		p.codeBlock(codeLang, codeBuf, th)
		codeBuf = nil
		inCode = false
	}

	for i < len(lines) {
		l := lines[i]
		if inCode {
			if strings.HasPrefix(strings.TrimSpace(l), "```") {
				flushCode()
				i++
				continue
			}
			codeBuf = append(codeBuf, l)
			i++
			continue
		}
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "```") {
			inCode = true
			codeLang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			i++
			continue
		}
		switch {
		case trimmed == "":
			p.blank()
			i++
		case isHeading(trimmed):
			level := headingLevel(trimmed)
			p.heading(level, strings.TrimSpace(trimmed[level:]), th)
			i++
		case trimmed == "---" || trimmed == "***" || trimmed == "___":
			p.rule(th)
			i++
		case strings.HasPrefix(trimmed, "> "):
			var quote []string
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(t, ">") {
					break
				}
				quote = append(quote, strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t, ">"), " ")))
				i++
			}
			p.quote(quote, th)
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || strings.HasPrefix(trimmed, "+ "):
			i = p.list(lines, i, th, 0)
		case strings.HasPrefix(trimmed, "|") && i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "|"):
			// Table: header, delimiter, body rows.
			var tbl []string
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				tbl = append(tbl, strings.TrimSpace(lines[i]))
				i++
			}
			p.table(tbl, th)
		default:
			// Paragraph: fold consecutive non-special lines.
			var para []string
			for i < len(lines) {
				t := strings.TrimSpace(lines[i])
				if t == "" || isHeading(t) || strings.HasPrefix(t, "```") || strings.HasPrefix(t, "> ") ||
					strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") ||
					t == "---" || strings.HasPrefix(t, "|") {
					break
				}
				para = append(para, t)
				i++
			}
			spans := p.inline(strings.Join(para, " "), th.text())
			p.line(spans)
		}
	}
}

func isHeading(s string) bool {
	for n := 6; n >= 1; n-- {
		if len(s) > n && strings.TrimRight(s[:n], "#") == "" && s[n] == ' ' {
			return true
		}
	}
	return false
}

func headingLevel(s string) int {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	return n
}

func (p *mdParser) heading(level int, text string, th *theme) {
	color := th.FG
	switch level {
	case 1:
		color = th.Teal
	case 2:
		color = th.Blue
	case 3:
		color = th.Purple
	case 4:
		color = th.GrayBright
	case 5:
		color = th.Comment
	default:
		color = th.GrayDim
	}
	st := engine.Style{}.WithFg(color).WithBg(th.BGBase)
	if level <= 5 {
		st = st.Bold()
	}
	p.line(p.inline(text, st))
}

func (p *mdParser) rule(th *theme) {
	p.line([]engine.Span{{Text: strings.Repeat("─", 24), Style: th.muted()}})
}

func (p *mdParser) quote(quoteLines []string, th *theme) {
	bar := engine.Style{}.WithFg(th.GrayBright).WithBg(th.BGBase).Italic()
	body := th.textDim().Italic()
	for _, ql := range quoteLines {
		spans := []engine.Span{{Text: "▎ ", Style: bar}}
		spans = append(spans, p.inline(ql, body)...)
		p.line(spans)
	}
}

func (p *mdParser) list(lines []string, i int, th *theme, depth int) int {
	itemBody := th.text()
	for i < len(lines) {
		l := strings.TrimSpace(lines[i])
		checked, isTask := parseTask(l)
		switch {
		case strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "* ") || strings.HasPrefix(l, "+ "):
			marker := "•  "
			if isTask {
				if checked {
					marker = "☑  "
				} else {
					marker = "☐  "
				}
			}
			body := strings.TrimSpace(l[2:])
			spans := []engine.Span{
				{Text: strings.Repeat("  ", depth) + marker, Style: th.style(th.GrayBright)},
			}
			spans = append(spans, p.inline(body, itemBody)...)
			p.line(spans)
			i++
		default:
			if depth < 2 && strings.HasPrefix(l, "  ") {
				i = p.list(lines, i, th, depth+1)
				continue
			}
			return i
		}
	}
	return i
}

func parseTask(s string) (checked, ok bool) {
	if strings.HasPrefix(s, "- [ ] ") || strings.HasPrefix(s, "* [ ] ") {
		return false, true
	}
	if strings.HasPrefix(s, "- [x] ") || strings.HasPrefix(s, "* [x] ") || strings.HasPrefix(s, "- [X] ") {
		return true, true
	}
	return false, false
}

func (p *mdParser) table(rows []string, th *theme) {
	cells := func(row string) []string {
		row = strings.TrimPrefix(row, "|")
		row = strings.TrimSuffix(row, "|")
		parts := strings.Split(row, "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	widths := map[int]int{}
	parsed := make([][]string, 0, len(rows))
	for r, row := range rows {
		if r == 1 {
			continue // delimiter
		}
		c := cells(row)
		parsed = append(parsed, c)
		for i, v := range c {
			if len(v) > widths[i] {
				widths[i] = len(v)
			}
		}
	}
	for r, c := range parsed {
		var spans []engine.Span
		for i, v := range c {
			st := th.text()
			if r == 0 {
				st = st.Bold()
			}
			pad := widths[i] - len(v)
			spans = append(spans, engine.Span{Text: v + strings.Repeat(" ", pad), Style: st})
			if i < len(c)-1 {
				spans = append(spans, engine.Span{Text: " │ ", Style: th.muted()})
			}
		}
		p.line(spans)
	}
}

func (p *mdParser) codeBlock(lang string, code []string, th *theme) {
	if len(code) == 0 {
		return
	}
	width := 0
	for _, c := range code {
		if len(c) > width {
			width = len(c)
		}
	}
	// Header row with the language tag, then body rows padded to the block.
	if lang != "" {
		p.line([]engine.Span{{Text: lang, Style: th.style(th.GrayDim)}})
	}
	st := engine.Style{}.WithFg(th.FGDark).WithBg(th.BGDark)
	for _, c := range code {
		text := c + strings.Repeat(" ", width-len(c))
		p.line([]engine.Span{{Text: " " + text + " ", Style: st}})
	}
	p.blank()
}

// inline converts one text run into styled spans, resolving **bold**,
// *italic*, ~~strike~~, `code`, [text](url) links, and bare @mentions.
func (p *mdParser) inline(s string, base engine.Style) []engine.Span {
	var out []engine.Span
	emit := func(text string, st engine.Style) {
		if text != "" {
			out = append(out, engine.Span{Text: text, Style: st})
		}
	}
	runes := []rune(s)
	i := 0
	var buf strings.Builder
	flush := func() { emit(buf.String(), base); buf.Reset() }

	for i < len(runes) {
		matched := false
		for _, m := range mdBoldMarkers {
			if strings.HasPrefix(string(runes[i:]), m) {
				if end := indexFrom(string(runes[i+2:]), m); end >= 0 {
					flush()
					inner := string(runes[i+2 : i+2+end])
					for _, sp := range p.inline(inner, base) {
						out = append(out, engine.Span{Text: sp.Text, Style: sp.Style.Bold()})
					}
					i += 2 + end + 2
					matched = true
					break
				}
			}
		}
		if matched {
			continue
		}
		if strings.HasPrefix(string(runes[i:]), "~~") {
			if end := indexFrom(string(runes[i+2:]), "~~"); end >= 0 {
				flush()
				out = append(out, engine.Span{Text: string(runes[i+2 : i+2+end]), Style: base})
				i += 2 + end + 2
				continue
			}
		}
		if runes[i] == '`' {
			if end := indexFrom(string(runes[i+1:]), "`"); end >= 0 {
				flush()
				codeSt := engine.Style{}.WithFg(base.Fg).WithBg(engine.Color{})
				out = append(out, engine.Span{Text: string(runes[i+1 : i+1+end]), Style: base.Bold()})
				_ = codeSt
				i += 1 + end + 1
				continue
			}
		}
		if runes[i] == '[' {
			if close := indexFrom(string(runes[i:]), "]("); close >= 0 {
				rest := string(runes[i+close+2:])
				if closeParen := indexFrom(rest, ")"); closeParen >= 0 {
					flush()
					text := string(runes[i+1 : i+close])
					url := rest[:closeParen]
					linkSt := engine.Style{}.WithFg(base.Fg).Underline()
					_ = url
					out = append(out, engine.Span{Text: text, Style: linkSt})
					i += close + 2 + closeParen + 1
					continue
				}
			}
		}
		if runes[i] == '*' || runes[i] == '_' {
			if end := indexFrom(string(runes[i+1:]), string(runes[i])); end > 0 {
				flush()
				inner := string(runes[i+1 : i+1+end])
				for _, sp := range p.inline(inner, base) {
					out = append(out, engine.Span{Text: sp.Text, Style: sp.Style.Italic()})
				}
				i += 1 + end + 1
				continue
			}
		}
		buf.WriteRune(runes[i])
		i++
	}
	flush()
	return out
}

func indexFrom(s, sub string) int {
	return strings.Index(s, sub)
}
