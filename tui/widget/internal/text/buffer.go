package text

import (
	"slices"
	"strings"
)

// Buffer is the multi-line grapheme-addressed buffer substrate shared by
// TextArea and Editor: a []string of lines with a cursor
// (line + cluster column), a sticky desired column for vertical moves, and a
// single char-wise selection anchor.
//
// # Architectural Model
//
// Methods on Buffer are PURE buffer and motion operations. It maintains no widget
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
//     a measuring function, ensuring Buffer never binds to a global or wrong width policy.
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
// A Buffer is a cursor over a text. The text (Doc) is embedded by pointer, so every
// cursor over one document shares its lines: an EditorCore's text is its shown Buffer's, and a
// TextArea's is its own. Doc is data only, with no methods, so embedding it promotes fields
// and never a method; b.Lines, b.ChangedFrom and b.log read as they always have.
type Buffer struct {
	*Doc
	Ln, Col int // cursor line + cluster column
	Desired int // sticky column (cells) for vertical moves; -1 unset
	Anchor  *Pos
}

// Doc is a document's lines and what follows their changes: the part of a Buffer every
// cursor over the same document shares.
type Doc struct {
	// Lines are the document's lines. Only this file assigns them, through the line primitives
	// (SetLine, InsertLines, RemoveLines, ReplaceLines, DeleteRegion, InsertText) and SetValue;
	// internal/audit holds every other file to it, and this file's writes to those functions.
	Lines []string
	// ChangedFrom is the first line any change has touched since the owner
	// last took it (TakeChanged): what a line-by-line consumer — the
	// Editor's highlighter cache — must look at again. Every change to
	// lines calls Touch.
	ChangedFrom int
	// Log, when on (an EditorCore's buffer), is each change to the line structure since the owner
	// last took them, in order: what follows the text (marks) replays them. A change within one
	// line is not logged; marks keep their columns then, as Vim's do. A field with methods of its
	// own, as touchFrom is a function: the buffer's changes record themselves without a call on the
	// receiver that an embedder's override would not see.
	Log EditLog
	// UndoRec, when set (a Buffer's text: its editHistory), is handed every change the line
	// primitives make, as the change that takes it back. Undo is built from these alone, so it is
	// right only while the primitives are the lines' only writers: internal/audit's line-primitives
	// guard holds them to it.
	UndoRec LineRecorder
}

// LineRecorder keeps what takes a document's line changes back: an editHistory.
type LineRecorder interface {
	// Recording reports whether it wants the next change; the primitives copy nothing otherwise.
	Recording() bool
	// Record takes one change back: put op.old in place of lines [op.lo, op.lo+op.n).
	Record(op UndoOp)
}

// UndoOp takes one change to the lines back: old goes in place of lines [lo, lo+n), the n the
// change put there. A change within one line is one line for one; an insert has no old lines.
type UndoOp struct {
	Lo, N int
	Old   []string
}

// recorder is t's undo recorder when it wants a change, else nil: the primitives' one check.
func recorder(t *Doc) LineRecorder {
	if t.UndoRec != nil && t.UndoRec.Recording() {
		return t.UndoRec
	}
	return nil
}

// LineEdit is one change to the buffer's line structure, as Vim's mark_adjust and mark_col_adjust
// see it:
//   - a splice: lines [ln, ln+del) were removed and ins lines put in their place, so a mark on a
//     removed line is gone and one after them moves by ins-del;
//   - a column move (move): a mark on line from at column cut or later (every column, with all)
//     goes to line to, at column at+(col-cut), at itself for a column before cut;
//   - a reset (a new document): every mark is gone.
//
// A producer logs a split's or a join's column move in the order the lines are then: a join moves
// the joined lines' marks before the splice that removes those lines.
type LineEdit struct {
	Move, Reset  bool
	Ln, Del, Ins int
	From, To     int
	Cut, At      int
	All          bool
}

// EditLog is the buffer's log of changes to its line structure; it logs nothing until on.
type EditLog struct {
	On    bool
	edits []LineEdit
}

// splice records lines [ln, ln+del) replaced by ins lines.
func (l *EditLog) splice(ln, del, ins int) {
	if l.On && (del != 0 || ins != 0) {
		l.edits = append(l.edits, LineEdit{Ln: ln, Del: del, Ins: ins})
	}
}

