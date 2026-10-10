// Package flow lays out a paragraph of differently styled text: spans in fonts, colours and
// decorations, broken into lines, with every grapheme cluster's position kept for drawing, the
// caret and hit tests. HTMLView's inline content and the gui Editor's Rendered lines are both
// laid out here, so they break and place text alike.
//
// Lay, Paint and the queries run on the goroutine that owns the shaper: the UI loop.
package flow

import (
	"image/color"
	"math"
	"strings"
	"unicode"

	"github.com/yongjohnlee80/golib/gui"
)

// Span is a stretch of text in one style.
type Span struct {
	Text              string
	Font              gui.Font
	Color             color.NRGBA // zero: Options.Color
	Background        color.NRGBA // zero: none
	Underline, Strike bool
	Link              string // "": not a link
	// Line and Col place the span's first cluster in a line-based source: cluster i of Text is
	// cluster Col+i of Line, one to one. Line < 0: no such mapping, and Src is used instead.
	Line, Col int
	Src       [2]int // a block-grain source range (HTMLView's data-src)
	Hidden    bool   // laid out with no width: its clusters keep positions, all at one x
	// SpaceWidth is a kept space's advance, and a tab's is TabSize of it: what an indentation
	// is drawn by. 0: the font's own space.
	SpaceWidth float32
	// Room lays a Hidden span out as blank room this wide, its clusters at its start: what puts
	// the text after it at a column (a table's cell). 0: a Hidden span takes no room.
	Room float32
	Atom *Atom // an inline box (an image, a widget) in place of Text
	// LetterSpacing is added after each cluster, as CSS's letter-spacing; 0: none. A spaced span's
	// clusters are drawn one by one at their places.
	LetterSpacing float32
}

// Atom is an inline box: W wide, H tall, its baseline Baseline below its top.
type Atom struct {
	W, H, Baseline float32
	Paint          func(c gui.Canvas) // c is the atom's rect
}

// Align is where a line's content sits in the width.
type Align uint8

const (
	Start Align = iota
	Center
	End
)

// WhiteSpace is how spaces and line breaks in the text are treated, as CSS's white-space.
type WhiteSpace uint8

const (
	Normal  WhiteSpace = iota // runs of spaces and breaks collapse to one space; lines wrap
	Pre                       // kept; lines break only at '\n'
	PreWrap                   // kept; lines also wrap at spaces
)

// Options shape a paragraph.
type Options struct {
	Width      float32 // 0: no wrapping
	Align      Align
	LineHeight float32 // a multiple of each font's size; 0: 1.2
	WhiteSpace WhiteSpace
	Color      color.NRGBA // a span's zero colour
	// TabSize is a kept tab's width, in spaces: a tab is drawn as that much room, never as a
	// glyph. 0: 4.
	TabSize int
	// Left is room before every line; Hang more before each line a wrap starts (not one a '\n'
	// starts): a list item's text lines up after its bullet.
	Left, Hang float32
}

// Pos is a cluster boundary: Offset bytes into span Span's Text.
type Pos struct{ Span, Offset int }

// Para is a laid-out paragraph.
type Para struct {
	Lines  []Line
	Width  float32 // the widest line's content
	Height float32

	opts   Options
	pieces []piece
	last   int  // the last piece placed on a line so far; -1 before any
	cont   bool // the next line continues a wrapped one: it hangs
}

// lead is the room before the line being placed: Left, and Hang on a wrap's continuation.
func (p *Para) lead() float32 {
	if p.cont {
		return p.opts.Left + p.opts.Hang
	}
	return p.opts.Left
}

// Line is one line of a paragraph, Y from the paragraph's top.
type Line struct {
	Y, H, Baseline float32
	Frags          []Frag
	hard           bool // ended by a '\n' in the text
}

// Frag is span Span's bytes [From, To) on one line, drawn from X.
type Frag struct {
	Span     int
	From, To int
	X, W     float32
	Layout   *gui.TextLayout   // nil for a hidden span, an atom, or a collapsed space alone
	glyphs   []*gui.TextLayout // a letter-spaced frag's, cluster by cluster, in place of Layout
	display  string
	dclus    []int     // each cluster's byte offset in display
	offsets  []int     // each display cluster's byte offset in the span's Text
	xs       []float32 // cluster edges from X: len(offsets)+1
	ascent   float32
	sized    bool // a sized space run: no other piece joins its frag
}

