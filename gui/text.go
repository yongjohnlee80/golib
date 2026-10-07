package gui

import (
	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"
)

// Font is how text is shaped: a typeface list (a CSS font-family list, as WithFont takes), a size
// in logical pixels, and its weight and style.
type Font struct {
	Family string // "" is the UI typeface: the system's sans-serif
	Size   float32
	Bold   bool
	Italic bool
}

// uiFamily is the typeface a Font with no Family uses.
const uiFamily = "sans-serif, emoji"

// TextShaper shapes text for a View, with the window's fonts and at its scale: proportional
// fonts, sizes, weights, wrapping, bidi and fallback fonts. Measure with the shaper a Canvas
// gives, never another, so what is measured is what is drawn.
type TextShaper struct {
	s     *text.Shaper
	scale float32 // device pixels per logical pixel
}

// TextLayout is shaped text, ready to draw. Its sizes are logical pixels.
type TextLayout struct {
	Width           float32 // the widest line
	Height          float32 // every line, top of the first to the bottom of the last
	Ascent, Descent float32 // the first line's, from its baseline
	lines           []shapedLine
	scale           float32
}

// shapedLine is one line's outlines and colour glyphs, drawn with its dot at (x, y): device
// pixels from the first line's dot. Gio shapes a run on one baseline, so each line is its own.
type shapedLine struct {
	path    clip.PathSpec
	bitmaps op.CallOp
	x, y    float32
}

// Layout shapes s in f. maxWidth > 0 wraps lines at that width; 0 keeps one line.
func (t *TextShaper) Layout(s string, f Font, maxWidth float32) *TextLayout {
	fam := f.Family
	if fam == "" {
		fam = uiFamily
	}
	ft := font.Font{Typeface: font.Typeface(fam)}
	if f.Bold {
		ft.Weight = font.Bold
	}
	if f.Italic {
		ft.Style = font.Italic
	}
	maxW := 1 << 20
	if maxWidth > 0 {
		maxW = max(int(maxWidth*t.scale), 1)
	}
	t.s.LayoutString(text.Parameters{
		Font:     ft,
		PxPerEm:  fixed.Int26_6(f.Size * t.scale * 64),
		MaxWidth: maxW,
	}, s)
	tl := &TextLayout{scale: t.scale}
	var line []text.Glyph
	var firstY int32
	var right fixed.Int26_6
	var lastDescent fixed.Int26_6
	flush := func() {
		if len(line) == 0 {
			return
		}
		tl.lines = append(tl.lines, shapedLine{
			path:    t.s.Shape(line),
			bitmaps: t.s.Bitmaps(line),
			x:       float32(line[0].X) / 64,
			y:       float32(line[0].Y - firstY),
		})
		line = line[:0:0]
	}
	for g, ok := t.s.NextGlyph(); ok; g, ok = t.s.NextGlyph() {
		if len(tl.lines) == 0 && len(line) == 0 {
			firstY = g.Y
			tl.Ascent = float32(g.Ascent) / 64 / t.scale
			tl.Descent = float32(g.Descent) / 64 / t.scale
		} else if len(line) > 0 && g.Y != line[0].Y {
			flush()
		}
		line = append(line, g)
		right = max(right, g.X+g.Advance)
		lastDescent = g.Descent
	}
	if len(line) > 0 {
		lastY := line[0].Y
		flush()
		tl.Width = float32(right) / 64 / t.scale
		tl.Height = tl.Ascent + (float32(lastY-firstY)+float32(lastDescent)/64)/t.scale
	}
	return tl
}
