package widget

import (
	"strings"
	"unicode"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/vt"
)

// normalState is the Terminal's normal mode: a cursor and a view over the
// absolute lines (the scrollback, then the screen). Absolute indices stay
// put while output arrives; lines dropped off a full scrollback shift them,
// which follow corrects.
type normalState struct {
	top       int // first shown line
	line, col int // the cursor
	visual    int // 0 none, 1 char-wise, 2 line-wise
	aLine     int // the selection's anchor
	aCol      int
	pendingG  bool
	searching bool
	backward  bool // the search being typed, or the last one, goes up
	query     string
	last      string // the last search, for n and N
	dropped   int    // the screen's Dropped() when indices were last true
}

const (
	visualNone = iota
	visualChar
	visualLine
)

func (n *normalState) enter(t *Terminal) {
	row, col, _, _ := t.scr.Cursor()
	sb := t.scr.Scrollback()
	*n = normalState{last: n.last, backward: n.backward}
	n.top, n.line, n.col = sb, sb+row, col
	n.dropped = t.scr.Dropped()
	n.clamp(t)
}

func (n *normalState) leave() {
	n.visual, n.pendingG, n.searching, n.query = visualNone, false, false, ""
}

// follow keeps the view on the same text as output arrives.
func (n *normalState) follow(t *Terminal) {
	if t.mode != TerminalNormal {
		return
	}
	if d := t.scr.Dropped() - n.dropped; d > 0 {
		n.top -= d
		n.line -= d
		n.aLine -= d
		n.dropped += d
	}
	n.clamp(t)
}

// clamp keeps the cursor on a line and in the view, and the view on lines.
func (n *normalState) clamp(t *Terminal) {
	count := t.lineCount()
	_, cols := t.scr.Size()
	h := max(t.shownRows(), 1)
	n.line = min(max(n.line, 0), count-1)
	n.aLine = min(max(n.aLine, 0), count-1)
	n.col = min(max(n.col, 0), cols-1)
	n.top = min(max(n.top, 0), max(count-h, 0))
	if n.line < n.top {
		n.top = n.line
	}
	if n.line >= n.top+h {
		n.top = n.line - h + 1
	}
}

// scroll moves the view by d lines, carrying the cursor when it would leave.
func (n *normalState) scroll(t *Terminal, d int) {
	h := max(t.shownRows(), 1)
	n.top = min(max(n.top+d, 0), max(t.lineCount()-h, 0))
	n.line = min(max(n.line, n.top), n.top+h-1)
	n.clamp(t)
	t.MarkDirty()
}

// key handles a normal-mode key. Keys it does not use reach the host.
func (n *normalState) key(t *Terminal, k tui.KeyEvent) bool {
	if n.searching {
		return n.searchKey(t, k)
	}
	if n.pendingG && isKey(k, literalG...) {
		// g's grammar: the literal g after it is the first line, wherever a map moved the prefix
		n.pendingG = false
		n.line, n.col = 0, 0
		n.moved(t)
		return true
	}
	cmd, ok := t.Context().KeyCommand(k, terminalNormalCommands...)
	if !ok {
		if k.Mods.Chord()&(tui.ModCtrl|tui.ModAlt|tui.ModSuper|tui.ModHyper|tui.ModMeta) == 0 {
			n.pendingG = false // g's grammar: an unbound key ends it, and is the host's
		}
		return false
	}
	act, ok := terminalNormalActions[cmd.ID]
	if !ok {
		return false // TestEveryNormalCommandHasAnAction keeps this unreached
	}
	return act(n, t)
}

// normalAction runs one normal-mode command; the bool is whether the key was used.
type normalAction func(n *normalState, t *Terminal) bool

