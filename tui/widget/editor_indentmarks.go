package widget

import "github.com/yongjohnlee80/golib/indent"

// IndentMarks selects which lines show their indentation: each leading space drawn as a dimmed
// "·" and each leading tab as a dimmed "→", as Vim's listchars does, so the indentation of the
// line being edited can be read at a glance. Only the leading run is marked; spaces between
// words stay blank.
type IndentMarks uint8

const (
	// IndentMarksOff marks nothing (the default).
	IndentMarksOff IndentMarks = iota
	// IndentMarksCursorLine marks the cursor's line while the editor has the focus, in any
	// mode: the line the cursor is on is the line being edited.
	IndentMarksCursorLine
	// IndentMarksAll marks every line.
	IndentMarksAll
)

// CoreIndentMarks sets which lines show their indentation.
func CoreIndentMarks(m IndentMarks) CoreOption { return func(c *EditorCore) { c.indentMarks = m } }

// SetIndentMarks changes which lines show their indentation, and repaints.
func (c *EditorCore) SetIndentMarks(m IndentMarks) {
	if c.indentMarks == m {
		return
	}
	c.indentMarks = m
	c.markDirty()
}

// IndentMarks reports which lines show their indentation.
func (c *EditorCore) IndentMarks() IndentMarks { return c.indentMarks }

// IndentMarked is how many of line ln's leading clusters a widget draws as indentation marks
// when the mode marks that line, else 0. focused is the widget's focus: IndentMarksCursorLine
// marks only a line being edited. The indentation is indent.Leading's, the rule auto-indent
// uses, and a cluster is marked when it starts inside it: a leading space that carries a
// combining mark is one cluster, and still a space of indentation.
func (c *EditorCore) IndentMarked(ln int, focused bool) int {
	switch c.indentMarks {
	case IndentMarksAll:
	case IndentMarksCursorLine:
		if !focused || ln != c.buf.ln {
			return 0
		}
	default:
		return 0
	}
	line := c.LineAt(ln)
	lead := len(indent.Leading(line))
	n, off := 0, 0
	for _, cl := range clusters(line) {
		if off >= lead {
			break
		}
		n++
		off += len(cl)
	}
	return n
}
