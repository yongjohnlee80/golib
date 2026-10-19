package gui

import (
	"cmp"
	"strings"
	"unicode/utf8"

	"github.com/yongjohnlee80/gio/font"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"
	"github.com/yongjohnlee80/gio/text"
	"golang.org/x/image/math/fixed"

	"github.com/yongjohnlee80/golib/tui"
)

// Proportional and monospace text shaping, measurement, and layout.
//
// The text subsystem bridges high-level typographical specifications ([Font]) with the
// underlying Gio font shaping engine ([text.Shaper]). It resolves font families, scales
// device metrics, and extracts exact sub-pixel grapheme cluster bounds.
//
// # Typographical Layout & Measurement Pipeline
//
//	Input Text String & [Font] (Logical Points, Weight, Style)
//	                 │
//	                 ▼
//	┌─────────────────────────────────────────────────────────────┐
//	│                 TextShaper.LayoutString                     │
//	│  - Resolves font family against Prose / Mono defaults       │
//	│  - Scales em dimensions by display scale (PxPerEm)          │
//	│  - Performs bidirectional text analysis (bidi) & shaping    │
//	└──────────────────────────────┬──────────────────────────────┘
//	                               │
//	         ┌─────────────────────┴─────────────────────┐
//	         ▼                                           ▼
//	TextShaper.Measure (Cluster Metrics):       TextShaper.Layout (Renderable):
//	- Grapheme cluster byte offsets             - Shaped vector glyph outlines
//	- Fractional X start positions per cluster  - Multi-line wrapping bounds
//	- Typographical Ascent & Descent bounds     - Pre-recorded bitmap emoji ops
//
// # Font Family Resolution: Prose vs. Mono
//
// A [Font] specifies a family name using CSS font-family syntax:
//   - An empty family ("") selects the window's proportional prose typeface ([Fonts.Prose]).
//   - [CellFamily] ("\x00cell") dynamically binds to the window's cell font ([Fonts.Mono]).
//   - Explicit family names (e.g. "Helvetica", "Fira Code") are looked up directly.

// Font specifies typographical attributes for text rendering.
type Font struct {
	Family string  // Typeface name; "" selects prose UI font, CellFamily selects terminal monospace
	Size   float32 // Point size in logical pixels (dp)
	Bold   bool    // Bold font weight
	Italic bool    // Italicized font style
}

// uiFamily defines the default fallback typeface for proportional UI text.
const uiFamily = "sans-serif, emoji"

// CellFamily is a symbolic font family alias that resolves to the window's current monospace cell font.
// Used by rich text renderers, markdown code spans, and syntax-highlighted editor blocks.
const CellFamily = "\x00cell"

// Fonts holds the active typeface mappings for a window.
//
// Fields:
//   - Prose: Primary font family used for general text when [Font.Family] is empty.
//   - Mono: Monospace font family bound to [CellFamily].
//   - Gen: Monotonically increasing generation counter incremented whenever font configuration changes.
type Fonts struct {
	Prose, Mono string
	Gen         uint64
}

// defaultFonts returns the baseline typographical families prior to explicit window configuration.
func defaultFonts() Fonts { return Fonts{Prose: uiFamily, Mono: MonospaceFamily()} }

// TextShaper performs font resolution, glyph shaping, and layout measurement.
//
// Invariants:
//   - Not safe for concurrent use across goroutines. Canvas code on the UI loop uses
//     its own shaper, while background worker threads acquire isolated instances via [AcquireMeasurer].
type TextShaper struct {
	s     *text.Shaper
	paths *shapeCache // the window's outline paths, for a canvas's shaper; nil: shaped each time
	scale float32     // device pixels per logical pixel
	fonts Fonts       // active font family mappings
}

// shape is gs's outline path: from the window's cache when the shaper is a canvas's.
func (t *TextShaper) shape(gs []text.Glyph) clip.PathSpec {
	if t.paths != nil {
		return t.paths.Shape(gs)
	}
	return t.s.Shape(gs)
}

// Fonts returns the active font family bindings resolved by this shaper.
func (t *TextShaper) Fonts() Fonts { return t.fonts }

// TextLayout holds shaped, ready-to-render text lines.
// All dimensions and coordinates are expressed in logical pixels (dp).
type TextLayout struct {
	Width           float32 // width of the widest line
	Height          float32 // total height from top of first line to bottom of last line
	Ascent, Descent float32 // baseline ascent and descent of the first line
	lines           []shapedLine
	scale           float32
	font            Font   // what it was shaped in, and its family as resolved: for a canvas
	family          string // that writes words rather than outlines (SVGCanvas)
}

// shapedLine is one line's outlines and colour glyphs, drawn with its dot at (x, y): device
// pixels from the first line's dot. Gio shapes a run on one baseline, so each line is its own.
type shapedLine struct {
	path    clip.PathSpec
	bitmaps op.CallOp
	x, y    float32
	runs    []textRun // the words it says, where: what an SVGCanvas writes
}

