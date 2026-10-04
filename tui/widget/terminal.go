package widget

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/pty"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/vt"
)

// TerminalMode is where a Terminal's keys go.
type TerminalMode uint8

const (
	// TerminalInput sends every key to the program, except the ways out.
	TerminalInput TerminalMode = iota
	// TerminalNormal moves a cursor over the scrollback and screen; the
	// program keeps running, and keys the mode does not use reach the host.
	TerminalNormal
)

// String renders the mode for status bars.
func (m TerminalMode) String() string {
	if m == TerminalNormal {
		return "NORMAL"
	}
	return "TERMINAL"
}

// TerminalProcess is the program a Terminal runs: its output is read, keys
// are written, and Close hangs it up (it may block, and is called off the
// UI loop). *pty.PTY is one.
type TerminalProcess interface {
	io.ReadWriter
	Resize(rows, cols int) error
	Close() error
	Wait() (int, error)
}

// TerminalOption configures a Terminal.
type TerminalOption func(*terminalConfig)

type terminalConfig struct {
	path        string
	args        []string
	dir         string
	env         []string
	scrollback  int
	vimKeys     bool
	themeColors bool
	onExit      func(int)
	onTitle     func(string)
	onMode      func(TerminalMode)
	start       func(pty.Cmd) (TerminalProcess, error)
}

// WithCommand is the program to run; the default is $SHELL, else /bin/sh.
func WithCommand(path string, args ...string) TerminalOption {
	return func(c *terminalConfig) { c.path, c.args = path, args }
}

// WithDir is the program's working directory.
func WithDir(dir string) TerminalOption { return func(c *terminalConfig) { c.dir = dir } }

// WithEnv adds variables to the environment the program inherits.
func WithEnv(env ...string) TerminalOption {
	return func(c *terminalConfig) { c.env = append(c.env, env...) }
}

// WithScrollback keeps up to n lines above the screen (vt.DefaultScrollback
// by default).
func WithScrollback(n int) TerminalOption { return func(c *terminalConfig) { c.scrollback = n } }

// WithVimKeys makes Esc leave input mode for normal mode, as the editor's
// insert mode does, except while the program shows the alternate screen
// (a full-screen program such as vim or less needs Esc itself).
func WithVimKeys(on bool) TerminalOption { return func(c *terminalConfig) { c.vimKeys = on } }

// WithThemeColors paints the program's default foreground and background in
// the theme's (the default). The 16 ANSI colours are painted as ANSI, in
// the host terminal's palette like every other ANSI colour in the
// application; 256-colour and truecolor values are painted as given.
func WithThemeColors(on bool) TerminalOption {
	return func(c *terminalConfig) { c.themeColors = on }
}

// WithOnExit is called on the UI loop when the program exits, with its exit
// status (128 + the signal for a signalled program).
func WithOnExit(fn func(code int)) TerminalOption { return func(c *terminalConfig) { c.onExit = fn } }

// WithOnTitle is called on the UI loop when the program sets its title.
func WithOnTitle(fn func(string)) TerminalOption { return func(c *terminalConfig) { c.onTitle = fn } }

// WithOnMode is called on the UI loop when the mode changes.
func WithOnMode(fn func(TerminalMode)) TerminalOption {
	return func(c *terminalConfig) { c.onMode = fn }
}

// WithStarter replaces how the program is started: a host can run it
// somewhere else, and tests run a fake. The default is pty.Start.
func WithStarter(fn func(pty.Cmd) (TerminalProcess, error)) TerminalOption {
	return func(c *terminalConfig) { c.start = fn }
}