type pieceKind uint8

const (
	word pieceKind = iota
	space
	newline
	atom
)

// piece is the unit of breaking: a word, a run of spaces, a hard break or an atom, of one span.
type piece struct {
	kind     pieceKind
	span     int
	from, to int    // bytes of the span's Text
	display  string // what is drawn: a collapsed run of spaces is " " or ""
	m        gui.Measured
	w        float32
	cjk      bool // breakable after it
	sized    bool // a kept space run whose widths are its own, not the shaper's: a tab, or SpaceWidth
}

// Lay lays spans out.
func Lay(spans []Span, o Options, t *gui.TextShaper) *Para {
	if o.LineHeight <= 0 {
		o.LineHeight = 1.2
	}
	p := &Para{opts: o, last: -1}
	if o.TabSize <= 0 {
		o.TabSize = 4
	}
	p.opts = o
	p.pieces = split(spans, o.WhiteSpace)
	for i := range p.pieces {
		pc := &p.pieces[i]
		sp := spans[pc.span]
		switch {
		case pc.kind == atom:
			pc.w = sp.Atom.W
		case sp.Hidden:
			pc.m = t.Measure(pc.display, sp.Font)
			for j := range pc.m.X {
				pc.m.X[j] = 0
			}
		default:
			pc.m = t.Measure(pc.display, sp.Font)
			if pc.kind == space && o.WhiteSpace != Normal {
				sizeSpaces(pc, sp, o.TabSize)
			}
			if ls := sp.LetterSpacing; ls != 0 && pc.kind != newline {
				// each cluster's edge moves by the spacing of every cluster before it, its own too
				for j := 1; j < len(pc.m.X); j++ {
					pc.m.X[j] += float32(j) * ls
				}
			}
			pc.w = pc.m.X[len(pc.m.X)-1]
		}
	}
	// a hidden span with Room: its room at the end of its last piece
	for i := len(p.pieces) - 1; i >= 0; i-- {
		pc := &p.pieces[i]
		sp := spans[pc.span]
		if !sp.Hidden || sp.Room <= 0 || pc.kind == atom || (i+1 < len(p.pieces) && p.pieces[i+1].span == pc.span) {
			continue
		}
		pc.m.X[len(pc.m.X)-1] = sp.Room
		pc.w = sp.Room
	}
	p.breakLines(spans, t)
	return p
}

// split cuts the spans into pieces, collapsing white space under Normal.
func split(spans []Span, ws WhiteSpace) []piece {
	var out []piece
	lastSpace := true // at the paragraph's start, a collapsible space is dropped
	for si, sp := range spans {
		if sp.Atom != nil {
			out = append(out, piece{kind: atom, span: si, display: "￼"})
			lastSpace = false
			continue
		}
		s := sp.Text
		i := 0
		for i < len(s) {
			r, n := decode(s[i:])
			switch {
			case r == '\n' && ws != Normal:
				out = append(out, piece{kind: newline, span: si, from: i, to: i + n})
				i += n
				lastSpace = true
			case isSpace(r):
				j := i
				for j < len(s) {
					r2, n2 := decode(s[j:])
					if !isSpace(r2) || (r2 == '\n' && ws != Normal) {
						break
					}
					j += n2
				}
				disp := s[i:j]
				if ws != Normal {
					// a tab has no glyph: it is shown as a space, one cluster for one, and given
					// its width by sizeSpaces
					disp = strings.ReplaceAll(disp, "\t", " ")
				}
				if ws == Normal {
					disp = " "
					if lastSpace {
						disp = ""
					}
				}
				out = append(out, piece{kind: space, span: si, from: i, to: j, display: disp})
				lastSpace = ws == Normal
				i = j
			default:
				j := i
				for j < len(s) {
					r2, n2 := decode(s[j:])
					if isSpace(r2) {
						break
					}
					j += n2
					if isCJK(r2) || r2 == '-' {
						break // a break opportunity after it
					}
				}
				w := s[i:j]
				last, _ := lastRune(w)
				out = append(out, piece{kind: word, span: si, from: i, to: j, display: w, cjk: isCJK(last) || last == '-'})
				lastSpace = false
				i = j
			}
		}
	}
	// Words that follow each other with no space (across spans, or after a CJK or '-' cut) form
	// one unbreakable run unless the first allows a break after it: breakLines reads cjk.
	return out
}

