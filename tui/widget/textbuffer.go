package widget

import "strings"

// textBuffer is the multi-line grapheme-addressed buffer substrate shared by
// TextArea and Editor: a []string of lines with a cursor
// (line + cluster column), a sticky desired column for vertical moves, and a
// single char-wise selection anchor. Methods here are PURE buffer/motion
// operations — no widget concerns (no dirt marking, events, viewport, or
// styles), so the two widgets keep their own interaction semantics on one
// tested core. Cell-based computations take the measuring function as a
// parameter rather than binding to a widget.
type textBuffer struct {
	lines   []string
	ln, col int // cursor line + cluster column
	desired int // sticky column (cells) for vertical moves; -1 unset
	anchor  *taPos
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
	b.lines = append(b.lines[:lo.ln], append([]string{joined}, b.lines[hi.ln+1:]...)...)
	b.ln, b.col = lo.ln, lo.col
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
	cs := b.lineClusters(b.ln)
	head := strings.Join(cs[:b.col], "")
	tail := strings.Join(cs[b.col:], "")
	if len(parts) == 1 {
		b.lines[b.ln] = head + parts[0] + tail
		b.col += len(clusters(parts[0]))
	} else {
		newLines := make([]string, 0, len(parts))
		newLines = append(newLines, head+parts[0])
		newLines = append(newLines, parts[1:len(parts)-1]...)
		lastPart := parts[len(parts)-1]
		newLines = append(newLines, lastPart+tail)
		b.lines = append(b.lines[:b.ln], append(newLines, b.lines[b.ln+1:]...)...)
		b.ln += len(parts) - 1
		b.col = len(clusters(lastPart))
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