// Terminal runs one program on a pseudo-terminal and shows its screen. In
// input mode the keys go to the program; in normal mode they move over the
// scrollback, select and yank, and the host's own keys work.
//
// The program outlives the Terminal's mounts: a drawer that hides it
// unmounts it, and the program keeps running and its screen keeps taking
// output. It ends with Stop, with its own exit, or with the App (App.Done).
//
// Ways out of input mode: Ctrl-\ Ctrl-n always goes to normal mode, and
// Ctrl-\ Ctrl-\ sends one Ctrl-\. After Ctrl-\ the next event decides, with
// no timeout, and the prefix never eats a key: any other key, or a paste,
// is sent after a Ctrl-\. Losing focus, unmounting or Stop while it is
// pending sends the Ctrl-\. With WithVimKeys, Esc goes to normal mode too,
// except on the alternate screen.
type Terminal struct {
	Base
	cfg terminalConfig

	scr        *vt.Screen
	rows, cols int // the size applied to the screen and the program
	w, h       int // the size layout gave

	proc     TerminalProcess
	procMu   sync.Mutex      // guards live, which the App's end reads off the loop
	live     TerminalProcess // the process not yet closed
	gen      int             // the program's generation: output and exits of older ones are dropped
	wr       *bufWriter
	in       *inputQueue
	pumps    sync.WaitGroup
	running  bool
	exited   bool
	exitCode int
	pending  bool // Start was asked before the mount
	alive    bool // mounted
	watching bool // a goroutine ends the program with the App

	mode   TerminalMode
	prefix bool // Ctrl-\ is pending
	mouse  mouseTracker
	title  string

	normal normalState
}

// NewTerminal returns a Terminal; Start runs its program.
func NewTerminal(opts ...TerminalOption) *Terminal {
	cfg := terminalConfig{scrollback: vt.DefaultScrollback, themeColors: true}
	for _, o := range opts {
		o(&cfg)
	}
	cfg.path = defaultShell(cfg.path)
	if cfg.start == nil {
		cfg.start = func(c pty.Cmd) (TerminalProcess, error) { return pty.Start(c) }
	}
	t := &Terminal{cfg: cfg, rows: 24, cols: 80}
	t.scr = t.newScreen()
	return t
}

// defaultShell is path, or the user's $SHELL, or /bin/sh.
func defaultShell(path string) string {
	if path == "" {
		path = os.Getenv("SHELL")
	}
	if path == "" {
		path = "/bin/sh"
	}
	return path
}

func (t *Terminal) newScreen() *vt.Screen {
	return vt.New(t.rows, t.cols, vt.WithScrollback(t.cfg.scrollback), vt.WithReply(t.send))
}

// Init mounts the Terminal; a Start asked before the mount runs now.
// Unmounting leaves the program running (a hidden drawer unmounts its
// content); only a pending Ctrl-\ is sent.
func (t *Terminal) Init(ctx *tui.Context) {
	t.Base.Init(ctx)
	t.alive = true
	ctx.OnUnmount(func() {
		t.flushPrefix()
		t.alive = false
	})
	if !t.watching {
		t.watching = true
		app := ctx.App()
		go func() {
			<-app.Done()
			t.endWithApp()
		}()
	}
	if t.pending {
		t.pending = false
		_ = t.Start() // a failure shows on the screen, and Enter retries
	}
}

// AcceptsFocus makes the Terminal focusable.
func (t *Terminal) AcceptsFocus() bool { return true }

// Mode is where keys go.
func (t *Terminal) Mode() TerminalMode { return t.mode }

// SetCommand changes the program; it takes effect at the next Start. An
// empty path is the user's $SHELL, else /bin/sh.
func (t *Terminal) SetCommand(path string, args ...string) {
	t.cfg.path, t.cfg.args = defaultShell(path), args
}

// SetDir changes the working directory; it takes effect at the next Start.
func (t *Terminal) SetDir(dir string) { t.cfg.dir = dir }

// SetVimKeys turns WithVimKeys' Esc on or off while the Terminal runs.
func (t *Terminal) SetVimKeys(on bool) { t.cfg.vimKeys = on }

// SetThemeColors turns WithThemeColors on or off while the Terminal runs.
func (t *Terminal) SetThemeColors(on bool) {
	t.cfg.themeColors = on
	t.MarkDirty()
}

