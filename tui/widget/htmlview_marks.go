package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui/style"
)

// HTMLVIEW MARKS — a find over the page: every match of the host's find in the text the page
// shows, marked, and the current one drawn as a selection and brought into view. The text is what
// the reader sees, block by block: its spans' text, the layout's decorations left out (a list
// marker, a table's cell separators), so a link's target, or a word inside markup syntax, is not
// found. The host says how a block's text is searched (case, whole words), as its own find does.

// HTMLLayoutMarks is a layout that marks a find's matches in the text it shows.
type HTMLLayoutMarks interface {
	// SetMarks marks every match find returns in each block's text (byte ranges, from-to, in
	// order), none current, and returns how many there are; nil clears them. The layout keeps find
	// and marks a new document with it.
	SetMarks(find func(text string) [][2]int) int
	// SetCurrentMark makes the i-th mark the current one; out of range, none. A wide block pans to
	// show it.
	SetCurrentMark(i int)
	// MarkY is the y of the i-th mark's first row; false when there is no such mark.
	MarkY(i int) (float32, bool)
}

// SetMarks marks every match of find in the text the page shows, none current, and returns how
// many there are; nil clears them. A layout that marks nothing (not HTMLLayoutMarks) has none.
func (v *HTMLView) SetMarks(find func(text string) [][2]int) int {
	l, ok := v.layout.(HTMLLayoutMarks)
	if !ok {
		return 0
	}
	n := l.SetMarks(find)
	v.MarkDirty()
	return n
}

// MarkTo makes the i-th mark the current one, drawn as a selection, and brings it into view: to the
// view's top when it is outside the view. Out of range, none is current and the view stays.
func (v *HTMLView) MarkTo(i int) {
	l, ok := v.layout.(HTMLLayoutMarks)
	if !ok {
		return
	}
	l.SetCurrentMark(i)
	if y, ok := l.MarkY(i); ok && (y < v.scrollY || y >= v.scrollY+v.viewHeight()) {
		v.scrollTo(y)
	}
	v.MarkDirty()
}

// markRange is one mark: the boundary before its first cluster and the one after its last.
type markRange struct{ from, to DocPos }

// cellMarks is the cell layout's marks: the find it keeps, its matches in document order, and
// the current one (-1: none).
type cellMarks struct {
	find  func(text string) [][2]int
	marks []markRange
	cur   int
}

var _ HTMLLayoutMarks = (*cellHTML)(nil)

func (c *cellHTML) SetMarks(find func(text string) [][2]int) int {
	c.m.find, c.m.cur = find, -1
	c.markAll()
	return len(c.m.marks)
}

func (c *cellHTML) SetCurrentMark(i int) {
	if i < 0 || i >= len(c.m.marks) {
		i = -1
	}
	c.m.cur = i
	if i >= 0 {
		c.panToMark(c.m.marks[i])
	}
}

// panToMark pans a wide code block so the mark's first row of it shows between the box's sides, or a
// wide table so it shows between the mark columns: its start, when the whole of it can't.
func (c *cellHTML) panToMark(m markRange) {
	bi := m.from.Block
	if c.over(bi) == 0 || bi >= len(c.tops) {
		return
	}
	for ri := c.tops[bi]; ri < len(c.rows) && c.rows[ri].block == bi; ri++ {
		r := &c.rows[ri]
		lo, hi := -1, -1
		for _, k := range r.cls {
			if k.sp != nil && !k.deco && k.pos.Before(m.to) && m.from.Before(k.end) {
				if lo < 0 {
					lo = k.x
				}
				hi = k.x + k.w
			}
		}
		if lo < 0 {
			continue
		}
		shift := 0 // a wide table's line is drawn a column right, past the « column (shownAt)
		if r.pans {
			shift = 1
		}
		off := c.offset(bi)
		if hi+shift-off > c.width-1 { // past the right side
			off = hi + shift - (c.width - 1)
		}
		if first := c.codeLeft(r) + 1; lo+shift-off < first { // before the left side: its inset's cell kept
			off = lo + shift - first - 1
		}
		c.panBlock(bi, off-c.offset(bi))
		return
	}
}

func (c *cellHTML) MarkY(i int) (float32, bool) {
	if i < 0 || i >= len(c.m.marks) {
		return 0, false
	}
	at := c.m.marks[i].from
	if at.Block >= len(c.tops) {
		return 0, false
	}
	for ri := c.tops[at.Block]; ri < len(c.rows) && c.rows[ri].block == at.Block; ri++ {
		for _, k := range c.rows[ri].cls {
			if k.sp != nil && !at.Before(k.pos) && at.Before(k.end) {
				return float32(ri), true
			}
		}
	}
	return float32(c.tops[at.Block]), true
}

// markAll finds the kept find's matches in the document's blocks; a new document keeps no current
// mark past its count.
func (c *cellHTML) markAll() {
	c.m.marks = c.m.marks[:0]
	if c.m.find == nil {
		c.m.cur = -1
		return
	}
	for bi := range c.doc {
		text, at := blockText(bi, &c.doc[bi])
		for _, r := range c.m.find(text) {
			if r[0] < 0 || r[0] >= r[1] || r[1] > len(text) {
				continue
			}
			c.m.marks = append(c.m.marks, markRange{at(r[0], false), at(r[1], true)})
		}
	}
	if c.m.cur >= len(c.m.marks) {
		c.m.cur = -1
	}
}

// blockText is block bi's text as the page shows it, its decorations left out, and at, the
// position of a byte offset in it: the start of the span it falls in, or, for a match's end, the
// end of the span before it.
func blockText(bi int, b *hblock) (string, func(off int, end bool) DocPos) {
	var sb strings.Builder
	type run struct{ span, start, end int }
	var runs []run
	for si := range b.spans {
		sp := &b.spans[si]
		if sp.deco || sp.text == "" {
			continue
		}
		runs = append(runs, run{si, sb.Len(), sb.Len() + len(sp.text)})
		sb.WriteString(sp.text)
	}
	return sb.String(), func(off int, end bool) DocPos {
		for _, r := range runs {
			if off >= r.start && off < r.end || end && off == r.end {
				return DocPos{Block: bi, Span: r.span, Offset: off - r.start}
			}
		}
		if len(runs) == 0 {
			return DocPos{Block: bi}
		}
		last := runs[len(runs)-1]
		return DocPos{Block: bi, Span: last.span, Offset: last.end - last.start}
	}
}

// markOf is how cluster k is marked: inside a mark, and inside the current one.
func (c *cellHTML) markOf(k hcl) (marked, current bool) {
	if k.sp == nil || k.deco || k.pos == k.end {
		return false, false
	}
	for i, m := range c.m.marks {
		if m.from.Block > k.pos.Block {
			break
		}
		if k.pos.Before(m.to) && m.from.Before(k.end) {
			return true, i == c.m.cur
		}
	}
	return false, false
}

// markStyle is a marked cluster's ground, as the theme sets off a warning.
var markStyle = style.New().Background(style.TokenWarning).Foreground(style.TokenTextOnWarning)
