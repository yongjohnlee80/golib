package widget

import (
	"image/color"
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/languages"
	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// Markdown Live-Preview Rendering Pipeline:
//
//	[ Raw Markdown Source Buffer ]
//	               |
//	               v
//	[ parse/markdown Block & Inline Parser ]
//	               |
//	               +---> Cursor Block Inspection:
//	               |       - Active Cursor Block: display raw source & editable markup
//	               |       - Inactive Blocks: format styled prose & hide delimiters
//	               |
//	               v
//	[ Flow Paragraph Assembly & Styling ]
//	     - Scaled Headings (H1 to H6 proportional scaling)
//	     - Bullet and numbered list formatting
//	     - Fenced code block syntax highlighting
//	     - Embedded Mermaid diagram vector rendering
//	               |
//	               v
//	[ Pixel Canvas Layout & Vector Output ]
//
// MarkdownRenderer renders Markdown text inside an [Editor]'s Rendered mode as a dynamic,
// high-fidelity live preview.
//
// Headings scale proportionally based on level; inline formatting (emphasis, strong, strikethrough,
// links) formats visually with delimiters hidden; and fenced code blocks feature syntax highlighting
// and embedded vector diagrams. When the user positions the caret within a block, that block
// seamlessly reveals its raw markdown syntax for editing.
type MarkdownRenderer struct {
	h1           float32
	diagrams     Diagrammer
	languages    *highlight.Catalog
	hideMetadata bool
	metadata     markdownMetadata
}

// MarkdownOption sets up a MarkdownRenderer.
type MarkdownOption func(*MarkdownRenderer)

// The capabilities the Editor discovers in a renderer: a MarkdownRenderer has each.
var (
	_ Renderer          = (*MarkdownRenderer)(nil)
	_ ReflowRenderer    = (*MarkdownRenderer)(nil)
	_ StructureRenderer = (*MarkdownRenderer)(nil)
)

// NewMarkdownRenderer is a MarkdownRenderer.
func NewMarkdownRenderer(opts ...MarkdownOption) *MarkdownRenderer {
	r := &MarkdownRenderer{h1: 2, languages: highlight.NewRepository(languages.Definitions()...).Snapshot()}
	for _, o := range opts {
		if o != nil { // a nil option is no option
			o(r)
		}
	}
	return r
}

// WithHeadingScale is h1's size as a multiple of the text's; the levels step down to 1.0 at h6.
func WithHeadingScale(h1 float32) MarkdownOption {
	return func(r *MarkdownRenderer) { r.h1 = max(h1, 1) }
}

// WithHideMetadata omits closed frontmatter and HTML comments from the reading view, without
// changing source positions. A block being edited still reveals its source, as other Markdown
// blocks do; Raw mode always shows the original buffer. Incomplete frontmatter stays visible.
func WithHideMetadata() MarkdownOption { return func(r *MarkdownRenderer) { r.hideMetadata = true } }

// ReflowFrom includes earlier lines whose metadata presentation changed in the last source
// update. Repeated Blocks calls for that same source keep the answer (InCode also calls Blocks).
func (r *MarkdownRenderer) ReflowFrom(changedLine int) int {
	if r.hideMetadata && r.metadata.changed >= 0 {
		return min(changedLine, r.metadata.changed)
	}
	return changedLine
}

// WithDiagrams draws a fenced block a Diagrammer answers Ready for (a mermaid or vega-lite fence)
// as its picture. Without one, or until it answers, the block is drawn as code.
func WithDiagrams(d Diagrammer) MarkdownOption { return func(r *MarkdownRenderer) { r.diagrams = d } }

// WithCodeLanguages uses the same immutable provider catalog as source/Raw editors.
func WithCodeLanguages(catalog *highlight.Catalog) MarkdownOption {
	return func(r *MarkdownRenderer) {
		if catalog != nil {
			r.languages = catalog
		}
	}
}

// Diagrams is the Diagrammer the renderer draws diagram fences with; nil: none, drawn as code.
func (r *MarkdownRenderer) Diagrams() Diagrammer { return r.diagrams }

// Line spacing, as multiples of each font's size: one place to tune it, or to make it an option
// should a host need to.
const (
	proseLineHeight = 1.5  // the Rendered view's text
	codeLineHeight  = 1.35 // a code block's lines, and frontmatter's
	rawLineHeight   = 1.35 // the Raw view
)

// fence is a fenced code block's opening: its character, its length and its info string.
type fence struct {
	ch   byte
	n    int
	info string
}

// openFence reads a line as a fence opening (markdown.FenceOpens), with its info string.
func openFence(line string) (fence, bool) {
	ch, n, ok := markdown.FenceOpens(line)
	if !ok {
		return fence{}, false
	}
	return fence{ch: ch, n: n, info: strings.TrimSpace(strings.TrimLeft(line, " ")[n:])}, true
}

// closes reports whether line closes f (markdown.FenceCloses).
func (f fence) closes(line string) bool { return markdown.FenceCloses(line, f.ch, f.n) }

// Blocks are the fenced code blocks, each a block of its lines (an unclosed one runs to the end),
// and every other line alone.
func (r *MarkdownRenderer) Blocks(lines []string, from, to int) []Block {
	if r.hideMetadata {
		r.metadata.prepare(lines)
	}
	var out []Block
	front, _ := frontmatterEnd(lines)
	for i := 0; i < len(lines); {
		end := i + 1
		if i == 0 && front > 0 {
			end = front
		} else if r.hideMetadata && r.metadata.hidden[i] {
			for end < len(lines) && r.metadata.hidden[end] {
				end++
			}
		} else if r.hideMetadata && r.metadata.continued[i] {
			// The prefix is a comment begun on an earlier line, not a Markdown block opener.
		} else if f, ok := openFence(lines[i]); ok {
			for end < len(lines) && !f.closes(lines[end]) {
				end++
			}
			end = min(end+1, len(lines))
		} else if n := tableEnd(lines, i); n > 0 {
			end = n
		} else if startsIndentedCode(lines, i) {
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
	code := flow.Options{Width: width, WhiteSpace: flow.PreWrap, Color: th.Text, LineHeight: codeLineHeight}
	if front, closed := frontmatterEnd(lines); b.From == 0 && front > 0 {
		if r.hideMetadata && closed && !cursorInside {
			return BlockLayout{}
		}
		return r.layFrontmatter(b, lines, closed, code, cursorInside, t, th)
	}
	if r.hideMetadata && r.metadata.hidden[b.From] {
		if !cursorInside {
			return BlockLayout{}
		}
		return r.layIndented(b, lines, code, t, th)
	}
	if r.hideMetadata && r.metadata.continued[b.From] {
		if cursorInside {
			spans := rawSpans(b.From, lines[b.From], th.Mono, nil)
			p := flow.Lay(spans, code, t)
			return BlockLayout{Lines: []LineLayout{{Para: p, Spans: spans}}, Height: p.Height}
		}
		ll, extra := r.layLine(lines, b.From, width, t, th)
		ll.Y = extra
		return BlockLayout{Lines: []LineLayout{ll}, Height: extra + ll.Para.Height}
	}
	if f, ok := openFence(lines[b.From]); ok {
		return r.layFence(b, f, lines, code, cursorInside, t, th, host)
	}
	if tableEnd(lines, b.From) > 0 {
		return r.layTable(b, lines, width, cursorInside, t, th)
	}
	if startsIndentedCode(lines, b.From) {
		return r.layIndented(b, lines, code, t, th)
	}
	ln := b.From
	if cursorInside {
		code.LineHeight = rawLineHeight
		spans := rawSpans(ln, lines[ln], th.Mono, nil)
		p := flow.Lay(spans, code, t)
		return BlockLayout{Lines: []LineLayout{{Para: p, Spans: spans}}, Height: p.Height}
	}
	ll, extra := r.layLine(lines, ln, width, t, th)
	ll.Y = extra
	return BlockLayout{Lines: []LineLayout{ll}, Height: extra + ll.Para.Height}
}

// layLine lays a prose line out off the cursor: a rule as a rule, a quote with its bars, a list
// item hanging after its bullet, any other line as its Markdown. extra is room above a heading.
func (r *MarkdownRenderer) layLine(lines []string, ln int, width float32, t *gui.TextShaper, th Theme) (LineLayout, float32) {
	line := lines[ln]
	em := th.Prose.Size
	opts := flow.Options{Width: width, WhiteSpace: flow.PreWrap, Color: mix(th.Text, th.Background, 0.9), LineHeight: proseLineHeight}
	rule := or(th.Marker, th.Muted)
	if isRule(lines, ln) {
		spans := []flow.Span{{Text: line, Font: th.Prose, Line: ln, Hidden: true}}
		p := flow.Lay(spans, opts, t)
		bar := Mark{Rect: gui.Rect{Y: p.Height / 2, W: width, H: 1}, Color: mix(rule, th.Background, 0.5)}
		return LineLayout{Para: p, Spans: spans, Marks: []Mark{bar}}, 0
	}
	var spans []flow.Span
	depth, off := quoteDepth(line)
	if depth > 0 {
		spans = append(spans, flow.Span{Text: line[:off], Font: th.Prose, Line: ln, Hidden: true})
		opts.Left = float32(depth) * em * 0.9
		opts.Color = mix(th.Text, th.Background, 0.75)
	}
	body, level, hang := r.bodySpans(ln, line[off:], t, th)
	for i := range body {
		body[i].Col += clusters(line[:off])
	}
	spans = append(spans, body...)
	if r.hideMetadata {
		spans = r.metadata.mask(spans, ln)
	}
	if hang > 0 {
		opts.Left += em * 0.3
		opts.Hang = hang
	}
	p := flow.Lay(spans, opts, t)
	ll := LineLayout{Para: p, Spans: spans}
	for k := range depth {
		ll.Marks = append(ll.Marks, Mark{Rect: gui.Rect{X: float32(k)*em*0.9 + em*0.15, W: 3, H: p.Height}, Color: mix(rule, th.Background, 0.6)})
	}
	extra := float32(0)
	if level > 0 {
		extra = em * 0.4
	}
	return ll, extra
}

// bodySpans are a line's spans past any quote marks: a list item's, which hangs by hang, or any
// line's, a heading at level.
func (r *MarkdownRenderer) bodySpans(ln int, text string, t *gui.TextShaper, th Theme) (spans []flow.Span, level int, hang float32) {
	if indent, marker, ok := tuiwidget.MarkdownListItem(text); ok {
		spans = r.listSpans(ln, text, indent, marker, th)
		for _, s := range spans {
			if s.Col >= len(indent)+len(marker) {
				break
			}
			hang += spanWidth(s, t)
		}
		return spans, 0, hang
	}
	spans, level = r.lineSpans(ln, text, th)
	return spans, level, 0
}

// spanWidth is how wide s is drawn: its spaces at SpaceWidth (a tab four), else as shaped.
func spanWidth(s flow.Span, t *gui.TextShaper) float32 {
	if s.SpaceWidth > 0 && strings.Trim(s.Text, " \t") == "" {
		w := float32(0)
		for _, c := range s.Text {
			w += s.SpaceWidth
			if c == '\t' {
				w += 3 * s.SpaceWidth
			}
		}
		return w
	}
	m := t.Measure(s.Text, s.Font)
	return m.Width()
}

// isRule reports whether line ln is a thematic break: "---", "***" or "___", three or more. A
// "---" right under a line of text underlines it instead (a setext heading), so is left as text.
func isRule(lines []string, ln int) bool {
	s := strings.TrimSpace(lines[ln])
	if !markdown.ThematicBreak(s) || isListItem(lines[ln]) {
		return false
	}
	return s[0] != '-' || ln == 0 || isBlank(lines[ln-1])
}

// quoteDepth reads a line's blockquote marks: how many '>' (each with the space after it) before
// its text, and where the text starts.
func quoteDepth(line string) (depth, off int) {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	for i < len(line) && line[i] == '>' {
		depth++
		i++
		if i < len(line) && line[i] == ' ' {
			i++
		}
	}
	if depth == 0 {
		return 0, 0
	}
	return depth, i
}

// frontmatterEnd is where a document's frontmatter ends, past its closing line, as the Raw
// view's highlighter reads it: a "---" first line, closed by "---" or "...". Unclosed (while it
// is being written), it runs to the end. 0: none.
func frontmatterEnd(lines []string) (end int, closed bool) {
	if len(lines) == 0 || !markdown.FrontmatterOpens(lines[0]) {
		return 0, false
	}
	for k := 1; k < len(lines); k++ {
		if markdown.FrontmatterCloses(lines[k]) {
			return k + 1, true
		}
	}
	return len(lines), false
}

// layFrontmatter lays frontmatter out as a quiet block: its keys and values small, monospace,
// its "---" lines hidden off the cursor.
func (r *MarkdownRenderer) layFrontmatter(b Block, lines []string, closed bool, opts flow.Options, inside bool, t *gui.TextShaper, th Theme) BlockLayout {
	bl := BlockLayout{Background: th.CodeBackground}
	font := th.Mono
	font.Size *= 0.88
	key, val := or(th.Emph, th.Muted), or(th.Quiet, th.Muted)
	var y float32
	for ln := b.From; ln < b.To; ln++ {
		line := lines[ln]
		var spans []flow.Span
		switch {
		case inside:
			spans = rawSpans(ln, line, th.Mono, nil)
		case ln == b.From || (closed && ln == b.To-1):
			spans = []flow.Span{{Text: line, Font: font, Line: ln, Hidden: true}}
		default:
			colon := strings.IndexByte(line, ':') // a key, as Raw reads one: not a "- " list entry
			if strings.HasPrefix(strings.TrimLeft(line, " "), "-") {
				colon = -1
			}
			spans = rawSpans(ln, line, font, func(col int) color.NRGBA {
				if col < colon {
					return key
				}
				return val
			})
		}
		p := flow.Lay(spans, opts, t)
		bl.Lines = append(bl.Lines, LineLayout{Para: p, Spans: spans, Y: y})
		y += p.Height
	}
	bl.Height = y
	return bl
}

// layFence lays a fenced block out: its lines in monospace on a tinted ground, the fence lines
// dimmed, or a diagram's picture when the Diagrammer has one.
func (r *MarkdownRenderer) layFence(b Block, f fence, lines []string, opts flow.Options, inside bool, t *gui.TextShaper, th Theme, host DiagramHost) BlockLayout {
	bl := BlockLayout{Background: th.CodeBackground}
	if !inside && r.diagrams != nil && host != nil && f.info != "" {
		src := strings.Join(lines[min(b.From+1, b.To):max(b.To-1, b.From+1)], "\n")
		lang, _, _ := strings.Cut(f.info, " ")
		// at the shaper's scale: what a diagram measures is what the window draws
		a := host.Diagram(r.diagrams, b, DiagramRequest{Lang: lang, Src: src, Width: opts.Width, Scale: t.Scale(), Theme: diagramFonts(th, t.Fonts())})
		// Ready, or Pending with the block's last picture: draw the picture. Otherwise the code.
		if a.State != Declined && a.Pic != nil {
			bl.Picture, bl.PictureSize, bl.Height = a.Pic, a.Size, a.Size.H
			bl.Background.A = 0
			return bl
		}
	}
	dim := th.Mono
	dim.Size *= 0.85
	var source highlight.Source
	if words := strings.Fields(f.info); len(words) > 0 {
		if d, ok := r.languages.DefinitionForLanguage(words[0]); ok && !d.DocumentAdapter {
			source = d.NewSource(r.languages)
		}
	}
	state := highlight.State(0)
	var y float32
	for ln := b.From; ln < b.To; ln++ {
		isFence := ln == b.From || (ln == b.To-1 && f.closes(lines[ln]) && ln > b.From)
		var spans []flow.Span
		switch {
		case isFence && !inside:
			spans = fenceSpans(ln, lines[ln], dim, th)
		default:
			if !isFence && source.Highlighter != nil {
				offset := min(len(lines[ln])-len(strings.TrimLeft(lines[ln], " ")), len(lines[b.From])-len(strings.TrimLeft(lines[b.From], " ")))
				hl, next := source.Highlighter.HighlightBlock(lines[ln][offset:], state)
				hl = append([]highlight.Span(nil), hl...)
				state = next
				for i := range hl {
					hl[i].Start += offset
					hl[i].End += offset
				}
				colours := sourceColours(lines[ln], hl, th)
				spans = rawSpans(ln, lines[ln], th.Mono, func(col int) color.NRGBA { return colours[col] })
			} else {
				spans = rawSpans(ln, lines[ln], th.Mono, nil)
			}
		}
		p := flow.Lay(spans, opts, t)
		bl.Lines = append(bl.Lines, LineLayout{Para: p, Spans: spans, Y: y})
		y += p.Height
	}
	bl.Height = y
	return bl
}

// sourceColours resolves per-grapheme syntax highlighting colors for a line of text.
func sourceColours(line string, spans []highlight.Span, th Theme) []color.NRGBA {
	var out []color.NRGBA
	at, si := 0, 0
	for cluster := range tui.Graphemes(line) {
		for si < len(spans) && spans[si].End <= at {
			si++
		}
		colour := th.Text
		if si < len(spans) && spans[si].Start <= at && at < spans[si].End {
			kind := int(spans[si].Style)
			if kind < len(th.Syntax) && th.Syntax[kind].A != 0 {
				colour = th.Syntax[kind]
			}
		}
		out = append(out, colour)
		at += len(cluster)
	}
	return out
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

// InCode reports whether line ln is inside a code block, fenced or indented, as the renderer
// lays the lines out: there "- x" is code, not a list item.
func (r *MarkdownRenderer) InCode(lines []string, ln int) bool {
	if front, _ := frontmatterEnd(lines); ln < front {
		return true
	}
	for _, b := range r.Blocks(lines, 0, len(lines)) { // the whole document: a block's From is its start
		if b.From <= ln && ln < b.To {
			_, fenced := openFence(lines[b.From])
			return fenced || startsIndentedCode(lines, b.From)
		}
	}
	return false
}

// fenceSpans draw a fence line off the cursor: its fence characters hidden, an info string
// muted. The line keeps its clusters, so the caret still maps onto it.
func fenceSpans(ln int, line string, font gui.Font, th Theme) []flow.Span {
	s := strings.TrimLeft(line, " ")
	n := len(line) - len(s)
	for n < len(line) && (line[n] == '`' || line[n] == '~') {
		n++
	}
	spans := []flow.Span{{Text: line[:n], Font: font, Line: ln, Hidden: true}}
	if n < len(line) {
		spans = append(spans, flow.Span{Text: line[n:], Font: font, Color: th.Muted, Line: ln, Col: clusters(line[:n])})
	}
	return spans
}

// startsIndentedCode reports whether line i starts an indented code block: indented as code, at
// the top or after a blank line, and not inside a list, where indentation nests an item or
// continues one.
func startsIndentedCode(lines []string, i int) bool {
	if !indentedCode(lines[i]) || (i > 0 && !isBlank(lines[i-1])) {
		return false
	}
	for j := i - 1; j >= 0; j-- {
		switch {
		case isBlank(lines[j]):
		case isListItem(lines[j]):
			return false
		case lines[j][0] == ' ' || lines[j][0] == '\t':
		default:
			return true
		}
	}
	return true
}

// isListItem reports whether the line begins with a valid Markdown list item marker.
func isListItem(line string) bool {
	_, _, ok := tuiwidget.MarkdownListItem(line)
	return ok
}

// indentedCode reports a line indented as code: four spaces or a tab before any text.
func indentedCode(line string) bool {
	if isBlank(line) {
		return false
	}
	return strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ")
}

// isBlank reports whether the line contains only whitespace characters.
func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

// mdAttr is how one byte of a line is drawn.
type mdAttr struct {
	hidden                           bool
	bold, italic, code, strike, link bool
	wiki, tag                        bool // an Obsidian [[wikilink]] or ![[embed]]; a #tag
	heading                          int
}

// lineSpans are a line's spans with its Markdown drawn: the marks hidden, the rest styled. level
// is the line's heading level, 0 for none.
func (r *MarkdownRenderer) lineSpans(ln int, line string, th Theme) ([]flow.Span, int) {
	if line == "" {
		return []flow.Span{{Font: th.Prose, Line: ln}}, 0
	}
	if indent, marker, ok := tuiwidget.MarkdownListItem(line); ok {
		return r.listSpans(ln, line, indent, marker, th), 0
	}
	src := []byte(line)
	doc := markdown.Parse(src, markdown.WithGFM(), markdown.WithObsidian())
	attrs := make([]mdAttr, len(src))
	level := 0
	var walk func(n *markdown.Node)
	walk = func(n *markdown.Node) {
		switch n.Kind {
		case markdown.KindHeading:
			level = n.Level()
			for i := n.Span.Start; i < n.Span.End; i++ {
				attrs[i].heading = n.Level()
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
		case markdown.KindWikilink, markdown.KindEmbed:
			// its one child is what it shows (the alias, else the target): the brackets and a
			// target behind an alias are hidden
			markRange(attrs, n.Span.Start, n.Span.End, n, func(a *mdAttr) { a.wiki = true })
		case markdown.KindTag:
			for i := n.Span.Start; i < n.Span.End; i++ {
				attrs[i].tag = true
			}
		case markdown.KindText:
			// a backslash escape draws the character it escapes, as CommonMark renders it: its
			// backslash is a mark, hidden off the cursor. Where the parser takes no escapes, the
			// backslashes are text: a code span's, and an autolink's, whose Literal is only its
			// decoded entities
			if n.Literal != nil && (n.Parent == nil || n.Parent.Kind != markdown.KindAutolink) {
				for i := n.Span.Start; i+1 < n.Span.End; i++ {
					if src[i] == '\\' && isASCIIPunct(src[i+1]) {
						hide(attrs, i, i+1)
						i++
					}
				}
			}
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

// bullets are the bullet glyphs by nesting level, the deepest repeating.
var bullets = [...]string{"•", "◦", "▪"}

// listSpans draw a list item off the cursor: its indentation as room, a bullet as a bullet glyph
// for its level (a number kept as written), and its text as any line's, read without the
// indentation so a deep item is never code. Every source cluster keeps a cluster in the spans.
func (r *MarkdownRenderer) listSpans(ln int, line, indent, marker string, th Theme) []flow.Span {
	var spans []flow.Span
	em := th.Prose.Size
	if indent != "" {
		spans = append(spans, flow.Span{Text: indent, Font: th.Prose, Line: ln, SpaceWidth: em * 0.7})
	}
	col := len(indent) // indentation and markers are one byte a cluster
	bullet := strings.TrimRight(marker, " ")
	if strings.ContainsRune("-*+", rune(marker[0])) {
		level := 0
		for _, c := range indent {
			level++
			if c == '\t' {
				level += 3
			}
		}
		level = (level / 2) % len(bullets)
		bf := th.Prose
		bf.Size *= 1.15
		spans = append(spans, flow.Span{Text: bullets[level], Font: bf, Color: or(th.Marker, th.Muted), Line: ln, Col: col})
		bullet = bullet[1:]
		col++
	}
	if bullet != "" { // a number, or a task's box
		spans = append(spans, flow.Span{Text: bullet, Font: th.Prose, Color: or(th.Marker, th.Muted), Line: ln, Col: col})
		col += len(bullet)
	}
	if gap := marker[len(strings.TrimRight(marker, " ")):]; gap != "" {
		spans = append(spans, flow.Span{Text: gap, Font: th.Prose, Line: ln, Col: col, SpaceWidth: em * 0.45})
		col += len(gap)
	}
	if rest := line[col:]; rest != "" {
		body, _ := r.lineSpans(ln, rest, th)
		for i := range body {
			body[i].Col += col
		}
		spans = append(spans, body...)
	}
	return spans
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

// isASCIIPunct reports whether c is ASCII punctuation: what a backslash escapes.
func isASCIIPunct(c byte) bool {
	return strings.IndexByte("!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~", c) >= 0
}

// hide marks a range of byte attributes as hidden in Rendered mode.
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
	// the Raw view's colours: the innermost construct's, a link's over all
	switch {
	case a.link:
		sp.Color, sp.Underline = or(th.Link, th.Accent), true
	case a.wiki:
		sp.Color, sp.Underline = or(th.Wiki, or(th.Link, th.Accent)), true
	case a.tag:
		sp.Color = th.Tag
	case a.code:
		sp.Color = th.Code
	case a.heading > 0:
		sp.Color = th.Heading
	case a.bold:
		sp.Color = th.Strong
	case a.italic:
		sp.Color = th.Emph
	case a.strike:
		sp.Color = th.Quiet
	}
	sp.Font = f
	return sp
}
