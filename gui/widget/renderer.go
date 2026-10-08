package widget

import (
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	"github.com/yongjohnlee80/golib/tui"
)

// EditorMode is how an Editor draws its text.
type EditorMode uint8

const (
	// Raw draws the text as it is, in the monospace font, with its syntax colours.
	Raw EditorMode = iota
	// Rendered draws it through the Editor's Renderer: a Markdown heading at its size, emphasis in
	// its style, its marks hidden but on the cursor's block.
	Rendered
)

// A Renderer lays out the buffer's lines for the Rendered mode. It is told whether the cursor is
// in a block, so a construct under the cursor shows its source. It keeps no state but a cache it
// may hold, keyed by the lines it was given.
type Renderer interface {
	// Blocks splits lines [from, to) into blocks: a line, or a run of lines drawn as one (a fenced
	// code block, a table, a diagram). The blocks cover [from, to) in order, without gaps.
	Blocks(lines []string, from, to int) []Block
	// LayOut lays block b out at width. Every span it returns maps one to one onto its source
	// line's clusters (flow.Span's Line and Col), and no span crosses a line, so the caret and a
	// click land on the right cluster.
	// host asks a Diagrammer on the widget's behalf (DiagramHost); a renderer never calls one
	// directly, since the widget owns each request's lifetime.
	LayOut(b Block, lines []string, width float32, cursorInside bool, t *gui.TextShaper, th Theme, host DiagramHost) BlockLayout
}

// Block is lines [From, To) of the buffer.
type Block struct{ From, To int }

// BlockLayout is a block as drawn: a paragraph per source line, or a picture.
type BlockLayout struct {
	Lines       []LineLayout
	Picture     gui.View // a rendered diagram or image, drawn below Lines; nil for text
	PictureSize gui.Size
	Background  color.NRGBA // zero: none (a fenced block's tint)
	Height      float32
}

// LineLayout is one source line's paragraph, Y from the block's top.
type LineLayout struct {
	Para  *flow.Para
	Spans []flow.Span
	Y     float32
	Marks []Mark // drawn under the text: a quote's bar, a rule
}

// Mark is a filled rectangle drawn with a line, Rect from the line's top-left.
type Mark struct {
	Rect  gui.Rect
	Color color.NRGBA
}

// Theme is what a Renderer draws with, taken from the Editor's cells when it paints.
type Theme struct {
	Text, Background, Muted, Accent, CodeBackground color.NRGBA
	Caret                                           color.NRGBA // zero: the text's
	LineNumbers                                     color.NRGBA // the gutter's numbers
	Prose                                           gui.Font    // rendered text
	Mono                                            gui.Font    // raw text and code
	// The Raw view's Markdown colours, from the editor's syntax styles, so the two views read
	// alike: a heading, strong, emphasis, code, a link, a marker (a bullet, a quote's bar, a
	// rule), and what is there to be skipped (frontmatter). Zero: the text's (a link: Accent).
	Heading, Strong, Emph, Code, Link, Marker, Quiet color.NRGBA
}

// or is c, or else d when c is unset.
func or(c, d color.NRGBA) color.NRGBA {
	if c.A == 0 {
		return d
	}
	return c
}

// rawSpans are line ln's spans in the Raw look: one per run of clusters in one colour, in the
// monospace font. color is a cluster's colour by its index; nil: all in the text colour.
func rawSpans(ln int, text string, font gui.Font, color func(col int) color.NRGBA) []flow.Span {
	if text == "" {
		return []flow.Span{{Font: font, Line: ln}}
	}
	var spans []flow.Span
	col, start, startCol := 0, 0, 0
	off := 0
	var cur colorKey
	first := true
	for c := range tui.Graphemes(text) {
		k := colorKey{}
		if color != nil {
			k = colorKey{c: color(col), set: true}
		}
		if !first && k != cur {
			spans = append(spans, flow.Span{Text: text[start:off], Font: font, Color: cur.c, Line: ln, Col: startCol})
			start, startCol = off, col
		}
		cur, first = k, false
		off += len(c)
		col++
	}
	return append(spans, flow.Span{Text: text[start:], Font: font, Color: cur.c, Line: ln, Col: startCol})
}

type colorKey struct {
	c   color.NRGBA
	set bool
}

// DiagramHost asks a Diagrammer for a block's picture on the widget's behalf. It owns each
// request: its context, cancelled when the block's source changes or the widget unmounts; its
// generation, so a late answer for an older source is dropped; and its ready callback, which
// brings the answer back to the widget's loop and lays the block out again.
type DiagramHost interface {
	Diagram(d Diagrammer, b Block, req DiagramRequest) DiagramAnswer
}
