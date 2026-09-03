// Package engine implements the terminal rendering and input foundation for
// the myagent pager: a cell-buffer screen with diff-based flushes, a style
// model with truecolor-to-256 quantization, and a raw input decoder. It is
// self-contained by design — no external TUI framework.
package engine

import (
	"fmt"
	"strings"
)

// Color is a terminal color: either an RGB truecolor value, an indexed
// palette entry, or the terminal default.
type Color struct {
	R, G, B uint8
	Idx     int // 0-255 when IsIndexed
	IsIdx   bool
	Default bool
}

// Def returns the terminal-default color.
func Def() Color { return Color{Default: true} }

// Idx returns an indexed palette color.
func Idx(i int) Color { return Color{Idx: i, IsIdx: true} }

// RGB returns a truecolor color.
func RGB(r, g, b uint8) Color { return Color{R: r, G: g, B: b} }

// Seq renders the SGR parameters that select this color given a foreground
// base (38/39 vs 48/49 are handled by the caller via the parameter offsets).
func (c Color) seq(fg bool) string {
	base := 48
	fgBase := 39
	if fg {
		base = 38
		fgBase = 39
	}
	switch {
	case c.Default:
		return fmt.Sprintf("%d", fgBase)
	case c.IsIdx:
		return fmt.Sprintf("%d;5;%d", base, c.Idx)
	default:
		return fmt.Sprintf("%d;2;%d;%d;%d", base, c.R, c.G, c.B)
	}
}

// Quantize downgrades a truecolor value to the nearest 256-color entry.
func (c Color) Quantize() Color { return c.quantize() }

// quantize maps a truecolor value onto the nearest entry of the xterm-256
// palette. Indexed and default colors pass through. The 256-color cube
// (16-231) uses levels {0,95,135,175,215,255}; grays live in 232-255.
func (c Color) quantize() Color {
	if !c.Default && !c.IsIdx {
		c.IsIdx = true
		c.Idx = nearest256(c.R, c.G, c.B)
	}
	return c
}

func nearest256(r, g, b uint8) int {
	levels := [6]uint8{0, 95, 135, 175, 215, 255}
	best, bestDist := 16, 1<<30
	for ri := 0; ri < 6; ri++ {
		for gi := 0; gi < 6; gi++ {
			for bi := 0; bi < 6; bi++ {
				dr, dg, db := int(r)-int(levels[ri]), int(g)-int(levels[gi]), int(b)-int(levels[bi])
				d := dr*dr + dg*dg + db*db
				if d < bestDist {
					bestDist = d
					best = 16 + 36*ri + 6*gi + bi
				}
			}
		}
	}
	for i := 0; i < 24; i++ {
		v := uint8(8 + i*10)
		dr, dg, db := int(r)-int(v), int(g)-int(v), int(b)-int(v)
		d := dr*dr + dg*dg + db*db
		if d < bestDist {
			bestDist = d
			best = 232 + i
		}
	}
	return best
}

// Attr is a bitmask of text attributes.
type Attr uint8

const (
	AttrBold Attr = 1 << iota
	AttrDim
	AttrItalic
	AttrUnderline
	AttrReverse
	AttrStrikethrough
)

// Style is a full cell style: colors plus attributes.
type Style struct {
	Fg, Bg Color
	Attr   Attr
}

// WithFg returns a copy with the foreground set.
func (s Style) WithFg(c Color) Style { s.Fg = c; return s }

// WithBg returns a copy with the background set.
func (s Style) WithBg(c Color) Style { s.Bg = c; return s }

// WithAttr returns a copy with the attributes OR-ed in.
func (s Style) WithAttr(a Attr) Style { s.Attr |= a; return s }

// WithoutAttr returns a copy with the attributes cleared.
func (s Style) WithoutAttr(a Attr) Style { s.Attr &^= a; return s }

// Bold returns a copy with bold set.
func (s Style) Bold() Style { return s.WithAttr(AttrBold) }

// Dim returns a copy with dim set.
func (s Style) Dim() Style { return s.WithAttr(AttrDim) }

// Italic returns a copy with italic set.
func (s Style) Italic() Style { return s.WithAttr(AttrItalic) }

// Underline returns a copy with underline set.
func (s Style) Underline() Style { return s.WithAttr(AttrUnderline) }

// IsZero reports whether the style selects nothing.
func (s Style) IsZero() bool {
	return s.Fg.Default && s.Bg.Default && s.Attr == 0
}

// diff renders only the SGR parameters needed to move from prev to s, or a
// full reset sequence when that is cheaper.
func (s Style) diff(prev Style, full string) string {
	if s == prev {
		return ""
	}
	if s.IsZero() {
		return "\x1b[0m"
	}
	var p strings.Builder
	p.WriteString("\x1b[")
	first := true
	emit := func(part string) {
		if !first {
			p.WriteByte(';')
		}
		first = false
		p.WriteString(part)
	}
	if s.Fg != prev.Fg {
		emit(s.Fg.seq(true))
	}
	if s.Bg != prev.Bg {
		emit(s.Bg.seq(false))
	}
	intensity := AttrBold | AttrDim
	if removed := prev.Attr & intensity &^ s.Attr; removed != 0 {
		// SGR 22 clears both intensity flags, so re-enable the one that remains.
		emit("22")
		if s.Attr&AttrBold != 0 {
			emit("1")
		}
		if s.Attr&AttrDim != 0 {
			emit("2")
		}
	} else {
		if s.Attr&AttrBold != 0 && prev.Attr&AttrBold == 0 {
			emit("1")
		}
		if s.Attr&AttrDim != 0 && prev.Attr&AttrDim == 0 {
			emit("2")
		}
	}
	for _, mod := range []struct {
		a Attr
		c string
		r string
	}{
		{AttrItalic, "3", "23"},
		{AttrUnderline, "4", "24"},
		{AttrReverse, "7", "27"},
		{AttrStrikethrough, "9", "29"},
	} {
		want := s.Attr&mod.a != 0
		had := prev.Attr&mod.a != 0
		if want != had {
			if want {
				emit(mod.c)
			} else {
				emit(mod.r)
			}
		}
	}
	p.WriteString("m")
	_ = full
	return p.String()
}

// Blend linearly interpolates two truecolor colors by t in [0,1] (0 returns
// from, 1 returns to). Default-colored inputs return to/from respectively.
func Blend(from, to Color, t float64) Color {
	if from.Default {
		return to
	}
	if to.Default {
		return from
	}
	if t <= 0 {
		return from
	}
	if t >= 1 {
		return to
	}
	if from.IsIdx {
		from = from.quantize().deindex()
	}
	if to.IsIdx {
		to = to.quantize().deindex()
	}
	mix := func(a, b uint8) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t) }
	return RGB(mix(from.R, to.R), mix(from.G, to.G), mix(from.B, to.B))
}

func (c Color) deindex() Color {
	if !c.IsIdx {
		return c
	}
	i := c.Idx
	switch {
	case i < 16:
		base := [16][3]uint8{
			{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
			{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
			{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
			{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
		}[i]
		return RGB(base[0], base[1], base[2])
	case i < 232:
		i -= 16
		levels := [6]uint8{0, 95, 135, 175, 215, 255}
		return RGB(levels[i/36], levels[(i/6)%6], levels[i%6])
	default:
		v := uint8(8 + (i-232)*10)
		return RGB(v, v, v)
	}
}
