package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
)

// TABLES — a GFM table drawn as a grid off the cursor: its cells at their columns, aligned as
// its delimiter row says, the header bold on a tint, a border around every cell and a rule under
// the header. A row is one source line, as every line is: its pipes and the spaces around a
// cell's text are Hidden spans with Room, the room that puts the next cell's text at its column,
// so every source cluster keeps a place for the caret. The cursor in the table shows its source.

// align is a column's alignment, from the delimiter row.
type align uint8

const (
	alignLeft align = iota
	alignCenter
	alignRight
)

// cell is a row's cell: its text's bytes [from, to) of the line, trimmed; from == to for an empty
// cell, at where its text would be.
type cell struct{ from, to int }

// tableCells splits a row into its cells at its unescaped pipes, the outer ones optional.
func tableCells(line string) []cell {
	s, e := len(line)-len(strings.TrimLeft(line, " \t")), len(strings.TrimRight(line, " \t"))
	if s < e && line[s] == '|' {
		s++
	}
	if e > s && line[e-1] == '|' && (e < 2 || line[e-2] != '\\') {
		e--
	}
	var out []cell
	add := func(from, to int) {
		for from < to && (line[from] == ' ' || line[from] == '\t') {
			from++
		}
		for to > from && (line[to-1] == ' ' || line[to-1] == '\t') {
			to--
		}
		out = append(out, cell{from, to})
	}
	start := s
	for i := s; i < e; i++ {
		switch line[i] {
		case '\\':
			i++ // an escaped character, a pipe included, is the cell's
		case '|':
			add(start, i)
			start = i + 1
		}
	}
	add(start, e)
	return out
}

// delimiterAligns reads a delimiter row ("| --- | :-: | --: |"): a column's alignment each, ok
// when every cell is dashes with optional colons.
func delimiterAligns(line string) ([]align, bool) {
	if !strings.Contains(line, "-") {
		return nil, false
	}
	cells := tableCells(line)
	if len(cells) == 0 {
		return nil, false
	}
	out := make([]align, len(cells))
	for i, c := range cells {
		s := line[c.from:c.to]
		left, right := strings.HasPrefix(s, ":"), strings.HasSuffix(s, ":")
		body := strings.Trim(s, ":")
		if body == "" || strings.Trim(body, "-") != "" {
			return nil, false
		}
		switch {
		case left && right:
			out[i] = alignCenter
		case right:
			out[i] = alignRight
		}
	}
	return out, true
}

// tableEnd is where a table starting at line i ends, past its last row, as GFM reads one: a
// header row with a pipe, a delimiter row with as many cells, then body rows until a blank line
// or the start of another block (a heading, a quote, a fence, a rule, a list item), a row with no
// pipe included. 0: no table there.
func tableEnd(lines []string, i int) int {
	if i+1 >= len(lines) || !strings.Contains(lines[i], "|") || indentedCode(lines[i]) || startsBlock(lines[i]) {
		return 0
	}
	aligns, ok := delimiterAligns(lines[i+1])
	if !ok || len(aligns) != len(tableCells(lines[i])) {
		return 0
	}
	end := i + 2
	for end < len(lines) && !isBlank(lines[end]) && !startsBlock(lines[end]) {
		end++
	}
	return end
}

// startsBlock reports a line that starts a block of its own, which ends a table: a heading, a
// quote, a fence, a thematic break or a list item.
func startsBlock(line string) bool {
	s := strings.TrimLeft(line, " ")
	if len(line)-len(s) > 3 {
		return false
	}
	if _, ok := openFence(line); ok {
		return true
	}
	if d, _ := quoteDepth(line); d > 0 {
		return true
	}
	if n := len(s) - len(strings.TrimLeft(s, "#")); n >= 1 && n <= 6 && (n == len(s) || s[n] == ' ') {
		return true
	}
	return isThematic(strings.TrimSpace(line)) || isListItem(line)
}

// InTable reports whether line ln is a row of a table: its source is not a list, whatever it
// starts with.
func (r *MarkdownRenderer) InTable(lines []string, ln int) bool {
	for _, b := range r.Blocks(lines, 0, len(lines)) {
		if b.From <= ln && ln < b.To {
			return tableEnd(lines, b.From) > 0
		}
	}
	return false
}

