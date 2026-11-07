package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// The syntax styles a Markdown highlighter gives each construct, which a Rendered view's looks
// follow so the two views read alike: a heading, strong, emphasis, code, a link, a marker (a
// bullet, a quote's bar, a rule), what is there to be skipped, a [[wikilink]] and a #tag. Both
// editors read their Rendered looks by them.
const (
	MarkdownHeadingStyle = highlight.Keyword
	MarkdownStrongStyle  = highlight.DataType
	MarkdownEmphStyle    = highlight.Attribute
	MarkdownCodeStyle    = highlight.String
	MarkdownLinkStyle    = highlight.Import
	MarkdownMarkerStyle  = highlight.Operator
	MarkdownQuietStyle   = highlight.Comment
	MarkdownWikiStyle    = highlight.Function
	MarkdownTagStyle     = highlight.Constant
)

// MarkdownCellRenderer draws Markdown in the cell Editor's Rendered view: headings, emphasis,
// strong, code spans, links, wikilinks and tags with their marks hidden and every other cluster
// kept where its source is; a list item's marker as its bullet; a quote's mark as a bar; a code
// block's lines in the code look; frontmatter and comments hidden (HideMetadata). A block being
// typed in is laid as its source. Tables, diagrams and images are drawn as their source (later
// steps draw them).
type MarkdownCellRenderer struct {
	MarkdownBlocks
}

var (
	_ CellRenderer      = (*MarkdownCellRenderer)(nil)
	_ MarkdownStructure = (*MarkdownCellRenderer)(nil)
)

// LayOut lays block b out to width cells.
func (r *MarkdownCellRenderer) LayOut(b Block, lines []string, width int, measure func(string) int, typing bool, th CellTheme) CellBlock {
	width = max(width, 1)
	var cb CellBlock
	lb := mdCellLines{width: width, measure: measure, th: th}
	switch {
	case typing:
		for ln := b.From; ln < b.To; ln++ {
			lb.source(&cb, ln, lines[ln], th.Text)
		}
	case b.From == 0 && r.frontmatterHidden(lines):
		// closed frontmatter, hidden: no row of its own (a fold)
	case r.HideMetadata && r.Meta.Hidden(b.From):
		// a run of comments, hidden: no row
	default:
		if f, ok := mdOpenFence(lines[b.From]); ok {
			r.fenced(&lb, &cb, b, lines, f)
		} else if MarkdownTableEnd(lines, b.From) > 0 || MarkdownStartsIndentedCode(lines, b.From) {
			code := MarkdownStartsIndentedCode(lines, b.From)
			for ln := b.From; ln < b.To; ln++ {
				st := th.Text
				if code {
					st = lb.role(MarkdownCodeStyle)
				}
				lb.source(&cb, ln, lines[ln], st)
			}
		} else {
			for ln := b.From; ln < b.To; ln++ {
				r.line(&lb, &cb, ln, lines[ln])
			}
		}
	}
	for _, row := range cb.Rows {
		w := 0
		for _, c := range row.Cells {
			w += c.W
		}
		cb.Width = max(cb.Width, w)
	}
	return cb
}

// frontmatterHidden reports whether lines open with closed frontmatter the view hides.
func (r *MarkdownCellRenderer) frontmatterHidden(lines []string) bool {
	_, closed := MarkdownFrontmatterEnd(lines)
	return r.HideMetadata && closed
}

// fenced lays a fenced code block: its opening fence as its info string, muted (no row when it has
// none), its lines in the code look, its closing fence no row.
func (r *MarkdownCellRenderer) fenced(lb *mdCellLines, cb *CellBlock, b Block, lines []string, f mdFence) {
	open := lines[b.From]
	lead := len(open) - len(strings.TrimLeft(open, " "))
	info := strings.TrimSpace(open[lead+f.n:])
	if info != "" {
		col := clusterIndex(open, strings.Index(open[lead+f.n:], info)+lead+f.n)
		lb.cells(cb, b.From, open[strings.Index(open, info):], col, func(int) style.Style { return lb.role(MarkdownQuietStyle) }, nil)
	}
	for ln := b.From + 1; ln < b.To; ln++ {
		if ln == b.To-1 && markdown.FenceCloses(lines[ln], f.ch, f.n) {
			break // the closing fence: no row
		}
		lb.source(cb, ln, lines[ln], lb.role(MarkdownCodeStyle))
	}
}