// textRun is a line's text from x, in device pixels as the line's own x is.
type textRun struct {
	text string
	x    float32
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

// font is f as Gio selects it to shape s: with an installed family after it for each rune of s
// it lacks (covered), the same for a measure and a layout.
func (t *TextShaper) font(s string, f Font) font.Font {
	ft := gioFont(f, t.fonts)
	ft.Typeface = covered(t.s, ft.Typeface, s)
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

// Measure calculates typographical metrics and horizontal cluster positions for a single line of text.
//
// Grapheme Cluster Alignment:
//   - Aligns measurements with [tui.Graphemes] segmentation so that character indexing in widgets
//     and cursor navigation match visual layout positions exactly.
//   - Multi-rune ligatures (such as "->" or "==" in programming fonts) divide advance widths
//     evenly across their constituent runes.
//   - Returns [Measured] containing cluster byte offsets, horizontal starting boundaries, font ascent,
//     and font descent. Input string s must not contain newline characters.
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
		Font:     t.font(s, f),
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

// Layout shapes text string s into a renderable multi-line [TextLayout].
//
// Parameters:
//   - s: text content (may contain newlines).
//   - f: font styling and size.
//   - maxWidth: maximum width in logical pixels before wrapping. A value <= 0 disables wrapping,
//     keeping all text on a single continuous line.
//
// Returns a [TextLayout] holding vector path outlines, bitmap glyphs, and layout dimensions.
func (t *TextShaper) Layout(s string, f Font, maxWidth float32) *TextLayout {
	ft := t.font(s, f)
	maxW := 1 << 20
	if maxWidth > 0 {
		maxW = max(int(maxWidth*t.scale), 1)
	}
	t.s.LayoutString(text.Parameters{
		Font:     ft,
		PxPerEm:  fixed.Int26_6(f.Size * t.scale * 64),
		MaxWidth: maxW,
	}, s)
	tl := &TextLayout{scale: t.scale, font: f, family: string(ft.Typeface)}
	var line []text.Glyph
	var firstY int32
	var right fixed.Int26_6
	var lastDescent fixed.Int26_6
	rs, used := []rune(s), 0 // the runes the lines so far said: a glyph's cluster counts its own
	flush := func() {
		if len(line) == 0 {
			return
		}
		n := 0
		for _, g := range line {
			n += int(g.Runes)
		}
		end := min(used+n, len(rs))
		x := float32(line[0].X) / 64
		tl.lines = append(tl.lines, shapedLine{
			path:    t.shape(line),
			bitmaps: t.s.Bitmaps(line),
			x:       x,
			y:       float32(line[0].Y - firstY),
			runs:    []textRun{{text: string(rs[used:end]), x: x}},
		})
		used = end
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

// TextRun is a piece of one line of text placed at X, in logical pixels from the line's start.
type TextRun struct {
	Text string
	X    float32
}

// LayoutRuns shapes runs of text in one font on one line, each at its own X, as one layout: a
// table row's cells drawn with one DrawText rather than one each. The runs share a baseline, the
// tallest run's ascent below the layout's top; a newline in a run is drawn as a space.
func (t *TextShaper) LayoutRuns(runs []TextRun, f Font) *TextLayout {
	tl := &TextLayout{scale: t.scale, font: f, family: string(gioFont(f, t.fonts).Typeface)}
	var glyphs []text.Glyph
	var right fixed.Int26_6
	var said []textRun
	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		s := strings.Map(oneLine, r.Text)
		t.s.LayoutString(text.Parameters{
			Font:     t.font(s, f),
			PxPerEm:  fixed.Int26_6(f.Size * t.scale * 64),
			MaxWidth: 1 << 20,
		}, s)
		at := fixed.Int26_6(r.X * t.scale * 64)
		said = append(said, textRun{text: s, x: float32(at) / 64})
		first, origin := true, fixed.Int26_6(0)
		for g, ok := t.s.NextGlyph(); ok; g, ok = t.s.NextGlyph() {
			if first {
				first, origin = false, g.X
			}
			g.X += at - origin
			tl.Ascent = max(tl.Ascent, float32(g.Ascent)/64/t.scale)
			tl.Descent = max(tl.Descent, float32(g.Descent)/64/t.scale)
			right = max(right, g.X+g.Advance)
			glyphs = append(glyphs, g)
		}
	}
	if len(glyphs) == 0 {
		return tl
	}
	tl.lines = []shapedLine{{path: t.shape(glyphs), bitmaps: t.s.Bitmaps(glyphs), x: float32(glyphs[0].X) / 64, runs: said}}
	tl.Width = float32(right) / 64 / t.scale
	tl.Height = tl.Ascent + tl.Descent
	return tl
}

// oneLine is s with its line breaks as spaces.
func oneLine(r rune) rune {
	if r == '\n' || r == '\r' {
		return ' '
	}
	return r
}
