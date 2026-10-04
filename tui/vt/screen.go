package vt

import (
	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
	"github.com/yongjohnlee80/golib/tui/internal/vtparse"
	"github.com/yongjohnlee80/golib/tui/style"
)

// DefaultScrollback is the scrollback's length when WithScrollback is not
// given.
const DefaultScrollback = 5000

// Option configures a Screen.
type Option func(*Screen)

// WithScrollback keeps up to n lines scrolled off the primary screen; 0
// keeps none.
func WithScrollback(n int) Option {
	return func(s *Screen) {
		if n < 0 {
			n = 0
		}
		s.hist.max = n
	}
}

// WithReply sets where the terminal's answers go (DA, CPR, DECRQM): the
// program's input. Without it, queries go unanswered.
func WithReply(fn func([]byte)) Option {
	return func(s *Screen) { s.reply = fn }
}

// line is one row of cells. wrapped says autowrap broke it, so it continues
// on the next row; reflow joins such rows back. pad says its last cell is
// the blank a wide cluster left when it did not fit there, which reflow
// drops rather than keep as a space.
type line struct {
	cells   []Cell
	wrapped bool
	pad     bool
}

// charset is a designated character set (G0, G1).
type charset uint8

const (
	csASCII charset = iota
	csDEC           // DEC special graphics: line drawing
	csUK            // '#' is '£'
)

// cursor is the cursor's position and what DECSC saves with it.
type cursor struct {
	row, col int
	pen      Cell // Attr, FG and BG for what is written next
	wrapNext bool // the last column was written: the next print wraps first
	origin   bool
	charsets [2]charset
	gl       int // which of charsets is invoked (SI: 0, SO: 1)
}

// buffer is one screen's grid and its DECSC slot.
type buffer struct {
	lines    []line
	saved    cursor
	hasSaved bool
}

// Screen is the emulator's state. Write feeds it a program's output; the
// accessors read the result.
type Screen struct {
	rows, cols int

	p    vtparse.Parser
	emit func(*vtparse.Action)

	pri, alt buffer
	buf      *buffer // pri, or alt while the alternate screen is shown
	cur      cursor
	top, bot int // the scroll region's rows, inclusive
	tabs     []bool

	modes Modes
	shape CursorShape
	title string
	hist  history
	reply func([]byte)

	// last is the cell the previous print wrote, while nothing has moved
	// the cursor since: a combining mark or joiner extends its cluster.
	last struct {
		row, col int
		ok       bool
	}
	// lastGraphic is what REP repeats.
	lastGraphic string
}

// New returns a rows x cols screen in its power-on state.
func New(rows, cols int, opts ...Option) *Screen {
	rows, cols = max(rows, 1), max(cols, 1)
	s := &Screen{rows: rows, cols: cols, hist: history{max: DefaultScrollback}}
	s.p.Output = true
	s.emit = s.handle
	for _, o := range opts {
		o(s)
	}
	s.reset()
	return s
}

// reset is the power-on state (RIS), keeping the size, the options and the
// scrollback.
func (s *Screen) reset() {
	s.pri = buffer{lines: s.blankLines(s.rows, style.Color{})}
	s.alt = buffer{lines: s.blankLines(s.rows, style.Color{})}
	s.buf = &s.pri
	s.cur = cursor{}
	s.top, s.bot = 0, s.rows-1
	s.resetTabs()
	s.modes = Modes{Autowrap: true, CursorVisible: true}
	s.shape = CursorDefault
	s.title = ""
	s.last.ok = false
	s.lastGraphic = ""
	s.p.Reset()
}

func (s *Screen) resetTabs() {
	s.tabs = make([]bool, s.cols)
	for i := 8; i < s.cols; i += 8 {
		s.tabs[i] = true
	}
}

func (s *Screen) blankLine(bg style.Color) line {
	cells := make([]Cell, s.cols)
	for i := range cells {
		cells[i] = blank(bg)
	}
	return line{cells: cells}
}

func (s *Screen) blankLines(n int, bg style.Color) []line {
	ls := make([]line, n)
	for i := range ls {
		ls[i] = s.blankLine(bg)
	}
	return ls
}

// Write feeds the program's output to the screen. It always consumes all of
// p; sequences split across writes decode as if contiguous.
func (s *Screen) Write(p []byte) (int, error) {
	for _, b := range p {
		s.p.Feed(b, s.emit)
	}
	return len(p), nil
}

