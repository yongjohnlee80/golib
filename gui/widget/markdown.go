package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/tui"
)

// MarkdownRenderer draws Markdown in an Editor's Rendered mode, as a live preview does: a heading
// at its size, emphasis, strong, strikethrough, code and links in their styles, with their marks
// hidden, and a fenced code block on a tinted ground. The block holding the cursor shows its
// source, every mark visible, in the monospace font.
//
// A fenced block's fence lines stay on screen, dimmed, rather than hidden: every line keeps a
// place for the caret to land.
type MarkdownRenderer struct {
	h1       float32
	diagrams Diagrammer
}

// MarkdownOption sets up a MarkdownRenderer.
type MarkdownOption func(*MarkdownRenderer)

// NewMarkdownRenderer is a MarkdownRenderer.
func NewMarkdownRenderer(opts ...MarkdownOption) *MarkdownRenderer {
	r := &MarkdownRenderer{h1: 2}
	for _, o := range opts {
		o(r)
	}
	return r
}

// HeadingScale is h1's size as a multiple of the text's; the levels step down to 1.0 at h6.
func HeadingScale(h1 float32) MarkdownOption {
	return func(r *MarkdownRenderer) { r.h1 = max(h1, 1) }
}

// WithDiagrams draws a fenced block a Diagrammer answers Ready for (a mermaid fence) as its
// picture. Without one, or until it answers, the block is drawn as code.
func WithDiagrams(d Diagrammer) MarkdownOption { return func(r *MarkdownRenderer) { r.diagrams = d } }

// fence is a fenced code block's opening: its character, its length and its info string.
type fence struct {
	ch   byte
	n    int
	info string
}

// openFence reads a line as a fence opening: up to three spaces, then three or more '`' or '~'.
func openFence(line string) (fence, bool) {
	s := strings.TrimLeft(line, " ")
	if len(line)-len(s) > 3 || len(s) < 3 || (s[0] != '`' && s[0] != '~') {
		return fence{}, false
	}
	n := 0
	for n < len(s) && s[n] == s[0] {
		n++
	}
	if n < 3 {
		return fence{}, false
	}
	info := strings.TrimSpace(s[n:])
	if s[0] == '`' && strings.Contains(info, "`") {
		return fence{}, false
	}
	return fence{ch: s[0], n: n, info: info}, true
}

// closes reports whether line closes f: its character, at least as many, nothing else.
func (f fence) closes(line string) bool {
	s := strings.TrimLeft(line, " ")
	if len(line)-len(s) > 3 {
		return false
	}
	n := 0
	for n < len(s) && s[n] == f.ch {
		n++
	}
	return n >= f.n && strings.TrimSpace(s[n:]) == ""
}

// Blocks are the fenced code blocks, each a block of its lines (an unclosed one runs to the end),
// and every other line alone.
func (r *MarkdownRenderer) Blocks(lines []string, from, to int) []Block {
	var out []Block
	for i := 0; i < len(lines); {
		end := i + 1
		if f, ok := openFence(lines[i]); ok {
			for end < len(lines) && !f.closes(lines[end]) {
				end++
			}
			end = min(end+1, len(lines))
		} else if indentedCode(lines[i]) && (i == 0 || isBlank(lines[i-1])) {
			// an indented code block: it cannot interrupt a paragraph, so it starts at the top or
			// after a blank line, and runs while lines stay indented or blank, its trailing
			// blanks left out
			for end < len(lines) && (indentedCode(lines[end]) || isBlank(lines[end])) {
				end++
			}
			for end > i+1 && isBlank(lines[end-1]) {
				end--
			}
		}
		if end > from && i < to {
			out = append(out, Block{From: max(i, from), To: min(end, to)})
		}
		i = end
	}
	return out
}

