package widget

import (
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// LIST EDITING — a Markdown list as a writer's editor keeps it, in Insert mode, when on
// (CoreListEditing / SetListEditing):
//
//   - Enter on an item starts the next one: the same indentation and marker, an ordered number
//     one up, a task's box unchecked. Enter on an empty item ends the list: the marker goes.
//   - Backspace just after the marker removes the marker, leaving the indentation.
//   - Tab anywhere on an item nests it one level, by its marker's width; Shift+Tab un-nests it.
//
// Nothing else changes: off a list item, and with a selection, the keys do what they always do.

// CoreListEditing turns list editing on or off.
func CoreListEditing(on bool) CoreOption { return func(c *EditorCore) { c.listEditing = on } }

// SetListEditing turns list editing on or off.
func (c *EditorCore) SetListEditing(on bool) { c.listEditing = on }

// ListEditing reports whether list editing is on.
func (c *EditorCore) ListEditing() bool { return c.listEditing }

// listItem is a list item's line read: its indentation, its marker (the bullet or number, the
// spaces after it, and a task's box) and the marker's own width, which a nested item indents by.
type listItem struct {
	indent, marker string
	unit           int // the bullet or number and one space: "- " is 2, "10. " is 4
}

// readListItem reads line as a list item: indentation, then '-', '*' or '+', or a number and '.'
// or ')', then a space. A thematic break ("* * *", "---") is not one.
func readListItem(line string) (listItem, bool) {
	body := strings.TrimLeft(line, " \t")
	indent := line[:len(line)-len(body)]
	n := 0
	switch {
	case body != "" && strings.ContainsRune("-*+", rune(body[0])):
		n = 1
	default:
		for n < len(body) && n < 9 && body[n] >= '0' && body[n] <= '9' {
			n++
		}
		if n == 0 || n >= len(body) || (body[n] != '.' && body[n] != ')') {
			return listItem{}, false
		}
		n++
	}
	if n >= len(body) || body[n] != ' ' || isThematicBreak(body) {
		return listItem{}, false
	}
	unit := n + 1
	m := unit
	for m < len(body) && m < n+4 && body[m] == ' ' {
		m++
	}
	if rest := body[m:]; len(rest) >= 3 && rest[0] == '[' && rest[2] == ']' && strings.ContainsRune(" xX", rune(rest[1])) &&
		(len(rest) == 3 || rest[3] == ' ') {
		m += min(4, len(rest))
	}
	return listItem{indent: indent, marker: body[:m], unit: unit}, true
}

// isThematicBreak reports a line of three or more of one of '-', '*', '_', and spaces.
func isThematicBreak(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || !strings.ContainsRune("-*_", rune(s[0])) {
		return false
	}
	n := 0
	for _, r := range s {
		switch {
		case r == rune(s[0]):
			n++
		case r != ' ' && r != '\t':
			return false
		}
	}
	return n >= 3
}

// next is the marker of the item after it: the number one up, a task's box unchecked.
func (it listItem) next() string {
	m := it.marker
	if m[0] >= '0' && m[0] <= '9' {
		d := 0
		for m[d] >= '0' && m[d] <= '9' {
			d++
		}
		n, _ := strconv.Atoi(m[:d])
		m = strconv.Itoa(n+1) + m[d:]
	}
	if i := strings.Index(m, "["); i >= 0 && i+2 < len(m) && m[i+2] == ']' {
		m = m[:i+1] + " " + m[i+2:]
	}
	return m
}

// listKey handles an Insert-mode key on a list item, reporting whether it did.
func (c *EditorCore) listKey(k tui.KeyEvent) bool {
	b := &c.buf
	if !c.listEditing || c.readOnly || c.vAnchor != (taPos{}) || b.anchor != nil {
		return false
	}
	if k.Mods&^tui.ModShift != 0 || (k.Mods&tui.ModShift != 0 && k.Code != tui.KeyTab) {
		return false
	}
	line := b.lines[b.ln]
	it, ok := readListItem(line)
	if !ok {
		return false
	}
	start := len(it.indent) + len(it.marker) // in clusters: both are ASCII or tabs
	switch k.Code {
	case tui.KeyEnter:
		if b.col < start {
			return false
		}
		c.beginGroup()
		if strings.TrimSpace(line[start:]) == "" {
			b.deleteRegion(taPos{b.ln, 0}, taPos{b.ln, len(b.lineClusters(b.ln))})
		} else {
			b.insertText("\n" + it.indent + it.next())
		}
		c.edited()
		return true
	case tui.KeyBackspace:
		if b.col != start {
			return false
		}
		c.beginGroup()
		b.deleteRegion(taPos{b.ln, len(it.indent)}, taPos{b.ln, start})
		c.edited()
		return true
	case tui.KeyTab:
		col := b.col
		c.beginGroup()
		if k.Mods&tui.ModShift == 0 {
			unit := strings.Repeat(" ", it.unit)
			if strings.HasPrefix(it.indent, "\t") {
				unit = "\t"
			}
			b.col = 0
			b.insertText(unit)
			b.col = col + len(unit)
		} else {
			n := 0
			for n < len(it.indent) && n < it.unit && it.indent[n] == ' ' {
				n++
			}
			if n == 0 && strings.HasPrefix(it.indent, "\t") {
				n = 1
			}
			if n == 0 {
				return true
			}
			b.deleteRegion(taPos{b.ln, 0}, taPos{b.ln, n})
			b.col = max(col-n, 0)
		}
		b.touch(b.ln)
		c.edited()
		return true
	}
	return false
}

// MarkdownListItem reads line as a Markdown list item, as list editing does: its indentation
// and its marker (the bullet or number, the spaces after it, and a task's box).
func MarkdownListItem(line string) (indent, marker string, ok bool) {
	it, ok := readListItem(line)
	return it.indent, it.marker, ok
}
