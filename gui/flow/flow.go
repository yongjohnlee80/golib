// Package flow lays out a paragraph of differently styled text: spans in fonts, colours and
// decorations, broken into lines, with every grapheme cluster's position kept for drawing, the
// caret and hit tests. HTMLView's inline content and the gui Editor's Rendered lines are both
// laid out here, so they break and place text alike.
//
// Lay, Paint and the queries run on the goroutine that owns the shaper: the UI loop.
package flow

import (
	"image/color"
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
	Atom      *Atom  // an inline box (an image, a widget) in place of Text
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
	Layout   *gui.TextLayout // nil for a hidden span, an atom, or a collapsed space alone
	display  string
	offsets  []int     // each display cluster's byte offset in the span's Text
	xs       []float32 // cluster edges from X: len(offsets)+1
	ascent   float32
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
}

// Lay lays spans out.
func Lay(spans []Span, o Options, t *gui.TextShaper) *Para {
	if o.LineHeight <= 0 {
		o.LineHeight = 1.2
	}
	p := &Para{opts: o}
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
			pc.w = pc.m.X[len(pc.m.X)-1]
		}
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
	}
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
		if wrap && x+w > o.Width && len(nonSpace(p.pieces, line)) > 0 {
			flush(false)
		}
		if wrap && w > o.Width && x == 0 {
			// a run wider than the line: break it between clusters
			for k := j - 1; k >= i; k-- { // from the end, so the indexes before k stay put
				p.splitWide(k, spans, t)
			}
			j, w = runEnd(i)
			if w > o.Width {
				// pieces now one cluster each, or atoms: place them one by one
				for k := i; k < j; k++ {
					if x+p.pieces[k].w > o.Width && len(line) > 0 {
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
		m := pc.m
		if pc == nil || len(m.X) == 0 {
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
		if n > 0 && l.Frags[n-1].Span == pc.span && l.Frags[n-1].To == pc.from && pc.kind != atom && spans[pc.span].Atom == nil {
			f := &l.Frags[n-1]
			base := f.xs[len(f.xs)-1]
			f.To = pc.to
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
		f := Frag{Span: pc.span, From: pc.from, To: pc.to, X: x, display: pc.display}
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
	l.H = above + below
	l.Baseline = y + above
	// the content width leaves out spaces hanging at the line's end
	content := x
	for k := len(idx) - 1; k >= 0 && p.pieces[idx[k]].kind == space; k-- {
		content -= p.pieces[idx[k]].w
	}
	shift := float32(0)
	if o.Width > 0 {
		switch o.Align {
		case Center:
			shift = (o.Width - content) / 2
		case End:
			shift = o.Width - content
		}
	}
	for k := range l.Frags {
		l.Frags[k].X += shift
		f := &l.Frags[k]
		if !spans[f.Span].Hidden && spans[f.Span].Atom == nil && f.display != "" && strings.TrimSpace(f.display) != "" {
			f.Layout = t.Layout(f.display, spans[f.Span].Font, 0)
		}
	}
	p.Width = max(p.Width, content)
	p.Lines = append(p.Lines, l)
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
