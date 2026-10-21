package widget

import (
	"slices"
	"strings"
)

// textBuffer is the multi-line grapheme-addressed buffer substrate shared by
// TextArea and Editor: a []string of lines with a cursor
// (line + cluster column), a sticky desired column for vertical moves, and a
// single char-wise selection anchor.
//
// # Architectural Model
//
// Methods on textBuffer are PURE buffer and motion operations. It maintains no widget
// concerns (no dirt marking, events, viewports, or style references). This guarantees
// that TextArea (standard typing) and Editor (modal Vim editing) share an identical,
// thoroughly tested multi-line buffer implementation while maintaining distinct
// event and visual semantics.
//
// # Architectural Invariants
//
//  1. Grapheme Addressing: Cursor columns and anchor offsets are counted strictly in
//     UAX #29 grapheme clusters, preventing unicode mutilation when slicing multibyte or
//     composed emoji characters.
//  2. Pure Measurement Independence: All cell-width calculations require the caller to pass
//     a measuring function, ensuring textBuffer never binds to a global or wrong width policy.
//  3. Desired Column Latch: Vertical motions (Up/Down) latch the desired horizontal cell position
//     on the initial move; traversing over shorter intermediate lines does not degrade the column
//     target when arriving at subsequent longer lines.
//  4. Line Splitting Normalization: SetValue normalizes CRLF and CR to LF before splitting.
//
// # Concurrency Model
//
//   - Ownership: loop-goroutine-owned through its embedding container ([TextArea] or [Editor]).
//   - Zero Background Mutation: Concurrent mutation from other goroutines is not supported.
//
// # The Text and the Cursor
//
// A textBuffer is a cursor over a text. The text (bufferText) is embedded by pointer, so every
// cursor over one document shares its lines: an EditorCore's text is its shown Buffer's, and a
// TextArea's is its own. bufferText is data only, with no methods, so embedding it promotes fields
// and never a method; b.textLines, b.changedFrom and b.log read as they always have.
type textBuffer struct {
	*bufferText
	ln, col int // cursor line + cluster column
	desired int // sticky column (cells) for vertical moves; -1 unset
	anchor  *taPos
}

// bufferText is a document's lines and what follows their changes: the part of a textBuffer every
// cursor over the same document shares.
type bufferText struct {
	// textLines are the document's lines. Only this file assigns them, through the line primitives
	// (setLine, insertLines, removeLines, replaceLines, setLines) and setValue; internal/audit holds
	// every other file to it.
	textLines []string
	// changedFrom is the first line any change has touched since the owner
	// last took it (takeChanged): what a line-by-line consumer — the
	// Editor's highlighter cache — must look at again. Every change to
	// lines calls touch.
	changedFrom int
	// log, when on (an EditorCore's buffer), is each change to the line structure since the owner
	// last took them, in order: what follows the text (marks) replays them. A change within one
	// line is not logged; marks keep their columns then, as Vim's do. A field with methods of its
	// own, as touchFrom is a function: the buffer's changes record themselves without a call on the
	// receiver that an embedder's override would not see.
	log editLog
}

// lineEdit is one change to the buffer's line structure, as Vim's mark_adjust and mark_col_adjust
// see it:
//   - a splice: lines [ln, ln+del) were removed and ins lines put in their place, so a mark on a
//     removed line is gone and one after them moves by ins-del;
//   - a column move (move): a mark on line from at column cut or later (every column, with all)
//     goes to line to, at column at+(col-cut), at itself for a column before cut;
//   - a reset (a new document): every mark is gone.
//
// A producer logs a split's or a join's column move in the order the lines are then: a join moves
// the joined lines' marks before the splice that removes those lines.
type lineEdit struct {
	move, reset  bool
	ln, del, ins int
	from, to     int
	cut, at      int
	all          bool
}

// editLog is the buffer's log of changes to its line structure; it logs nothing until on.
type editLog struct {
	on    bool
	edits []lineEdit
}

