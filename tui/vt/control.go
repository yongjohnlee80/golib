package vt

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui/internal/grapheme"

	"github.com/yongjohnlee80/golib/tui/internal/vtparse"
)

func (s *Screen) esc(a *vtparse.Action) {
	if a.Inter != "" {
		s.escInter(a)
		return
	}
	switch a.Final {
	case '7':
		s.saveCursor()
	case '8':
		s.restoreCursor()
	case 'D':
		s.index()
	case 'E':
		s.index()
		s.cur.col = 0
	case 'M':
		s.reverseIndex()
	case 'H':
		s.tabs[s.cur.col] = true
	case 'c':
		s.reset()
	case '=':
		s.modes.AppKeypad = true
	case '>':
		s.modes.AppKeypad = false
	}
	// ESC \ (ST) ends a string already dispatched; others are ignored.
}

func (s *Screen) escInter(a *vtparse.Action) {
	switch a.Inter {
	case "(", ")":
		g := 0
		if a.Inter == ")" {
			g = 1
		}
		switch a.Final {
		case '0':
			s.cur.charsets[g] = csDEC
		case 'A':
			s.cur.charsets[g] = csUK
		default:
			s.cur.charsets[g] = csASCII
		}
	case "#":
		if a.Final == '8' {
			s.alignment()
		}
	}
}

// alignment is DECALN: the screen filled with 'E', margins reset, home.
func (s *Screen) alignment() {
	for i := range s.buf.lines {
		ln := &s.buf.lines[i]
		ln.wrapped, ln.pad = false, false
		for j := range ln.cells {
			ln.cells[j] = Cell{Content: "E", Width: 1}
		}
	}
	s.top, s.bot = 0, s.rows-1
	s.cur.origin = false
	s.moveTo(0, 0)
}

// csi dispatches a control sequence: a DEC private one (?), a marked one (> < =), one with an
// intermediate byte, or a plain one by its final byte.
func (s *Screen) csi(a *vtparse.Action) {
	switch {
	case a.Priv == '?':
		s.csiPrivate(a)
	case a.Priv != 0:
		s.csiMarked(a)
	case a.Inter != "":
		s.csiIntermediate(a)
	default:
		s.csiFinal(a)
	}
}

// csiMarked is a CSI with a >, < or = marker: the secondary DA and the kitty keyboard stack. Any
// other marker, and XTMODKEYS and the like, are not supported.
func (s *Screen) csiMarked(a *vtparse.Action) {
	switch a.Priv {
	case '>':
		if a.Final == 'c' && a.Inter == "" && a.Param(0, 0) == 0 {
			s.reply1([]byte("\x1b[>1;10;0c")) // DA2: a VT220, firmware 10
		}
		if a.Final == 'u' && a.Inter == "" {
			s.pushKeyboard(a.Param(0, 0))
		}
	case '<':
		if a.Final == 'u' && a.Inter == "" {
			s.popKeyboard(a.Param(0, 1))
		}
	case '=':
		if a.Final == 'u' && a.Inter == "" {
			s.setKeyboard(a.Param(0, 0), a.Param(1, 1))
		}
	}
}

// csiIntermediate is a CSI with an intermediate byte: DECSCUSR (SP q), DECRQM ($ p) and DECSTR
// (! p). Any other is ignored.
func (s *Screen) csiIntermediate(a *vtparse.Action) {
	switch a.Inter {
	case " ":
		if a.Final == 'q' {
			s.cursorStyle(a.Param(0, 0))
		}
	case "$":
		if a.Final == 'p' {
			s.reportMode(false, a.Param(0, 0))
		}
	case "!":
		if a.Final == 'p' {
			s.softReset()
		}
	}
}