// Running reports whether the program is running.
func (t *Terminal) Running() bool { return t.running }

// Title is the title the program last set.
func (t *Terminal) Title() string { return t.title }

// Screen is the emulator, for reading what the program shows. It belongs
// to the UI loop.
func (t *Terminal) Screen() *vt.Screen { return t.scr }

// Start runs the program, unless it is running. Before the mount it runs
// once the Terminal is mounted. After an exit it starts a new one, below
// what the last one left on the screen. A start that fails says so on the
// screen and returns the error; Enter or i in the Terminal, or another
// Start, tries again.
func (t *Terminal) Start() error {
	if t.running {
		return nil
	}
	ctx := t.Context()
	if ctx == nil || !t.alive {
		t.pending = true
		return nil
	}
	env := append(os.Environ(), t.cfg.env...)
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	proc, err := t.cfg.start(pty.Cmd{
		Path: t.cfg.path, Args: t.cfg.args, Env: env, Dir: t.cfg.dir,
		Rows: t.rows, Cols: t.cols,
	})
	if err != nil {
		// A failed start ends like an exit: it says so on the screen, and
		// Enter or i tries again, whoever asked for this start.
		err = fmt.Errorf("terminal: %w", err)
		t.exited = true
		t.report(err)
		return err
	}
	t.gen++
	gen := t.gen
	t.proc = proc
	t.procMu.Lock()
	t.live = proc
	t.procMu.Unlock()
	t.running, t.exited = true, false
	t.in = newInputQueue(proc)
	t.wr = newBufWriter(func(b []byte) {
		if gen == t.gen { // hidden or not: the screen keeps taking output
			t.ingest(b)
		}
	})
	t.wr.bind(ctx.App())
	app := ctx.App()
	wr := t.wr
	t.pumps.Add(1)
	go func() {
		defer t.pumps.Done()
		buf := make([]byte, 32<<10)
		for {
			n, err := proc.Read(buf)
			if n > 0 {
				if _, werr := wr.Write(buf[:n]); werr != nil {
					return // stopped or unmounted
				}
			}
			if err != nil {
				break
			}
		}
		code, _ := proc.Wait()
		app.Update(func() {
			if gen == t.gen {
				t.exitWith(code)
			}
		})
	}()
	t.MarkDirty()
	return nil
}

// Stop hangs the program up. It returns at once; the hang-up and its grace
// period run off the UI loop.
func (t *Terminal) Stop() { t.stop() }

// endWithApp hangs the program up once the App has ended (the loop is
// gone, so this runs on the watcher's goroutine).
func (t *Terminal) endWithApp() {
	// The loop has ended, so the fields it owned are settled. No loop will
	// take keys off the input queue or output off the bridge again: release
	// the queue's writer and a reader blocked on the bridge's budget, then
	// hang the program up.
	if t.in != nil {
		t.in.close()
	}
	if t.wr != nil {
		t.wr.close()
	}
	if proc := t.takeLive(); proc != nil {
		proc.Close()
	}
}

// takeLive takes the process not yet closed, leaving none: whoever takes it
// closes it, once.
func (t *Terminal) takeLive() TerminalProcess {
	t.procMu.Lock()
	defer t.procMu.Unlock()
	proc := t.live
	t.live = nil
	return proc
}

func (t *Terminal) stop() {
	t.flushPrefix()
	if !t.running {
		return
	}
	t.gen++ // the old program's output and exit are dropped
	t.running = false
	t.wr.close()
	// Keys already queued (a flushed Ctrl-\ among them) reach the program
	// before the hang-up, unless it has stopped reading.
	drained := t.in.finish()
	if proc := t.takeLive(); proc != nil {
		go func() {
			select {
			case <-drained:
			case <-time.After(time.Second):
			}
			proc.Close()
		}()
	}
	t.MarkDirty()
}