// terminalNormalActions is what each of terminalNormalCommands does. The scrolls and Cancel keep
// g's grammar as it was; every other command ends it (moving).
var terminalNormalActions = map[string]normalAction{
	TermHalfUp.ID:   func(n *normalState, t *Terminal) bool { n.scroll(t, -max(t.shownRows(), 1)/2); return true },
	TermHalfDown.ID: func(n *normalState, t *Terminal) bool { n.scroll(t, max(t.shownRows(), 1)/2); return true },
	TermPageUp.ID:   func(n *normalState, t *Terminal) bool { n.scroll(t, -max(t.shownRows(), 1)); return true },
	TermPageDown.ID: func(n *normalState, t *Terminal) bool { n.scroll(t, max(t.shownRows(), 1)); return true },
	TermCancel.ID:   (*normalState).cancel,

	TermInput.ID:     moving(func(_ *normalState, t *Terminal) { t.setMode(TerminalInput) }),
	TermLeft.ID:      moving(func(n *normalState, _ *Terminal) { n.col-- }),
	TermRight.ID:     moving(func(n *normalState, _ *Terminal) { n.col++ }),
	TermDown.ID:      moving(func(n *normalState, _ *Terminal) { n.line++ }),
	TermUp.ID:        moving(func(n *normalState, _ *Terminal) { n.line-- }),
	TermLineStart.ID: moving(func(n *normalState, _ *Terminal) { n.col = 0 }),
	TermLineEnd.ID: moving(func(n *normalState, t *Terminal) {
		n.col = max(len(lineRunes(t.lineCells(n.line)))-1, 0)
	}),
	TermWord.ID:     moving(func(n *normalState, t *Terminal) { n.line, n.col = wordForward(t, n.line, n.col) }),
	TermWordBack.ID: moving(func(n *normalState, t *Terminal) { n.line, n.col = wordBackward(t, n.line, n.col) }),
	TermWordEnd.ID:  moving(func(n *normalState, t *Terminal) { n.line, n.col = wordEnd(t, n.line, n.col) }),
	TermGoPrefix.ID: func(n *normalState, _ *Terminal) bool { n.pendingG = true; return true },
	TermBottom.ID:   moving(func(n *normalState, t *Terminal) { n.line, n.col = t.lineCount()-1, 0 }),

	TermVisual.ID:     moving(func(n *normalState, _ *Terminal) { n.toggleVisual(visualChar) }),
	TermVisualLine.ID: moving(func(n *normalState, _ *Terminal) { n.toggleVisual(visualLine) }),
	TermYank.ID:       (*normalState).yank,

	TermSearch.ID:     moving(func(n *normalState, _ *Terminal) { n.searching, n.backward, n.query = true, false, "" }),
	TermSearchBack.ID: moving(func(n *normalState, _ *Terminal) { n.searching, n.backward, n.query = true, true, "" }),
	TermNext.ID:       moving(func(n *normalState, t *Terminal) { n.repeatSearch(t, false) }),
	TermPrev.ID:       moving(func(n *normalState, t *Terminal) { n.repeatSearch(t, true) }),
}

// moving is a command that ends g's grammar, runs fn, and keeps the cursor on the text.
func moving(fn func(n *normalState, t *Terminal)) normalAction {
	return func(n *normalState, t *Terminal) bool {
		n.pendingG = false
		fn(n, t)
		n.moved(t)
		return true
	}
}

// cancel ends a selection or g's grammar; with neither, Esc is the host's (a drawer closes).
func (n *normalState) cancel(t *Terminal) bool {
	if n.visual == visualNone && !n.pendingG {
		return false
	}
	n.visual, n.pendingG = visualNone, false
	t.MarkDirty()
	return true
}

// toggleVisual starts a selection of mode at the cursor, switches to mode, or ends mode's.
func (n *normalState) toggleVisual(mode int) {
	switch n.visual {
	case mode:
		n.visual = visualNone
	case visualNone:
		n.aLine, n.aCol = n.line, n.col
		n.visual = mode
	default:
		n.visual = mode
	}
}

// yank copies the selection and ends it; with none, the key is used and nothing moves.
func (n *normalState) yank(t *Terminal) bool {
	n.pendingG = false
	if n.visual == visualNone {
		return true
	}
	if ctx := t.Context(); ctx != nil {
		ctx.CopyToClipboard(n.selection(t))
	}
	n.visual = visualNone
	n.moved(t)
	return true
}

// repeatSearch runs the last search again, the other way when reverse.
func (n *normalState) repeatSearch(t *Terminal, reverse bool) {
	if n.last != "" {
		n.search(t, n.last, n.backward != reverse)
	}
}

