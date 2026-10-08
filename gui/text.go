package gui

import (
	"cmp"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"

	"github.com/yongjohnlee80/golib/tui"
)

// Font is how text is shaped: a typeface list (a CSS font-family list, as WithFont takes), a size
// in logical pixels, and its weight and style.
type Font struct {
	Family string // "" is the UI typeface: the system's sans-serif
	Size   float32
	Bold   bool
	Italic bool
}

// uiFamily is the typeface a Font with no Family uses, until the window names another (Fonts).
const uiFamily = "sans-serif, emoji"

// CellFamily is a Font's Family meaning the window's cell font: the monospace the terminal grid is
// drawn in, as it is set now. Code in a Rendered view and HTMLView's pre use it, so a change of the
// cell font reaches them.
const CellFamily = "\x00cell"

// Fonts are a window's families now: Prose for a Font with no Family, Mono for CellFamily. Gen
// changes with every change of either, or of the size: a view keeping shaped text keys it on Gen,
// so a family-only change shapes it again.
type Fonts struct {
	Prose, Mono string
	Gen         uint64
}

// defaultFonts are a window's families before it names any: the UI family, and the cells' default
// monospace.
func defaultFonts() Fonts { return Fonts{Prose: uiFamily, Mono: MonospaceFamily()} }

// TextShaper shapes text for a View, with the window's fonts and at its scale: proportional
// fonts, sizes, weights, wrapping, bidi and fallback fonts. Measure with the shaper a Canvas
// gives, never another, so what is measured is what is drawn.
type TextShaper struct {
	s     *text.Shaper
	scale float32 // device pixels per logical pixel
	fonts Fonts   // what Family "" and CellFamily mean
}

// Fonts are the families this shaper resolves an empty Family and CellFamily to.
func (t *TextShaper) Fonts() Fonts { return t.fonts }

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

// gioFont is f as Gio selects it.
func gioFont(f Font, fonts Fonts) font.Font {
	fam := f.Family
	switch fam {
	case "":
		fam = cmp.Or(fonts.Prose, uiFamily)
	case CellFamily:
		fam = cmp.Or(fonts.Mono, "monospace") // the generic family: MonospaceFamily would cycle through NativeStyle
	}
	ft := font.Font{Typeface: font.Typeface(fam)}
	if f.Bold {
		ft.Weight = font.Bold
	}
	if f.Italic {
		ft.Style = font.Italic
	}
	return ft
}

// Measured is a line of text as measured: where each grapheme cluster starts, as tui segments
// them (tui.Graphemes), so a widget's cluster indices and these positions agree.
type Measured struct {
	Clusters        []int     // byte offset in the text of each cluster
	X               []float32 // each cluster's left edge, then the line's width: len(Clusters)+1
	Ascent, Descent float32   // the font's, from the baseline
}

// Scale is the shaper's device pixels per logical pixel: what it measures at.
func (t *TextShaper) Scale() float32 { return t.scale }

// Measure shapes s in f on one line, as Layout(s, f, 0) shapes it, and reports each grapheme
// cluster's position. A cluster inside a ligature (one glyph for several clusters, as "->" in a
// coding font) gets a share of its width by runes. s holds no line break.
//
// Glyphs are placed left to right as Gio orders them; inside a right-to-left run a cluster's x
// is its glyph's left edge, not a caret position.
func (t *TextShaper) Measure(s string, f Font) Measured {
	m := Measured{}
	ascent, descent, runeX := t.runeEdges(s, f)
	if s == "" {
		ascent, descent, _ = t.runeEdges(" ", f)
	}
	m.Ascent, m.Descent = ascent, descent
	ri, off := 0, 0
	for c := range tui.Graphemes(s) {
		m.Clusters = append(m.Clusters, off)
		m.X = append(m.X, runeX[ri])
		ri += utf8.RuneCountInString(c)
		off += len(c)
	}
	m.X = append(m.X, runeX[len(runeX)-1])
	return m
}

// runeEdges shapes s on one line and returns the font's ascent and descent and the x where
// each rune starts, then the line's width: len(runes)+1 positions, in logical pixels.
func (t *TextShaper) runeEdges(s string, f Font) (ascent, descent float32, x []float32) {
	t.s.LayoutString(text.Parameters{
		Font:     gioFont(f, t.fonts),
		PxPerEm:  fixed.Int26_6(f.Size * t.scale * 64),
		MaxWidth: 1 << 20,
	}, s)
	n := utf8.RuneCountInString(s)
	x = make([]float32, n+1)
	px := func(v fixed.Int26_6) float32 { return float32(v) / 64 / t.scale }
	ri, first := 0, true
	var start, end fixed.Int26_6
	open := false
	for g, ok := t.s.NextGlyph(); ok; g, ok = t.s.NextGlyph() {
		if first {
			ascent, descent, first = px(g.Ascent), px(g.Descent), false
		}
		if !open {
			start, end, open = g.X, g.X, true
		}
		start = min(start, g.X)
		end = max(end, g.X+g.Advance)
		if g.Flags&text.FlagClusterBreak == 0 {
			continue
		}
		open = false
		runes := int(g.Runes)
		for k := 0; k < runes && ri+k < n; k++ {
			x[ri+k] = px(start) + (px(end)-px(start))*float32(k)/float32(runes)
		}
		ri += runes
		if ri <= n {
			x[min(ri, n)] = px(end)
		}
	}
	for ; ri < n; ri++ { // runes no glyph covered (a trailing line break): at the end
		x[ri+1] = x[ri]
	}
	return ascent, descent, x
}

// Layout shapes s in f. maxWidth > 0 wraps lines at that width; 0 keeps one line.
func (t *TextShaper) Layout(s string, f Font, maxWidth float32) *TextLayout {
	ft := gioFont(f, t.fonts)
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