// splice records lines [ln, ln+del) replaced by ins lines.
func (l *editLog) splice(ln, del, ins int) {
	if l.on && (del != 0 || ins != 0) {
		l.edits = append(l.edits, lineEdit{ln: ln, del: del, ins: ins})
	}
}

// move records line from's marks at column cut or later (every one, with all) moving to line to,
// at column at+(col-cut).
func (l *editLog) move(from, cut, to, at int, all bool) {
	if l.on {
		l.edits = append(l.edits, lineEdit{move: true, from: from, cut: cut, to: to, at: at, all: all})
	}
}

// reset records a new document: every mark is gone, whatever was logged before.
func (l *editLog) reset() {
	if l.on {
		l.edits = append(l.edits[:0], lineEdit{reset: true})
	}
}

// take returns the edits logged since the last call, and forgets them.
func (l *editLog) take() []lineEdit {
	e := l.edits
	l.edits = nil
	return e
}

// noChange is changedFrom when nothing has changed.
const noChange = int(^uint(0) >> 1)

// touch records that line ln, and possibly every line after it, changed.
func (b *textBuffer) touch(ln int) { touchFrom(&b.changedFrom, ln) }

// touchFrom lowers a changed-from mark to ln. A function, not a method, so
// the buffer's own changes record themselves without a call on the receiver
// that an embedder's override would not see.
func touchFrom(mark *int, ln int) { *mark = min(*mark, max(ln, 0)) }

// takeChanged returns the first line changed since the last call, noChange
// for none, and forgets it.
func (b *textBuffer) takeChanged() int {
	ln := b.changedFrom
	b.changedFrom = noChange
	return ln
}

// THE LINE PRIMITIVES. Every change to the lines goes through one of these, so each change touches
// what it changed (the highlighter's cache, the layout) and logs what it did to the line structure
// (marks, and the other editors over a shared buffer): a site that forgot either left them pointing
// at the wrong lines, with nothing to say so. No file but this one assigns to the lines
// (internal/audit). A join or a split logs its column moves (log.move) before the splice that
// removes or adds the lines, in the order the lines are then.

// setLine replaces line ln's text: a change within one line, so nothing is logged (marks keep their
// columns, as Vim's do).
func (b *textBuffer) setLine(ln int, s string) {
	b.textLines[ln] = s
	touchFrom(&b.changedFrom, ln)
}

// insertLines puts ls before line at (at len(lines): after the last).
func (b *textBuffer) insertLines(at int, ls ...string) {
	b.textLines = slices.Insert(b.textLines, at, ls...)
	touchFrom(&b.changedFrom, at)
	b.log.splice(at, 0, len(ls))
}

// removeLines removes lines [lo, hi). Removing every line leaves one empty line, a document's least.
func (b *textBuffer) removeLines(lo, hi int) {
	if lo == 0 && hi >= len(b.textLines) {
		b.textLines = []string{""}
		touchFrom(&b.changedFrom, 0)
		b.log.splice(0, hi, 1)
		return
	}
	b.textLines = slices.Delete(b.textLines, lo, hi)
	touchFrom(&b.changedFrom, lo)
	b.log.splice(lo, hi-lo, 0)
}

// replaceLines puts ls in place of lines [lo, hi).
func (b *textBuffer) replaceLines(lo, hi int, ls ...string) {
	b.textLines = slices.Replace(b.textLines, lo, hi, ls...)
	touchFrom(&b.changedFrom, lo)
	b.log.splice(lo, hi-lo, len(ls))
}

// setLines makes ls the lines whole, logging nothing: a snapshot restored (undo), whose caller
// decides what the log says about it.
func (b *textBuffer) setLines(ls []string) {
	b.textLines = ls
	touchFrom(&b.changedFrom, 0)
}

// newTextBuffer is a cursor over a text of its own: one empty line.
func newTextBuffer() textBuffer {
	return textBuffer{bufferText: newBufferText(), desired: -1}
}