// line lays one prose line: a list item's marker as its bullet, a quote's marks as a bar, an empty
// line as its own row, and the rest with its Markdown drawn.
func (r *MarkdownCellRenderer) line(lb *mdCellLines, cb *CellBlock, ln int, line string) {
	if strings.TrimSpace(line) == "" {
		cb.Rows = append(cb.Rows, CellRow{Anchor: ln})
		return
	}
	var lead []RowCell // drawn before the line's text: indentation, a bullet, a bar
	start := 0         // the byte the text starts at
	if indent, marker, ok := MarkdownListItem(line); ok {
		col := 0
		for _, c := range indent {
			lead = append(lead, RowCell{Text: string(c), W: max(lb.measure(string(c)), 1), Style: lb.th.Text, Line: ln, Col: col})
			col++
		}
		glyph := strings.TrimRight(marker, " ")
		if strings.ContainsRune("-*+", rune(marker[0])) {
			glyph = MarkdownBullet(indent) + glyph[1:]
		}
		for c := range tui.Graphemes(glyph) {
			lead = append(lead, RowCell{Text: c, W: max(lb.measure(c), 1), Style: lb.role(MarkdownMarkerStyle), Line: ln, Col: col})
			col++
		}
		for range len(marker) - len(strings.TrimRight(marker, " ")) {
			lead = append(lead, RowCell{Text: " ", W: 1, Style: lb.th.Text, Line: ln, Col: col})
			col++
		}
		start = len(indent) + len(marker)
	} else if depth, off := MarkdownQuoteDepth(line); depth > 0 {
		for i := range depth {
			lead = append(lead, RowCell{Text: "│", W: 1, Style: lb.role(MarkdownMarkerStyle), Line: ln, Col: clusterIndex(line, strings.IndexByte(line[i:], '>')+i)}, RowCell{Text: " ", W: 1, Style: lb.th.Text, Line: -1})
		}
		start = off
	}
	text := line[start:]
	attrs, _ := MarkdownLineAttrs(text)
	var mask []bool
	if r.HideMetadata {
		mask, _ = r.Meta.HiddenClusters(ln)
	}
	base := clusterIndex(line, start)
	lb.cells(cb, ln, text, base, func(off int) style.Style { return lb.styleOf(attrs[off]) }, func(off, col int) bool {
		return attrs[off].Hidden || attrs[off].Br > 0 || (col < len(mask) && mask[col])
	}, lead...)
}

// mdCellLines lays text into rows of cells at a width.
type mdCellLines struct {
	width   int
	measure func(string) int
	th      CellTheme
}

// role is a Markdown construct's look: its syntax style over the text's.
func (lb *mdCellLines) role(k highlight.Style) style.Style {
	return lb.th.Syntax[k].Inherit(lb.th.Text)
}

// styleOf is a byte's look by its attributes: the innermost construct's colour, as the Raw view's.
func (lb *mdCellLines) styleOf(a MarkdownAttr) style.Style {
	st := lb.th.Text
	switch {
	case a.Link:
		st = lb.role(MarkdownLinkStyle).Underline(true)
	case a.Wiki:
		st = lb.role(MarkdownWikiStyle).Underline(true)
	case a.Tag:
		st = lb.role(MarkdownTagStyle)
	case a.Code:
		st = lb.role(MarkdownCodeStyle)
	case a.Heading > 0:
		st = lb.role(MarkdownHeadingStyle).Bold(true)
	case a.Bold:
		st = lb.role(MarkdownStrongStyle)
	case a.Italic:
		st = lb.role(MarkdownEmphStyle)
	case a.Strike:
		st = lb.role(MarkdownQuietStyle)
	}
	if a.Bold {
		st = st.Bold(true)
	}
	if a.Italic {
		st = st.Italic(true)
	}
	if a.Strike {
		st = st.Strikethrough(true)
	}
	return st
}

// source lays a line as it is, in st, wrapped.
func (lb *mdCellLines) source(cb *CellBlock, ln int, line string, st style.Style) {
	if line == "" {
		cb.Rows = append(cb.Rows, CellRow{Anchor: ln})
		return
	}
	lb.cells(cb, ln, line, 0, func(int) style.Style { return st }, nil)
}

// cells lays text (a line's, from its cluster base) as rows: lead first, then each cluster not
// hidden, in its look, wrapped at the width before the word that would cross it (or inside a word
// wider than the row). A line with no cell shown is a row of its own (its Anchor).
func (lb *mdCellLines) cells(cb *CellBlock, ln int, text string, base int, look func(off int) style.Style, hidden func(off, col int) bool, lead ...RowCell) {
	row := CellRow{Anchor: -1, Cells: lead}
	w := 0
	for _, c := range lead {
		w += c.W
	}
	lastSpace := -1 // the index in row.Cells after the last space: where a wrap may break
	off, col := 0, base
	for c := range tui.Graphemes(text) {
		if hidden == nil || !hidden(off, col) {
			cw, shown := max(lb.measure(c), 1), c // the editor's measure counts a tab as its cells
			if c == "\t" {
				shown = " " // drawn blank, as wide as the measure says
			}
			if w+cw > lb.width && len(row.Cells) > 0 {
				carry := []RowCell(nil)
				if lastSpace > 0 && lastSpace < len(row.Cells) && c != " " {
					carry = append(carry, row.Cells[lastSpace:]...)
					row.Cells = row.Cells[:lastSpace]
				}
				cb.Rows = append(cb.Rows, row)
				row, w, lastSpace = CellRow{Anchor: -1, Cells: carry}, 0, -1
				for _, rc := range carry {
					w += rc.W
				}
			}
			row.Cells = append(row.Cells, RowCell{Text: shown, W: cw, Style: look(off), Line: ln, Col: col})
			w += cw
			if c == " " {
				lastSpace = len(row.Cells)
			}
		}
		off += len(c)
		col++
	}
	if len(row.Cells) == 0 || onlyDecorations(row.Cells) {
		row.Anchor = ln
	}
	cb.Rows = append(cb.Rows, row)
}

// onlyDecorations reports whether cells show none of a source line's clusters.
func onlyDecorations(cells []RowCell) bool {
	for _, c := range cells {
		if c.Line >= 0 {
			return false
		}
	}
	return true
}

// clusterIndex is the cluster byte off of s starts.
func clusterIndex(s string, off int) int {
	n, at := 0, 0
	for c := range tui.Graphemes(s) {
		if at >= off {
			return n
		}
		at += len(c)
		n++
	}
	return n
}