// breakLines places the pieces on lines.
func (p *Para) breakLines(spans []Span, t *gui.TextShaper) {
	o := p.opts
	wrap := o.Width > 0 && o.WhiteSpace != Pre
	var line []int // piece indexes
	var x float32
	flush := func(hard bool) {
		p.addLine(spans, line, hard, t)
		line, x = line[:0:0], 0
		p.cont = !hard
	}
	// limit is the width the line being placed has, after its lead
	limit := func() float32 { return max(o.Width-p.lead(), 1) }
	// runEnd is the end of the unbreakable run starting at i: words joined without a space or
	// a break opportunity between them.
	runEnd := func(i int) (int, float32) {
		w := p.pieces[i].w
		j := i + 1
		for j < len(p.pieces) && p.pieces[j-1].kind == word && !p.pieces[j-1].cjk && (p.pieces[j].kind == word || p.pieces[j].kind == atom) {
			w += p.pieces[j].w
			j++
		}
		return j, w
	}
	for i := 0; i < len(p.pieces); {
		pc := p.pieces[i]
		switch pc.kind {
		case newline:
			line = append(line, i)
			flush(true)
			i++
			continue
		case space:
			line = append(line, i) // a space never starts a break; at a line's end it hangs
			x += pc.w
			i++
			continue
		}
		j, w := runEnd(i)
		if wrap && x+w > limit() && len(nonSpace(p.pieces, line)) > 0 {
			flush(false)
		}
		if wrap && w > limit() && x == 0 {
			// a run wider than the line: break it between clusters
			for k := j - 1; k >= i; k-- { // from the end, so the indexes before k stay put
				p.splitWide(k, spans, t)
			}
			j, w = runEnd(i)
			if w > limit() {
				// pieces now one cluster each, or atoms: place them one by one
				for k := i; k < j; k++ {
					if x+p.pieces[k].w > limit() && len(line) > 0 {
						flush(false)
					}
					line = append(line, k)
					x += p.pieces[k].w
				}
				i = j
				continue
			}
		}
		for k := i; k < j; k++ {
			line = append(line, k)
		}
		x += w
		i = j
	}
	if len(line) > 0 || len(p.Lines) == 0 || p.Lines[len(p.Lines)-1].hard {
		flush(false)
	}
	for _, l := range p.Lines {
		p.Height = max(p.Height, l.Y+l.H)
	}
}

// splitWide cuts piece k into one piece per cluster, in place, so a word wider than the line
// can break between clusters.
func (p *Para) splitWide(k int, spans []Span, t *gui.TextShaper) {
	pc := p.pieces[k]
	if pc.kind != word || len(pc.m.Clusters) <= 1 {
		return
	}
	var parts []piece
	for c := range pc.m.Clusters {
		end := len(pc.display)
		if c+1 < len(pc.m.Clusters) {
			end = pc.m.Clusters[c+1]
		}
		d := pc.display[pc.m.Clusters[c]:end]
		np := piece{kind: word, span: pc.span, from: pc.from + pc.m.Clusters[c], to: pc.from + end, display: d, cjk: true}
		np.m = t.Measure(d, spans[pc.span].Font)
		np.w = np.m.X[len(np.m.X)-1]
		if spans[pc.span].Hidden {
			np.w = 0
			for e := range np.m.X {
				np.m.X[e] = 0
			}
		}
		parts = append(parts, np)
	}
	parts[len(parts)-1].cjk = pc.cjk
	p.pieces = append(p.pieces[:k], append(parts, p.pieces[k+1:]...)...)
}

