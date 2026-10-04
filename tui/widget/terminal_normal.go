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
	h := max(t.h, 1)
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
	h := max(t.h, 1)
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
	mods := k.Mods.Chord()
	if mods&(tui.ModAlt|tui.ModSuper|tui.ModHyper|tui.ModMeta) != 0 {
		return false
	}
	h := max(t.h, 1)
	if mods&tui.ModCtrl != 0 {
		switch {
		case isCtrl(k, 'u'):
			n.scroll(t, -h/2)
		case isCtrl(k, 'd'):
			n.scroll(t, h/2)
		case isCtrl(k, 'b'):
			n.scroll(t, -h)
		case isCtrl(k, 'f'):
			n.scroll(t, h)
		default:
			return false
		}
		return true
	}
	if k.Code == tui.KeyEscape {
		if n.visual != visualNone || n.pendingG {
			n.visual, n.pendingG = visualNone, false
			t.MarkDirty()
			return true
		}
		return false // nothing to cancel: the host's Esc (a drawer closes)
	}
	key := k.Text
	switch k.Code {
	case tui.KeyLeft:
		key = "h"
	case tui.KeyRight:
		key = "l"
	case tui.KeyUp:
		key = "k"
	case tui.KeyDown:
		key = "j"
	case tui.KeyHome:
		key = "0"
	case tui.KeyEnd:
		key = "$"
	case tui.KeyPageUp:
		n.scroll(t, -h)
		return true
	case tui.KeyPageDown:
		n.scroll(t, h)
		return true
	case tui.KeyEnter:
		key = "\r"
	}
	if n.pendingG {
		n.pendingG = false
		if key == "g" {
			n.line, n.col = 0, 0
			n.moved(t)
			return true
		}
	}
	switch key {
	case "i", "a", "\r":
		t.setMode(TerminalInput)
	case "h":
		n.col--
	case "l":
		n.col++
	case "j":
		n.line++
	case "k":
		n.line--
	case "0":
		n.col = 0
	case "$":
		n.col = max(len(lineRunes(t.lineCells(n.line)))-1, 0)
	case "w":
		n.line, n.col = wordForward(t, n.line, n.col)
	case "b":
		n.line, n.col = wordBackward(t, n.line, n.col)
	case "e":
		n.line, n.col = wordEnd(t, n.line, n.col)
	case "g":
		n.pendingG = true
		return true
	case "G":
		n.line, n.col = t.lineCount()-1, 0
	case "v", "V":
		mode := visualChar
		if key == "V" {
			mode = visualLine
		}
		if n.visual == mode {
			n.visual = visualNone
		} else {
			if n.visual == visualNone {
				n.aLine, n.aCol = n.line, n.col
			}
			n.visual = mode
		}
	case "y":
		if n.visual == visualNone {
			return true
		}
		if ctx := t.Context(); ctx != nil {
			ctx.CopyToClipboard(n.selection(t))
		}
		n.visual = visualNone
	case "/", "?":
		n.searching, n.backward, n.query = true, key == "?", ""
	case "n", "N":
		if n.last != "" {
			n.search(t, n.last, n.backward != (key == "N"))
		}
	default:
		return false
	}
	n.moved(t)
	return true
}

func (n *normalState) moved(t *Terminal) {
	n.clamp(t)
	t.MarkDirty()
}

// searchKey edits the search being typed on the bottom row.
func (n *normalState) searchKey(t *Terminal, k tui.KeyEvent) bool {
	switch {
	case k.Code == tui.KeyEscape:
		n.searching = false
	case k.Code == tui.KeyEnter:
		n.searching = false
		if n.query != "" {
			n.last = n.query
			n.search(t, n.query, n.backward)
		}
	case k.Code == tui.KeyBackspace:
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

func runeAt(t *Terminal, line, col int) rune {
	rs := lineRunes(t.lineCells(line))
	if col < 0 || col >= len(rs) {
		return ' '
	}
	return rs[col]
}

// step moves one cell forward or back across lines; ok is false at an end.
func step(t *Terminal, line, col, d int) (int, int, bool) {
	_, cols := t.scr.Size()
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
func wordForward(t *Terminal, line, col int) (int, int) {
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
func wordBackward(t *Terminal, line, col int) (int, int) {
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
func wordEnd(t *Terminal, line, col int) (int, int) {
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
func (n *normalState) selection(t *Terminal) string {
	l0, c0, l1, c1 := n.span()
	var b strings.Builder
	for line := l0; line <= l1; line++ {
		rs := lineRunes(t.lineCells(line))
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
		return len([]rune(n.query)) + 1, max(t.h, 1) - 1, true
	}
	y = n.line - n.top
	return n.col, y, y >= 0 && y < max(t.h, 1)
}

// wrapped reports whether absolute line i continues on the next.
func (t *Terminal) wrapped(i int) bool {
	sb := t.scr.Scrollback()
	if i < sb {
		return t.scr.LineWrapped(i)
	}
	return t.scr.Wrapped(i - sb)
}
