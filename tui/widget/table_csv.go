package widget

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/tui/style"
)

// TABLE CSV — a CSV or TSV document in an EditorCore as a TableSource, a
// TableEditor and a TableCommitter, so a TableCore edits the file the user has open. The first record
// is the fields. An edit writes back over the bytes of the records it changes and nothing else, as
// one undo step: a record keeps its own line ending, a gap of blank lines between records is never
// rewritten, and every other byte of the file is as it was.
//
// The core's text is the file with its line breaks as "\n" (EditorCore normalizes CRLF and CR at
// load), so a record's span is a half-open byte range of that text, mapped to the core's positions
// (lines, grapheme columns) only to edit.

// The limits past which a CSV table is read only: Raw edits the file.
const (
	maxCSVRecords = 100_000
	maxCSVLine    = 1 << 20 // bytes
)

// TableRevisioner is a source whose content has a revision: two equal revisions are the same
// content. A TableCore fences an open edit to it.
type TableRevisioner interface {
	Revision() uint64
}

// TableCommitter is a source that applies a whole edit as one operation: every step validated
// first, then the text written once (one undo step); false writes nothing.
type TableCommitter interface {
	Commit(op TableOp) bool
}

// TableOp is one edit: an optional new row and/or field, then a cell's value.
type TableOp struct {
	InsertRow   int        // -1: none; else the source row inserted (Len() appends)
	InsertField int        // -1: none; else the field inserted (len(Fields()) appends)
	Field       TableField // the inserted field's title and align
	Row, Col    int        // the cell set, in the source's coordinates after the inserts; Row -1 is a title
	Text        string
}

// TableCSV is a CSV (comma ',') or TSV ('\t') document in an EditorCore. Loop-owned, as its core.
type TableCSV struct {
	core  *EditorCore
	comma rune
	rev   uint64 // the core's revision the records were read at
	read  bool

	text    string     // the core's text as read
	starts  []int      // the byte offset of each line of text
	header  *csvRecord // the first record; nil for an empty file
	records []csvRecord
	width   int    // the fields: the widest record's cells
	why     string // why the table is read only; "" writable
}

// csvRecord is a record and its bytes in the text, [from, to): to is past its line ending, or the
// text's end.
type csvRecord struct {
	cells    []string
	from, to int
}

// NewTableCSV reads core as CSV (comma ',') or TSV ('\t').
func NewTableCSV(core *EditorCore, comma rune) *TableCSV {
	t := &TableCSV{core: core, comma: comma}
	t.Sync()
	return t
}

// Sync reads the text again when the core changed since (its revision): an edit made in Raw, an
// undo, a reload. It reports whether it read.
func (t *TableCSV) Sync() bool {
	if t.read && t.rev == t.core.Revision() {
		return false
	}
	t.readText(t.core.Value())
	t.rev, t.read = t.core.Revision(), true
	return true
}

// readText reads text: the records with their spans, or why it cannot be edited.
func (t *TableCSV) readText(text string) {
	t.text, t.header, t.records, t.width, t.why = text, nil, nil, 0, ""
	t.starts = t.starts[:0]
	t.starts = append(t.starts, 0)
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			t.starts = append(t.starts, i+1)
		}
	}
	for i := range len(t.starts) {
		if t.lineEnd(i)-t.starts[i] > maxCSVLine {
			t.why = fmt.Sprintf("line %d is longer than %d bytes", i+1, maxCSVLine)
			return
		}
	}
	// Strict quoting: a lazily read field (a bare quote, or one left open to the end of the file,
	// which takes the rest of the file as its value) is not one a write could give back, so it is a
	// parse error and the table read only
	r := csv.NewReader(strings.NewReader(text))
	r.Comma, r.FieldsPerRecord = t.comma, -1
	for {
		cells, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				t.why = fmt.Sprintf("line %d: %v", pe.Line, pe.Err)
			} else {
				t.why = err.Error()
			}
			t.header, t.records = nil, nil
			return
		}
		// The record starts on its first field's line: Read skips blank lines BEFORE a record, so
		// the previous record's end is not this one's start.
		line, _ := r.FieldPos(0)
		rec := csvRecord{cells: cells, from: t.starts[line-1], to: int(r.InputOffset())}
		if t.header == nil {
			t.header = &rec
		} else {
			if len(t.records) == maxCSVRecords {
				t.why = fmt.Sprintf("more than %d records", maxCSVRecords)
				return
			}
			t.records = append(t.records, rec)
		}
		t.width = max(t.width, len(cells))
	}
}