// Move records line from's marks at column cut or later (every one, with all) moving to line to,
// at column at+(col-cut).
func (l *EditLog) Move(from, cut, to, at int, all bool) {
	if l.On {
		l.edits = append(l.edits, LineEdit{Move: true, From: from, Cut: cut, To: to, At: at, All: all})
	}
}

// reset records a new document: every mark is gone, whatever was logged before.
func (l *EditLog) reset() {
	if l.On {
		l.edits = append(l.edits[:0], LineEdit{Reset: true})
	}
}

// Take returns the edits logged since the last call, and forgets them.
func (l *EditLog) Take() []LineEdit {
	e := l.edits
	l.edits = nil
	return e
}

// NoChange is ChangedFrom when nothing has changed.
const NoChange = int(^uint(0) >> 1)

// Touch records that line ln, and possibly every line after it, changed.
func (b *Buffer) Touch(ln int) { touchFrom(&b.ChangedFrom, ln) }

// touchFrom lowers a changed-from mark to ln. A function, not a method, so
// the buffer's own changes record themselves without a call on the receiver
// that an embedder's override would not see.
func touchFrom(mark *int, ln int) { *mark = min(*mark, max(ln, 0)) }

// TakeChanged returns the first line changed since the last call, NoChange
// for none, and forgets it.
func (b *Buffer) TakeChanged() int {
	ln := b.ChangedFrom
	b.ChangedFrom = NoChange
	return ln
}

// THE LINE PRIMITIVES. Every change to the lines goes through one of these, so each change touches
// what it changed (the highlighter's cache, the layout) and logs what it did to the line structure
// (marks, and the other editors over a shared buffer): a site that forgot either left them pointing
// at the wrong lines, with nothing to say so. No file but this one assigns to the lines
// (internal/audit). A join or a split logs its column moves (log.move) before the splice that
// removes or adds the lines, in the order the lines are then.

// SetLine replaces line ln's text: a change within one line, so nothing is logged (marks keep their
// columns, as Vim's do).
func (b *Buffer) SetLine(ln int, s string) {
	if r := recorder(b.Doc); r != nil {
		r.Record(UndoOp{Lo: ln, N: 1, Old: []string{b.Lines[ln]}})
	}
	b.Lines[ln] = s
	touchFrom(&b.ChangedFrom, ln)
}

// InsertLines puts ls before line at (at len(lines): after the last).
func (b *Buffer) InsertLines(at int, ls ...string) {
	if r := recorder(b.Doc); r != nil && len(ls) > 0 {
		r.Record(UndoOp{Lo: at, N: len(ls)})
	}
	b.Lines = slices.Insert(b.Lines, at, ls...)
	touchFrom(&b.ChangedFrom, at)
	b.Log.splice(at, 0, len(ls))
}

// RemoveLines removes lines [lo, hi). Removing every line leaves one empty line, a document's least.
func (b *Buffer) RemoveLines(lo, hi int) {
	if r := recorder(b.Doc); r != nil && hi > lo {
		n := 0
		if lo == 0 && hi >= len(b.Lines) {
			n = 1 // the empty line left
		}
		r.Record(UndoOp{Lo: lo, N: n, Old: slices.Clone(b.Lines[lo:min(hi, len(b.Lines))])})
	}
	if lo == 0 && hi >= len(b.Lines) {
		b.Lines = []string{""}
		touchFrom(&b.ChangedFrom, 0)
		b.Log.splice(0, hi, 1)
		return
	}
	b.Lines = slices.Delete(b.Lines, lo, hi)
	touchFrom(&b.ChangedFrom, lo)
	b.Log.splice(lo, hi-lo, 0)
}

// ReplaceLines puts ls in place of lines [lo, hi).
func (b *Buffer) ReplaceLines(lo, hi int, ls ...string) {
	if r := recorder(b.Doc); r != nil && (hi > lo || len(ls) > 0) {
		r.Record(UndoOp{Lo: lo, N: len(ls), Old: slices.Clone(b.Lines[lo:hi])})
	}
	b.Lines = slices.Replace(b.Lines, lo, hi, ls...)
	touchFrom(&b.ChangedFrom, lo)
	b.Log.splice(lo, hi-lo, len(ls))
}

