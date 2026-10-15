package widget

import (
	"slices"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TABLE TEXT — a Markdown table in an EditorCore's buffer as a TableSource and a TableEditor, so a
// TableCore's operations work on the text the user is writing: each change rewrites the table's
// lines in one undo step, every column padded to its widest cell and the delimiter row written from
// the columns' alignment. The parser here is the one the editor's Tab keys and a renderer read.

// tableCells is a table row's cells: each the cluster columns of the text between two pipes,
// [start, end), spaces at either end left out. ok is false for a line that is not a table row.
func tableCells(line string) (cells [][2]int, ok bool) {
	t := strings.TrimSpace(line)
	if len(t) < 2 || t[0] != '|' || t[len(t)-1] != '|' {
		return nil, false
	}
	cs := clusters(line)
	var pipes []int
	for i, cl := range cs {
		if cl == "|" && (i == 0 || cs[i-1] != "\\") {
			pipes = append(pipes, i)
		}
	}
	for i := 0; i+1 < len(pipes); i++ {
		lo, hi := pipes[i]+1, pipes[i+1]
		for lo < hi && cs[lo] == " " {
			lo++
		}
		for hi > lo && cs[hi-1] == " " {
			hi--
		}
		cells = append(cells, [2]int{lo, hi})
	}
	return cells, len(cells) > 0
}

// isDelimiterRow is a table's |---|:--:| row.
func isDelimiterRow(line string) bool {
	t := strings.Trim(strings.TrimSpace(line), "|")
	return t != "" && strings.Trim(t, "-:| ") == "" && strings.Contains(t, "-")
}

// MarkdownTableCells is a table row's cells, each the [start, end) cluster columns of its text,
// spaces at either end left out; an escaped \| is text. ok is false for a line that is not a row.
func MarkdownTableCells(line string) (cells [][2]int, ok bool) { return tableCells(line) }

// MarkdownDelimiterAligns is a delimiter row's alignment per column: :-- and --- left, :-: centre,
// --: right. ok is false for a line that is not a delimiter row.
func MarkdownDelimiterAligns(line string) (aligns []style.Align, ok bool) {
	if !isDelimiterRow(line) {
		return nil, false
	}
	cs := clusters(line)
	cells, _ := tableCells(line)
	for _, c := range cells {
		aligns = append(aligns, delimiterAlign(strings.Join(cs[c[0]:c[1]], "")))
	}
	return aligns, true
}

func delimiterAlign(d string) style.Align {
	left, right := strings.HasPrefix(d, ":"), strings.HasSuffix(d, ":")
	switch {
	case left && right:
		return style.AlignCenter
	case right:
		return style.AlignRight
	}
	return style.AlignLeft
}

// TableText is the Markdown table holding a line of an EditorCore's buffer: a header row, a
// delimiter row, and the rows under them.
type TableText struct {
	core     *EditorCore
	top, end int // the header's line and the last row's, inclusive
}

// TableAt is the table holding line ln: the run of table rows around it (the lines table editing
// applies on), whose second line is a delimiter row. ok is false off a table.
func TableAt(c *EditorCore, ln int) (*TableText, bool) {
	if _, ok := c.tableRow(ln); !ok {
		return nil, false
	}
	top, end := ln, ln
	for top > 0 {
		if _, ok := c.tableRow(top - 1); !ok {
			break
		}
		top--
	}
	for end+1 < c.LineCount() {
		if _, ok := c.tableRow(end + 1); !ok {
			break
		}
		end++
	}
	if end < top+1 || !isDelimiterRow(c.LineAt(top+1)) {
		return nil, false
	}
	return &TableText{core: c, top: top, end: end}, true
}

// Lines are the table's first and last lines in the buffer.
func (t *TableText) Lines() (top, end int) { return t.top, t.end }

// cellTexts is line ln's cells as text.
func (t *TableText) cellTexts(ln int) []string {
	line := t.core.LineAt(ln)
	cs := clusters(line)
	cells, _ := tableCells(line)
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = strings.Join(cs[c[0]:c[1]], "")
	}
	return out
}

// Fields are the header's cells, aligned as the delimiter row says.
func (t *TableText) Fields() []TableField {
	titles := t.cellTexts(t.top)
	aligns, _ := MarkdownDelimiterAligns(t.core.LineAt(t.top + 1))
	out := make([]TableField, len(titles))
	for i, title := range titles {
		out[i].Title = title
		if i < len(aligns) {
			out[i].Align = aligns[i]
		}
	}
	return out
}

// Len is the rows under the delimiter row.
func (t *TableText) Len() int { return t.end - t.top - 1 }

// Cell is a row's cell text; a row short of cells has empty ones.
func (t *TableText) Cell(row, col int) string {
	if row < 0 || row >= t.Len() {
		return ""
	}
	cells := t.cellTexts(t.top + 2 + row)
	if col < 0 || col >= len(cells) {
		return ""
	}
	return cells[col]
}

// tableData is a table read whole: the fields and every row, each as wide as the fields.
type tableData struct {
	fields []TableField
	rows   [][]string
}

func (t *TableText) read() tableData {
	d := tableData{fields: t.Fields()}
	for r := range t.Len() {
		row := make([]string, len(d.fields))
		for c := range row {
			row[c] = t.Cell(r, c)
		}
		d.rows = append(d.rows, row)
	}
	return d
}