func (t *Terminal) exitWith(code int) {
	t.running, t.exited, t.exitCode = false, true, code
	t.wr.close()
	t.in.close()
	if proc := t.takeLive(); proc != nil {
		go proc.Close() // the program is gone; this releases its terminal
	}
	t.prefix = false
	t.scr.Write(fmt.Appendf(nil, "\r\n[process exited %d]", code))
	if t.cfg.onExit != nil {
		t.cfg.onExit(code)
	}
	t.MarkDirty()
}

// report shows a start failure on the screen, where the program would be.
func (t *Terminal) report(err error) {
	t.scr.Write([]byte("\r\n[" + err.Error() + "]"))
	t.MarkDirty()
}

func (t *Terminal) ingest(b []byte) {
	t.scr.Write(b)
	if title := t.scr.Title(); title != t.title {
		t.title = title
		if t.cfg.onTitle != nil {
			t.cfg.onTitle(title)
		}
	}
	t.normal.follow(t)
	t.MarkDirty()
}

// send queues bytes for the program without blocking the loop.
func (t *Terminal) send(b []byte) {
	if t.running && len(b) > 0 {
		t.in.push(b)
	}
}

func (t *Terminal) setMode(m TerminalMode) {
	if t.mode == m {
		return
	}
	t.mode = m
	if m == TerminalNormal {
		t.normal.enter(t)
	} else {
		t.normal.leave()
	}
	if t.cfg.onMode != nil {
		t.cfg.onMode(m)
	}
	t.MarkDirty()
}

// flushPrefix sends a pending Ctrl-\ to a running program.
func (t *Terminal) flushPrefix() {
	if t.prefix {
		t.prefix = false
		t.send([]byte{0x1c})
	}
}

// Layout takes all the space offered; a new size reaches the screen and
// the program after layout.
func (t *Terminal) Layout(c tui.Constraints) tui.Size {
	t.w = boundedMax(c.MaxW, max(c.MinW, 1))
	t.h = boundedMax(c.MaxH, max(c.MinH, 1))
	if ctx := t.Context(); ctx != nil && (t.w != t.cols || t.h != t.rows) {
		w, h := t.w, t.h
		ctx.AfterLayout("terminal.resize", func() { t.resize(h, w) })
	}
	return c.Constrain(tui.Size{W: t.w, H: t.h})
}

func (t *Terminal) resize(rows, cols int) {
	if rows == t.rows && cols == t.cols {
		return
	}
	t.rows, t.cols = rows, cols
	t.scr.Resize(rows, cols)
	if t.running {
		proc := t.proc
		go proc.Resize(rows, cols)
	}
	t.normal.clamp(t)
	t.MarkDirty()
}

// HandleEvent routes keys, pastes, the mouse and focus by mode.
func (t *Terminal) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.FocusEvent:
		if !e.Gained {
			t.flushPrefix()
		}
		if t.running && t.scr.Modes().FocusEvents && !e.Terminal {
			if e.Gained {
				t.send([]byte("\x1b[I"))
			} else {
				t.send([]byte("\x1b[O"))
			}
		}
		return false
	case tui.PasteEvent:
		if t.mode == TerminalNormal || !t.running {
			return false
		}
		if t.prefix {
			t.prefix = false
			t.send([]byte{0x1c})
		}
		t.send(encodePaste(e.Text, t.scr.Modes()))
		return true
	case tui.MouseEvent:
		return t.handleMouse(e)
	case tui.KeyEvent:
		if e.Kind == tui.KeyRelease {
			return false
		}
		if t.mode == TerminalNormal {
			return t.normal.key(t, e)
		}
		return t.inputKey(e)
	}
	return false
}

func isCtrl(k tui.KeyEvent, r rune) bool {
	return k.Mods.Chord()&^tui.ModShift == tui.ModCtrl && (k.Code == r || k.Code == r-'a'+'A')
}

