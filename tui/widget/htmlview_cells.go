package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE CELL LAYOUT — an HTMLView in a terminal: the document's text, wrapped to the widget's
// width and read-only, one unit a cell. Headings are bold, links underlined, code tinted,
// preformatted text shaded; quotes and callouts carry a bar, list items a hanging marker, and a
// table's columns line up when they fit. Images show their alt text.

// hcl is one laid-out grapheme cluster: where it is drawn and which document position it is.
type hcl struct {
	text string
	x, w int
	pos  DocPos // the boundary before it
	end  DocPos // the boundary after it
	sp   *hspan // nil: a deco cluster of the layout's own (a quote bar, a marker, padding)
	deco bool   // never copied as text (a marker, a separator, padding)
}

// hrow is one row of the layout: a blank gap, or a block's wrapped line.
type hrow struct {
	block int // the block it belongs to (a gap row: the block after it)
	first bool
	cls   []hcl
	shade bool // a preformatted block's row: shaded across the width
	rule  bool
}

// cellHTML lays an HTMLView out in cells (HTMLLayout).
type cellHTML struct {
	measure func(string) int
	src     []byte
	doc     []hblock
	width   int
	rows    []hrow
	tops    []int // each block's first row (its gap excluded)
}

func newCellHTML(measure func(string) int) *cellHTML {
	return &cellHTML{measure: measure, width: 1}
}

var _ HTMLLayout = (*cellHTML)(nil)

// SetSource reads a new document and lays it out at the width it has.
func (c *cellHTML) SetSource(src []byte) {
	c.src = src
	c.doc = readHTMLDoc(src)
	c.layOut()
}

// SetWidth lays the document out again at w cells, when that is a change.
func (c *cellHTML) SetWidth(w float32) {
	n := max(int(w), 1)
	if n == c.width && c.rows != nil {
		return
	}
	c.width = n
	c.layOut()
}

func (c *cellHTML) Height() float32 { return float32(len(c.rows)) }

// layOut wraps every block to the width.
func (c *cellHTML) layOut() {
	c.rows, c.tops = c.rows[:0], make([]int, len(c.doc))
	widths := c.columnWidths()
	for bi := range c.doc {
		b := &c.doc[bi]
		if b.gap {
			c.rows = append(c.rows, hrow{block: bi})
		}
		c.tops[bi] = len(c.rows)
		c.layBlock(bi, b, widths[b.table])
	}
	if len(c.rows) == 0 {
		c.rows = append(c.rows, hrow{block: -1})
	}
}

// columnWidths are each table's column widths, in cells, when its columns fit side by side at
// the width; a table that does not fit gets none, and its rows wrap as text.
func (c *cellHTML) columnWidths() map[int][]int {
	out := map[int][]int{}
	natural := map[int][]int{}
	room := map[int]int{}
	for bi := range c.doc {
		b := &c.doc[bi]
		if b.kind != hRow {
			continue
		}
		cols := natural[b.table]
		for ci := range b.cellStarts {
			w := 0
			for _, sp := range b.spans[b.cellStarts[ci]:c.cellEnd(b, ci)] {
				w += c.measure(sp.text)
			}
			if ci >= len(cols) {
				cols = append(cols, 0)
			}
			cols[ci] = max(cols[ci], w)
		}
		natural[b.table] = cols
		room[b.table] = c.width - 2*b.quote - b.indent
	}
	for t, cols := range natural {
		sum := 3 * max(len(cols)-1, 0) // the " │ " between cells
		for _, w := range cols {
			sum += w
		}
		if sum <= room[t] {
			out[t] = cols
		}
	}
	return out
}

// cellEnd is the span past cell ci of row b (its separator, or the row's end).
func (c *cellHTML) cellEnd(b *hblock, ci int) int {
	if ci+1 < len(b.cellStarts) {
		return b.cellStarts[ci+1] - 1
	}
	return len(b.spans)
}