// write replaces the table's lines with d, padded and aligned, as one undo step; ok is false when
// the buffer refuses the edit.
func (t *TableText) write(d tableData) bool {
	widths := make([]int, len(d.fields))
	for c, f := range d.fields {
		widths[c] = max(3, tui.StringWidth(f.Title))
		for _, row := range d.rows {
			widths[c] = max(widths[c], tui.StringWidth(row[c]))
		}
	}
	line := func(cells []string, aligns []style.Align) string {
		var b strings.Builder
		b.WriteString("|")
		for c, w := range widths {
			b.WriteString(" " + padAligned(cells[c], w, aligns[c]) + " |")
		}
		return b.String()
	}
	aligns := make([]style.Align, len(d.fields))
	titles := make([]string, len(d.fields))
	delim := make([]string, len(d.fields))
	for c, f := range d.fields {
		aligns[c], titles[c] = f.Align, f.Title
		delim[c] = delimiterCell(f.Align, widths[c])
	}
	lines := []string{line(titles, aligns), line(delim, make([]style.Align, len(d.fields)))}
	for _, row := range d.rows {
		lines = append(lines, line(row, aligns))
	}
	last := t.core.LineAt(t.end)
	r := TextRange{From: TextPos{Line: t.top}, To: TextPos{Line: t.end, Col: len(clusters(last))}}
	err := t.core.Batch(func() error {
		_, err := t.core.Replace(r, strings.Join(lines, "\n"))
		return err
	})
	if err != nil {
		return false
	}
	t.end = t.top + len(lines) - 1
	return true
}

// padAligned is s in a column w wide, placed as a says.
func padAligned(s string, w int, a style.Align) string {
	gap := max(w-tui.StringWidth(s), 0)
	switch a {
	case style.AlignRight:
		return strings.Repeat(" ", gap) + s
	case style.AlignCenter:
		return strings.Repeat(" ", gap/2) + s + strings.Repeat(" ", gap-gap/2)
	}
	return s + strings.Repeat(" ", gap)
}

// delimiterCell is a delimiter row's cell for a column w wide: --- (left), :-: (centre), --: (right).
func delimiterCell(a style.Align, w int) string {
	switch a {
	case style.AlignCenter:
		return ":" + strings.Repeat("-", w-2) + ":"
	case style.AlignRight:
		return strings.Repeat("-", w-1) + ":"
	}
	return strings.Repeat("-", w)
}

// Format rewrites the table padded and aligned, its content unchanged.
func (t *TableText) Format() bool { return t.write(t.read()) }

// SetCell sets a row's cell. A | in text is escaped, so it stays one cell.
func (t *TableText) SetCell(row, col int, text string) bool {
	d := t.read()
	if row < 0 || row >= len(d.rows) || col < 0 || col >= len(d.fields) {
		return false
	}
	d.rows[row][col] = escapePipes(text)
	return t.write(d)
}

// InsertRow adds an empty row at index at (0 to Len).
func (t *TableText) InsertRow(at int) bool {
	d := t.read()
	if at < 0 || at > len(d.rows) {
		return false
	}
	d.rows = slices.Insert(d.rows, at, make([]string, len(d.fields)))
	return t.write(d)
}

// DeleteRow removes a row.
func (t *TableText) DeleteRow(at int) bool {
	d := t.read()
	if at < 0 || at >= len(d.rows) {
		return false
	}
	d.rows = slices.Delete(d.rows, at, at+1)
	return t.write(d)
}

// InsertField adds a column at index at (0 to the fields' count), its cells empty.
func (t *TableText) InsertField(at int, f TableField) bool {
	d := t.read()
	if at < 0 || at > len(d.fields) {
		return false
	}
	f.Title = escapePipes(f.Title)
	d.fields = slices.Insert(d.fields, at, f)
	for i := range d.rows {
		d.rows[i] = slices.Insert(d.rows[i], at, "")
	}
	return t.write(d)
}

// DeleteField removes a column; the last one is refused, since a table needs one.
func (t *TableText) DeleteField(at int) bool {
	d := t.read()
	if at < 0 || at >= len(d.fields) || len(d.fields) == 1 {
		return false
	}
	d.fields = slices.Delete(d.fields, at, at+1)
	for i := range d.rows {
		d.rows[i] = slices.Delete(d.rows[i], at, at+1)
	}
	return t.write(d)
}

// SetAlign sets a column's alignment.
func (t *TableText) SetAlign(col int, a style.Align) bool {
	d := t.read()
	if col < 0 || col >= len(d.fields) {
		return false
	}
	d.fields[col].Align = a
	return t.write(d)
}

// SortBy orders the rows by a column, as a TableCore sorts (numbers as numbers), and writes them
// back in that order as one undo step.
func (t *TableText) SortBy(col int, desc bool) bool {
	d := t.read()
	if col < 0 || col >= len(d.fields) {
		return false
	}
	slices.SortStableFunc(d.rows, func(a, b []string) int {
		r := compareCells(a[col], b[col])
		if desc {
			r = -r
		}
		return r
	})
	return t.write(d)
}

func escapePipes(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r == '|' && (i == 0 || s[i-1] != '\\') {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