func (n *normalState) moved(t *Terminal) {
	n.clamp(t)
	t.MarkDirty()
}

// searchKey edits the search being typed on the bottom row.
func (n *normalState) searchKey(t *Terminal, k tui.KeyEvent) bool {
	cmd, isCmd := t.Context().KeyCommand(k, terminalSearchCommands...)
	switch {
	case isCmd && cmd.ID == TermSearchClose.ID:
		n.searching = false
	case isCmd && cmd.ID == TermSearchRun.ID:
		n.searching = false
		if n.query != "" {
			n.last = n.query
			n.search(t, n.query, n.backward)
		}
	case isCmd && cmd.ID == TermSearchBackspace.ID:
		if r := []rune(n.query); len(r) > 0 {
			n.query = string(r[:len(r)-1])
		} else {
			n.searching = false
		}
	case k.Text != "" && k.Mods.Chord()&nonTextMods == 0:
		n.query += k.Text
	default:
		return true // a search line takes every key while it is open
	}
	n.moved(t)
	return true
}

// search moves the cursor to the next match of q, wrapping around.
func (n *normalState) search(t *Terminal, q string, backward bool) {
	count := t.lineCount()
	needle := []rune(q)
	for i := 0; i <= count; i++ {
		line := n.line + i
		if backward {
			line = n.line - i
		}
		line = ((line % count) + count) % count
		cols := matchCols(lineRunes(t.lineCells(line)), needle)
		if backward {
			for j := len(cols) - 1; j >= 0; j-- {
				if i > 0 || cols[j] < n.col {
					n.line, n.col = line, cols[j]
					return
				}
			}
		} else {
			for _, c := range cols {
				if i > 0 || c > n.col {
					n.line, n.col = line, c
					return
				}
			}
		}
		if i == count { // wrapped to the cursor's own line
			break
		}
	}
}

// matchCols are the cell columns where needle starts in cells.
func matchCols(cells []rune, needle []rune) []int {
	var out []int
	for i := 0; i+len(needle) <= len(cells); i++ {
		if string(cells[i:i+len(needle)]) == string(needle) {
			out = append(out, i)
		}
	}
	return out
}

// lineRunes is a line's text, one rune per cell (a wide cluster's
// continuation is a NUL, a blank a space), trailing blanks trimmed.
func lineRunes(cells []vt.Cell) []rune {
	out := make([]rune, len(cells))
	for i, c := range cells {
		switch {
		case c.Width == 0:
			out[i] = 0
		case c.Content == "":
			out[i] = ' '
		default:
			out[i] = []rune(c.Content)[0]
		}
	}
	n := len(out)
	for n > 0 && out[n-1] == ' ' {
		n--
	}
	return out[:n]
}

func isWordRune(r rune) bool { return r != 0 && !unicode.IsSpace(r) }

// cellLines is what the normal-mode motions read: the lines, each line's runes, the width a line
// wraps at, and whether a line continues on the next. Terminal is one; a fake is a test's.
type cellLines interface {
	lineCount() int
	runes(i int) []rune
	width() int
	wrapped(i int) bool
}

// runes is line i's runes, as the motions read it (cellLines).
func (t *Terminal) runes(i int) []rune { return lineRunes(t.lineCells(i)) }

// width is the columns a line wraps at (cellLines).
func (t *Terminal) width() int {
	_, cols := t.scr.Size()
	return cols
}

func runeAt(t cellLines, line, col int) rune {
	rs := t.runes(line)
	if col < 0 || col >= len(rs) {
		return ' '
	}
	return rs[col]
}

// step moves one cell forward or back across lines; ok is false at an end.
func step(t cellLines, line, col, d int) (int, int, bool) {
	cols := t.width()
	col += d
	if col >= cols {
		if line+1 >= t.lineCount() {
			return line, cols - 1, false
		}
		return line + 1, 0, true
	}
	if col < 0 {
		if line == 0 {
			return 0, 0, false
		}
		return line - 1, cols - 1, true
	}
	return line, col, true
}