// lineEnd is the byte offset of line i's end, before its "\n".
func (t *TableCSV) lineEnd(i int) int {
	if i+1 < len(t.starts) {
		return t.starts[i+1] - 1
	}
	return len(t.text)
}

// ReadOnly is why the table cannot be edited ("" when it can): a parse error, a limit, or the
// document read only.
func (t *TableCSV) ReadOnly() string {
	t.Sync()
	if t.why == "" && t.core.ReadOnly() {
		return "the document is read only"
	}
	return t.why
}

// Revision is the core's: the content the records were read from.
func (t *TableCSV) Revision() uint64 { return t.core.Revision() }

// Fields are the first record's cells; a record wider than it adds fields titled "column N".
func (t *TableCSV) Fields() []TableField {
	out := make([]TableField, t.width)
	for i := range out {
		if t.header != nil && i < len(t.header.cells) {
			out[i].Title = t.header.cells[i]
		} else {
			out[i].Title = fmt.Sprintf("column %d", i+1)
		}
	}
	return out
}

// Len is the records after the first.
func (t *TableCSV) Len() int { return len(t.records) }

// Cell is a cell's exact value, newlines and tabs included (a view draws them on one line); a
// record short of cells has empty ones.
func (t *TableCSV) Cell(row, col int) string {
	if row < 0 || row >= len(t.records) || col < 0 || col >= len(t.records[row].cells) {
		return ""
	}
	return t.records[row].cells[col]
}

// encode is a record's text, quoted as encoding/csv's Writer quotes it, with no line ending.
func (t *TableCSV) encode(cells []string) string { return encodeRecord(t.comma, cells) }

