package widget

import "strings"

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
type textBuffer struct {
	lines   []string
	ln, col int // cursor line + cluster column
	desired int // sticky column (cells) for vertical moves; -1 unset
	anchor  *taPos
	// changedFrom is the first line any change has touched since the owner
	// last took it (takeChanged): what a line-by-line consumer — the
	// Editor's highlighter cache — must look at again. Every change to
	// lines calls touch.
	changedFrom int
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

func newTextBuffer() textBuffer {
	return textBuffer{lines: []string{""}, desired: -1}
}

// value returns the buffer joined with newlines.
func (b *textBuffer) value() string { return strings.Join(b.lines, "\n") }

// setValue replaces the buffer, normalizing newlines; cursor to the end,
// selection cleared, sticky column reset.
func (b *textBuffer) setValue(s string) {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	b.lines = strings.Split(s, "\n")
	touchFrom(&b.changedFrom, 0)
	b.ln = len(b.lines) - 1
	b.col = len(clusters(b.lines[b.ln]))
	b.anchor = nil
	b.desired = -1
}

// lineClusters returns the clusters of line i.
func (b *textBuffer) lineClusters(i int) []string { return clusters(b.lines[i]) }

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
	b.lines = append(b.lines[:lo.ln], append([]string{joined}, b.lines[hi.ln+1:]...)...)
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
		sb.WriteString(b.lines[i])
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
		b.lines[b.ln] = head + parts[0] + tail
		// The inserted text may COMBINE with the cluster before the cursor.
		// Counting its clusters in isolation creates a phantom column (e +
		// a combining accent is one grapheme, not two), then the next typed
		// rune slices past cs and panics. Count the recomposed prefix instead.
		b.col = min(len(clusters(head+parts[0])), len(clusters(b.lines[b.ln])))
	} else {
		newLines := make([]string, 0, len(parts))
		newLines = append(newLines, head+parts[0])
		newLines = append(newLines, parts[1:len(parts)-1]...)
		lastPart := parts[len(parts)-1]
		newLines = append(newLines, lastPart+tail)
		b.lines = append(b.lines[:b.ln], append(newLines, b.lines[b.ln+1:]...)...)
		b.ln += len(parts) - 1
		b.col = min(len(clusters(lastPart)), len(clusters(b.lines[b.ln])))
	}
	b.anchor = nil
	b.desired = -1
}

// moveCursor moves the cursor with clamping, managing the selection anchor.
func (b *textBuffer) moveCursor(ln, col int, extend bool) {
	ln = max(0, min(ln, len(b.lines)-1))
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
	ln := max(0, min(b.ln+delta, len(b.lines)-1))
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
		if b.ln < len(b.lines)-1 {
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

// wrapUsableWidth is the width text actually wraps to: the viewport minus the
// scrollbar column when one is showing, and never less than one cell.
func wrapUsableWidth(lines []string, v wrapView) int {
	w := v.w
	if wrapScrollable(lines, v) {
		w--
	}
	return max(w, 1)
}

// wrapRowsOfLine is how many screen rows logical line i occupies.
func wrapRowsOfLine(lines []string, i int, v wrapView) int {
	if v.wrap == WrapNone {
		return 1
	}
	return len(wrapRanges(clusters(lines[i]), wrapUsableWidth(lines, v), v.measure))
}

// wrapPosOf maps a logical (line, column) to the screen row within that line
// and the cell offset across it.
func wrapPosOf(lines []string, ln, col int, v wrapView) (row, x int) {
	cs := clusters(lines[ln])
	rows := wrapRanges(cs, wrapUsableWidth(lines, v), v.measure)
	for i, r := range rows {
		if col < r[0] {
			return i, 0 // col is a wrap-consumed break space
		}
		if col <= r[1] || i == len(rows)-1 {
			return i, cellsBefore(cs[r[0]:], min(col, r[1])-r[0], v.measure)
		}
	}
	return 0, 0
}