// layTable lays a table out: its source when the cursor is in it, else a grid.
//
// A table wider than the page is not drawn past the page's edge: its columns share the
// width, each wide one wrapping its cells' text at its share, and it is cropped at the
// page's edge by the caller's clip, never overflowing.
func (r *MarkdownRenderer) layTable(b Block, lines []string, width float32, inside bool, t *gui.TextShaper, th Theme) BlockLayout {
	if inside {
		var bl BlockLayout
		opts := flow.Options{Width: width, WhiteSpace: flow.PreWrap, Color: th.Text, LineHeight: rawLineHeight}
		for ln := b.From; ln < b.To; ln++ {
			spans := rawSpans(ln, lines[ln], th.Mono, nil)
			p := flow.Lay(spans, opts, t)
			bl.Lines = append(bl.Lines, LineLayout{Para: p, Spans: spans, Y: bl.Height})
			bl.Height += p.Height
		}
		return bl
	}
	em := th.Prose.Size
	pad := em * 0.6
	aligns, _ := delimiterAligns(lines[b.From+1])

	// each row's cells, laid out alone, to measure the columns
	type rowCells struct {
		cells []cell
		spans [][]flow.Span
		w     []float32
	}
	rows := make([]rowCells, b.To-b.From)
	ncols := len(tableCells(lines[b.From])) // the header's: a body row's extra cells are ignored, as GFM's
	colW := make([]float32, ncols)
	for i := range rows {
		ln := b.From + i
		if i == 1 {
			continue // the delimiter row
		}
		rc := rowCells{cells: tableCells(lines[ln])}
		if len(rc.cells) > ncols {
			rc.cells = rc.cells[:ncols] // the extras' bytes join the row's trailing hidden run
		}
		for _, c := range rc.cells {
			var spans []flow.Span
			var w float32
			if c.to > c.from {
				spans, _ = r.lineSpans(ln, lines[ln][c.from:c.to], th)
				for k := range spans {
					spans[k].Col += clusters(lines[ln][:c.from])
					if i == 0 {
						spans[k].Font.Bold = true
					}
					if !spans[k].Hidden {
						w += spanWidth(spans[k], t)
					}
				}
			}
			rc.spans, rc.w = append(rc.spans, spans), append(rc.w, w)
		}
		for k, w := range rc.w {
			colW[k] = max(colW[k], w)
		}
		rows[i] = rc
	}

	// A table that would run past the page's width shares the width instead: narrow
	// columns keep their natural width where possible, and wider columns share the
	// remaining budget in proportion to their excess, wrapping text in their columns.
	// width 0 (no measure yet) leaves the natural widths alone, as the raw view does.
	tableW := func() float32 {
		var w float32
		for _, cw := range colW {
			w += cw + 2*pad
		}
		return w
	}()
	if width > 0 && tableW > width {
		budget := width - float32(len(colW))*2*pad // the columns' text, padding paid
		share := make([]float32, len(colW))
		var reserved, flexible float32
		for k, cw := range colW {
			share[k] = min(cw, 2*em)
			reserved += share[k]
			flexible += cw - share[k]
		}
		if budget <= 0 {
			clear(share)
		} else if budget < reserved {
			for k := range share {
				share[k] *= budget / reserved
			}
		} else if flexible > 0 {
			for k, cw := range colW {
				share[k] += (budget - reserved) * (cw - share[k]) / flexible
			}
		}
		colW = share
	}

	colX := make([]float32, len(colW)+1)
	for k, w := range colW {
		colX[k+1] = colX[k] + w + 2*pad
	}
	total := colX[len(colW)]
	border := mix(or(th.Marker, th.Muted), th.Background, 0.45)

	// A wrapped cell's lines: the cell's spans laid at its column's width, one paragraph
	// per cell, so a row grows to hold its tallest cell's wrapped lines.
	laidCells := func(i int, rc rowCells) []*flow.Para {
		out := make([]*flow.Para, len(rc.cells))
		for k, c := range rc.cells {
			if c.to > c.from {
				opts := flow.Options{Width: colW[k], WhiteSpace: flow.PreWrap, Color: mix(th.Text, th.Background, 0.9), LineHeight: proseLineHeight}
				out[k] = flow.Lay(rc.spans[k], opts, t)
			}
		}
		return out
	}

	var bl BlockLayout
	for i := range rows {
		ln := b.From + i
		line := lines[ln]
		opts := flow.Options{Width: width, WhiteSpace: flow.PreWrap, Color: mix(th.Text, th.Background, 0.9), LineHeight: proseLineHeight}
		var spans []flow.Span
		var marks []Mark
		if i == 1 {
			// the delimiter row: hidden, a strip as thin as the rule under the header, which it is
			opts.LineHeight = 1
			thin := th.Prose
			thin.Size = 2
			spans = []flow.Span{{Text: line, Font: thin, Line: ln, Hidden: true, Room: max(total, 0.01)}}
			p := flow.Lay(spans, opts, t)
			marks = append(marks, Mark{Rect: gui.Rect{W: total + 1, H: p.Height}, Color: border})
			bl.Lines = append(bl.Lines, LineLayout{Para: p, Spans: spans, Y: bl.Height, Marks: marks, Unnumbered: true})
			bl.Height += p.Height
			continue
		}
		rc := rows[i]
		cells := laidCells(i, rc)
		// A row's height is its tallest cell's, wrapped lines included. The row's
		// own line lays its cells' first wrapped lines; each further line index
		// becomes one more LineLayout holding every cell's jth line at its column.
		rowH := float32(0)
		for k := range rc.cells {
			if cells[k] != nil {
				rowH = max(rowH, cells[k].Height)
			}
		}
		if rowH == 0 {
			rowH = em * proseLineHeight
		}
		rowY := bl.Height
		var x float32 // where the text laid so far ends
		gap := 0      // the start of the hidden run not yet laid: pipes and padding
		for k, c := range rc.cells {
			at := colX[k] + pad
			if k < len(aligns) && cells[k] != nil {
				content := cells[k].Width
				switch aligns[k] {
				case alignCenter:
					at += (colW[k] - content) / 2
				case alignRight:
					at += colW[k] - content
				}
			}
			if c.to == c.from {
				continue // an empty cell: its bytes join the hidden run
			}
			if c.from > gap {
				spans = append(spans, flow.Span{Text: line[gap:c.from], Font: th.Prose, Line: ln, Col: clusters(line[:gap]),
					Hidden: true, Room: max(at-x, 0.01)})
			}
			spans = append(spans, firstLineSpans(cells[k], rc.spans[k])...)
			firstWidth := float32(0)
			for _, frag := range cells[k].Lines[0].Frags {
				firstWidth = max(firstWidth, frag.X+frag.W)
			}
			x, gap = at+firstWidth, c.to
		}
		if gap < len(line) {
			spans = append(spans, flow.Span{Text: line[gap:], Font: th.Prose, Line: ln, Col: clusters(line[:gap]),
				Hidden: true, Room: max(total-x, 0.01)})
		}
		if len(spans) == 0 {
			spans = []flow.Span{{Font: th.Prose, Line: ln}}
		}
		p := flow.Lay(spans, opts, t)
		if i == 0 {
			marks = append(marks, Mark{Rect: gui.Rect{W: total, H: rowH}, Color: mix(th.Text, th.Background, 0.06)},
				Mark{Rect: gui.Rect{W: total, H: 1}, Color: border})
		}
		if i > 0 { // the header's bottom is the delimiter row's rule
			marks = append(marks, Mark{Rect: gui.Rect{Y: rowH - 1, W: total, H: 1}, Color: border})
		}
		for _, cx := range colX {
			marks = append(marks, Mark{Rect: gui.Rect{X: cx, W: 1, H: rowH}, Color: border})
		}
		bl.Lines = append(bl.Lines, LineLayout{Para: p, Spans: spans, Y: rowY, Marks: marks})
		// a wrapped cell's further lines: one LineLayout per line index j, every
		// cell's jth line at its column in the same one — all columns' wraps share
		// the row's Y grid. The spans are visible and mapped to their true source,
		// so the wrapped text paints and the caret can travel into it.
		for j := 1; ; j++ {
			var ws []flow.Span
			var px float32 // where the last laid text ends
			any := false
			lineY := float32(0)
			for k := range rc.cells {
				if cells[k] == nil || j >= len(cells[k].Lines) {
					continue
				}
				any = true
				lineY = max(lineY, cells[k].Lines[j].Y)
				at := colX[k] + pad
				if px < at {
					ws = append(ws, flow.Span{Text: " ", Font: th.Prose, Line: ln, Hidden: true, Room: at - px})
				}
				for _, fr := range cells[k].Lines[j].Frags {
					s := rc.spans[k][fr.Span]
					s.Text = s.Text[fr.From:fr.To]
					ws = append(ws, s)
					px = at + fr.X + fr.W
				}
			}
			if !any {
				break
			}
			if len(ws) == 0 {
				ws = []flow.Span{{Font: th.Prose, Line: ln}}
			}
			opts2 := opts
			opts2.WhiteSpace = flow.Pre
			p2 := flow.Lay(ws, opts2, t)
			bl.Lines = append(bl.Lines, LineLayout{Para: p2, Spans: ws, Y: rowY + lineY})
		}
		bl.Height += rowH
	}
	return bl
}

// firstLineSpans is the first wrapped line's spans as laid: each frag of the
// paragraph's first line, its slice of its span.
func firstLineSpans(p *flow.Para, src []flow.Span) []flow.Span {
	if p == nil || len(p.Lines) == 0 {
		return nil
	}
	var out []flow.Span
	for _, fr := range p.Lines[0].Frags {
		s := src[fr.Span]
		s.Text = s.Text[fr.From:fr.To]
		out = append(out, s)
	}
	return out
}