// newBufferText is an empty document: one empty line.
func newBufferText() *bufferText { return &bufferText{textLines: []string{""}} }

// value returns the buffer joined with newlines.
func (b *textBuffer) value() string { return strings.Join(b.textLines, "\n") }

// setValue replaces the buffer, normalizing newlines; cursor to the end,
// selection cleared, sticky column reset.
func (b *textBuffer) setValue(s string) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	b.textLines = strings.Split(s, "\n")
	touchFrom(&b.changedFrom, 0)
	b.log.reset()
	b.ln = len(b.textLines) - 1
	b.col = len(clusters(b.textLines[b.ln]))
	b.anchor = nil
	b.desired = -1
}

// lineClusters returns the clusters of line i.
func (b *textBuffer) lineClusters(i int) []string { return clusters(b.textLines[i]) }

// cellsAt is the display offset (cells) of (ln, col) under measure.
func (b *textBuffer) cellsAt(ln, col int, measure func(string) int) int {
	return cellsBefore(b.lineClusters(ln), col, measure)
}

// clampCol clamps a cluster column into line ln.
func (b *textBuffer) clampCol(ln, col int) int {
	return max(0, min(col, len(b.lineClusters(ln))))
}

// colForCells returns the cluster column in line ln closest to the cell
// offset cells (for sticky vertical movement).
func (b *textBuffer) colForCells(ln, cells int, measure func(string) int) int {
	cs := b.lineClusters(ln)
	w := 0
	for i, c := range cs {
		cw := measure(c)
		if w+cw > cells {
			return i
		}
		w += cw
	}
	return len(cs)
}

// selection returns the ordered selection region, ok=false when none.
func (b *textBuffer) selection() (lo, hi taPos, ok bool) {
	if b.anchor == nil || (b.anchor.ln == b.ln && b.anchor.col == b.col) {
		return taPos{}, taPos{}, false
	}
	a, bb := *b.anchor, taPos{ln: b.ln, col: b.col}
	if a.ln > bb.ln || (a.ln == bb.ln && a.col > bb.col) {
		a, bb = bb, a
	}
	return a, bb, true
}

// deleteRegion removes [lo, hi) and moves the cursor to lo.
func (b *textBuffer) deleteRegion(lo, hi taPos) {
	first := b.lineClusters(lo.ln)[:lo.col]
	last := b.lineClusters(hi.ln)[hi.col:]
	joined := strings.Join(first, "") + strings.Join(last, "")
	touchFrom(&b.changedFrom, lo.ln)
	if hi.ln > lo.ln { // the last line joins the first: its marks move onto it, then the rest go
		b.log.move(hi.ln, hi.col, lo.ln, lo.col, true)
		b.log.splice(lo.ln+1, hi.ln-lo.ln, 0)
	}
	b.textLines = append(b.textLines[:lo.ln], append([]string{joined}, b.textLines[hi.ln+1:]...)...)
	b.ln, b.col = lo.ln, min(lo.col, len(clusters(joined)))
	b.anchor = nil
}

// textIn returns the text of [lo, hi) with newlines at line boundaries.
func (b *textBuffer) textIn(lo, hi taPos) string {
	if lo.ln == hi.ln {
		cs := b.lineClusters(lo.ln)
		return strings.Join(cs[lo.col:hi.col], "")
	}
	var sb strings.Builder
	sb.WriteString(strings.Join(b.lineClusters(lo.ln)[lo.col:], ""))
	for i := lo.ln + 1; i < hi.ln; i++ {
		sb.WriteString("\n")
		sb.WriteString(b.textLines[i])
	}
	sb.WriteString("\n")
	sb.WriteString(strings.Join(b.lineClusters(hi.ln)[:hi.col], ""))
	return sb.String()
}