// encodeRecord is a record's text with comma as the delimiter, quoted as encoding/csv's Writer
// quotes it, with no line ending.
func encodeRecord(comma rune, cells []string) string {
	if len(cells) == 0 || len(cells) == 1 && cells[0] == "" {
		// a record of one empty field: the Writer writes an empty line, which a reader skips as a
		// blank one, and the record would be gone
		return `""`
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Comma = comma
	_ = w.Write(cells)
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n")
}

// writable is whether an edit may write now: the records read from the core as it is.
func (t *TableCSV) writable() bool {
	t.Sync()
	return t.why == "" && !t.core.ReadOnly()
}

// TableCellsCommitter is a source that applies a compound edit as one operation: the whole plan
// validated first, then the text written once (one undo step); false writes nothing.
type TableCellsCommitter interface {
	CommitCells(op TableCellsOp) bool
}

// TableCellsOp is one compound edit over ONE snapshot of the source.
//
// Structure is in the snapshot's coordinates: DeleteRows and DeleteFields name rows and fields as
// read; InsertRows and InsertFields insert before a row or field as read (Len() and the fields'
// count append). Several inserts at one position go in list order; an insert before a deleted row
// or field goes where it was.
//
// Cells are in the RESULT's coordinates, after every structural change: Row -1 is a title. A cell
// may be in an inserted row or field.
//
// Refused whole, nothing written: a duplicate or out-of-range index, no field left, a cell outside
// the result, a cell named twice. Repeated insert positions are allowed: they go in list order.
type TableCellsOp struct {
	DeleteRows   []int
	DeleteFields []int
	InsertRows   []int
	InsertFields []TableFieldAt
	Cells        []TableCell
}

// TableFieldAt is a field inserted before the field At as read.
type TableFieldAt struct {
	At    int
	Field TableField
}

// TableCell is a cell's new value, in the result's coordinates; Row -1 is a title.
type TableCell struct {
	Row, Col int
	Text     string
}

// Commit applies op as one write: TableOp's row and field inserts and its cell, as a TableCellsOp
// (its cell is in the result's coordinates already).
func (t *TableCSV) Commit(op TableOp) bool {
	c := TableCellsOp{Cells: []TableCell{{Row: op.Row, Col: op.Col, Text: op.Text}}}
	if op.InsertRow >= 0 {
		c.InsertRows = []int{op.InsertRow}
	}
	if op.InsertField >= 0 {
		c.InsertFields = []TableFieldAt{{At: op.InsertField, Field: op.Field}}
	}
	return t.CommitCells(c)
}

// CommitCells applies op as one write: the plan built and checked whole, then one Replace of the
// bytes from the first record changed to the last, the gaps between them copied as they are.
func (t *TableCSV) CommitCells(op TableCellsOp) bool {
	if !t.writable() {
		return false
	}
	all, ok := t.plan(op)
	return ok && t.writeRecords(all)
}

// recordWrite is one record of the result, in text order: an existing record (rec != nil) kept,
// rewritten (dirty) or deleted; or a new one (rec == nil).
type recordWrite struct {
	rec     *csvRecord
	cells   []string
	dirty   bool
	deleted bool // its bytes are written as nothing; the gaps around it stay
}

// planCell is a cell while the fields move: absent past a short record's end, until something
// after it is present.
type planCell struct {
	v      string
	absent bool
}

// plan is op over the records as read, in text order, and whether op is valid: rows deleted in
// place and inserted before the row they precede; then each record's fields deleted and inserted;
// then the cells set. A record is dirty when its cells differ from what was read.
func (t *TableCSV) plan(op TableCellsOp) ([]recordWrite, bool) {
	delRow, delField, width, ok := t.validateCellsOp(op)
	if !ok {
		return nil, false
	}
	all := t.planRows(op, delRow)
	t.planFields(all, op, delField)
	if !planCells(all, op, width) {
		return nil, false
	}
	return all, true
}

// validateCellsOp checks op's indices against the records as read: the rows and fields deleted (as
// sets), the result's width, and whether op is valid at all.
func (t *TableCSV) validateCellsOp(op TableCellsOp) (delRow, delField map[int]bool, width int, ok bool) {
	n := len(t.records)
	delRow, ok = indexSet(op.DeleteRows, n)
	if !ok {
		return nil, nil, 0, false
	}
	delField, ok = indexSet(op.DeleteFields, t.width)
	if !ok {
		return nil, nil, 0, false
	}
	width = t.width - len(delField) + len(op.InsertFields)
	if width < 1 && (len(op.DeleteFields) > 0 || len(op.InsertFields) > 0) {
		return nil, nil, 0, false // no field left
	}
	for _, at := range op.InsertRows {
		if at < 0 || at > n {
			return nil, nil, 0, false
		}
	}
	for _, f := range op.InsertFields {
		if f.At < 0 || f.At > t.width {
			return nil, nil, 0, false
		}
	}
	return delRow, delField, width, true
}

// planRows is the rows in text order, each a copy of its cells: the header (one made for an empty
// file), then each record with the inserts before it, deleted ones marked, then the inserts at the end.
func (t *TableCSV) planRows(op TableCellsOp, delRow map[int]bool) []recordWrite {
	n := len(t.records)
	all := make([]recordWrite, 0, n+1+len(op.InsertRows))
	if t.header != nil {
		all = append(all, recordWrite{rec: t.header, cells: slices.Clone(t.header.cells)})
	} else {
		all = append(all, recordWrite{cells: make([]string, t.width), dirty: true})
	}
	inserts := make(map[int]int, len(op.InsertRows)) // records inserted before each row as read
	for _, at := range op.InsertRows {
		inserts[at]++
	}
	inserted := func(before int) {
		for range inserts[before] {
			all = append(all, recordWrite{cells: make([]string, t.width), dirty: true})
		}
	}
	for i := range t.records {
		inserted(i)
		all = append(all, recordWrite{rec: &t.records[i], cells: slices.Clone(t.records[i].cells),
			deleted: delRow[i], dirty: delRow[i]})
	}
	inserted(n)
	return all
}

// planFields deletes and inserts fields in every row kept, walked by the field as read.
func (t *TableCSV) planFields(all []recordWrite, op TableCellsOp, delField map[int]bool) {
	if len(delField) > 0 || len(op.InsertFields) > 0 {
		ins := make(map[int][]TableField, len(op.InsertFields)) // fields inserted before each, in order
		for _, f := range op.InsertFields {
			ins[f.At] = append(ins[f.At], f.Field)
		}
		for i := range all {
			if all[i].deleted {
				continue
			}
			all[i].cells = moveFields(all[i].cells, t.width, delField, ins, i == 0)
		}
	}
}

// planCells sets op's cells in the result's rows (the header, then each record kept) and marks a
// record dirty when its cells differ from what was read. false: a cell is out of range, or named twice.
func planCells(all []recordWrite, op TableCellsOp, width int) bool {
	rows := make([]int, 0, len(all))
	for i := 1; i < len(all); i++ {
		if !all[i].deleted {
			rows = append(rows, i)
		}
	}
	named := make(map[TableCell]bool, len(op.Cells)) // by Row and Col alone: a cell named twice refuses
	for _, c := range op.Cells {
		if c.Row < -1 || c.Row >= len(rows) || c.Col < 0 || c.Col >= width {
			return false
		}
		k := TableCell{Row: c.Row, Col: c.Col}
		if named[k] {
			return false
		}
		named[k] = true
		at := 0
		if c.Row >= 0 {
			at = rows[c.Row]
		}
		cells := all[at].cells
		if len(cells) <= c.Col { // a short record is padded up to the cell set, no further
			cells = append(cells, make([]string, c.Col+1-len(cells))...)
		}
		cells[c.Col] = c.Text
		all[at].cells = cells
	}
	for i := range all {
		if all[i].rec != nil && !all[i].deleted && !slices.Equal(all[i].cells, all[i].rec.cells) {
			all[i].dirty = true
		}
	}
	return true
}

// indexSet is idx as a set, and whether every index is in [0, n) and none repeats.
func indexSet(idx []int, n int) (map[int]bool, bool) {
	set := make(map[int]bool, len(idx))
	for _, i := range idx {
		if i < 0 || i >= n || set[i] {
			return nil, false
		}
		set[i] = true
	}
	return set, true
}

// moveFields is a record's cells with fields deleted and inserted, both named as read (width
// fields); an inserted field's cell is its title in the header, "" elsewhere. A short record stays
// short: a field past its end is filled only when a present cell comes after it.
func moveFields(cells []string, width int, del map[int]bool, ins map[int][]TableField, header bool) []string {
	out := make([]planCell, 0, width+len(ins))
	for j := 0; j <= width; j++ {
		for _, f := range ins[j] {
			v := ""
			if header {
				v = f.Title
			}
			out = append(out, planCell{v: v})
		}
		if j == width || del[j] {
			continue
		}
		if j < len(cells) {
			out = append(out, planCell{v: cells[j]})
		} else {
			out = append(out, planCell{absent: true})
		}
	}
	for len(out) > 0 && out[len(out)-1].absent {
		out = out[:len(out)-1]
	}
	res := make([]string, len(out))
	for i, c := range out {
		res[i] = c.v
	}
	return res
}

// writeRecords writes the records that changed. The span replaced runs from the first changed
// record's start (or, for one inserted, the end of the record before it) to the last's end; between
// them every record and gap is copied, rewritten or, for one deleted, left out with its line ending.
// Nothing changed writes nothing.
func (t *TableCSV) writeRecords(all []recordWrite) bool {
	first, last := -1, -1
	for i, w := range all {
		if w.dirty {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return true
	}
	from, to := len(t.text), 0
	for i := first; i <= last; i++ {
		if r := all[i].rec; r != nil {
			from, to = min(from, r.from), max(to, r.to)
			continue
		}
		at := 0
		if prev := prevRecord(all, i); prev != nil {
			at = prev.to
		}
		from, to = min(from, at), max(to, at)
	}
	var b strings.Builder
	// ended is whether what comes before the next write ends a line: the text before the span,
	// then what was written. A record left out does not count, so an insert after a deleted last
	// record with no line ending follows the line ending before it.
	ended := from == 0 || t.text[from-1] == '\n'
	wrote := func(s string) {
		if s != "" {
			b.WriteString(s)
			ended = s[len(s)-1] == '\n'
		}
	}
	pos := from
	for i := first; i <= last; i++ {
		r := all[i].rec
		if r == nil { // inserted here: after the record before it, or at the top
			// it takes the line ending of the record before it; after a last record with none, the
			// line break goes before it, so the file still ends as it did
			lead, before := !ended, from > 0 || b.Len() > 0
			if lead {
				wrote("\n")
			}
			wrote(t.encode(all[i].cells))
			// a line ending after it unless it is the whole text: nothing before it, nothing after
			if !lead && (before || pos < len(t.text) || i < last) {
				wrote("\n")
			}
			continue
		}
		wrote(t.text[pos:r.from]) // the gap before it, as it is
		body := t.text[r.from:r.to]
		switch {
		case all[i].deleted:
		case all[i].dirty:
			ending := ""
			if strings.HasSuffix(body, "\n") {
				ending = "\n"
			}
			wrote(t.encode(all[i].cells) + ending)
		default:
			wrote(body)
		}
		pos = r.to
	}
	rng := TextRange{From: t.posOf(from), To: t.posOf(to)}
	err := t.core.Batch(func() error {
		_, err := t.core.Replace(rng, b.String())
		return err
	})
	t.Sync()
	return err == nil
}

// prevRecord is the record in the text before all[i]: the nearest earlier one read, deleted or not.
func prevRecord(all []recordWrite, i int) *csvRecord {
	for j := i - 1; j >= 0; j-- {
		if all[j].rec != nil {
			return all[j].rec
		}
	}
	return nil
}

// posOf is byte offset off of the text as the core's position: its line, and its column in
// grapheme clusters. A record's bytes start and end at a line start, the text's end, or (a record
// with no line ending) a line's end, so off is never inside a cluster.
func (t *TableCSV) posOf(off int) TextPos {
	line := sort.Search(len(t.starts), func(i int) bool { return t.starts[i] > off }) - 1
	return TextPos{Line: line, Col: len(clusters(t.text[t.starts[line]:off]))}
}

// SetCell sets a cell; row -1 is a field's title.
func (t *TableCSV) SetCell(row, col int, text string) bool {
	return t.Commit(TableOp{InsertRow: -1, InsertField: -1, Row: row, Col: col, Text: text})
}

// InsertRow inserts a record of empty cells at index at (0 to Len).
func (t *TableCSV) InsertRow(at int) bool {
	if !t.writable() || at < 0 || at > len(t.records) || t.width == 0 {
		return false
	}
	return t.Commit(TableOp{InsertRow: at, InsertField: -1, Row: at, Col: 0, Text: ""})
}

// DeleteRow removes a record's bytes, its line ending with them; the gaps around it stay.
func (t *TableCSV) DeleteRow(at int) bool {
	if !t.writable() || at < 0 || at >= len(t.records) {
		return false
	}
	return t.CommitCells(TableCellsOp{DeleteRows: []int{at}})
}

// InsertField adds a field at index at (0 to the fields' count): every record rewritten, one step.
func (t *TableCSV) InsertField(at int, f TableField) bool {
	if !t.writable() || at < 0 || at > t.width || t.header == nil {
		return false
	}
	return t.Commit(TableOp{InsertRow: -1, InsertField: at, Field: f, Row: -1, Col: at, Text: f.Title})
}

// DeleteField removes a field from every record that has it, as one step; the last field is
// refused.
func (t *TableCSV) DeleteField(at int) bool {
	if !t.writable() || at < 0 || at >= t.width || t.width == 1 {
		return false
	}
	return t.CommitCells(TableCellsOp{DeleteFields: []int{at}})
}

// SetAlign is refused: CSV has no alignment.
func (t *TableCSV) SetAlign(int, style.Align) bool { return false }

var (
	_ TableSource         = (*TableCSV)(nil)
	_ TableEditor         = (*TableCSV)(nil)
	_ TableCommitter      = (*TableCSV)(nil)
	_ TableCellsCommitter = (*TableCSV)(nil)
	_ TableRevisioner     = (*TableCSV)(nil)
)