// Pos is a place in a text: line ln, cluster column col.
type Pos struct{ Ln, Col int }

// Less reports whether p comes before q in the text.
func (p Pos) Less(q Pos) bool { return p.Ln < q.Ln || p.Ln == q.Ln && p.Col < q.Col }

// In reports whether p lies in the region [lo, hiEx).
func (p Pos) In(lo, hiEx Pos) bool { return !p.Less(lo) && p.Less(hiEx) }

// Ordered is a and b, the earlier first.
func Ordered(a, b Pos) (lo, hi Pos) {
	if b.Less(a) {
		return b, a
	}
	return a, b
}

// NewBuffer is a cursor over a text of its own: one empty line.
func NewBuffer() Buffer {
	return Buffer{Doc: NewDoc(), Desired: -1}
}

// NewDoc is an empty document: one empty line.
func NewDoc() *Doc { return &Doc{Lines: []string{""}} }

// Value returns the buffer joined with newlines.
func (b *Buffer) Value() string { return strings.Join(b.Lines, "\n") }

// SetValue replaces the buffer, normalizing newlines; cursor to the end,
// selection cleared, sticky column reset.
func (b *Buffer) SetValue(s string) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	ls := strings.Split(s, "\n")
	if r := recorder(b.Doc); r != nil {
		r.Record(UndoOp{Lo: 0, N: len(ls), Old: slices.Clone(b.Lines)})
	}
	b.Lines = ls
	touchFrom(&b.ChangedFrom, 0)
	b.Log.reset()
	b.Ln = len(b.Lines) - 1
	b.Col = len(Clusters(b.Lines[b.Ln]))
	b.Anchor = nil
	b.Desired = -1
}

// LineClusters returns the clusters of line i.
func (b *Buffer) LineClusters(i int) []string { return Clusters(b.Lines[i]) }

// CellsAt is the display offset (cells) of (ln, col) under measure.
func (b *Buffer) CellsAt(ln, col int, measure func(string) int) int {
	return CellsBefore(b.LineClusters(ln), col, measure)
}

// ClampCol clamps a cluster column into line ln.
func (b *Buffer) ClampCol(ln, col int) int {
	return max(0, min(col, len(b.LineClusters(ln))))
}