// Size is the screen's rows and columns.
func (s *Screen) Size() (rows, cols int) { return s.rows, s.cols }

// Cell is the visible grid's cell at row, col; outside the grid it is a
// blank.
func (s *Screen) Cell(row, col int) Cell {
	if row < 0 || row >= s.rows || col < 0 || col >= s.cols {
		return blank(style.Color{})
	}
	return s.buf.lines[row].cells[col]
}

// Wrapped reports whether the visible row continues on the next one, because
// autowrap broke it.
func (s *Screen) Wrapped(row int) bool {
	if row < 0 || row >= s.rows {
		return false
	}
	return s.buf.lines[row].wrapped
}

// Scrollback is the number of lines held above the primary screen.
func (s *Screen) Scrollback() int { return s.hist.len() }

// Line is scrollback line i, oldest first. The slice is the screen's own;
// a caller that keeps it past the next Write copies it.
func (s *Screen) Line(i int) []Cell {
	if i < 0 || i >= s.hist.len() {
		return nil
	}
	return s.hist.at(i).cells
}

// Cursor is the cursor's position on the visible grid, whether the program
// shows it, and its shape.
func (s *Screen) Cursor() (row, col int, visible bool, shape CursorShape) {
	return s.cur.row, s.cur.col, s.modes.CursorVisible, s.shape
}

// Modes are the terminal's current modes.
func (s *Screen) Modes() Modes { return s.modes }

// Title is the window title the program last set (OSC 0 or 2).
func (s *Screen) Title() string { return s.title }

func (s *Screen) handle(a *vtparse.Action) {
	if a.Kind == vtparse.Print {
		s.print(a.Rune)
		return
	}
	s.last.ok = false
	switch a.Kind {
	case vtparse.Execute:
		s.lastGraphic = ""
		s.execute(a.Byte)
	case vtparse.Esc:
		s.lastGraphic = ""
		s.esc(a)
	case vtparse.CSI:
		if a.Final != 'b' || a.Priv != 0 || a.Inter != "" {
			s.lastGraphic = "" // only REP itself keeps what REP repeats
		}
		s.csi(a)
	case vtparse.OSC:
		s.lastGraphic = ""
		s.osc(a.Data)
	}
	// DCS and APC strings carry nothing this screen answers.
}

// decGraphics maps 0x5F..0x7E under the DEC special graphics set.
var decGraphics = [...]rune{
	' ', '◆', '▒', '␉', '␌', '␍', '␊', '°', '±', '␤', '␋', '┘', '┐', '┌', '└', '┼',
	'⎺', '⎻', '─', '⎼', '⎽', '├', '┤', '┴', '┬', '│', '≤', '≥', 'π', '≠', '£', '·',
}

func (s *Screen) print(r rune) {
	switch s.cur.charsets[s.cur.gl] {
	case csDEC:
		if r >= 0x5F && r <= 0x7E {
			r = decGraphics[r-0x5F]
		}
	case csUK:
		if r == '#' {
			r = '£'
		}
	}
	if s.last.ok && s.join(r) {
		return
	}
	c := string(r)
	w := grapheme.ClusterWidth(c, false)
	if w == 0 {
		return // a zero-width rune with nothing to join: xterm drops it too
	}
	s.put(c, w)
}

// join extends the last printed cluster with r when they form one grapheme
// cluster (a combining mark, a ZWJ sequence, a flag's second half, a
// variation selector). A cluster that becomes wide takes the next column
// when it is free to.
func (s *Screen) join(r rune) bool {
	ln := &s.buf.lines[s.last.row]
	c := &ln.cells[s.last.col]
	if !joins(c.Content, r) {
		return false
	}
	c.Content += string(r)
	s.lastGraphic = c.Content
	if c.Width == 1 && grapheme.ClusterWidth(c.Content, false) == 2 && s.last.col+1 < s.cols {
		next := s.last.col + 1
		s.clearWide(ln, next)
		c.Width = 2
		ln.cells[next] = Cell{Attr: c.Attr, FG: c.FG, BG: c.BG}
		if s.cur.row == s.last.row && s.cur.col == next && !s.cur.wrapNext {
			s.advance(1)
		}
	}
	return true
}

// joins reports whether r continues the cluster prev.
func joins(prev string, r rune) bool {
	if prev == "" {
		return false
	}
	if r < 0x300 && prev[len(prev)-1] < 0x80 {
		// Nothing below U+0300 extends a cluster, and an ASCII character
		// prefixes none: the common case skips segmentation.
		return false
	}
	joined := prev + string(r)
	for c := range grapheme.Clusters(joined) {
		return len(c) == len(joined)
	}
	return false
}