// wordForward is w: the start of the next run of non-blanks.
func wordForward(t cellLines, line, col int) (int, int) {
	l, c := line, col
	ok := true
	for ok && isWordRune(runeAt(t, l, c)) {
		l2, c2, more := step(t, l, c, 1)
		if l2 != l {
			l, c = l2, c2
			break // a line end ends a word
		}
		l, c, ok = l2, c2, more
	}
	for ok && !isWordRune(runeAt(t, l, c)) {
		l, c, ok = step(t, l, c, 1)
	}
	if !ok {
		return line, col
	}
	return l, c
}

// wordBackward is b: the start of this or the previous run of non-blanks.
func wordBackward(t cellLines, line, col int) (int, int) {
	l, c, ok := step(t, line, col, -1)
	for ok && !isWordRune(runeAt(t, l, c)) {
		l, c, ok = step(t, l, c, -1)
	}
	for ok {
		l2, c2, more := step(t, l, c, -1)
		if !more || l2 != l || !isWordRune(runeAt(t, l2, c2)) {
			break
		}
		l, c = l2, c2
	}
	return l, c
}

// wordEnd is e: the end of this or the next run of non-blanks.
func wordEnd(t cellLines, line, col int) (int, int) {
	l, c, ok := step(t, line, col, 1)
	for ok && !isWordRune(runeAt(t, l, c)) {
		l, c, ok = step(t, l, c, 1)
	}
	for ok {
		l2, c2, more := step(t, l, c, 1)
		if !more || l2 != l || !isWordRune(runeAt(t, l2, c2)) {
			break
		}
		l, c = l2, c2
	}
	return l, c
}

// span orders the selection's ends.
func (n *normalState) span() (l0, c0, l1, c1 int) {
	l0, c0, l1, c1 = n.aLine, n.aCol, n.line, n.col
	if l1 < l0 || (l1 == l0 && c1 < c0) {
		l0, c0, l1, c1 = l1, c1, l0, c0
	}
	return
}

func (n *normalState) selected(line, col int) bool {
	if n.visual == visualNone {
		return false
	}
	l0, c0, l1, c1 := n.span()
	if line < l0 || line > l1 {
		return false
	}
	if n.visual == visualLine {
		return true
	}
	return (line > l0 || col >= c0) && (line < l1 || col <= c1)
}

// selection is the selected text. Lines autowrap broke are joined; others
// end in a newline, and trailing blanks are trimmed.
func (n *normalState) selection(t cellLines) string {
	l0, c0, l1, c1 := n.span()
	var b strings.Builder
	for line := l0; line <= l1; line++ {
		rs := t.runes(line)
		from, to := 0, len(rs)
		if n.visual == visualChar {
			if line == l0 {
				from = min(c0, len(rs))
			}
			if line == l1 {
				to = min(c1+1, len(rs))
			}
		}
		for _, r := range rs[from:to] {
			if r != 0 {
				b.WriteRune(r)
			}
		}
		if line < l1 && !t.wrapped(line) {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// render draws the search line over the bottom row while one is typed.
func (n *normalState) render(t *Terminal, s tui.Surface) {
	if !n.searching {
		return
	}
	size := s.Size()
	prompt := "/"
	if n.backward {
		prompt = "?"
	}
	st := style.New().Foreground(style.TokenForeground).Background(style.TokenBackground)
	text := []rune(prompt + n.query)
	for x := 0; x < size.W; x++ {
		c := " "
		if x < len(text) {
			c = string(text[x])
		}
		s.SetCell(x, size.H-1, c, st)
	}
}

func (n *normalState) cursor(t *Terminal) (x, y int, ok bool) {
	if n.searching {
		return len([]rune(n.query)) + 1, max(t.shownRows(), 1) - 1, true
	}
	y = n.line - n.top
	return n.col, y, y >= 0 && y < max(t.shownRows(), 1)
}

// wrapped reports whether absolute line i continues on the next.
func (t *Terminal) wrapped(i int) bool {
	sb := t.scr.Scrollback()
	if i < sb {
		return t.scr.LineWrapped(i)
	}
	return t.scr.Wrapped(i - sb)
}