// csiFinal is a plain CSI, by its final byte.
func (s *Screen) csiFinal(a *vtparse.Action) {
	n := max(a.Param(0, 1), 1) // a count: 0 and absent both mean 1
	switch a.Final {
	case '@':
		ln := &s.buf.lines[s.cur.row]
		s.insertCells(ln, s.cur.col, n)
		s.cur.wrapNext = false
	case 'A':
		s.moveRow(-n)
	case 'B', 'e':
		s.moveRow(n)
	case 'C', 'a':
		s.moveCol(n)
	case 'D':
		s.moveCol(-n)
	case 'E':
		s.moveRow(n)
		s.cur.col = 0
	case 'F':
		s.moveRow(-n)
		s.cur.col = 0
	case 'G', '`':
		s.cur.col = min(n-1, s.cols-1)
		s.cur.wrapNext = false
	case 'H', 'f':
		s.moveTo(max(a.Param(0, 1), 1)-1, max(a.Param(1, 1), 1)-1)
	case 'd':
		col := s.cur.col
		s.moveTo(n-1, col)
	case 'I':
		s.tab(n)
	case 'Z':
		s.backTab(n)
	case 'J':
		s.eraseDisplay(a.Param(0, 0))
	case 'K':
		s.eraseLine(a.Param(0, 0))
	case 'L':
		s.insertLines(n)
	case 'M':
		s.deleteLines(n)
	case 'P':
		s.deleteCells(n)
	case 'S':
		s.scrollUp(n)
	case 'T':
		if len(a.Params) <= 1 {
			s.scrollDown(n) // five parameters is xterm's mouse highlight
		}
	case 'X':
		s.eraseCells(n)
	case 'b':
		s.repeat(n)
	case 'c':
		if a.Param(0, 0) == 0 {
			s.reply1([]byte("\x1b[?62;22c")) // DA1: VT220 with ANSI colour
		}
	case 'g':
		switch a.Param(0, 0) {
		case 0:
			s.tabs[s.cur.col] = false
		case 3:
			clear(s.tabs)
		}
	case 'h', 'l':
		for i := range a.Params {
			s.setANSIMode(a.Param(i, 0), a.Final == 'h')
		}
	case 'm':
		s.sgr(a)
	case 'n':
		s.statusReport(false, a.Param(0, 0))
	case 'r':
		s.setMargins(a.Param(0, 1), a.Param(1, s.rows))
	case 's':
		s.saveCursor()
	case 'u':
		s.restoreCursor()
	}
	// t (window operations) and the rest are ignored.
}

func (s *Screen) csiPrivate(a *vtparse.Action) {
	switch {
	case a.Inter == "" && (a.Final == 'h' || a.Final == 'l'):
		for i := range a.Params {
			s.setPrivateMode(a.Param(i, 0), a.Final == 'h')
		}
	case a.Inter == "$" && a.Final == 'p':
		s.reportMode(true, a.Param(0, 0))
	case a.Inter == "" && a.Final == 'n':
		s.statusReport(true, a.Param(0, 0))
	case a.Inter == "" && a.Final == 'J':
		s.eraseDisplay(a.Param(0, 0)) // DECSED: no protected cells here
	case a.Inter == "" && a.Final == 'K':
		s.eraseLine(a.Param(0, 0)) // DECSEL
	case a.Inter == "" && a.Final == 'u':
		// The kitty keyboard query: the flags in force, which a program reads
		// to learn the protocol is understood and what of its push was kept.
		s.reply1([]byte(fmt.Sprintf("\x1b[?%du", s.buf.keyboardFlags())))
	}
}

// pushKeyboard is CSI > flags u: flags, those this screen supports, become
// the current set on the shown screen's stack.
func (s *Screen) pushKeyboard(flags int) {
	b := s.buf
	if len(b.kbd) == kittyStackMax {
		b.kbd = append(b.kbd[:0], b.kbd[1:]...)
	}
	b.kbd = append(b.kbd, flags&KittyDisambiguate)
	s.modes.KeyboardFlags = b.keyboardFlags()
}

// popKeyboard is CSI < n u: the last n sets go, and an emptied stack is the
// legacy encoding.
func (s *Screen) popKeyboard(n int) {
	b := s.buf
	b.kbd = b.kbd[:len(b.kbd)-min(max(n, 1), len(b.kbd))]
	s.modes.KeyboardFlags = b.keyboardFlags()
}

// setKeyboard is CSI = flags ; mode u, changing the current set in place:
// mode 1 replaces it, 2 sets the given bits, 3 clears them.
func (s *Screen) setKeyboard(flags, mode int) {
	b := s.buf
	if len(b.kbd) == 0 {
		b.kbd = append(b.kbd, 0)
	}
	cur := &b.kbd[len(b.kbd)-1]
	flags &= KittyDisambiguate
	switch mode {
	case 1:
		*cur = flags
	case 2:
		*cur |= flags
	case 3:
		*cur &^= flags
	}
	s.modes.KeyboardFlags = b.keyboardFlags()
}

