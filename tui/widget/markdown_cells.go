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
// typed in is laid as its source. A table is boxed, its columns narrowed to the width (FitColumns),
// or, too many to narrow, laid at their own widths. Diagrams and images are drawn as their source
// (later steps draw them).
type MarkdownCellRenderer struct {
	MarkdownBlocks
	// Languages highlights a fenced block in its info string's language, as gui's Markdown
	// renderer and the HTML page do; nil: code in the code look only.
	Languages *highlight.Catalog
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
		text := func(int) style.Style { return th.Text }
		for ln := b.From; ln < b.To; ln++ {
			lb.source(&cb, ln, lines[ln], text)
		}
	case b.From == 0 && r.frontmatterHidden(lines):
		// closed frontmatter, hidden: no row of its own (a fold)
	case r.HideMetadata && r.Meta.Hidden(b.From):
		// a run of comments, hidden: no row
	default:
		if f, ok := mdOpenFence(lines[b.From]); ok {
			r.fenced(&lb, &cb, b, lines, f)
		} else if end := MarkdownTableEnd(lines, b.From); end > 0 {
			end = min(end, b.To)
			r.table(&lb, &cb, b.From, end, lines)
			for ln := end; ln < b.To; ln++ {
				r.line(&lb, &cb, ln, lines[ln])
			}
		} else if MarkdownStartsIndentedCode(lines, b.From) {
			code := lb.role(MarkdownCodeStyle)
			for ln := b.From; ln < b.To; ln++ {
				lb.source(&cb, ln, lines[ln], func(int) style.Style { return code })
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

// tableCell is one shown cluster of a table's cell, and the source text it measures by.
type tableCell struct {
	RowCell
	src string
}

// table lays the GFM table on lines [from, end) boxed: its header row, the rule its delimiter row
// stands for (that line's row), its body rows. Each cell shows its Markdown, aligned as the
// delimiter says and wrapped in its column, a bare <br> breaking it. The columns are narrowed to the
// width by FitColumns or, too many to narrow, laid at their natural widths: the block is then wider
// than the width.
func (r *MarkdownCellRenderer) table(lb *mdCellLines, cb *CellBlock, from, end int, lines []string) {
	delim, _ := MarkdownGFMRow(lines[from+1])
	aligns, _ := MarkdownDelimiterAligns(delim)
	n := len(aligns)
	var rows [][][][]tableCell // per row line (the header first): per column, its <br> segments
	var lns []int
	natural := make([]int, n)
	for ln := from; ln < end; ln++ {
		if ln == from+1 {
			continue
		}
		cells := lb.tableRow(ln, lines[ln], n, ln == from)
		for ci, segs := range cells {
			for _, seg := range segs {
				w := 0
				for _, c := range seg {
					w += c.W
				}
				natural[ci] = max(natural[ci], w)
			}
		}
		rows, lns = append(rows, cells), append(lns, ln)
	}
	cols, frame := natural, 4+3*max(n-1, 0) // the box's "│ " and " │", and " │ " between cells
	sum := frame
	for _, w := range natural {
		sum += w
	}
	if sum > lb.width {
		if fit := FitColumns(natural, lb.width-frame); fit != nil {
			cols = fit
		}
	}
	box := lb.role(MarkdownQuietStyle)
	deco := func(row *CellRow, s string, st style.Style) {
		row.Cells = append(row.Cells, RowCell{Text: s, W: 1, Style: st, Line: -1})
	}
	rule := func(left, joint, right string, anchor int) {
		row := CellRow{Anchor: anchor, Unnumbered: anchor < 0}
		deco(&row, left, box)
		for ci, w := range cols {
			if ci > 0 {
				deco(&row, joint, box)
			}
			for range w + 2 {
				deco(&row, "─", box)
			}
		}
		deco(&row, right, box)
		cb.Rows = append(cb.Rows, row)
	}
	rule("┌", "┬", "┐", -1)
	for i, cells := range rows {
		lined := make([][][]RowCell, n) // each column's display lines: its segments wrapped to it
		h := 1
		for ci := range n {
			for _, seg := range cells[ci] {
				srcs := make([]string, len(seg))
				for k, c := range seg {
					srcs[k] = c.src
				}
				for _, rr := range wrapRanges(srcs, cols[ci], lb.measure) {
					var line []RowCell
					for _, c := range seg[rr[0]:rr[1]] {
						line = append(line, c.RowCell)
					}
					lined[ci] = append(lined[ci], line)
				}
			}
			h = max(h, len(lined[ci]))
		}
		for k := range h {
			row := CellRow{Anchor: -1}
			deco(&row, "│", box)
			deco(&row, " ", lb.th.Text)
			for ci := range n {
				if ci > 0 {
					deco(&row, " ", lb.th.Text)
					deco(&row, "│", box)
					deco(&row, " ", lb.th.Text)
				}
				var line []RowCell
				if k < len(lined[ci]) {
					line = lined[ci][k]
				}
				pad := cols[ci]
				for _, c := range line {
					pad -= c.W
				}
				pad = max(pad, 0)
				left := 0
				switch aligns[ci] {
				case style.AlignRight:
					left = pad
				case style.AlignCenter:
					left = pad / 2
				}
				for range left {
					deco(&row, " ", lb.th.Text)
				}
				row.Cells = append(row.Cells, line...)
				for range pad - left {
					deco(&row, " ", lb.th.Text)
				}
			}
			deco(&row, " ", lb.th.Text)
			deco(&row, "│", box)
			if k == 0 && onlyDecorations(row.Cells) { // a row line of empty cells stands for its line
				row.Anchor = lns[i]
			}
			cb.Rows = append(cb.Rows, row)
		}
		if i == 0 {
			rule("├", "┼", "┤", from+1) // the delimiter row's
		}
	}
	rule("└", "┴", "┘", -1)
}

// tableRow is line ln's cells, n of them (a missing one empty, one past n dropped): each its
// clusters not hidden, in their look (a header's bold), in the segments a bare <br> ends.
func (lb *mdCellLines) tableRow(ln int, line string, n int, head bool) [][][]tableCell {
	row, shift := MarkdownGFMRow(line)
	spans, _ := MarkdownTableCells(row)
	attrs, _ := MarkdownLineAttrs(line)
	var cs []string
	var offs []int
	off := 0
	for c := range tui.Graphemes(row) {
		cs, offs = append(cs, c), append(offs, off)
		off += len(c)
	}
	out := make([][][]tableCell, n)
	for ci := range out {
		out[ci] = [][]tableCell{nil}
	}
	for ci, sp := range spans {
		if ci >= n {
			break
		}
		lastBr := 0
		for k := sp[0]; k < sp[1]; k++ {
			at := offs[k] - shift // the byte in line; the pipes MarkdownGFMRow added are none of its
			if at < 0 || at >= len(line) {
				continue
			}
			a := attrs[at]
			if a.Br > 0 {
				if a.Br != lastBr { // a <br>'s first byte: two in a row are two
					lastBr = a.Br
					out[ci] = append(out[ci], nil)
				}
				continue
			}
			if a.Hidden {
				continue
			}
			st := lb.styleOf(a)
			if head {
				st = st.Bold(true)
			}
			segs := out[ci]
			segs[len(segs)-1] = append(segs[len(segs)-1], tableCell{lb.cell(cs[k], st, ln, k-shift), cs[k]})
		}
	}
	return out
}

// frontmatterHidden reports whether lines open with closed frontmatter the view hides.
func (r *MarkdownCellRenderer) frontmatterHidden(lines []string) bool {
	_, closed := MarkdownFrontmatterEnd(lines)
	return r.HideMetadata && closed
}

// fenced lays a fenced code block: its opening fence as its info string, muted (no row when it has
// none), its lines in the code look, highlighted in the info string's language when Languages has
// it (its state carried from line to line), its closing fence no row.
func (r *MarkdownCellRenderer) fenced(lb *mdCellLines, cb *CellBlock, b Block, lines []string, f mdFence) {
	open := lines[b.From]
	lead := len(open) - len(strings.TrimLeft(open, " "))
	rest := open[lead+f.n:]
	info := strings.TrimSpace(rest)
	if info != "" {
		at := lead + f.n + strings.Index(rest, info) // the info string's own bytes, after the fence's
		muted := func(int) style.Style { return lb.role(MarkdownQuietStyle) }
		lb.cells(cb, b.From, open[at:at+len(info)], clusterIndex(open, at), muted, nil, nil, nil, nil)
	}
	var hl highlight.Highlighter
	if words := strings.Fields(info); len(words) > 0 {
		if d, ok := r.Languages.DefinitionForLanguage(words[0]); ok && !d.DocumentAdapter {
			hl = d.NewSource(r.Languages).Highlighter
		}
	}
	code := lb.role(MarkdownCodeStyle)
	var state highlight.State
	for ln := b.From + 1; ln < b.To; ln++ {
		line := lines[ln]
		if ln == b.To-1 && markdown.FenceCloses(line, f.ch, f.n) {
			break // the closing fence: no row
		}
		if hl == nil {
			lb.source(cb, ln, line, func(int) style.Style { return code })
			continue
		}
		// the fence's indentation is not the code's, as gui's fenced block reads it
		offset := min(len(line)-len(strings.TrimLeft(line, " ")), lead)
		spans, next := hl.HighlightBlock(line[offset:], state)
		spans, state = append([]highlight.Span(nil), spans...), next
		lb.source(cb, ln, line, func(off int) style.Style { return lb.syntax(spans, off-offset, code) })
	}
}

// line lays one prose line: a list item's marker as its bullet, its later rows hung under its text;
// a quote's marks as bars, on every row; an empty line as its own row; and the rest with its
// Markdown drawn.
func (r *MarkdownCellRenderer) line(lb *mdCellLines, cb *CellBlock, ln int, line string) {
	if strings.TrimSpace(line) == "" {
		cb.Rows = append(cb.Rows, CellRow{Anchor: ln})
		return
	}
	var lead, cont []RowCell // drawn before the line's text, and before each later row of it
	start := 0               // the byte the text starts at
	if indent, marker, ok := MarkdownListItem(line); ok {
		col := 0
		for _, c := range indent {
			lead = append(lead, lb.cell(string(c), lb.th.Text, ln, col))
			col++
		}
		glyph := strings.TrimRight(marker, " ")
		if strings.ContainsRune("-*+", rune(marker[0])) {
			glyph = MarkdownBullet(indent) + glyph[1:]
		}
		for c := range tui.Graphemes(glyph) {
			lead = append(lead, lb.cell(c, lb.role(MarkdownMarkerStyle), ln, col))
			col++
		}
		for range len(marker) - len(strings.TrimRight(marker, " ")) {
			lead = append(lead, lb.cell(" ", lb.th.Text, ln, col))
			col++
		}
		for _, c := range lead { // the hanging indent: as wide as the item's lead
			for range c.W {
				cont = append(cont, RowCell{Text: " ", W: 1, Style: lb.th.Text, Line: -1})
			}
		}
		start = len(indent) + len(marker)
	} else if depth, off := MarkdownQuoteDepth(line); depth > 0 {
		at := 0 // past the marks read so far
		for range depth {
			at += strings.IndexByte(line[at:], '>')
			bar := lb.role(MarkdownMarkerStyle)
			lead = append(lead, RowCell{Text: "│", W: 1, Style: bar, Line: ln, Col: clusterIndex(line, at)}, RowCell{Text: " ", W: 1, Style: lb.th.Text, Line: -1})
			cont = append(cont, RowCell{Text: "│", W: 1, Style: bar, Line: -1}, RowCell{Text: " ", W: 1, Style: lb.th.Text, Line: -1})
			at++
		}
		start = off
	}
	text := line[start:]
	attrs, _ := MarkdownLineAttrs(text)
	var mask []bool
	if r.HideMetadata {
		mask, _ = r.Meta.HiddenClusters(ln)
	}
	lb.cells(cb, ln, text, clusterIndex(line, start), func(off int) style.Style { return lb.styleOf(attrs[off]) }, func(off, col int) bool {
		return attrs[off].Hidden || attrs[off].Br > 0 || (col < len(mask) && mask[col])
	}, func(off int) int { return attrs[off].Br }, lead, cont)
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

// syntax is the look of the byte at off of a highlighted line: its span's syntax style over base,
// base where no span covers it.
func (lb *mdCellLines) syntax(spans []highlight.Span, off int, base style.Style) style.Style {
	for _, sp := range spans {
		if off < sp.Start {
			break
		}
		if off < sp.End {
			if int(sp.Style) < len(lb.th.Syntax) {
				return lb.th.Syntax[sp.Style].Inherit(base)
			}
			break
		}
	}
	return base
}

// cell is source cluster c of line ln at col, in st: a tab drawn as blank cells, as many as the
// editor's measure gives it, never a tab on the screen.
func (lb *mdCellLines) cell(c string, st style.Style, ln, col int) RowCell {
	shown := c
	if c == "\t" {
		shown = " "
	}
	return RowCell{Text: shown, W: max(lb.measure(c), 1), Style: st, Line: ln, Col: col}
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

// source lays a line as it is, each byte in look(off), wrapped.
func (lb *mdCellLines) source(cb *CellBlock, ln int, line string, look func(off int) style.Style) {
	if line == "" {
		cb.Rows = append(cb.Rows, CellRow{Anchor: ln})
		return
	}
	lb.cells(cb, ln, line, 0, look, nil, nil, nil, nil)
}

// cells lays text (a line's, from its cluster base) as rows: lead first, then each cluster not
// hidden, in its look, wrapped at the width before the word that would cross it (or inside a word,
// or a lead, wider than the row); each later row starts with cont (a hanging indent, a quote's
// bars) when it leaves room. A bare <br> (brk: its id at a byte, 0 none) ends its row; a row it
// leaves with nothing of the line's shown holds the <br>, a blank cell, so the caret has a place on
// it. The line's first row, with no cell of its own shown, is its Anchor's.
func (lb *mdCellLines) cells(cb *CellBlock, ln int, text string, base int, look func(off int) style.Style, hidden func(off, col int) bool, brk func(off int) int, lead, cont []RowCell) {
	contW := 0
	for _, c := range cont {
		contW += c.W
	}
	if contW >= lb.width {
		cont, contW = nil, 0
	}
	first := len(cb.Rows)
	row := CellRow{Anchor: -1}
	w, from := 0, 0 // the row's width, and where its own cells start (past cont)
	lastSpace := -1 // the index in row.Cells after the last space: where a wrap may break
	next := func(carry []RowCell) {
		cb.Rows = append(cb.Rows, row)
		row = CellRow{Anchor: -1, Cells: append(append([]RowCell(nil), cont...), carry...)}
		w, from, lastSpace = contW, len(cont), -1
		for _, c := range carry {
			w += c.W
		}
	}
	put := func(c RowCell, space bool) {
		if w+c.W > lb.width && len(row.Cells) > from {
			var carry []RowCell
			if !space && lastSpace > from && lastSpace < len(row.Cells) {
				carry = append(carry, row.Cells[lastSpace:]...)
				row.Cells = row.Cells[:lastSpace]
			}
			next(carry)
		}
		row.Cells = append(row.Cells, c)
		w += c.W
		if space {
			lastSpace = len(row.Cells)
		}
	}
	shown := func() bool { return !onlyDecorations(row.Cells) }
	brCol := -1 // the column of the <br> waiting to end its row
	breakRow := func() {
		if !shown() {
			row.Cells = append(row.Cells, RowCell{Text: " ", W: 1, Style: lb.th.Text, Line: ln, Col: brCol})
		}
		next(nil)
	}
	for _, c := range lead {
		put(c, false)
	}
	off, col, lastBr := 0, base, 0
	for c := range tui.Graphemes(text) {
		if brk != nil {
			if id := brk(off); id > 0 && id != lastBr { // a <br>'s first byte: two in a row are two
				lastBr = id
				if brCol >= 0 {
					breakRow()
				}
				brCol = col
			}
		}
		if hidden == nil || !hidden(off, col) {
			if brCol >= 0 {
				breakRow()
				brCol = -1
			}
			put(lb.cell(c, look(off), ln, col), c == " ")
		}
		off += len(c)
		col++
	}
	if brCol >= 0 && !shown() { // a trailing <br><br>: the empty row the last one leaves
		row.Cells = append(row.Cells, RowCell{Text: " ", W: 1, Style: lb.th.Text, Line: ln, Col: brCol})
	}
	if len(cb.Rows) == first && !shown() {
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