// LayOut lays block b out.
func (r *MarkdownRenderer) LayOut(b Block, lines []string, width float32, cursorInside bool, t *gui.TextShaper, th Theme, host DiagramHost) BlockLayout {
	opts := flow.Options{Width: width, WhiteSpace: flow.PreWrap, Color: th.Text}
	if f, ok := openFence(lines[b.From]); ok {
		return r.layFence(b, f, lines, opts, cursorInside, t, th, host)
	}
	if indentedCode(lines[b.From]) && (b.From == 0 || isBlank(lines[b.From-1])) {
		return r.layIndented(b, lines, opts, t, th)
	}
	ln := b.From
	var spans []flow.Span
	extra := float32(0)
	if cursorInside {
		spans = rawSpans(ln, lines[ln], th.Mono, nil)
	} else {
		var level int
		spans, level = r.lineSpans(ln, lines[ln], th)
		if level > 0 {
			extra = th.Prose.Size * 0.4
		}
	}
	p := flow.Lay(spans, opts, t)
	return BlockLayout{Lines: []LineLayout{{Para: p, Spans: spans, Y: extra}}, Height: extra + p.Height}
}

// layFence lays a fenced block out: its lines in monospace on a tinted ground, the fence lines
// dimmed, or a diagram's picture when the Diagrammer has one.
func (r *MarkdownRenderer) layFence(b Block, f fence, lines []string, opts flow.Options, inside bool, t *gui.TextShaper, th Theme, host DiagramHost) BlockLayout {
	bl := BlockLayout{Background: th.CodeBackground}
	if !inside && r.diagrams != nil && host != nil && f.info != "" {
		src := strings.Join(lines[min(b.From+1, b.To):max(b.To-1, b.From+1)], "\n")
		lang, _, _ := strings.Cut(f.info, " ")
		a := host.Diagram(r.diagrams, b, DiagramRequest{Lang: lang, Src: src, Width: opts.Width, Scale: 1, Theme: th})
		// Ready, or Pending with the block's last picture: draw the picture. Otherwise the code.
		if a.State != Declined && a.Pic != nil {
			bl.Picture, bl.PictureSize, bl.Height = a.Pic, a.Size, a.Size.H
			bl.Background.A = 0
			return bl
		}
	}
	dim := th.Mono
	dim.Size *= 0.85
	var y float32
	for ln := b.From; ln < b.To; ln++ {
		isFence := ln == b.From || (ln == b.To-1 && f.closes(lines[ln]) && ln > b.From)
		var spans []flow.Span
		switch {
		case isFence && !inside:
			spans = rawSpans(ln, lines[ln], dim, nil)
			for i := range spans {
				spans[i].Color = th.Muted
			}
		default:
			spans = rawSpans(ln, lines[ln], th.Mono, nil)
		}
		p := flow.Lay(spans, opts, t)
		bl.Lines = append(bl.Lines, LineLayout{Para: p, Spans: spans, Y: y})
		y += p.Height
	}
	bl.Height = y
	return bl
}

// layIndented lays an indented code block out as code: its lines as written, monospace, on the
// code background. Nothing in it is Markdown, so a '#' there is never a heading.
func (r *MarkdownRenderer) layIndented(b Block, lines []string, opts flow.Options, t *gui.TextShaper, th Theme) BlockLayout {
	bl := BlockLayout{Background: th.CodeBackground}
	var y float32
	for ln := b.From; ln < b.To; ln++ {
		spans := rawSpans(ln, lines[ln], th.Mono, nil)
		p := flow.Lay(spans, opts, t)
		bl.Lines = append(bl.Lines, LineLayout{Para: p, Spans: spans, Y: y})
		y += p.Height
	}
	bl.Height = y
	return bl
}

// indentedCode reports a line indented as code: four spaces or a tab before any text.
func indentedCode(line string) bool {
	if isBlank(line) {
		return false
	}
	return strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ")
}

func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

// mdAttr is how one byte of a line is drawn.
type mdAttr struct {
	hidden                           bool
	bold, italic, code, strike, link bool
	heading                          int
}

