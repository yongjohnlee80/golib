package widget

import (
	"context"
	"errors"
	"math"
	"strings"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
	tuimermaid "github.com/yongjohnlee80/golib/tui/mermaid"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE CELL LAYOUT — an HTMLView in a terminal: the document's text, wrapped to the widget's
// width and read-only, one unit a cell. Headings are bold and told apart by level (h1 and h2 ruled
// under), links underlined, inline code shaded and tinted, preformatted text shaded and inset;
// quotes carry a bar, a callout's in its type's color with its title; list items a hanging marker,
// a task's its checkbox; a table that fits is boxed, its header ruled off and its cells aligned,
// and one that does not wraps as rows of text. Highlighted text is marked. Images show their alt
// text.

// hcl is one laid-out grapheme cluster: where it is drawn and which document position it is.
type hcl struct {
	text string
	x, w int
	pos  DocPos          // the boundary before it
	end  DocPos          // the boundary after it
	sp   *hspan          // nil: a deco cluster of the layout's own (a quote bar, a marker, padding)
	deco bool            // never copied as text (a marker, a separator, padding)
	fg   style.ColorSpec // a deco cluster's own color (a callout's bar, a heading's rule); nil: its kind's
}

// hrow is one row of the layout: a blank gap, or a block's wrapped line.
type hrow struct {
	block int // the block it belongs to (a gap row: the block after it)
	first bool
	cls   []hcl
	shade bool // a preformatted block's row: shaded across the width
	rule  bool
	// grid is a diagram's, drawn in cells across its rows (layDiagram); gridRow is which of its
	// rows this one is. nil for any other row.
	grid    *tuimermaid.Grid
	gridRow int
}

// cellHTML lays an HTMLView out in cells (HTMLLayout).
type cellHTML struct {
	measure func(string) int
	src     []byte
	doc     []hblock
	width   int
	rows    []hrow
	tops    []int // each block's first row (its gap excluded)
	// notice says a diagram of a kind not drawn in cells isn't drawn here (WithDiagramNotice);
	// nil: diagramNotice's English.
	notice func(kind string) string
	m      cellMarks // a find's matches (htmlview_marks.go)
}

func newCellHTML(measure func(string) int) *cellHTML {
	return &cellHTML{measure: measure, width: 1, m: cellMarks{cur: -1}}
}

var _ HTMLLayout = (*cellHTML)(nil)

// SetSource reads a new document and lays it out at the width it has.
func (c *cellHTML) SetSource(src []byte) {
	c.src = src
	c.doc = readHTMLDoc(src)
	c.layOut()
	c.markAll()
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

// layOut wraps every block to the width: a table that fits between its box's rules (its top, under
// its header row, its bottom), h1 and h2 above their rules.
func (c *cellHTML) layOut() {
	c.rows, c.tops = c.rows[:0], make([]int, len(c.doc))
	widths := c.columnWidths()
	for bi := range c.doc {
		b := &c.doc[bi]
		if b.gap {
			c.rows = append(c.rows, hrow{block: bi, cls: c.gapBars(bi)})
		}
		c.tops[bi] = len(c.rows)
		cols := widths[b.table]
		boxed := b.kind == hRow && cols != nil
		if boxed && c.tableEdge(bi, -1) {
			c.layBox(bi, b, cols, "┌", "┬", "┐")
		}
		c.layBlock(bi, b, cols)
		switch {
		case boxed && c.tableEdge(bi, 1):
			c.layBox(bi, b, cols, "└", "┴", "┘")
		case boxed && b.head:
			c.layBox(bi, b, cols, "├", "┼", "┤")
		case b.kind == hHeading && b.level <= 2:
			c.layHeadingRule(bi, b)
		}
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
		sum := 4 + 3*max(len(cols)-1, 0) // the box's "│ " and " │", and the " │ " between cells
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

// preTabStop is how many cells apart a preformatted line's tab stops are.
const preTabStop = 4

// gapBars are the quote bars a gap row before block bi carries: those of the quotes it shares with
// the block before, so a quote's bar runs on through the blank rows between its blocks.
func (c *cellHTML) gapBars(bi int) []hcl {
	if bi == 0 {
		return nil
	}
	b, prev := &c.doc[bi], &c.doc[bi-1]
	n := 0
	for n < min(b.quote, prev.quote) && n < len(b.bars) && n < len(prev.bars) && b.bars[n] == prev.bars[n] {
		n++
	}
	var out []hcl
	at := DocPos{Block: bi}
	for q := range n {
		out = append(out, hcl{text: "▎", x: 2 * q, w: 1, pos: at, end: at, deco: true, fg: calloutTone(b.bars[q])})
	}
	return out
}

// tableEdge reports whether row bi is its table's first (dir -1) or last (dir 1).
func (c *cellHTML) tableEdge(bi, dir int) bool {
	n := bi + dir
	return n < 0 || n >= len(c.doc) || c.doc[n].kind != hRow || c.doc[n].table != c.doc[bi].table
}

// layBox is a rule of row bi's table box: left, the joints between its columns and right, each
// column's rule as wide as the column and its padding.
func (c *cellHTML) layBox(bi int, b *hblock, cols []int, left, joint, right string) {
	row := hrow{block: bi, cls: c.prefix(b, false)}
	x := 2*b.quote + b.indent
	at := DocPos{Block: bi}
	put := func(s string) {
		row.cls = append(row.cls, hcl{text: s, x: x, w: 1, pos: at, end: at, deco: true, fg: style.TokenBorder})
		x++
	}
	put(left)
	for ci, w := range cols {
		if ci > 0 {
			put(joint)
		}
		for range w + 2 {
			put(hruleCluster)
		}
	}
	put(right)
	c.rows = append(c.rows, row)
}

// layHeadingRule is the rule under an h1 (heavy, in the primary color) or an h2 (light, a border's),
// across the room the heading has.
func (c *cellHTML) layHeadingRule(bi int, b *hblock) {
	rule, fg := "━", style.ColorSpec(style.TokenPrimary)
	if b.level == 2 {
		rule, fg = hruleCluster, style.TokenBorder
	}
	row := hrow{block: bi, cls: c.prefix(b, false)}
	x := 2*b.quote + b.indent
	at := DocPos{Block: bi}
	for range max(c.width-x, 1) {
		row.cls = append(row.cls, hcl{text: rule, x: x, w: 1, pos: at, end: at, deco: true, fg: fg})
		x++
	}
	c.rows = append(c.rows, row)
}

// layBlock wraps block bi into rows.
func (c *cellHTML) layBlock(bi int, b *hblock, cols []int) {
	if b.kind == hPre && b.diagram != "" {
		c.layDiagram(bi, b)
		return
	}
	room := max(c.width-2*b.quote-b.indent, 1)
	if b.kind == hRule {
		c.layRule(bi, b, room)
		return
	}
	firstRow := true
	for _, ln := range c.blockLines(bi, b, cols) {
		cs := make([]string, len(ln))
		for i, k := range ln {
			cs[i] = k.text
		}
		for _, rr := range wrapRanges(cs, room, c.measure) {
			row := hrow{block: bi, first: firstRow, shade: b.kind == hPre, cls: c.prefix(b, firstRow)}
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

// measureFunc is a width function as tui/mermaid's Measurer.
type measureFunc func(string) int

func (f measureFunc) StringWidth(s string) int { return f(s) }

// errTooWide is a flowchart wider than the room it would be drawn in: it shows the notice.
var errTooWide = errors.New("diagram wider than the room")

// diagramNotice is what a diagram the terminal doesn't draw shows instead, in English: a host
// that speaks other languages says it its own way (WithDiagramNotice).
func diagramNotice(kind string) string {
	return kind + " diagram: not drawn in the terminal (Raw shows its source)"
}

// layDiagram lays a <pre class="mermaid"> out: a kind tui/mermaid draws as its grid, one row of
// the layout per row of the grid; any other kind, one that doesn't parse, one too large to lay
// out, or a flowchart wider than the room (an LR or RL one laid top to bottom first), as a muted
// line saying it isn't drawn here, wrapped to the room, never its source (Raw shows that).
func (c *cellHTML) layDiagram(bi int, b *hblock) {
	kind := "mermaid"
	room := max(c.width-2*b.quote-b.indent, 1)
	if d, err := pm.Parse(b.diagram); err == nil {
		kind = d.Kind().String()
		if tuimermaid.Supported(d.Kind()) {
			g, err := tuimermaid.Lay(context.Background(), d, measureFunc(c.measure))
			if fc, ok := d.(*pm.FlowchartDiagram); ok && err == nil && g.Size.W > room {
				g, err = nil, errTooWide
				if fc.Dir == pm.LR || fc.Dir == pm.RL {
					down := *fc
					down.Dir = pm.TB
					if g2, err2 := tuimermaid.Lay(context.Background(), &down, measureFunc(c.measure)); err2 == nil && g2.Size.W <= room {
						g, err = g2, nil
					}
				}
			}
			if err == nil && g.Size.H > 0 {
				for i := range g.Size.H {
					c.rows = append(c.rows, hrow{block: bi, first: i == 0, grid: g, gridRow: i})
				}
				return
			}
		}
	}
	say := diagramNotice
	if c.notice != nil {
		say = c.notice
	}
	var cs []string
	for k := range grapheme.Clusters("▸ " + say(kind)) {
		cs = append(cs, k)
	}
	for i, rr := range wrapRanges(cs, room, c.measure) { // wrapped to the room, as any paragraph
		row := hrow{block: bi, first: i == 0, cls: c.prefix(b, i == 0)}
		x := 2*b.quote + b.indent
		for _, k := range cs[rr[0]:rr[1]] {
			w := c.measure(k)
			row.cls = append(row.cls, hcl{text: k, x: x, w: w, deco: true})
			x += w
		}
		c.rows = append(c.rows, row)
	}
}

// diagramTheme is a diagram's styles in the cell layout: lines as the text (a diagram's edges are
// its content: the border's tone, a theme's mid, is under 2:1 on the light themes' pages), outlines
// in the accent, labels as the text.
var diagramTheme = tuimermaid.Theme{
	Line:     style.New().Foreground(style.TokenForeground),
	Box:      style.New().Foreground(style.TokenAccent),
	Label:    style.New().Foreground(style.TokenForeground),
	Selected: style.New().Foreground(style.TokenForeground).Reverse(true),
}

// prefix is the decoration a row of b starts with: a bar and a space per quote level, then the
// indent, with the list marker right-aligned in it on the block's first row.
func (c *cellHTML) prefix(b *hblock, first bool) []hcl {
	var out []hcl
	x := 0
	for q := range b.quote {
		kind := ""
		if q < len(b.bars) {
			kind = b.bars[q]
		}
		out = append(out, hcl{text: "▎", x: x, w: 1, deco: true, fg: calloutTone(kind)}, hcl{text: " ", x: x + 1, w: 1, deco: true})
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
		for g := range tui.Graphemes(mark) { // a cluster a cell, as a Surface draws them ("1." is two)
			gw := c.measure(g)
			k := hcl{text: g, x: x, w: gw, deco: true}
			if g == "☑" {
				k.fg = style.TokenSuccess
			}
			out = append(out, k)
			x += gw
		}
		for x < 2*b.quote+b.indent {
			out = append(out, hcl{text: " ", x: x, w: 1, deco: true})
			x++
		}
	}
	return out
}

// layRule is a rule block: one row of room rule clusters after its prefix.
func (c *cellHTML) layRule(bi int, b *hblock, room int) {
	row := hrow{block: bi, first: true, rule: true, cls: c.prefix(b, true)}
	x := 2*b.quote + b.indent
	end := DocPos{Block: bi}
	for range room {
		row.cls = append(row.cls, hcl{text: hruleCluster, x: x, w: 1, pos: end, end: end, deco: true})
		x++
	}
	c.rows = append(c.rows, row)
}

// lineCluster is one cluster of a block's line, with its document position.
type lineCluster struct {
	text     string
	pos, end DocPos
	sp       *hspan
	deco     bool
}

// blockLines is block bi's clusters, each with its position, split into the lines a hard break
// ("\n") ends, which wrap on their own. A boxed table row (cols) is edged by its box, its cells
// padded to their columns as they align, a missing cell blank; a preformatted line is inset a cell.
func (c *cellHTML) blockLines(bi int, b *hblock, cols []int) [][]lineCluster {
	var lines [][]lineCluster
	var line []lineCluster
	pad := func(n int, at DocPos) {
		for range n {
			line = append(line, lineCluster{text: " ", pos: at, end: at, deco: true})
		}
	}
	edge := func(s string, at DocPos) {
		for g := range tui.Graphemes(s) {
			line = append(line, lineCluster{text: g, pos: at, end: at, deco: true})
		}
	}
	boxed := b.kind == hRow && cols != nil
	width := func(ci int) int {
		w := 0
		for _, s := range b.spans[b.cellStarts[ci]:c.cellEnd(b, ci)] {
			w += c.measure(s.text)
		}
		return w
	}
	lead := func(ci int) int { // the padding before cell ci's text, as the cell aligns
		if ci >= len(cols) || ci >= len(b.align) {
			return 0
		}
		switch room := cols[ci] - width(ci); b.align[ci] {
		case 'r':
			return room
		case 'c':
			return room / 2
		}
		return 0
	}
	start := DocPos{Block: bi}
	if boxed {
		edge("│ ", start)
		if len(b.cellStarts) > 0 {
			pad(lead(0), start)
		}
	}
	if b.kind == hPre {
		pad(1, start)
	}
	col := 0 // a preformatted line's column, for its tab stops
	cell := 0
	for si := range b.spans {
		sp := &b.spans[si]
		if boxed && cell+1 < len(b.cellStarts) && si == b.cellStarts[cell+1]-1 {
			// before a separator: pad the cell to its column
			pad(cols[cell]-width(cell)-lead(cell), DocPos{Block: bi, Span: si})
			cell++
		}
		off := 0
		for g := range tui.Graphemes(sp.text) {
			at := DocPos{Block: bi, Span: si, Offset: off}
			off += len(g)
			if g == "\n" && !sp.deco {
				lines = append(lines, line)
				line = nil
				col = 0
				if b.kind == hPre {
					pad(1, at)
				}
				continue
			}
			end := DocPos{Block: bi, Span: si, Offset: off}
			if g == "\t" && b.kind == hPre {
				// a tab is a space of its text up to the next stop: copied as the tab it is
				n := preTabStop - col%preTabStop
				line = append(line, lineCluster{text: " ", pos: at, end: end, sp: sp})
				pad(n-1, end)
				col += n
				continue
			}
			col += c.measure(g)
			line = append(line, lineCluster{text: g, pos: at, end: end, sp: sp, deco: sp.deco})
		}
		if boxed && cell > 0 && cell < len(b.cellStarts) && si+1 == b.cellStarts[cell] {
			pad(lead(cell), DocPos{Block: bi, Span: si + 1})
		}
	}
	if boxed {
		end := DocPos{Block: bi}
		if n := len(b.spans); n > 0 {
			end = DocPos{Block: bi, Span: n - 1, Offset: len(b.spans[n-1].text)}
		}
		if last := len(b.cellStarts) - 1; last >= 0 {
			pad(cols[last]-width(last)-lead(last), end)
		}
		if len(b.cellStarts) == 0 && len(cols) > 0 { // a row of no cells: a blank first one
			pad(cols[0], end)
		}
		for ci := max(len(b.cellStarts), 1); ci < len(cols); ci++ { // a row short of cells: blank ones
			edge(" │ ", end)
			pad(cols[ci], end)
		}
		edge(" │", end)
	}
	return append(lines, line)
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
	from, to = OrderDocPos(from, to)
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
	from, to = OrderDocPos(from, to)
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

// LinkRects are the links' runs on the rows between top and bottom: each row's adjacent
// clusters of one link, one rect.
func (c *cellHTML) LinkRects(top, bottom float32, dst []Rect32) []Rect32 {
	for y := max(int(top), 0); y < min(int(math.Ceil(float64(bottom))), len(c.rows)); y++ {
		start, end, link := 0, 0, ""
		flush := func() {
			if link != "" && end > start {
				dst = append(dst, Rect32{X: float32(start), Y: float32(y), W: float32(end - start), H: 1})
			}
		}
		for _, k := range c.rows[y].cls {
			l := ""
			if k.sp != nil {
				l = k.sp.link
			}
			if l != link || k.x != end {
				flush()
				start, link = k.x, l
			}
			end = k.x + k.w
		}
		flush()
	}
	return dst
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
// else the first that starts after it, else (a byte after every block, the blank lines that end a
// note) the last.
func (c *cellHTML) BlockTop(b int) (float32, bool) {
	bi := SourceIndex(len(c.doc), func(i int) [2]int { return c.doc[i].src }, b)
	if bi < 0 {
		return 0, false
	}
	return float32(c.tops[bi]), true
}

var _ HTMLLayoutCells = (*cellHTML)(nil)

// PaintCells implements HTMLLayoutCells: the rows from top on, the selection from..to reversed.
func (c *cellHTML) PaintCells(s tui.Surface, top, w int, from, to DocPos) {
	sz := s.Size()
	from, to = OrderDocPos(from, to)
	selecting := from != to
	var painted *tuimermaid.Grid // the diagram drawn last: each is painted once, from its first row shown
	for y := 0; y < sz.H; y++ {
		ri := top + y
		if ri < 0 || ri >= len(c.rows) {
			continue
		}
		r := &c.rows[ri]
		if r.grid != nil {
			if r.grid != painted {
				painted = r.grid
				b := &c.doc[r.block]
				r.grid.Paint(s, 2*b.quote+b.indent, y-r.gridRow, -1, diagramTheme) // the surface clips it
			}
			continue
		}
		if r.shade {
			s.Fill(tui.Rect{X: 2 * c.doc[r.block].quote, Y: y, W: max(w-2*c.doc[r.block].quote, 0), H: 1}, " ", style.New().Background(style.TokenSurface))
		}
		for _, k := range r.cls {
			if k.x+k.w > w {
				break
			}
			st := c.styleOf(r, k)
			if marked, current := c.markOf(k); marked {
				st = markStyle.Reverse(current)
			}
			if selecting && selected(k, from, to) {
				st = st.Reverse(true)
			}
			s.SetCell(k.x, y, k.text, st)
		}
	}
}

// styleOf is a cluster's style: its span's formatting, its block's; a deco's own color, a box
// rule's border tone, else a muted one.
func (c *cellHTML) styleOf(r *hrow, k hcl) style.Style {
	st := style.New().Foreground(style.TokenForeground)
	if r.rule {
		return style.New().Foreground(style.TokenBorder)
	}
	if k.sp == nil || k.deco {
		deco := style.New().Foreground(style.TokenTextMuted)
		switch {
		case k.fg != nil:
			deco = style.New().Foreground(k.fg)
		case strings.ContainsAny(k.text, "│┌┬┐├┼┤└┴┘─"):
			deco = style.New().Foreground(style.TokenBorder)
		}
		if r.shade && k.text != "▎" {
			deco = deco.Background(style.TokenSurface) // a preformatted row's inset and tab stops are shaded too
		}
		return deco
	}
	b := &c.doc[r.block]
	if r.shade {
		st = st.Background(style.TokenSurface)
	}
	if b.kind == hHeading {
		st = headingStyle(st, b.level)
	}
	if b.title != "" {
		st = st.Foreground(calloutTone(b.title))
	}
	sp := k.sp
	if sp.bold {
		st = st.Bold(true)
	}
	if sp.italic {
		st = st.Italic(true)
	}
	if sp.code && b.kind != hPre {
		st = st.Foreground(style.TokenAccent).Background(style.TokenSurface)
	}
	if sp.mark {
		st = st.Foreground(style.TokenTextOnWarning).Background(style.TokenWarning)
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

// headingStyle is a heading's at level: h1 and h2 bold in the primary color (their rules tell
// them apart), h3 bold in the accent, h4 bold, h5 bold and muted, h6 italic and muted.
func headingStyle(st style.Style, level int) style.Style {
	switch level {
	case 1, 2:
		return st.Bold(true).Foreground(style.TokenPrimary)
	case 3:
		return st.Bold(true).Foreground(style.TokenAccent)
	case 4:
		return st.Bold(true)
	case 5:
		return st.Bold(true).Foreground(style.TokenTextMuted)
	}
	return st.Italic(true).Foreground(style.TokenTextMuted)
}

// calloutTone is the color of a quote's bar and a callout's title by the callout's type (Obsidian's
// types and their aliases): a blockquote's ("") a border's; note, info, todo and any type it doesn't
// know, the primary.
func calloutTone(kind string) style.ColorSpec {
	switch kind {
	case "":
		return style.TokenBorder
	case "tip", "hint", "important", "success", "check", "done":
		return style.TokenSuccess
	case "warning", "caution", "attention":
		return style.TokenWarning
	case "danger", "error", "bug", "failure", "fail", "missing":
		return style.TokenError
	case "question", "help", "faq":
		return style.TokenAccent
	case "example", "quote", "cite", "abstract", "summary", "tldr":
		return style.TokenSecondary
	}
	return style.TokenPrimary
}

// selected reports whether cluster k is inside the selection from..to: a cluster of the
// document's own spans (a cell separator too, between selected cells), never the layout's padding.
func selected(k hcl, from, to DocPos) bool {
	return k.sp != nil && k.pos != k.end && k.pos.Before(to) && from.Before(k.end)
}
