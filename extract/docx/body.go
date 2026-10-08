package docx

import (
	"encoding/xml"
	"strconv"
	"strings"
)

// para is the paragraph being read: its text, style and list position.
type para struct {
	text   strings.Builder
	style  string
	numID  string
	ilvl   int
	inList bool
}

// table is the table being read: its rows of cells, written once the table closes. Only one row
// is open at a time; finished rows are written as they close.
type table struct {
	row     []string
	cell    strings.Builder
	rows    int
	columns int
	inCell  bool
}

// readDocument streams word/document.xml into Markdown.
func (x *doc) readDocument(d *xml.Decoder) error {
	var p *para
	var tables []*table // nested tables flatten into the outermost
	skip := 0           // depth of a subtree being skipped (media, deleted text), 0 when none
	inText := false
	return x.tokens(d, func(tok xml.Token, depth int) error {
		if skip > 0 {
			if _, ok := tok.(xml.EndElement); ok && depth == skip {
				skip = 0
			}
			return nil
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "drawing", "pict", "object", "del", "instrText", "footnoteReference", "commentReference", "fldData":
				skip = depth
			case "p":
				p = &para{}
			case "pStyle":
				if p != nil {
					p.style = attr(t, "val")
				}
			case "numId":
				if p != nil {
					p.numID, p.inList = attr(t, "val"), attr(t, "val") != "0"
				}
			case "ilvl":
				if p != nil {
					p.ilvl, _ = strconv.Atoi(attr(t, "val"))
				}
			case "t":
				inText = true
			case "tab":
				if p != nil {
					p.text.WriteByte('\t')
				}
			case "br", "cr":
				if p != nil {
					p.text.WriteByte('\n')
				}
			case "tbl":
				tables = append(tables, &table{})
			case "tr":
				if len(tables) == 1 {
					tables[0].row = tables[0].row[:0]
				}
			case "tc":
				if len(tables) == 1 {
					tables[0].inCell = true
					tables[0].cell.Reset()
				}
			}
		case xml.CharData:
			if inText && p != nil {
				if p.text.Len()+len(t) > x.maxBlock {
					return ErrBlockTooLarge
				}
				p.text.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				if p == nil {
					return nil
				}
				if len(tables) > 0 {
					tb := tables[0]
					if tb.inCell {
						if tb.cell.Len() > 0 {
							tb.cell.WriteString("<br>")
						}
						tb.cell.WriteString(strings.TrimSpace(p.text.String())) // escapeCell turns its breaks into <br>
						if tb.cell.Len() > x.maxBlock {
							return ErrBlockTooLarge
						}
					}
				} else if err := x.paragraph(p); err != nil {
					return err
				}
				p = nil
			case "tc":
				if len(tables) == 1 {
					tb := tables[0]
					tb.row = append(tb.row, escapeCell(tb.cell.String()))
					tb.inCell = false
				}
			case "tr":
				if len(tables) == 1 {
					if err := x.tableRow(tables[0]); err != nil {
						return err
					}
				}
			case "tbl":
				if len(tables) > 0 {
					tables = tables[:len(tables)-1]
					if len(tables) == 0 {
						x.inList = false
					}
				}
			}
		}
		return nil
	})
}

// paragraph writes one paragraph as a block: a heading, a list item, or text.
func (x *doc) paragraph(p *para) error {
	text := strings.TrimSpace(p.text.String())
	if text == "" {
		return nil
	}
	var line string
	list := false
	switch {
	case x.styles[p.style] > 0:
		line = strings.Repeat("#", x.styles[p.style]) + " " + strings.ReplaceAll(text, "\n", " ")
	case p.inList:
		list = true
		marker := "- "
		if lv := x.numFmt[p.numID]; lv != nil && lv[p.ilvl] {
			marker = "1. "
		}
		line = strings.Repeat("  ", max(p.ilvl, 0)) + marker + strings.ReplaceAll(text, "\n", " ")
	default:
		line = text
	}
	return x.block(line, list)
}

// block writes a block: list items one per line, everything else separated by a blank line.
func (x *doc) block(s string, list bool) error {
	if x.started {
		sep := "\n\n"
		if list && x.inList {
			sep = "\n"
		}
		if _, err := x.out.WriteString(sep); err != nil {
			return err
		}
	}
	x.started, x.inList = true, list
	_, err := x.out.WriteString(s)
	return err
}

// tableRow writes a finished row; the first row is the header, followed by its separator.
func (x *doc) tableRow(tb *table) error {
	if len(tb.row) == 0 {
		return nil
	}
	if tb.rows == 0 {
		tb.columns = len(tb.row)
	}
	cells := tb.row
	for len(cells) < tb.columns {
		cells = append(cells, "")
	}
	line := "| " + strings.Join(cells, " | ") + " |"
	if len(line) > x.maxBlock {
		return ErrBlockTooLarge
	}
	if tb.rows == 0 {
		if err := x.block(line, false); err != nil {
			return err
		}
		sep := strings.Repeat("| --- ", len(cells)) + "|"
		if _, err := x.out.WriteString("\n" + sep); err != nil {
			return err
		}
	} else if _, err := x.out.WriteString("\n" + line); err != nil {
		return err
	}
	tb.rows++
	return nil
}

// escapeCell keeps a cell on its row: pipes escaped, and no line breaks but <br>.
func escapeCell(s string) string {
	s = strings.ReplaceAll(s, `|`, `\|`)
	return strings.ReplaceAll(s, "\n", "<br>")
}