// lineSpans are a line's spans with its Markdown drawn: the marks hidden, the rest styled. level
// is the line's heading level, 0 for none.
func (r *MarkdownRenderer) lineSpans(ln int, line string, th Theme) ([]flow.Span, int) {
	if line == "" {
		return []flow.Span{{Font: th.Prose, Line: ln}}, 0
	}
	src := []byte(line)
	doc := markdown.Parse(src, markdown.GFM())
	attrs := make([]mdAttr, len(src))
	level := 0
	var walk func(n *markdown.Node)
	walk = func(n *markdown.Node) {
		switch n.Kind {
		case markdown.KindHeading:
			level = n.Level
			for i := n.Span.Start; i < n.Span.End; i++ {
				attrs[i].heading = n.Level
			}
			if n.FirstChild == nil {
				hide(attrs, n.Span.Start, n.Span.End)
			} else {
				hide(attrs, n.Span.Start, n.FirstChild.Span.Start)
				if tail := line[n.LastChild.Span.End:n.Span.End]; strings.Trim(tail, "# ") == "" {
					hide(attrs, n.LastChild.Span.End, n.Span.End)
				}
			}
		case markdown.KindEmph:
			markRange(attrs, n.Span.Start, n.Span.End, n, func(a *mdAttr) { a.italic = true })
		case markdown.KindStrong:
			markRange(attrs, n.Span.Start, n.Span.End, n, func(a *mdAttr) { a.bold = true })
		case markdown.KindStrikethrough:
			markRange(attrs, n.Span.Start, n.Span.End, n, func(a *mdAttr) { a.strike = true })
		case markdown.KindLink:
			markRange(attrs, n.Span.Start, n.Span.End, n, func(a *mdAttr) { a.link = true })
		case markdown.KindCodeSpan:
			s, e := n.Span.Start, n.Span.End
			open := 0
			for s+open < e && src[s+open] == '`' {
				open++
			}
			hide(attrs, s, s+open)
			hide(attrs, max(e-open, s+open), e)
			for i := s + open; i < e-open; i++ {
				attrs[i].code = true
			}
		}
		for c := n.FirstChild; c != nil; c = c.Next {
			walk(c)
		}
	}
	walk(doc.Root)
	return r.spansOf(ln, line, attrs, th), level
}

// markRange styles a node's content bytes with set, and hides its own marks: the bytes of its
// span that none of its children cover.
func markRange(attrs []mdAttr, start, end int, n *markdown.Node, set func(*mdAttr)) {
	covered := make([]bool, end-start)
	for c := n.FirstChild; c != nil; c = c.Next {
		for i := max(c.Span.Start, start); i < min(c.Span.End, end); i++ {
			covered[i-start] = true
		}
	}
	for i := start; i < end; i++ {
		set(&attrs[i])
		if !covered[i-start] {
			attrs[i].hidden = true
		}
	}
}

func hide(attrs []mdAttr, start, end int) {
	for i := max(start, 0); i < min(end, len(attrs)); i++ {
		attrs[i].hidden = true
	}
}

// spansOf groups a line's clusters into spans by their drawing, each cluster taking its first
// byte's attributes.
func (r *MarkdownRenderer) spansOf(ln int, line string, attrs []mdAttr, th Theme) []flow.Span {
	var spans []flow.Span
	off, col, start, startCol := 0, 0, 0, 0
	var cur mdAttr
	first := true
	flush := func(end int) {
		spans = append(spans, r.span(line[start:end], cur, ln, startCol, th))
	}
	for c := range tui.Graphemes(line) {
		a := attrs[off]
		if !first && a != cur {
			flush(off)
			start, startCol = off, col
		}
		cur, first = a, false
		off += len(c)
		col++
	}
	flush(len(line))
	return spans
}

// span is text drawn as a says.
func (r *MarkdownRenderer) span(text string, a mdAttr, ln, col int, th Theme) flow.Span {
	f := th.Prose
	sp := flow.Span{Text: text, Line: ln, Col: col, Hidden: a.hidden}
	if a.heading > 0 {
		f.Size *= r.h1 - (r.h1-1)*float32(a.heading-1)/5
		f.Bold = true
	}
	if a.code {
		size := f.Size * 0.9
		f = th.Mono
		f.Size = size
		sp.Background = th.CodeBackground
	}
	f.Bold = f.Bold || a.bold
	f.Italic = a.italic
	sp.Strike = a.strike
	if a.link {
		sp.Color, sp.Underline = th.Accent, true
	}
	sp.Font = f
	return sp
}