func (s *Screen) statusReport(private bool, n int) {
	switch n {
	case 5:
		if !private {
			s.reply1([]byte("\x1b[0n"))
		}
	case 6:
		row := s.cur.row + 1
		if s.cur.origin {
			row -= s.top
		}
		if private {
			s.reply1(fmt.Appendf(nil, "\x1b[?%d;%dR", row, s.cur.col+1))
		} else {
			s.reply1(fmt.Appendf(nil, "\x1b[%d;%dR", row, s.cur.col+1))
		}
	}
}

func (s *Screen) cursorStyle(n int) {
	switch n {
	case 0:
		s.shape, s.modes.CursorBlink = CursorDefault, false
	case 1, 2:
		s.shape, s.modes.CursorBlink = CursorBlock, n == 1
	case 3, 4:
		s.shape, s.modes.CursorBlink = CursorUnderline, n == 3
	case 5, 6:
		s.shape, s.modes.CursorBlink = CursorBar, n == 5
	}
}

// softReset is DECSTR.
func (s *Screen) softReset() {
	s.modes.Insert = false
	s.modes.Origin = false
	s.modes.Autowrap = true
	s.modes.AppCursor = false
	s.modes.AppKeypad = false
	s.modes.CursorVisible = true
	s.top, s.bot = 0, s.rows-1
	s.cur.pen = Cell{}
	s.cur.origin = false
	s.cur.charsets = [2]charset{}
	s.cur.gl = 0
	s.cur.wrapNext = false
	s.buf.saved = cursor{}
	s.buf.hasSaved = false
}

func (s *Screen) setMargins(top, bot int) {
	top, bot = max(top, 1)-1, min(max(bot, 1), s.rows)-1
	if top >= bot {
		return
	}
	s.top, s.bot = top, bot
	s.moveTo(0, 0)
}

func (s *Screen) setANSIMode(n int, on bool) {
	switch n {
	case 4:
		s.modes.Insert = on
	case 20:
		s.modes.NewLine = on
	}
}

// privateMode is one DEC private mode: how SM/RM (CSI ? n h/l) sets it, and how DECRQM reads it
// back. A mode with no get is set but never reported (1048 saves and restores the cursor; it has
// no state to report).
type privateMode struct {
	get func(Modes) bool
	set func(s *Screen, on bool)
}

// privateModes are the DEC private modes the screen keeps, by number: one row each, so SM/RM and
// DECRQM cannot disagree about which modes there are.
var privateModes = map[int]privateMode{
	1: {func(m Modes) bool { return m.AppCursor }, func(s *Screen, on bool) { s.modes.AppCursor = on }},
	6: {func(m Modes) bool { return m.Origin }, func(s *Screen, on bool) {
		s.cur.origin = on
		s.modes.Origin = on
		s.moveTo(0, 0)
	}},
	7: {func(m Modes) bool { return m.Autowrap }, func(s *Screen, on bool) {
		s.modes.Autowrap = on
		if !on {
			s.cur.wrapNext = false
		}
	}},
	9:    {func(m Modes) bool { return m.Mouse == MouseX10 }, func(s *Screen, on bool) { s.setMouse(MouseX10, on) }},
	12:   {func(m Modes) bool { return m.CursorBlink }, func(s *Screen, on bool) { s.modes.CursorBlink = on }},
	25:   {func(m Modes) bool { return m.CursorVisible }, func(s *Screen, on bool) { s.modes.CursorVisible = on }},
	47:   {func(m Modes) bool { return m.AltScreen }, func(s *Screen, on bool) { s.switchScreen(on, false) }},
	66:   {func(m Modes) bool { return m.AppKeypad }, func(s *Screen, on bool) { s.modes.AppKeypad = on }},
	1000: {func(m Modes) bool { return m.Mouse == MouseClick }, func(s *Screen, on bool) { s.setMouse(MouseClick, on) }},
	1002: {func(m Modes) bool { return m.Mouse == MouseDrag }, func(s *Screen, on bool) { s.setMouse(MouseDrag, on) }},
	1003: {func(m Modes) bool { return m.Mouse == MouseMotion }, func(s *Screen, on bool) { s.setMouse(MouseMotion, on) }},
	1004: {func(m Modes) bool { return m.FocusEvents }, func(s *Screen, on bool) { s.modes.FocusEvents = on }},
	1006: {func(m Modes) bool { return m.MouseSGR }, func(s *Screen, on bool) { s.modes.MouseSGR = on }},
	1047: {func(m Modes) bool { return m.AltScreen }, func(s *Screen, on bool) {
		if !on && s.buf == &s.alt {
			s.clearBuffer(&s.alt)
		}
		s.switchScreen(on, false)
	}},
	1048: {nil, func(s *Screen, on bool) {
		if on {
			s.saveCursor()
		} else {
			s.restoreCursor()
		}
	}},
	1049: {func(m Modes) bool { return m.AltScreen }, func(s *Screen, on bool) {
		if on {
			if s.buf == &s.alt {
				return
			}
			s.saveCursor()
			s.switchScreen(true, true)
		} else {
			if s.buf != &s.alt {
				return
			}
			s.switchScreen(false, false)
			s.restoreCursor()
		}
	}},
	2004: {func(m Modes) bool { return m.BracketedPaste }, func(s *Screen, on bool) { s.modes.BracketedPaste = on }},
	2026: {func(m Modes) bool { return m.Synchronized }, func(s *Screen, on bool) { s.modes.Synchronized = on }},
}