const hruleCluster = "─"

// layBlock wraps block bi into rows.
func (c *cellHTML) layBlock(bi int, b *hblock, cols []int) {
	prefix := func(first bool) []hcl {
		var out []hcl
		x := 0
		for range b.quote {
			out = append(out, hcl{text: "▎", x: x, w: 1, deco: true}, hcl{text: " ", x: x + 1, w: 1, deco: true})
			x += 2
		}
		if b.indent > 0 {
			mark := ""
			if first {
				mark = b.marker
			}
			mw := c.measure(mark)
			pad := max(b.indent-mw-1, 0)
			for range pad {
				out = append(out, hcl{text: " ", x: x, w: 1, deco: true})
				x++
			}
			if mark != "" {
				out = append(out, hcl{text: mark, x: x, w: mw, deco: true})
				x += mw
			}
			for x < 2*b.quote+b.indent {
				out = append(out, hcl{text: " ", x: x, w: 1, deco: true})
				x++
			}
		}
		return out
	}
	room := max(c.width-2*b.quote-b.indent, 1)
	if b.kind == hRule {
		row := hrow{block: bi, first: true, rule: true, cls: prefix(true)}
		x := 2*b.quote + b.indent
		end := DocPos{Block: bi}
		for range room {
			row.cls = append(row.cls, hcl{text: hruleCluster, x: x, w: 1, pos: end, end: end, deco: true})
			x++
		}
		c.rows = append(c.rows, row)
		return
	}
	// the block's clusters, each with its position; a hard break ("\n") splits the block into
	// lines that wrap on their own
	type cl struct {
		text     string
		pos, end DocPos
		sp       *hspan
		deco     bool
	}
	var lines [][]cl
	var line []cl
	pad := func(n int, at DocPos) {
		for range n {
			line = append(line, cl{text: " ", pos: at, end: at, deco: true})
		}
	}
	cell := 0
	for si := range b.spans {
		sp := &b.spans[si]
		if b.kind == hRow && cols != nil && cell+1 < len(b.cellStarts) && si == b.cellStarts[cell+1]-1 {
			// before a separator: pad the cell to its column
			w := 0
			for _, s := range b.spans[b.cellStarts[cell]:si] {
				w += c.measure(s.text)
			}
			pad(cols[cell]-w, DocPos{Block: bi, Span: si})
			cell++
		}
		off := 0
		for g := range tui.Graphemes(sp.text) {
			at := DocPos{Block: bi, Span: si, Offset: off}
			off += len(g)
			if g == "\n" && !sp.deco {
				lines = append(lines, line)
				line = nil
				continue
			}
			line = append(line, cl{text: g, pos: at, end: DocPos{Block: bi, Span: si, Offset: off}, sp: sp, deco: sp.deco})
		}
	}
	lines = append(lines, line)
	firstRow := true
	for _, ln := range lines {
		cs := make([]string, len(ln))
		for i, k := range ln {
			cs[i] = k.text
		}
		for _, rr := range wrapRanges(cs, room, c.measure) {
			row := hrow{block: bi, first: firstRow, shade: b.kind == hPre, cls: prefix(firstRow)}
			firstRow = false
			x := 2*b.quote + b.indent
			for i := rr[0]; i < rr[1]; i++ {
				k := ln[i]
				w := c.measure(k.text)
				row.cls = append(row.cls, hcl{text: k.text, x: x, w: w, pos: k.pos, end: k.end, sp: k.sp, deco: k.deco})
				x += w
			}
			c.rows = append(c.rows, row)
		}
	}
}

// rowAt is the row at y, clamped to the document.
func (c *cellHTML) rowAt(y float32) int {
	r := int(y)
	if y < 0 {
		r = 0
	}
	return min(max(r, 0), len(c.rows)-1)
}