func nonSpace(ps []piece, idx []int) []int {
	var out []int
	for _, i := range idx {
		if ps[i].kind != space {
			out = append(out, i)
		}
	}
	return out
}

// addLine makes a Line of pieces, merging a span's consecutive pieces into one Frag.
func (p *Para) addLine(spans []Span, idx []int, hard bool, t *gui.TextShaper) {
	o := p.opts
	var y float32
	if n := len(p.Lines); n > 0 {
		y = p.Lines[n-1].Y + p.Lines[n-1].H
	}
	l := Line{Y: y, hard: hard}
	var above, below float32
	measureLine := func(si int, pc *piece) {
		sp := spans[si]
		if pc != nil && pc.kind == atom {
			above, below = max(above, sp.Atom.Baseline), max(below, sp.Atom.H-sp.Atom.Baseline)
			return
		}
		var m gui.Measured
		if pc != nil {
			m = pc.m
		}
		if len(m.X) == 0 {
			m = t.Measure("", sp.Font)
		}
		lead := sp.Font.Size*o.LineHeight - (m.Ascent + m.Descent)
		above, below = max(above, m.Ascent+lead/2), max(below, m.Descent+lead/2)
	}
	var x float32
	for _, i := range idx {
		pc := &p.pieces[i]
		if pc.kind == newline {
			if len(l.Frags) == 0 {
				measureLine(pc.span, pc)
			}
			continue
		}
		measureLine(pc.span, pc)
		sp := spans[pc.span]
		n := len(l.Frags)
		if n > 0 && l.Frags[n-1].Span == pc.span && l.Frags[n-1].To == pc.from && pc.kind != atom && spans[pc.span].Atom == nil &&
			!pc.sized && !l.Frags[n-1].sized {
			f := &l.Frags[n-1]
			base := f.xs[len(f.xs)-1]
			f.To = pc.to
			for _, off := range pc.m.Clusters {
				f.dclus = append(f.dclus, len(f.display)+off)
			}
			f.display += pc.display
			// base was the frag's end edge; each cluster appended adds its own end edge, so the
			// edges stay one more than the clusters. A collapsed space shown as nothing adds none.
			for c, off := range pc.m.Clusters {
				f.offsets = append(f.offsets, pc.from+mapOffset(pc, off))
				f.xs = append(f.xs, base+pc.m.X[c+1])
			}
			f.W = f.xs[len(f.xs)-1]
			x += pc.w
			continue
		}
		f := Frag{Span: pc.span, From: pc.from, To: pc.to, X: x, display: pc.display, sized: pc.sized}
		f.dclus = append([]int(nil), pc.m.Clusters...)
		f.xs = []float32{0}
		for c, off := range pc.m.Clusters {
			f.offsets = append(f.offsets, pc.from+mapOffset(pc, off))
			f.xs = append(f.xs, pc.m.X[c+1])
		}
		if pc.kind == atom {
			f.offsets, f.xs = []int{0}, []float32{0, sp.Atom.W}
		}
		f.W = pc.w
		f.ascent = pc.m.Ascent
		l.Frags = append(l.Frags, f)
		x += pc.w
	}
	if len(idx) == 0 && len(spans) > 0 {
		measureLine(len(spans)-1, nil)
	}
	if len(l.Frags) == 0 {
		// A line with no text (a '\n' alone, or the empty line after a final '\n') still holds
		// one boundary, so the caret and At can land on it: a zero-width frag at that position.
		if f, ok := p.emptyAt(idx, spans); ok {
			l.Frags = append(l.Frags, f)
		}
	}
	for _, i := range idx {
		p.last = max(p.last, i)
	}
	l.H = above + below
	l.Baseline = y + above
	// the content width leaves out spaces hanging at the line's end
	content := x
	for k := len(idx) - 1; k >= 0 && p.pieces[idx[k]].kind == space; k-- {
		content -= p.pieces[idx[k]].w
	}
	lead := p.lead()
	shift := lead
	if o.Width > 0 {
		switch o.Align {
		case Center:
			shift += (o.Width - lead - content) / 2
		case End:
			shift += o.Width - lead - content
		}
	}
	for k := range l.Frags {
		l.Frags[k].X += shift
		f := &l.Frags[k]
		if !spans[f.Span].Hidden && spans[f.Span].Atom == nil && f.display != "" && strings.TrimSpace(f.display) != "" {
			if spans[f.Span].LetterSpacing != 0 && len(f.dclus) > 0 {
				f.glyphs = clusterLayouts(f, spans[f.Span].Font, t)
			} else {
				f.Layout = t.Layout(f.display, spans[f.Span].Font, 0)
				// A frag joined from pieces measured alone can shape to another width whole: a
				// space among Hangul takes the CJK fallback font's narrower space. Its edges, and
				// the caret, then run ahead of its glyphs by that much per space, a gap growing
				// along the line. Drawn cluster by cluster at its edges, it agrees with them.
				if len(f.dclus) > 1 && math.Abs(float64(f.Layout.Width-f.xs[len(f.xs)-1])) > 0.01 {
					f.Layout, f.glyphs = nil, clusterLayouts(f, spans[f.Span].Font, t)
				}
			}
		}
	}
	p.Width = max(p.Width, lead+content)
	p.Lines = append(p.Lines, l)
}

