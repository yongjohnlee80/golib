package widget

import (
	"strings"

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
	// transient is a view the wheel opened under WithModelessScroll: the next key or paste, or the
	// view back at the bottom, returns to input (Terminal.endTransientView)
	transient bool
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
	n.visual, n.pendingG, n.searching, n.query, n.transient = visualNone, false, false, "", false
}

// atBottom reports the view on the screen's last lines, where input mode draws.
func (n *normalState) atBottom(t *Terminal) bool {
	return n.top >= max(t.lineCount()-max(t.shownRows(), 1), 0)
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
	if n.pendingG && n.goSecond(t, k) {
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

// goSecond is the key after g, wherever a map moved the prefix: the literal g is the first line, e
// and E the end of the previous word and WORD (ge, gE). It reports whether k was one; any other key
// goes on as a command.
func (n *normalState) goSecond(t *Terminal, k tui.KeyEvent) bool {
	switch {
	case isKey(k, literalG...):
		n.line, n.col = 0, 0
	case isKey(k, literalE...):
		n.wordMotion(t, wordEndBackFrom, false)
	case isKey(k, literalShiftE...):
		n.wordMotion(t, wordEndBackFrom, true)
	default:
		return false
	}
	n.pendingG = false
	n.moved(t)
	return true
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
	TermWord.ID:     moving(func(n *normalState, t *Terminal) { n.wordMotion(t, wordForwardFrom, false) }),
	TermWordBack.ID: moving(func(n *normalState, t *Terminal) { n.wordMotion(t, wordBackFrom, false) }),
	TermWordEnd.ID:  moving(func(n *normalState, t *Terminal) { n.wordMotion(t, wordEndFrom, false) }),
	TermWORD.ID:     moving(func(n *normalState, t *Terminal) { n.wordMotion(t, wordForwardFrom, true) }),
	TermWORDBack.ID: moving(func(n *normalState, t *Terminal) { n.wordMotion(t, wordBackFrom, true) }),
	TermWORDEnd.ID:  moving(func(n *normalState, t *Terminal) { n.wordMotion(t, wordEndFrom, true) }),
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

// cellLines is what a normal-mode selection reads: the lines, each line's runes, and whether a line
// continues on the next. Terminal is one; a fake is a test's. (The word motions read terminalWords.)
type cellLines interface {
	lineCount() int
	runes(i int) []rune
	wrapped(i int) bool
}

// runes is line i's runes, as a selection reads them (cellLines).
func (t *Terminal) runes(i int) []rune { return lineRunes(t.lineCells(i)) }

// terminalWords is a Terminal's lines as the word motions read them (wordText): each line's
// grapheme clusters, a wide cluster once, the trailing blanks gone. w, b, e and ge then mean in
// normal mode what they mean in the editor.
type terminalWords struct{ t *Terminal }

func (w terminalWords) lineCount() int { return w.t.lineCount() }

func (w terminalWords) lineClusters(i int) []string {
	cs, _ := lineClustersOf(w.t.lineCells(i))
	return cs
}

// lineClustersOf is a line's clusters and the cell each starts at: a wide cluster's continuation
// cell is no cluster, a blank cell is a space, and the trailing blanks are trimmed.
func lineClustersOf(cells []vt.Cell) (cs []string, cols []int) {
	for x, c := range cells {
		switch {
		case c.Width == 0:
			continue
		case c.Content == "":
			cs = append(cs, " ")
		default:
			cs = append(cs, c.Content)
		}
		cols = append(cols, x)
	}
	n := len(cs)
	for n > 0 && cs[n-1] == " " {
		n--
	}
	return cs[:n], cols[:n]
}

// clusterAtCell is the cluster index of cell col in a line's cells: the cluster covering it, or
// the line's end at or past the cell after its text.
func clusterAtCell(cells []vt.Cell, col int) int {
	_, cols := lineClustersOf(cells)
	if len(cols) == 0 || col >= cellAfterText(cells) {
		return len(cols)
	}
	idx := 0
	for j, c := range cols {
		if c > col {
			break
		}
		idx = j
	}
	return idx
}

// cellAfterText is the cell just past a line's text.
func cellAfterText(cells []vt.Cell) int {
	_, cols := lineClustersOf(cells)
	if len(cols) == 0 {
		return 0
	}
	last := cols[len(cols)-1]
	return last + max(int(cells[last].Width), 1)
}

// cellOfCluster is the cell cluster idx starts at; the line's end, as normal mode's cursor rests
// on a line's last character, is its last cluster's cell (0 on an empty line).
func cellOfCluster(cells []vt.Cell, idx int) int {
	_, cols := lineClustersOf(cells)
	switch {
	case len(cols) == 0:
		return 0
	case idx >= len(cols):
		return cols[len(cols)-1]
	}
	return cols[idx]
}

// wordMotion runs one of the editor's word motions from the cursor, by WORD with big, and puts the
// cursor where it lands.
func (n *normalState) wordMotion(t *Terminal, motion func(*wordWalker) (int, int), big bool) {
	ln, idx := motion(newWordWalker(terminalWords{t}, n.line, clusterAtCell(t.lineCells(n.line), n.col), big))
	n.line, n.col = ln, cellOfCluster(t.lineCells(ln), idx)
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
