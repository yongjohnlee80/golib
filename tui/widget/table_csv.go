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

// TABLE CSV (ADR 1792303119 §4.2) — a CSV or TSV document in an EditorCore as a TableSource, a
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
// content. A TableCore fences an open edit to it (§4.3c).
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
	t.load(t.core.Value())
	t.rev, t.read = t.core.Revision(), true
	return true
}

// load reads text: the records with their spans, or why it cannot be edited.
func (t *TableCSV) load(text string) {
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
func (t *TableCSV) encode(cells []string) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Comma = t.comma
	_ = w.Write(cells)
	w.Flush()
	return strings.TrimSuffix(b.String(), "\n")
}

// writable is whether an edit may write now: the records read from the core as it is.
func (t *TableCSV) writable() bool {
	t.Sync()
	return t.why == "" && !t.core.ReadOnly()
}

// Commit applies op as one write: the inserts and the cell computed first, then one Replace of the
// bytes from the first record changed to the last, the gaps between them copied as they are.
func (t *TableCSV) Commit(op TableOp) bool {
	if !t.writable() {
		return false
	}
	width := t.width
	if op.InsertField >= 0 {
		if op.InsertField > width {
			return false
		}
		width++
	}
	if op.InsertRow >= 0 && op.InsertRow > len(t.records) {
		return false
	}
	rows := len(t.records)
	if op.InsertRow >= 0 {
		rows++
	}
	if op.Col < 0 || op.Col >= width || op.Row < -1 || op.Row >= rows {
		return false
	}

	// every record as it will be: a copy of its cells, and whether its text changes
	all := make([]recordWrite, 0, len(t.records)+1)
	recs := make([]*csvRecord, 0, len(t.records)+1)
	if t.header != nil {
		all = append(all, recordWrite{cells: slices.Clone(t.header.cells)})
		recs = append(recs, t.header)
	}
	for i := range t.records {
		all = append(all, recordWrite{cells: slices.Clone(t.records[i].cells)})
		recs = append(recs, &t.records[i])
	}
	if t.header == nil { // an empty file: the edit makes its header
		all = append([]recordWrite{{cells: make([]string, t.width), dirty: true}}, all...)
		recs = append([]*csvRecord{nil}, recs...)
	}
	if op.InsertField >= 0 {
		for i := range all {
			c := all[i].cells
			if len(c) < op.InsertField {
				c = append(c, make([]string, op.InsertField-len(c))...)
			}
			title := ""
			if i == 0 {
				title = op.Field.Title
			}
			all[i].cells, all[i].dirty = slices.Insert(c, op.InsertField, title), true
		}
	}
	inserted := -1 // the index in all of the inserted record
	if op.InsertRow >= 0 {
		inserted = op.InsertRow + 1
		all = slices.Insert(all, inserted, recordWrite{cells: make([]string, width), dirty: true})
		recs = slices.Insert(recs, inserted, nil)
	}
	at := op.Row + 1
	c := all[at].cells
	if len(c) <= op.Col { // a short record is padded up to the cell edited, no further
		c = append(c, make([]string, op.Col+1-len(c))...)
	}
	if c[op.Col] != op.Text || all[at].dirty {
		c[op.Col] = op.Text
		all[at].cells, all[at].dirty = c, true
	}
	return t.writeRecords(all, recs)
}

// recordWrite is a record's cells after an edit, and whether its text changes.
type recordWrite struct {
	cells []string
	dirty bool
}

// writeRecords writes the records that changed: recs[i] is all[i]'s record in the text, nil for one
// inserted. The span replaced runs from the first changed record's start to the last's end; between
// them every record and gap is copied or rewritten in order. Nothing changed writes nothing.
func (t *TableCSV) writeRecords(all []recordWrite, recs []*csvRecord) bool {
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
	// the bytes replaced: from the first changed record's start (or, for one inserted, the end of the
	// record before it) to the last's end
	from, to := len(t.text), 0
	for i := first; i <= last; i++ {
		if recs[i] != nil {
			from, to = min(from, recs[i].from), max(to, recs[i].to)
			continue
		}
		at := 0
		if prev := t.prevRecord(recs, i); prev != nil {
			at = prev.to
		}
		from, to = min(from, at), max(to, at)
	}
	var b strings.Builder
	pos := from
	for i := first; i <= last; i++ {
		r := recs[i]
		if r == nil { // inserted at pos: after the record before it, or at the top
			// it takes the line ending of the record before it; after a last record with none, the
			// line break goes before it, so the file still ends as it did
			lead := pos > 0 && t.text[pos-1] != '\n'
			if lead {
				b.WriteByte('\n')
			}
			b.WriteString(t.encode(all[i].cells))
			if !lead && (pos > 0 || pos < len(t.text) || i < last) {
				b.WriteByte('\n')
			}
			continue
		}
		b.WriteString(t.text[pos:r.from]) // the gap before it, as it is
		body := t.text[r.from:r.to]
		ending := ""
		if strings.HasSuffix(body, "\n") {
			ending = "\n"
		}
		if all[i].dirty {
			b.WriteString(t.encode(all[i].cells) + ending)
		} else {
			b.WriteString(body)
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

// prevRecord is the record in the text before all[i]: the nearest earlier one that is not inserted.
func (t *TableCSV) prevRecord(recs []*csvRecord, i int) *csvRecord {
	for j := i - 1; j >= 0; j-- {
		if recs[j] != nil {
			return recs[j]
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
	r := t.records[at]
	rng := TextRange{From: t.posOf(r.from), To: t.posOf(r.to)}
	err := t.core.Batch(func() error {
		_, err := t.core.Replace(rng, "")
		return err
	})
	t.Sync()
	return err == nil
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
	all := make([]recordWrite, 0, len(t.records)+1)
	recs := make([]*csvRecord, 0, len(t.records)+1)
	for _, r := range append([]*csvRecord{t.header}, t.recordPtrs()...) {
		w := recordWrite{cells: r.cells}
		if at < len(r.cells) {
			w.cells, w.dirty = slices.Delete(slices.Clone(r.cells), at, at+1), true
		}
		all, recs = append(all, w), append(recs, r)
	}
	return t.writeRecords(all, recs)
}

func (t *TableCSV) recordPtrs() []*csvRecord {
	out := make([]*csvRecord, len(t.records))
	for i := range t.records {
		out[i] = &t.records[i]
	}
	return out
}

// SetAlign is refused: CSV has no alignment.
func (t *TableCSV) SetAlign(int, style.Align) bool { return false }

var (
	_ TableSource     = (*TableCSV)(nil)
	_ TableEditor     = (*TableCSV)(nil)
	_ TableCommitter  = (*TableCSV)(nil)
	_ TableRevisioner = (*TableCSV)(nil)
)