// texty are a row's clusters that are document text, in order.
func texty(r *hrow) []hcl {
	out := r.cls[:0:0]
	for _, k := range r.cls {
		if k.sp != nil && !k.deco {
			out = append(out, k)
		}
	}
	return out
}

// At is the document position nearest (x, y): the boundary before the cluster under x when x is
// in its left half, after it otherwise. A gap row answers the start of the block after it.
func (c *cellHTML) At(x, y float32) DocPos {
	if len(c.doc) == 0 {
		return DocPos{}
	}
	if y < 0 {
		return DocPos{}
	}
	if int(y) >= len(c.rows) {
		return c.endPos()
	}
	r := &c.rows[c.rowAt(y)]
	if !r.first && len(r.cls) == 0 || r.block < 0 {
		return DocPos{Block: max(r.block, 0)}
	}
	ts := texty(r)
	if len(ts) == 0 {
		if r.first || len(r.cls) == 0 {
			return DocPos{Block: r.block}
		}
		return r.cls[0].pos
	}
	if x < float32(ts[0].x) {
		return ts[0].pos
	}
	for _, k := range ts {
		if x < float32(k.x+k.w) {
			if x < float32(k.x)+float32(k.w)/2 {
				return k.pos
			}
			return k.end
		}
	}
	return ts[len(ts)-1].end
}

// endPos is the position after the document's last text.
func (c *cellHTML) endPos() DocPos {
	for bi := len(c.doc) - 1; bi >= 0; bi-- {
		if n := len(c.doc[bi].spans); n > 0 {
			return DocPos{Block: bi, Span: n - 1, Offset: len(c.doc[bi].spans[n-1].text)}
		}
	}
	return DocPos{}
}

// Rects are the cells a selection from..to covers, a rect for each row it touches.
func (c *cellHTML) Rects(from, to DocPos) []Rect32 {
	from, to = orderPos(from, to)
	if from == to {
		return nil
	}
	var out []Rect32
	for ri := range c.rows {
		r := &c.rows[ri]
		x0, x1 := -1, -1
		for _, k := range r.cls {
			if !selected(k, from, to) {
				continue
			}
			if x0 < 0 {
				x0 = k.x
			}
			x1 = k.x + k.w
		}
		if x0 >= 0 {
			out = append(out, Rect32{X: float32(x0), Y: float32(ri), W: float32(x1 - x0), H: 1})
		}
	}
	return out
}

// Text is the document's text from..to: the blocks it crosses on lines of their own, a hard
// break as a newline, a wrapped row joined to the next, a table's cells by tabs.
func (c *cellHTML) Text(from, to DocPos) string {
	from, to = orderPos(from, to)
	var sb strings.Builder
	for bi := from.Block; bi <= to.Block && bi < len(c.doc); bi++ {
		if bi > from.Block {
			sb.WriteByte('\n')
		}
		b := &c.doc[bi]
		for si := range b.spans {
			sp := &b.spans[si]
			lo, hi := 0, len(sp.text)
			if bi == from.Block && si < from.Span || bi == to.Block && si > to.Span {
				continue
			}
			if bi == from.Block && si == from.Span {
				lo = min(from.Offset, hi)
			}
			if bi == to.Block && si == to.Span {
				hi = min(to.Offset, hi)
			}
			if sp.deco {
				if lo == 0 && hi == len(sp.text) && !(bi == from.Block && si == from.Span) {
					sb.WriteString(sp.copy)
				}
				continue
			}
			if lo < hi {
				sb.WriteString(sp.text[lo:hi])
			}
		}
	}
	return sb.String()
}

// LinkAt is the link target of the text under (x, y); "" when there is none.
func (c *cellHTML) LinkAt(x, y float32) string {
	if y < 0 || int(y) >= len(c.rows) || x < 0 {
		return ""
	}
	for _, k := range c.rows[int(y)].cls {
		if k.sp != nil && x >= float32(k.x) && x < float32(k.x+k.w) {
			return k.sp.link
		}
	}
	return ""
}