func (s *Screen) setPrivateMode(n int, on bool) {
	if pm, ok := privateModes[n]; ok {
		pm.set(s, on)
	}
}

// setMouse turns a mouse mode on, or any mouse reporting off (xterm's
// reset of one mouse mode ends reporting).
func (s *Screen) setMouse(m MouseMode, on bool) {
	if on {
		s.modes.Mouse = m
	} else {
		s.modes.Mouse = MouseOff
	}
}

// switchScreen shows the alternate screen (on) or the primary. The cursor
// keeps its position, as xterm's does; clear empties the alternate screen
// on the way in.
func (s *Screen) switchScreen(on, clear bool) {
	if on == (s.buf == &s.alt) {
		return
	}
	if on {
		s.buf = &s.alt
		if clear {
			s.clearBuffer(&s.alt)
		}
	} else {
		s.buf = &s.pri
	}
	s.modes.AltScreen = on
	s.modes.KeyboardFlags = s.buf.keyboardFlags() // each screen has its own stack
	s.cur.wrapNext = false
}

func (s *Screen) clearBuffer(b *buffer) {
	for i := range b.lines {
		b.lines[i] = s.blankLine(s.cur.pen.BG)
	}
}

// modeValue is DECRPM's answer: 1 set, 2 reset, 0 not recognized.
func modeValue(known, on bool) int {
	switch {
	case !known:
		return 0
	case on:
		return 1
	}
	return 2
}

func (s *Screen) reportMode(private bool, n int) {
	known, on := true, false
	m := s.modes
	if private {
		pm, ok := privateModes[n]
		known = ok && pm.get != nil
		if known {
			on = pm.get(m)
		}
		s.reply1(fmt.Appendf(nil, "\x1b[?%d;%d$y", n, modeValue(known, on)))
		return
	}
	switch n {
	case 4:
		on = m.Insert
	case 20:
		on = m.NewLine
	default:
		known = false
	}
	s.reply1(fmt.Appendf(nil, "\x1b[%d;%d$y", n, modeValue(known, on)))
}

func (s *Screen) osc(data []byte) {
	ps, text, ok := bytes.Cut(data, []byte(";"))
	if !ok {
		return
	}
	switch string(ps) {
	case "0", "2":
		s.title = sanitizeTitle(text)
	}
	// 52 (clipboard) is dropped on purpose; everything else is ignored.
}