// inputKey handles a key in input mode.
func (t *Terminal) inputKey(k tui.KeyEvent) bool {
	if !t.running {
		// After an exit or a failed start, Enter or i starts a program.
		if t.exited && (k.Code == tui.KeyEnter || (k.Text == "i" && k.Mods.Chord() == 0)) {
			_ = t.Start() // a failure shows on the screen again
			return true
		}
		return false
	}
	if t.prefix {
		t.prefix = false
		switch {
		case isCtrl(k, 'n'):
			t.setMode(TerminalNormal)
			return true
		case isCtrl(k, '\\'):
			t.send([]byte{0x1c})
			return true
		}
		t.send([]byte{0x1c})
		t.send(encodeKey(k, t.scr.Modes()))
		return true
	}
	if isCtrl(k, '\\') {
		t.prefix = true
		return true
	}
	if k.Code == tui.KeyEscape && k.Mods.Chord() == 0 && t.cfg.vimKeys && !t.scr.Modes().AltScreen {
		t.setMode(TerminalNormal)
		return true
	}
	if b := encodeKey(k, t.scr.Modes()); b != nil {
		t.send(b)
		return true
	}
	return false
}

func (t *Terminal) handleMouse(e tui.MouseEvent) bool {
	m := t.scr.Modes()
	if t.mode == TerminalInput && t.running && m.Mouse != vt.MouseOff {
		if b := t.mouse.encode(e, m); b != nil {
			t.send(b)
		}
		return true
	}
	if e.Kind != tui.MouseWheel || (e.Button != tui.WheelUp && e.Button != tui.WheelDown) {
		return e.Kind == tui.MousePress
	}
	if m.AltScreen && t.mode == TerminalInput && t.running {
		// A full-screen program without mouse reporting scrolls with the
		// arrow keys, as xterm's alternate scroll does.
		key := tui.KeyEvent{Code: tui.KeyDown}
		if e.Button == tui.WheelUp {
			key.Code = tui.KeyUp
		}
		for range 3 {
			t.send(encodeKey(key, m))
		}
		return true
	}
	n := 3
	if e.Button == tui.WheelUp {
		n = -3
	}
	if t.mode == TerminalInput {
		if n > 0 {
			return true // already at the bottom
		}
		t.setMode(TerminalNormal)
	}
	t.normal.scroll(t, n)
	return true
}

// Render paints the visible lines: the live screen in input mode, and the
// normal-mode view over the scrollback in normal mode.
func (t *Terminal) Render(s tui.Surface) {
	size := s.Size()
	top := t.scr.Scrollback()
	if t.mode == TerminalNormal {
		top = t.normal.top
	}
	for y := 0; y < size.H; y++ {
		line := top + y
		cells := t.lineCells(line)
		for x := 0; x < size.W; x++ {
			var c vt.Cell
			if x < len(cells) {
				c = cells[x]
			} else {
				c = vt.Cell{Width: 1}
			}
			if c.Width == 0 {
				continue // painted with its wide cluster
			}
			content := c.Content
			if content == "" || c.Attr&vt.AttrInvisible != 0 {
				content = " "
			}
			st := t.cellStyle(c)
			if t.mode == TerminalNormal && t.normal.selected(line, x) {
				st = st.Reverse(c.Attr&vt.AttrReverse == 0)
			}
			s.SetCell(x, y, content, st)
		}
	}
	if t.mode == TerminalNormal {
		t.normal.render(t, s)
	}
}

// lineCells is absolute line i: scrollback lines first, then the screen.
func (t *Terminal) lineCells(i int) []vt.Cell {
	sb := t.scr.Scrollback()
	if i < 0 {
		return nil
	}
	if i < sb {
		return t.scr.Line(i)
	}
	row := i - sb
	rows, cols := t.scr.Size()
	if row >= rows {
		return nil
	}
	cells := make([]vt.Cell, cols)
	for c := range cells {
		cells[c] = t.scr.Cell(row, c)
	}
	return cells
}