// BlockAt is the block shown at row y: the start of its source bytes (-1 when it has none) and
// its first row.
func (c *cellHTML) BlockAt(y float32) (int, float32) {
	if len(c.doc) == 0 {
		return -1, 0
	}
	bi := c.rows[c.rowAt(y)].block
	if bi < 0 {
		return -1, 0
	}
	return c.doc[bi].src[0], float32(c.tops[bi])
}

// BlockTop is the first row of the block showing source byte b: the block whose source holds it,
// else the first that starts after it.
func (c *cellHTML) BlockTop(b int) (float32, bool) {
	after := -1
	for bi := range c.doc {
		s := c.doc[bi].src
		if s[0] < 0 {
			continue
		}
		if s[0] <= b && b < s[1] {
			return float32(c.tops[bi]), true
		}
		if after < 0 && s[0] >= b {
			after = bi
		}
	}
	if after >= 0 {
		return float32(c.tops[after]), true
	}
	return 0, false
}

// paint draws the rows from top on, the selection from..to reversed.
func (c *cellHTML) paint(s tui.Surface, top, w int, from, to DocPos) {
	sz := s.Size()
	from, to = orderPos(from, to)
	selecting := from != to
	for y := 0; y < sz.H; y++ {
		ri := top + y
		if ri < 0 || ri >= len(c.rows) {
			continue
		}
		r := &c.rows[ri]
		if r.shade {
			s.Fill(tui.Rect{X: 2 * c.doc[r.block].quote, Y: y, W: max(w-2*c.doc[r.block].quote, 0), H: 1}, " ", style.New().Background(style.TokenSurface))
		}
		for _, k := range r.cls {
			if k.x+k.w > w {
				break
			}
			st := c.styleOf(r, k)
			if selecting && selected(k, from, to) {
				st = st.Reverse(true)
			}
			s.SetCell(k.x, y, k.text, st)
		}
	}
}

// styleOf is a cluster's style: its span's formatting, its block's, the deco's muted tone.
func (c *cellHTML) styleOf(r *hrow, k hcl) style.Style {
	st := style.New().Foreground(style.TokenForeground)
	if r.rule {
		return style.New().Foreground(style.TokenBorder)
	}
	if k.sp == nil || k.deco {
		if k.text == "▎" {
			return style.New().Foreground(style.TokenBorder)
		}
		return style.New().Foreground(style.TokenTextMuted)
	}
	b := &c.doc[r.block]
	if r.shade {
		st = st.Background(style.TokenSurface)
	}
	if b.kind == hHeading {
		st = st.Bold(true)
		if b.level <= 2 {
			st = st.Foreground(style.TokenPrimary)
		}
	}
	sp := k.sp
	if sp.bold {
		st = st.Bold(true)
	}
	if sp.italic {
		st = st.Italic(true)
	}
	if sp.code && b.kind != hPre {
		st = st.Foreground(style.TokenAccent)
	}
	if sp.under {
		st = st.Underline(true)
	}
	if sp.strike {
		st = st.Strikethrough(true)
	}
	if sp.link != "" {
		st = st.Underline(true).Foreground(style.TokenPrimary)
	}
	return st
}

// selected reports whether cluster k is inside the selection from..to: a cluster of the
// document's own spans (a cell separator too, between selected cells), never the layout's padding.
func selected(k hcl, from, to DocPos) bool {
	return k.sp != nil && k.pos != k.end && posLess(k.pos, to) && posLess(from, k.end)
}

// posLess orders document positions.
func posLess(a, b DocPos) bool {
	if a.Block != b.Block {
		return a.Block < b.Block
	}
	if a.Span != b.Span {
		return a.Span < b.Span
	}
	return a.Offset < b.Offset
}

func orderPos(a, b DocPos) (DocPos, DocPos) {
	if posLess(b, a) {
		return b, a
	}
	return a, b
}