// sanitizeTitle keeps a title's valid, printable UTF-8.
func sanitizeTitle(b []byte) string {
	out := make([]rune, 0, len(b))
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		b = b[n:]
		if r == utf8.RuneError && n <= 1 || r < 0x20 || r == 0x7F || (r >= 0x80 && r < 0xA0) {
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func (s *Screen) repeat(n int) {
	if s.lastGraphic == "" {
		return
	}
	c := s.lastGraphic
	w := grapheme.ClusterWidth(c, false)
	n = min(n, s.rows*s.cols) // a huge count fills the screen at most
	for ; n > 0; n-- {
		s.put(c, w)
	}
}

// --- editing ---

// eraseCells blanks n cells from the cursor (ECH).
func (s *Screen) eraseCells(n int) {
	s.cur.wrapNext = false
	s.blankRange(&s.buf.lines[s.cur.row], s.cur.col, min(s.cur.col+n, s.cols))
}

// blankRange blanks [from, to) of a row, wide halves at its edges included.
func (s *Screen) blankRange(ln *line, from, to int) {
	if from >= to {
		return
	}
	s.clearWide(ln, from)
	s.clearWide(ln, to-1)
	bg := s.cur.pen.BG
	for i := from; i < to; i++ {
		ln.cells[i] = blank(bg)
	}
}

func (s *Screen) eraseLine(mode int) {
	s.cur.wrapNext = false
	ln := &s.buf.lines[s.cur.row]
	switch mode {
	case 0:
		s.blankRange(ln, s.cur.col, s.cols)
		ln.wrapped, ln.pad = false, false
	case 1:
		s.blankRange(ln, 0, s.cur.col+1)
	case 2:
		s.blankRange(ln, 0, s.cols)
		ln.wrapped, ln.pad = false, false
	}
}

func (s *Screen) eraseDisplay(mode int) {
	switch mode {
	case 0:
		s.eraseLine(0)
		for i := s.cur.row + 1; i < s.rows; i++ {
			s.buf.lines[i] = s.blankLine(s.cur.pen.BG)
		}
	case 1:
		for i := 0; i < s.cur.row; i++ {
			s.buf.lines[i] = s.blankLine(s.cur.pen.BG)
		}
		s.eraseLine(1)
	case 2:
		s.cur.wrapNext = false
		s.clearBuffer(s.buf)
	case 3:
		s.hist.clear()
	}
}

// insertCells shifts the row right from col by n blanks; cells pushed past
// the edge are lost (ICH, and IRM's print).
func (s *Screen) insertCells(ln *line, col, n int) {
	n = min(n, s.cols-col)
	if n <= 0 {
		return
	}
	if ln.cells[col].Width == 0 {
		s.clearWide(ln, col) // the insertion splits a wide cluster
	}
	if last := s.cols - n - 1; last >= col && ln.cells[last].Width == 2 {
		// Pushed to the edge, a wide cluster would lose its second half.
		ln.cells[last] = blank(ln.cells[last].BG)
	}
	copy(ln.cells[col+n:], ln.cells[col:s.cols-n])
	for i := col; i < col+n; i++ {
		ln.cells[i] = blank(s.cur.pen.BG)
	}
	ln.wrapped, ln.pad = false, false
}

// deleteCells removes n cells at the cursor, the rest of the row moving
// left and blanks entering at its end (DCH).
func (s *Screen) deleteCells(n int) {
	s.cur.wrapNext = false
	ln := &s.buf.lines[s.cur.row]
	col := s.cur.col
	n = min(n, s.cols-col)
	s.clearWide(ln, col)
	s.clearWide(ln, col+n-1)
	copy(ln.cells[col:], ln.cells[col+n:])
	for i := s.cols - n; i < s.cols; i++ {
		ln.cells[i] = blank(s.cur.pen.BG)
	}
}

// insertLines inserts n blank rows at the cursor's row inside the region
// (IL); rows pushed past the bottom margin are lost.
func (s *Screen) insertLines(n int) {
	if s.cur.row < s.top || s.cur.row > s.bot {
		return
	}
	top := s.top
	s.top = s.cur.row
	s.scrollDown(n)
	s.top = top
	s.cur.col = 0
	s.cur.wrapNext = false
}

// deleteLines removes n rows at the cursor's row inside the region (DL),
// blank rows entering at the bottom margin. Deleted rows never reach the
// scrollback.
func (s *Screen) deleteLines(n int) {
	if s.cur.row < s.top || s.cur.row > s.bot {
		return
	}
	n = min(n, s.bot-s.cur.row+1)
	ls := s.buf.lines
	copy(ls[s.cur.row:], ls[s.cur.row+n:s.bot+1])
	for i := s.bot - n + 1; i <= s.bot; i++ {
		ls[i] = s.blankLine(s.cur.pen.BG)
	}
	s.cur.col = 0
	s.cur.wrapNext = false
}