// put writes a cluster of width w at the cursor, wrapping first when a
// wrap is pending or a wide cluster does not fit.
func (s *Screen) put(c string, w int) {
	if w > s.cols {
		return
	}
	if s.cur.wrapNext {
		s.wrapLine()
	}
	if s.cur.col+w > s.cols {
		if !s.modes.Autowrap {
			s.cur.col = s.cols - w
		} else {
			// A wide cluster at the last column: the column stays blank and
			// the cluster starts the next row.
			ln := &s.buf.lines[s.cur.row]
			s.clearWide(ln, s.cur.col)
			ln.cells[s.cur.col] = blank(s.cur.pen.BG)
			ln.pad = true
			s.wrapLine()
		}
	}
	ln := &s.buf.lines[s.cur.row]
	if s.modes.Insert {
		s.insertCells(ln, s.cur.col, w)
	}
	s.clearWide(ln, s.cur.col)
	s.clearWide(ln, s.cur.col+w-1)
	if s.cur.col+w == s.cols {
		ln.pad = false
	}
	pen := s.cur.pen
	ln.cells[s.cur.col] = Cell{Content: c, Width: uint8(w), Attr: pen.Attr, FG: pen.FG, BG: pen.BG}
	if w == 2 {
		ln.cells[s.cur.col+1] = Cell{Attr: pen.Attr, FG: pen.FG, BG: pen.BG}
	}
	s.last.row, s.last.col, s.last.ok = s.cur.row, s.cur.col, true
	s.lastGraphic = c
	s.advance(w)
}

// advance moves the cursor right after a print, holding it at the last
// column with a pending wrap.
func (s *Screen) advance(w int) {
	s.cur.col += w
	if s.cur.col >= s.cols {
		s.cur.col = s.cols - 1
		s.cur.wrapNext = s.modes.Autowrap
	}
}

// wrapLine marks the row as continued and moves to the next row's start.
func (s *Screen) wrapLine() {
	s.buf.lines[s.cur.row].wrapped = true
	s.index()
	s.cur.col = 0
	s.cur.wrapNext = false
}

// clearWide blanks the other half of a wide cluster that col is part of, so
// overwriting either half never leaves half a character.
func (s *Screen) clearWide(ln *line, col int) {
	if col < 0 || col >= len(ln.cells) {
		return
	}
	switch c := ln.cells[col]; c.Width {
	case 0:
		if col > 0 {
			ln.cells[col-1] = blank(ln.cells[col-1].BG)
		}
		ln.cells[col] = blank(c.BG)
	case 2:
		if col+1 < len(ln.cells) {
			ln.cells[col+1] = blank(ln.cells[col+1].BG)
		}
		ln.cells[col] = blank(c.BG)
	}
}

func (s *Screen) execute(b byte) {
	switch b {
	case '\b':
		s.cur.wrapNext = false
		if s.cur.col > 0 {
			s.cur.col--
		}
	case '\t':
		s.tab(1)
	case '\n', '\v', '\f':
		s.index()
		if s.modes.NewLine {
			s.cur.col = 0
		}
	case '\r':
		s.cur.col = 0
		s.cur.wrapNext = false
	case 0x0E: // SO
		s.cur.gl = 1
	case 0x0F: // SI
		s.cur.gl = 0
	}
	// BEL and the rest of C0 do nothing on the screen.
}

// index moves down a row, scrolling the region at its bottom margin (IND).
func (s *Screen) index() {
	s.cur.wrapNext = false
	switch {
	case s.cur.row == s.bot:
		s.scrollUp(1)
	case s.cur.row < s.rows-1:
		s.cur.row++
	}
}

// reverseIndex moves up a row, scrolling the region down at its top margin
// (RI).
func (s *Screen) reverseIndex() {
	s.cur.wrapNext = false
	switch {
	case s.cur.row == s.top:
		s.scrollDown(1)
	case s.cur.row > 0:
		s.cur.row--
	}
}

