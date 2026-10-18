package widget

import (
	"encoding/csv"
	"errors"
	"io"
	"sort"
	"strings"
)

// TABLE CLIPBOARD TEXT — the cells a table copies and pastes, as text: quoted TSV, written by the record
// encoder TableCSV writes a file with and read by the reader it reads one with, so a copy reads
// back exactly and a spreadsheet reads it as TSV.
//
// Line breaks are "\n". encoding/csv drops a "\r" before a "\n", inside quotes too, so the codec's
// domain is LF text: EditorCore keeps a document's text in LF, and the App makes a clipboard read's
// and a paste's line breaks LF. A source that is not so normalized must normalize first; a value
// holding "\r\n" decodes as "\n".

// EncodeTableText is rows as quoted TSV: each row written as TableCSV writes a record (a lone
// empty cell as ""), joined by "\n", with no final line ending. A value with no tab, newline,
// quote or leading space is written as it is.
func EncodeTableText(rows [][]string) string {
	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(encodeRecord('\t', r))
	}
	return b.String()
}

// DecodeTableText reads text as TableCSV reads a file, with a tab as the delimiter: strict quotes,
// ragged rows and trailing empty cells kept. A final "\n" ends the last row; every other line no
// record holds is blank (the reader skips those) and is a row of one empty cell, found as the file
// reader finds a record's span: from its first field's line (FieldPos) to its end (InputOffset), so
// a blank line inside a quoted value stays in its cell. A parse error is returned with no rows.
func DecodeTableText(text string) ([][]string, error) {
	if text == "" {
		return nil, nil
	}
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' && i+1 < len(text) {
			starts = append(starts, i+1)
		}
	}
	lineOf := func(off int) int { return sort.SearchInts(starts, off+1) - 1 }
	type span struct {
		first, last int // the lines a record takes
		cells       []string
	}
	var recs []span
	r := csv.NewReader(strings.NewReader(text))
	r.Comma, r.FieldsPerRecord = '\t', -1
	for {
		cells, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		line, _ := r.FieldPos(0)
		end := int(r.InputOffset()) - 1 // its last byte: its line ending, or the text's last
		recs = append(recs, span{first: line - 1, last: lineOf(end), cells: cells})
	}
	var rows [][]string
	line := 0
	for _, s := range recs {
		for ; line < s.first; line++ {
			rows = append(rows, []string{""})
		}
		rows = append(rows, s.cells)
		line = s.last + 1
	}
	for ; line < len(starts); line++ {
		rows = append(rows, []string{""})
	}
	return rows, nil
}

// pastedRows is a pasted text as rows, in this order: one row of one cell, its value (a quoted
// single cell gives back what it quotes); rows; a text that does not decode but is one line with no
// tab, that line as it is (a bare quote pastes as typed); otherwise the parse error, and no rows.
func pastedRows(text string) ([][]string, error) {
	rows, err := DecodeTableText(text)
	if err == nil {
		return rows, nil
	}
	if line := strings.TrimSuffix(text, "\n"); !strings.ContainsAny(line, "\n\t") {
		return [][]string{{line}}, nil
	}
	return nil, err
}