// ColForCells returns the cluster column in line ln closest to the cell
// offset cells (for sticky vertical movement).
func (b *Buffer) ColForCells(ln, cells int, measure func(string) int) int {
	cs := b.LineClusters(ln)
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

// Selection returns the ordered selection region, ok=false when none.
func (b *Buffer) Selection() (lo, hi Pos, ok bool) {
	if b.Anchor == nil || (b.Anchor.Ln == b.Ln && b.Anchor.Col == b.Col) {
		return Pos{}, Pos{}, false
	}
	lo, hi = Ordered(*b.Anchor, Pos{Ln: b.Ln, Col: b.Col})
	return lo, hi, true
}

// DeleteRegion removes [lo, hi) and moves the cursor to lo.
func (b *Buffer) DeleteRegion(lo, hi Pos) {
	first := b.LineClusters(lo.Ln)[:lo.Col]
	last := b.LineClusters(hi.Ln)[hi.Col:]
	joined := strings.Join(first, "") + strings.Join(last, "")
	touchFrom(&b.ChangedFrom, lo.Ln)
	if hi.Ln > lo.Ln { // the last line joins the first: its marks move onto it, then the rest go
		b.Log.Move(hi.Ln, hi.Col, lo.Ln, lo.Col, true)
		b.Log.splice(lo.Ln+1, hi.Ln-lo.Ln, 0)
	}
	if r := recorder(b.Doc); r != nil {
		r.Record(UndoOp{Lo: lo.Ln, N: 1, Old: slices.Clone(b.Lines[lo.Ln : hi.Ln+1])})
	}
	b.Lines = slices.Replace(b.Lines, lo.Ln, hi.Ln+1, joined) // in place: no copy of the lines below
	b.Ln, b.Col = lo.Ln, min(lo.Col, len(Clusters(joined)))
	b.Anchor = nil
}

// TextIn returns the text of [lo, hi) with newlines at line boundaries.
func (b *Buffer) TextIn(lo, hi Pos) string {
	if lo.Ln == hi.Ln {
		cs := b.LineClusters(lo.Ln)
		return strings.Join(cs[lo.Col:hi.Col], "")
	}
	var sb strings.Builder
	sb.WriteString(strings.Join(b.LineClusters(lo.Ln)[lo.Col:], ""))
	for i := lo.Ln + 1; i < hi.Ln; i++ {
		sb.WriteString("\n")
		sb.WriteString(b.Lines[i])
	}
	sb.WriteString("\n")
	sb.WriteString(strings.Join(b.LineClusters(hi.Ln)[:hi.Col], ""))
	return sb.String()
}

// InsertText places text at the cursor (replacing any selection) as one
// atomic splice; newlines split lines. Pure — the caller owns viewport
// adjustment and change notification.
func (b *Buffer) InsertText(text string) {
	if lo, hi, ok := b.Selection(); ok {
		b.DeleteRegion(lo, hi)
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	parts := strings.Split(text, "\n")
	touchFrom(&b.ChangedFrom, b.Ln)
	cs := b.LineClusters(b.Ln)
	head := strings.Join(cs[:b.Col], "")
	tail := strings.Join(cs[b.Col:], "")
	if r := recorder(b.Doc); r != nil {
		r.Record(UndoOp{Lo: b.Ln, N: len(parts), Old: []string{b.Lines[b.Ln]}})
	}
	if len(parts) == 1 {
		b.Lines[b.Ln] = head + parts[0] + tail
		// The inserted text may COMBINE with the cluster before the cursor.
		// Counting its clusters in isolation creates a phantom column (e +
		// a combining accent is one grapheme, not two), then the next typed
		// rune slices past cs and panics. Count the recomposed prefix instead.
		b.Col = min(len(Clusters(head+parts[0])), len(Clusters(b.Lines[b.Ln])))
	} else {
		newLines := make([]string, 0, len(parts))
		newLines = append(newLines, head+parts[0])
		newLines = append(newLines, parts[1:len(parts)-1]...)
		lastPart := parts[len(parts)-1]
		newLines = append(newLines, lastPart+tail)
		b.Lines = slices.Replace(b.Lines, b.Ln, b.Ln+1, newLines...) // in place, growing as append does
		// the lines below move down, then the split line's marks after the cursor follow its tail
		b.Log.splice(b.Ln+1, 0, len(parts)-1)
		b.Log.Move(b.Ln, b.Col, b.Ln+len(parts)-1, len(Clusters(lastPart)), false)
		b.Ln += len(parts) - 1
		b.Col = min(len(Clusters(lastPart)), len(Clusters(b.Lines[b.Ln])))
	}
	b.Anchor = nil
	b.Desired = -1
}

// MoveCursor moves the cursor with clamping, managing the selection anchor.
func (b *Buffer) MoveCursor(ln, col int, extend bool) {
	ln = max(0, min(ln, len(b.Lines)-1))
	col = b.ClampCol(ln, col)
	if extend {
		if b.Anchor == nil {
			b.Anchor = &Pos{Ln: b.Ln, Col: b.Col}
		}
	} else {
		b.Anchor = nil
	}
	b.Ln, b.Col = ln, col
}

// VerticalTarget computes the (ln, col) delta lines away keeping the sticky
// column, updating desired as a side effect.
func (b *Buffer) VerticalTarget(delta int, measure func(string) int) (int, int) {
	if b.Desired < 0 {
		b.Desired = b.CellsAt(b.Ln, b.Col, measure)
	}
	ln := max(0, min(b.Ln+delta, len(b.Lines)-1))
	return ln, b.ColForCells(ln, b.Desired, measure)
}

// WordLeft/WordRight within the current line (line hop at the edges) —
// space-delimited readline hops.
func (b *Buffer) WordLeft() (int, int) {
	cs := b.LineClusters(b.Ln)
	i := b.Col
	if i == 0 {
		if b.Ln > 0 {
			return b.Ln - 1, len(b.LineClusters(b.Ln - 1))
		}
		return b.Ln, 0
	}
	for i > 0 && cs[i-1] == " " {
		i--
	}
	for i > 0 && cs[i-1] != " " {
		i--
	}
	return b.Ln, i
}

func (b *Buffer) WordRight() (int, int) {
	cs := b.LineClusters(b.Ln)
	i := b.Col
	if i == len(cs) {
		if b.Ln < len(b.Lines)-1 {
			return b.Ln + 1, 0
		}
		return b.Ln, i
	}
	for i < len(cs) && cs[i] == " " {
		i++
	}
	for i < len(cs) && cs[i] != " " {
		i++
	}
	return b.Ln, i
}

// WrapView is the layout state the soft-wrap geometry needs, which lives on the
// widget rather than on the buffer: the viewport size, whether lines soft-wrap, and the
// active measure func.
type WrapView struct {
	W, H    int
	Wrap    bool // lines soft-wrap (WrapSoft); else they scroll sideways (WrapNone)
	Measure func(string) int
	// Usable is the width text wraps to, once worked out (Settled): it
	// depends on whether the whole text overflows the viewport, so finding
	// it wraps a screenful of lines, and a caller wrapping many lines at one
	// size must not pay that again for every line.
	Usable int
}

// Settled is v with its usable width worked out once, for a caller about to
// wrap several lines of the same text at the same size.
func (v WrapView) Settled(lines []string) WrapView {
	if v.Wrap && v.Usable == 0 {
		v.Usable = WrapUsableWidth(lines, v)
	}
	return v
}

// UsableWidth is the Settled width, or works it out.
func (v WrapView) UsableWidth(lines []string) int {
	if v.Usable > 0 {
		return v.Usable
	}
	return WrapUsableWidth(lines, v)
}

// The four functions below are the soft-wrap geometry Editor and TextArea both
// need. They were byte-identical bodies on each widget; they are FREE FUNCTIONS
// rather than methods on Buffer because that is what they are — pure
// arithmetic over the lines and the viewport, with no receiver state.
//
// Making them methods was the obvious first move and the promotion guard
// refused it: a method on an embeddable base that calls a sibling on its own
// receiver ignores every override, and the guard's list "may only SHRINK".
// Taking the lines as a parameter means there is no base, no promotion, and
// nothing to allowlist.

// WrapScrollable reports whether the content exceeds the viewport height, so a
// scrollbar column is showing.
func WrapScrollable(lines []string, v WrapView) bool {
	if v.H <= 0 {
		return false
	}
	if !v.Wrap {
		return len(lines) > v.H
	}
	rows := 0
	for i := range lines {
		rows += len(WrapRanges(Clusters(lines[i]), max(v.W-1, 1), v.Measure))
		if rows > v.H {
			return true
		}
	}
	return false
}

// WrapMaxTop is the scroll range: the greatest top line at which the last
// line still ends on screen. Unwrapped, it is the lines past one screenful;
// wrapped, it counts each line's rows up from the bottom until they overflow
// the viewport. A last line taller than the viewport is its own top.
func WrapMaxTop(lines []string, v WrapView) int {
	if v.H <= 0 || len(lines) == 0 {
		return 0
	}
	if !v.Wrap {
		return max(len(lines)-v.H, 0)
	}
	w := WrapUsableWidth(lines, v)
	rows := 0
	for i := len(lines) - 1; i >= 0; i-- {
		rows += len(WrapRanges(Clusters(lines[i]), w, v.Measure))
		if rows > v.H {
			return min(i+1, len(lines)-1)
		}
	}
	return 0
}

// WrapUsableWidth is the width text actually wraps to: the viewport minus the
// scrollbar column when one is showing, and never less than one cell.
func WrapUsableWidth(lines []string, v WrapView) int {
	w := v.W
	if WrapScrollable(lines, v) {
		w--
	}
	return max(w, 1)
}

// LowestTop is the top line that keeps line ln on screen: top itself when
// ln's rows and those above it down to top fit in h, else the lowest line
// from which they do (ln itself when ln alone is taller than h). It walks up
// from ln once, at one Settled width, so a cursor move costs a screenful of
// wrapping rather than a screenful for every candidate top.
func LowestTop(lines []string, top, ln, h int, v WrapView) int {
	if top >= ln {
		return top
	}
	v = v.Settled(lines)
	rows, lowest := 0, ln
	for i := ln; i >= top; i-- {
		rows += WrapRowsOfLine(lines, i, v)
		if rows > h {
			break
		}
		lowest = i
	}
	return lowest
}

// WrapRowsOfLine is how many screen rows logical line i occupies.
func WrapRowsOfLine(lines []string, i int, v WrapView) int {
	if !v.Wrap {
		return 1
	}
	return len(WrapRanges(Clusters(lines[i]), v.UsableWidth(lines), v.Measure))
}

// WrapPosOf maps a logical (line, column) to the screen row within that line
// and the cell offset across it.
func WrapPosOf(lines []string, ln, col int, v WrapView) (row, x int) {
	cs := Clusters(lines[ln])
	rows := WrapRanges(cs, v.UsableWidth(lines), v.Measure)
	for i, r := range rows {
		if col < r[0] {
			return i, 0 // col is a wrap-consumed break space
		}
		// a column at a row's end is on the next row when that row starts there (a break inside
		// a word, or after a cluster wider than the row): its character is drawn there
		last := i == len(rows)-1
		if col < r[1] || last || col == r[1] && rows[i+1][0] != col {
			return i, CellsBefore(cs[r[0]:], min(col, r[1])-r[0], v.Measure)
		}
	}
	return 0, 0
}

// ColInRow is the column on rows[row] under x: the cluster whose cells hold x; past the row's end,
// the break space the wrap consumed after it when there is one, else the row's last cluster.
func ColInRow(cs []string, rows [][2]int, row, x int, measure func(string) int) int {
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

// RevealCaret is the window that shows the caret at line ln, cell cx of it, over lines in view v:
// the first line shown moved just far enough (and no lower than the last screenful needs), and in
// WrapNone the first cell shown following cx; wrapped lines have no horizontal scroll. TextArea and
// Editor reveal through it. A view with no size changes nothing.
func RevealCaret(lines []string, v WrapView, top, left, ln, cx int) (int, int) {
	if v.H <= 0 || v.W <= 0 {
		return top, left
	}
	top = min(top, ln)
	top = LowestTop(lines, top, ln, v.H, v)
	top = max(0, min(top, len(lines)-1))
	if v.Wrap {
		return top, 0
	}
	w := WrapUsableWidth(lines, v)
	left = min(left, cx)
	if cx >= left+w {
		left = cx - w + 1
	}
	return top, max(left, 0)
}

// CaretAt is where the caret at line ln, cluster col (cell cx), shows in the window from line top
// and cell left over lines in view v: its cell in the window's text area, and false when it is
// above, below or (WrapNone) left of nothing the window shows.
func CaretAt(lines []string, v WrapView, top, left, ln, col, cx int) (x, y int, ok bool) {
	if ln < top {
		return 0, 0, false
	}
	if !v.Wrap {
		x, y = cx-left, ln-top
		if y >= v.H && v.H > 0 {
			return 0, 0, false
		}
		return max(x, 0), max(y, 0), true
	}
	v = v.Settled(lines)
	for i := top; i < ln; i++ {
		y += WrapRowsOfLine(lines, i, v)
	}
	row, x := WrapPosOf(lines, ln, col, v)
	y += row
	if v.H > 0 && y >= v.H {
		return 0, 0, false
	}
	return x, y, true
}

// BackspaceRegion is what Backspace deletes at the cursor: the cluster before it, or the line break
// that joins this line to the one above. ok is false at the text's start, where it deletes nothing.
// TextArea and the editor's modeless input delete through it (and their own grouping).
func (b *Buffer) BackspaceRegion() (lo, hi Pos, ok bool) {
	switch {
	case b.Col > 0:
		return Pos{b.Ln, b.Col - 1}, Pos{b.Ln, b.Col}, true
	case b.Ln > 0:
		return Pos{b.Ln - 1, len(Clusters(b.Lines[b.Ln-1]))}, Pos{b.Ln, 0}, true
	}
	return Pos{}, Pos{}, false
}

// ForwardRegion is what Delete removes at the cursor: the cluster under it, or the line break that
// joins the next line to this one. ok is false at the text's end.
func (b *Buffer) ForwardRegion() (lo, hi Pos, ok bool) {
	switch {
	case b.Col < len(Clusters(b.Lines[b.Ln])):
		return Pos{b.Ln, b.Col}, Pos{b.Ln, b.Col + 1}, true
	case b.Ln < len(b.Lines)-1:
		return Pos{b.Ln, b.Col}, Pos{b.Ln + 1, 0}, true
	}
	return Pos{}, Pos{}, false
}
