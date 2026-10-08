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

// tableEnd is where a table starting at line i ends, past its last row: a row with a pipe, then
// a delimiter row with as many cells, then rows while lines hold a pipe. 0: no table there.
func tableEnd(lines []string, i int) int {
	if i+1 >= len(lines) || !strings.Contains(lines[i], "|") || indentedCode(lines[i]) {
		return 0
	}
	aligns, ok := delimiterAligns(lines[i+1])
	if !ok || len(aligns) != len(tableCells(lines[i])) {
		return 0
	}
	end := i + 2
	for end < len(lines) && !isBlank(lines[end]) && strings.Contains(lines[end], "|") {
		end++
	}
	return end
}

// layTable lays a table out: its source when the cursor is in it, else a grid.
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
	var colW []float32
	for i := range rows {
		ln := b.From + i
		if i == 1 {
			continue // the delimiter row
		}
		rc := rowCells{cells: tableCells(lines[ln])}
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
		for len(colW) < len(rc.w) {
			colW = append(colW, 0)
		}
		for k, w := range rc.w {
			colW[k] = max(colW[k], w)
		}
		rows[i] = rc
	}
	colX := make([]float32, len(colW)+1)
	for k, w := range colW {
		colX[k+1] = colX[k] + w + 2*pad
	}
	total := colX[len(colW)]
	border := mix(or(th.Marker, th.Muted), th.Background, 0.45)

	var bl BlockLayout
	for i := range rows {
		ln := b.From + i
		line := lines[ln]
		opts := flow.Options{WhiteSpace: flow.Pre, Color: mix(th.Text, th.Background, 0.9), LineHeight: proseLineHeight}
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
			bl.Lines = append(bl.Lines, LineLayout{Para: p, Spans: spans, Y: bl.Height, Marks: marks})
			bl.Height += p.Height
			continue
		}
		rc := rows[i]
		var x float32 // where the text laid so far ends
		gap := 0      // the start of the hidden run not yet laid: pipes and padding
		for k, c := range rc.cells {
			if c.to == c.from {
				continue // an empty cell: its bytes join the hidden run
			}
			at := colX[k] + pad
			if k < len(aligns) {
				switch aligns[k] {
				case alignCenter:
					at += (colW[k] - rc.w[k]) / 2
				case alignRight:
					at += colW[k] - rc.w[k]
				}
			}
			if c.from > gap {
				spans = append(spans, flow.Span{Text: line[gap:c.from], Font: th.Prose, Line: ln, Col: clusters(line[:gap]),
					Hidden: true, Room: max(at-x, 0.01)})
			}
			spans = append(spans, rc.spans[k]...)
			x, gap = at+rc.w[k], c.to
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
			marks = append(marks, Mark{Rect: gui.Rect{W: total, H: p.Height}, Color: mix(th.Text, th.Background, 0.06)},
				Mark{Rect: gui.Rect{W: total, H: 1}, Color: border})
		}
		if i > 0 { // the header's bottom is the delimiter row's rule
			marks = append(marks, Mark{Rect: gui.Rect{Y: p.Height - 1, W: total, H: 1}, Color: border})
		}
		for _, cx := range colX {
			marks = append(marks, Mark{Rect: gui.Rect{X: cx, W: 1, H: p.Height}, Color: border})
		}
		bl.Lines = append(bl.Lines, LineLayout{Para: p, Spans: spans, Y: bl.Height, Marks: marks})
		bl.Height += p.Height
	}
	return bl
}