// sizeSpaces gives a kept space run its widths: each space SpaceWidth (or the font's), each tab
// tabSize spaces. A run with neither a tab nor a SpaceWidth keeps the shaper's. The run's own frag
// then holds it, so the text after it is drawn from where the widths put it.
func sizeSpaces(pc *piece, sp Span, tabSize int) {
	src := sp.Text[pc.from:pc.to]
	if sp.SpaceWidth <= 0 && !strings.Contains(src, "\t") {
		return
	}
	pc.sized = true
	x := pc.m.X
	var at float32
	for c, off := range pc.m.Clusters {
		adv := x[c+1] - x[c]
		if sp.SpaceWidth > 0 {
			adv = sp.SpaceWidth
		}
		if src[off] == '\t' {
			adv *= float32(tabSize)
		}
		x[c] = at
		at += adv
	}
	x[len(pc.m.Clusters)] = at
}

// emptyAt is the zero-width frag of a line without text: before its '\n', or, for the line
// after a final '\n', after the last piece placed.
func (p *Para) emptyAt(idx []int, spans []Span) (Frag, bool) {
	for _, i := range idx {
		if pc := p.pieces[i]; pc.kind == newline {
			return Frag{Span: pc.span, From: pc.from, To: pc.from, xs: []float32{0}}, true
		}
	}
	if p.last >= 0 {
		pc := p.pieces[p.last]
		return Frag{Span: pc.span, From: pc.to, To: pc.to, xs: []float32{0}}, true
	}
	if len(spans) > 0 {
		return Frag{xs: []float32{0}}, true
	}
	return Frag{}, false
}

// mapOffset maps a byte offset in a piece's display to one in its source bytes. Only a
// collapsed run of spaces differs: its one shown space maps to the run's first byte.
func mapOffset(pc *piece, off int) int {
	if pc.kind == space && len(pc.display) != pc.to-pc.from {
		return 0
	}
	return off
}

func decode(s string) (rune, int) {
	for i, r := range s {
		if i == 0 {
			n := len(string(r))
			return r, n
		}
	}
	return 0, len(s)
}

func lastRune(s string) (rune, bool) {
	var last rune
	ok := false
	for _, r := range s {
		last, ok = r, true
	}
	return last, ok
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' }

// isCJK reports a character after which a line may break: CJK ideographs, kana and Hangul
// syllables (UAX #14's ID class, approximately).
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// clusterLayouts shapes a letter-spaced frag's clusters one by one, nil for a space: each is drawn
// at its own edge, which the spacing has moved.
func clusterLayouts(f *Frag, font gui.Font, t *gui.TextShaper) []*gui.TextLayout {
	out := make([]*gui.TextLayout, len(f.dclus))
	for c, off := range f.dclus {
		end := len(f.display)
		if c+1 < len(f.dclus) {
			end = f.dclus[c+1]
		}
		if text := f.display[off:end]; strings.TrimSpace(text) != "" {
			out[c] = t.Layout(text, font, 0)
		}
	}
	return out
}