// insertText places text at the cursor (replacing any selection) as one
// atomic splice; newlines split lines. Pure — the caller owns viewport
// adjustment and change notification.
func (b *textBuffer) insertText(text string) {
	if lo, hi, ok := b.selection(); ok {
		b.deleteRegion(lo, hi)
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	parts := strings.Split(text, "\n")
	touchFrom(&b.changedFrom, b.ln)
	cs := b.lineClusters(b.ln)
	head := strings.Join(cs[:b.col], "")
	tail := strings.Join(cs[b.col:], "")
	if len(parts) == 1 {
		b.textLines[b.ln] = head + parts[0] + tail
		// The inserted text may COMBINE with the cluster before the cursor.
		// Counting its clusters in isolation creates a phantom column (e +
		// a combining accent is one grapheme, not two), then the next typed
		// rune slices past cs and panics. Count the recomposed prefix instead.
		b.col = min(len(clusters(head+parts[0])), len(clusters(b.textLines[b.ln])))
	} else {
		newLines := make([]string, 0, len(parts))
		newLines = append(newLines, head+parts[0])
		newLines = append(newLines, parts[1:len(parts)-1]...)
		lastPart := parts[len(parts)-1]
		newLines = append(newLines, lastPart+tail)
		b.textLines = append(b.textLines[:b.ln], append(newLines, b.textLines[b.ln+1:]...)...)
		// the lines below move down, then the split line's marks after the cursor follow its tail
		b.log.splice(b.ln+1, 0, len(parts)-1)
		b.log.move(b.ln, b.col, b.ln+len(parts)-1, len(clusters(lastPart)), false)
		b.ln += len(parts) - 1
		b.col = min(len(clusters(lastPart)), len(clusters(b.textLines[b.ln])))
	}
	b.anchor = nil
	b.desired = -1
}

// moveCursor moves the cursor with clamping, managing the selection anchor.
func (b *textBuffer) moveCursor(ln, col int, extend bool) {
	ln = max(0, min(ln, len(b.textLines)-1))
	col = b.clampCol(ln, col)
	if extend {
		if b.anchor == nil {
			b.anchor = &taPos{ln: b.ln, col: b.col}
		}
	} else {
		b.anchor = nil
	}
	b.ln, b.col = ln, col
}

// verticalTarget computes the (ln, col) delta lines away keeping the sticky
// column, updating desired as a side effect.
func (b *textBuffer) verticalTarget(delta int, measure func(string) int) (int, int) {
	if b.desired < 0 {
		b.desired = b.cellsAt(b.ln, b.col, measure)
	}
	ln := max(0, min(b.ln+delta, len(b.textLines)-1))
	return ln, b.colForCells(ln, b.desired, measure)
}

// wordLeft/wordRight within the current line (line hop at the edges) —
// space-delimited readline hops.
func (b *textBuffer) wordLeft() (int, int) {
	cs := b.lineClusters(b.ln)
	i := b.col
	if i == 0 {
		if b.ln > 0 {
			return b.ln - 1, len(b.lineClusters(b.ln - 1))
		}
		return b.ln, 0
	}
	for i > 0 && cs[i-1] == " " {
		i--
	}
	for i > 0 && cs[i-1] != " " {
		i--
	}
	return b.ln, i
}

func (b *textBuffer) wordRight() (int, int) {
	cs := b.lineClusters(b.ln)
	i := b.col
	if i == len(cs) {
		if b.ln < len(b.textLines)-1 {
			return b.ln + 1, 0
		}
		return b.ln, i
	}
	for i < len(cs) && cs[i] == " " {
		i++
	}
	for i < len(cs) && cs[i] != " " {
		i++
	}
	return b.ln, i
}

// wrapView is the layout state the soft-wrap geometry needs, which lives on the
// widget rather than on the buffer: the viewport size, the wrap mode, and the
// active measure func.
type wrapView struct {
	w, h    int
	wrap    WrapMode
	measure func(string) int
	// usable is the width text wraps to, once worked out (settled): it
	// depends on whether the whole text overflows the viewport, so finding
	// it wraps a screenful of lines, and a caller wrapping many lines at one
	// size must not pay that again for every line.
	usable int
}

// settled is v with its usable width worked out once, for a caller about to
// wrap several lines of the same text at the same size.
func (v wrapView) settled(lines []string) wrapView {
	if v.wrap != WrapNone && v.usable == 0 {
		v.usable = wrapUsableWidth(lines, v)
	}
	return v
}

// usableWidth is the settled width, or works it out.
func (v wrapView) usableWidth(lines []string) int {
	if v.usable > 0 {
		return v.usable
	}
	return wrapUsableWidth(lines, v)
}

// The four functions below are the soft-wrap geometry Editor and TextArea both
// need. They were byte-identical bodies on each widget; they are FREE FUNCTIONS
// rather than methods on textBuffer because that is what they are — pure
// arithmetic over the lines and the viewport, with no receiver state.
//
// Making them methods was the obvious first move and the promotion guard
// refused it: a method on an embeddable base that calls a sibling on its own
// receiver ignores every override, and the guard's list "may only SHRINK".
// Taking the lines as a parameter means there is no base, no promotion, and
// nothing to allowlist.

// wrapScrollable reports whether the content exceeds the viewport height, so a
// scrollbar column is showing.
func wrapScrollable(lines []string, v wrapView) bool {
	if v.h <= 0 {
		return false
	}
	if v.wrap == WrapNone {
		return len(lines) > v.h
	}
	rows := 0
	for i := range lines {
		rows += len(wrapRanges(clusters(lines[i]), max(v.w-1, 1), v.measure))
		if rows > v.h {
			return true
		}
	}
	return false
}

// wrapMaxTop is the scroll range: the greatest top line at which the last
// line still ends on screen. Unwrapped, it is the lines past one screenful;
// wrapped, it counts each line's rows up from the bottom until they overflow
// the viewport. A last line taller than the viewport is its own top.
func wrapMaxTop(lines []string, v wrapView) int {
	if v.h <= 0 || len(lines) == 0 {
		return 0
	}
	if v.wrap == WrapNone {
		return max(len(lines)-v.h, 0)
	}
	w := wrapUsableWidth(lines, v)
	rows := 0
	for i := len(lines) - 1; i >= 0; i-- {
		rows += len(wrapRanges(clusters(lines[i]), w, v.measure))
		if rows > v.h {
			return min(i+1, len(lines)-1)
		}
	}
	return 0
}

// wrapUsableWidth is the width text actually wraps to: the viewport minus the
// scrollbar column when one is showing, and never less than one cell.
func wrapUsableWidth(lines []string, v wrapView) int {
	w := v.w
	if wrapScrollable(lines, v) {
		w--
	}
	return max(w, 1)
}

// lowestTop is the top line that keeps line ln on screen: top itself when
// ln's rows and those above it down to top fit in h, else the lowest line
// from which they do (ln itself when ln alone is taller than h). It walks up
// from ln once, at one settled width, so a cursor move costs a screenful of
// wrapping rather than a screenful for every candidate top.
func lowestTop(lines []string, top, ln, h int, v wrapView) int {
	if top >= ln {
		return top
	}
	v = v.settled(lines)
	rows, lowest := 0, ln
	for i := ln; i >= top; i-- {
		rows += wrapRowsOfLine(lines, i, v)
		if rows > h {
			break
		}
		lowest = i
	}
	return lowest
}

// wrapRowsOfLine is how many screen rows logical line i occupies.
func wrapRowsOfLine(lines []string, i int, v wrapView) int {
	if v.wrap == WrapNone {
		return 1
	}
	return len(wrapRanges(clusters(lines[i]), v.usableWidth(lines), v.measure))
}

// wrapPosOf maps a logical (line, column) to the screen row within that line
// and the cell offset across it.
func wrapPosOf(lines []string, ln, col int, v wrapView) (row, x int) {
	cs := clusters(lines[ln])
	rows := wrapRanges(cs, v.usableWidth(lines), v.measure)
	for i, r := range rows {
		if col < r[0] {
			return i, 0 // col is a wrap-consumed break space
		}
		// a column at a row's end is on the next row when that row starts there (a break inside
		// a word, or after a cluster wider than the row): its character is drawn there
		last := i == len(rows)-1
		if col < r[1] || last || col == r[1] && rows[i+1][0] != col {
			return i, cellsBefore(cs[r[0]:], min(col, r[1])-r[0], v.measure)
		}
	}
	return 0, 0
}

// colInRow is the column on rows[row] under x: the cluster whose cells hold x; past the row's end,
// the break space the wrap consumed after it when there is one, else the row's last cluster.
func colInRow(cs []string, rows [][2]int, row, x int, measure func(string) int) int {
	r := rows[row]
	w := 0
	for i := r[0]; i < r[1]; i++ {
		cw := measure(cs[i])
		if x < w+cw {
			return i
		}
		w += cw
	}
	if row+1 < len(rows) && rows[row+1][0] > r[1] {
		return r[1] // the break space
	}
	return max(r[0], r[1]-1)
}

// revealCaret is the window that shows the caret at line ln, cell cx of it, over lines in view v:
// the first line shown moved just far enough (and no lower than the last screenful needs), and in
// WrapNone the first cell shown following cx; wrapped lines have no horizontal scroll. TextArea and
// Editor reveal through it. A view with no size changes nothing.
func revealCaret(lines []string, v wrapView, top, left, ln, cx int) (int, int) {
	if v.h <= 0 || v.w <= 0 {
		return top, left
	}
	top = min(top, ln)
	top = lowestTop(lines, top, ln, v.h, v)
	top = max(0, min(top, len(lines)-1))
	if v.wrap != WrapNone {
		return top, 0
	}
	w := wrapUsableWidth(lines, v)
	left = min(left, cx)
	if cx >= left+w {
		left = cx - w + 1
	}
	return top, max(left, 0)
}

// caretAt is where the caret at line ln, cluster col (cell cx), shows in the window from line top
// and cell left over lines in view v: its cell in the window's text area, and false when it is
// above, below or (WrapNone) left of nothing the window shows.
func caretAt(lines []string, v wrapView, top, left, ln, col, cx int) (x, y int, ok bool) {
	if ln < top {
		return 0, 0, false
	}
	if v.wrap == WrapNone {
		x, y = cx-left, ln-top
		if y >= v.h && v.h > 0 {
			return 0, 0, false
		}
		return max(x, 0), max(y, 0), true
	}
	v = v.settled(lines)
	for i := top; i < ln; i++ {
		y += wrapRowsOfLine(lines, i, v)
	}
	row, x := wrapPosOf(lines, ln, col, v)
	y += row
	if v.h > 0 && y >= v.h {
		return 0, 0, false
	}
	return x, y, true
}

// backspaceRegion is what Backspace deletes at the cursor: the cluster before it, or the line break
// that joins this line to the one above. ok is false at the text's start, where it deletes nothing.
// TextArea and the editor's modeless input delete through it (and their own grouping).
func (b *textBuffer) backspaceRegion() (lo, hi taPos, ok bool) {
	switch {
	case b.col > 0:
		return taPos{b.ln, b.col - 1}, taPos{b.ln, b.col}, true
	case b.ln > 0:
		return taPos{b.ln - 1, len(clusters(b.textLines[b.ln-1]))}, taPos{b.ln, 0}, true
	}
	return taPos{}, taPos{}, false
}

// forwardRegion is what Delete removes at the cursor: the cluster under it, or the line break that
// joins the next line to this one. ok is false at the text's end.
func (b *textBuffer) forwardRegion() (lo, hi taPos, ok bool) {
	switch {
	case b.col < len(clusters(b.textLines[b.ln])):
		return taPos{b.ln, b.col}, taPos{b.ln, b.col + 1}, true
	case b.ln < len(b.textLines)-1:
		return taPos{b.ln, b.col}, taPos{b.ln + 1, 0}, true
	}
	return taPos{}, taPos{}, false
}