// scrollUp moves the region's rows up n. Rows leaving the top of the
// primary screen, with the region at the top, go to the scrollback.
func (s *Screen) scrollUp(n int) {
	n = min(n, s.bot-s.top+1)
	if n <= 0 {
		return
	}
	ls := s.buf.lines
	if s.top == 0 && s.buf == &s.pri {
		for i := 0; i < n; i++ {
			s.hist.push(ls[i])
		}
	}
	copy(ls[s.top:], ls[s.top+n:s.bot+1])
	for i := s.bot - n + 1; i <= s.bot; i++ {
		ls[i] = s.blankLine(s.cur.pen.BG)
	}
}

// scrollDown moves the region's rows down n, blank rows entering at its top.
func (s *Screen) scrollDown(n int) {
	n = min(n, s.bot-s.top+1)
	if n <= 0 {
		return
	}
	ls := s.buf.lines
	copy(ls[s.top+n:s.bot+1], ls[s.top:s.bot+1-n])
	for i := s.top; i < s.top+n; i++ {
		ls[i] = s.blankLine(s.cur.pen.BG)
	}
}

func (s *Screen) tab(n int) {
	s.cur.wrapNext = false
	for ; n > 0 && s.cur.col < s.cols-1; n-- {
		s.cur.col++
		for s.cur.col < s.cols-1 && !s.tabs[s.cur.col] {
			s.cur.col++
		}
	}
}

func (s *Screen) backTab(n int) {
	s.cur.wrapNext = false
	for ; n > 0 && s.cur.col > 0; n-- {
		s.cur.col--
		for s.cur.col > 0 && !s.tabs[s.cur.col] {
			s.cur.col--
		}
	}
}

// moveTo puts the cursor at row, col: relative to the scroll region under
// DECOM, and clamped to it then.
func (s *Screen) moveTo(row, col int) {
	if s.cur.origin {
		row = min(max(row+s.top, s.top), s.bot)
	} else {
		row = min(max(row, 0), s.rows-1)
	}
	s.cur.row = row
	s.cur.col = min(max(col, 0), s.cols-1)
	s.cur.wrapNext = false
}

// moveRow moves the cursor n rows (negative is up), stopping at a margin
// when it starts inside the region.
func (s *Screen) moveRow(n int) {
	lo, hi := 0, s.rows-1
	if s.cur.row >= s.top && s.cur.row <= s.bot {
		lo, hi = s.top, s.bot
	}
	s.cur.row = min(max(s.cur.row+n, lo), hi)
	s.cur.wrapNext = false
}

func (s *Screen) moveCol(n int) {
	s.cur.col = min(max(s.cur.col+n, 0), s.cols-1)
	s.cur.wrapNext = false
}

func (s *Screen) saveCursor() {
	s.buf.saved = s.cur
	s.buf.hasSaved = true
}

// restoreCursor is DECRC: the saved state, or home with the defaults when
// nothing was saved.
func (s *Screen) restoreCursor() {
	if !s.buf.hasSaved {
		s.cur = cursor{}
		return
	}
	s.cur = s.buf.saved
	s.cur.row = min(s.cur.row, s.rows-1)
	s.cur.col = min(s.cur.col, s.cols-1)
}

func (s *Screen) reply1(b []byte) {
	if s.reply != nil {
		s.reply(b)
	}
}

// history is the scrollback: a ring of at most max lines.
type history struct {
	lines []line
	start int
	n     int
	max   int
}

func (h *history) len() int { return h.n }

func (h *history) at(i int) line { return h.lines[(h.start+i)%len(h.lines)] }

func (h *history) push(l line) {
	if h.max == 0 {
		return
	}
	if len(h.lines) < h.max {
		// Grow until the cap, then the ring overwrites the oldest.
		if h.start == 0 && h.n == len(h.lines) {
			h.lines = append(h.lines, l)
			h.n++
			return
		}
	}
	if h.n < len(h.lines) {
		h.lines[(h.start+h.n)%len(h.lines)] = l
		h.n++
		return
	}
	h.lines[h.start] = l
	h.start = (h.start + 1) % len(h.lines)
}

// all returns the scrollback oldest first, as a fresh slice.
func (h *history) all() []line {
	out := make([]line, h.n)
	for i := range out {
		out[i] = h.at(i)
	}
	return out
}

// set replaces the scrollback with ls, keeping the newest max.
func (h *history) set(ls []line) {
	if len(ls) > h.max {
		ls = ls[len(ls)-h.max:]
	}
	h.lines = append([]line(nil), ls...)
	h.start, h.n = 0, len(ls)
}

func (h *history) clear() { h.lines, h.start, h.n = nil, 0, 0 }