// lineCount is the number of absolute lines: the scrollback and the screen.
func (t *Terminal) lineCount() int {
	rows, _ := t.scr.Size()
	return t.scr.Scrollback() + rows
}

func (t *Terminal) cellStyle(c vt.Cell) style.Style {
	st := style.New()
	switch {
	case !c.FG.IsDefault():
		st = st.Foreground(c.FG)
	case t.cfg.themeColors:
		st = st.Foreground(style.TokenForeground)
	}
	switch {
	case !c.BG.IsDefault():
		st = st.Background(c.BG)
	case t.cfg.themeColors:
		st = st.Background(style.TokenBackground)
	}
	a := c.Attr
	if a&vt.AttrBold != 0 {
		st = st.Bold(true)
	}
	if a&vt.AttrFaint != 0 {
		st = st.Faint(true)
	}
	if a&vt.AttrItalic != 0 {
		st = st.Italic(true)
	}
	if a&vt.AttrUnderline != 0 {
		st = st.Underline(true)
	}
	if a&vt.AttrBlink != 0 {
		st = st.Blink(true)
	}
	if a&vt.AttrReverse != 0 {
		st = st.Reverse(true)
	}
	if a&vt.AttrStrike != 0 {
		st = st.Strikethrough(true)
	}
	return st
}

// Cursor is the program's cursor in input mode (hidden when the program
// hides it), and the normal-mode cursor in normal mode.
func (t *Terminal) Cursor() (x, y int, ok bool) {
	if t.mode == TerminalNormal {
		return t.normal.cursor(t)
	}
	row, col, visible, _ := t.scr.Cursor()
	return col, row, visible && (t.running || !t.exited)
}

// CursorShape is the program's DECSCUSR shape in input mode, and a block in
// normal mode.
func (t *Terminal) CursorShape() tui.CursorShape {
	if t.mode == TerminalNormal {
		return tui.CursorShapeBlock
	}
	_, _, _, shape := t.scr.Cursor()
	switch shape {
	case vt.CursorBlock:
		return tui.CursorShapeBlock
	case vt.CursorUnderline:
		return tui.CursorShapeUnderline
	case vt.CursorBar:
		return tui.CursorShapeBar
	}
	return tui.CursorShapeDefault
}

// inputQueue writes keys to the program on its own goroutine, so a program
// that stops reading never blocks the UI loop.
type inputQueue struct {
	mu        sync.Mutex
	cond      *sync.Cond
	q         [][]byte
	closed    bool          // drop what is queued and stop
	finishing bool          // write what is queued, then stop
	done      chan struct{} // closed when the writer has stopped
}

func newInputQueue(w io.Writer) *inputQueue {
	iq := &inputQueue{done: make(chan struct{})}
	iq.cond = sync.NewCond(&iq.mu)
	go func() {
		defer close(iq.done)
		for {
			iq.mu.Lock()
			for len(iq.q) == 0 && !iq.closed && !iq.finishing {
				iq.cond.Wait()
			}
			if iq.closed || len(iq.q) == 0 {
				iq.mu.Unlock()
				return
			}
			b := iq.q[0]
			iq.q = iq.q[1:]
			iq.mu.Unlock()
			if _, err := w.Write(b); err != nil {
				iq.close()
				return
			}
		}
	}()
	return iq
}

func (iq *inputQueue) push(b []byte) {
	iq.mu.Lock()
	if !iq.closed && !iq.finishing {
		iq.q = append(iq.q, append([]byte(nil), b...))
		iq.cond.Signal()
	}
	iq.mu.Unlock()
}

// finish stops taking keys and returns a channel closed once the queued
// ones are written.
func (iq *inputQueue) finish() <-chan struct{} {
	iq.mu.Lock()
	iq.finishing = true
	iq.cond.Broadcast()
	iq.mu.Unlock()
	return iq.done
}

func (iq *inputQueue) close() {
	iq.mu.Lock()
	iq.closed = true
	iq.q = nil
	iq.cond.Broadcast()
	iq.mu.Unlock()
}
