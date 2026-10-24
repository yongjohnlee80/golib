package widget

import (
	"io"
	"os"
	"sync"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
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
// Wait must be repeatable and safe for concurrent callers: the output reader
// observes exit while the cleanup owner independently verifies reap.
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
	modeKey     func(tui.KeyEvent) bool // WithModeKey: the key that toggles input and normal mode
	modeless    bool                    // WithModelessScroll: the wheel's scrollback view ends at the next key
	themeColors bool
	onExit      func(int)
	onTitle     func(string)
	onMode      func(TerminalMode)
	start       func(pty.Cmd) (TerminalProcess, error)
	// look set at construction (WithMinimumContrast, WithCursorColor); the setters change it later
	minContrast   float64
	cursorColor   style.Color
	cursorColored bool
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

// WithModelessScroll makes the wheel's scrollback view no mode to leave, for a user who never
// learnt normal mode: the wheel opens the view (normal mode, its cursor and selection), and the next
// key or paste goes back to input and to the program, as if no view had opened; the view wheeled
// back to the bottom is input again too. Normal mode asked for (the mode key, Ctrl+\ Ctrl+n, Esc
// with WithVimKeys) stays modal. Off, the default, the wheel enters normal mode, as before.
func WithModelessScroll() TerminalOption { return func(c *terminalConfig) { c.modeless = true } }

// WithModeKey makes the keys match accepts toggle input and normal mode: from input mode into
// normal mode, and from normal mode back to input (as i and a do). It works while a full-screen
// program runs too, since such a program keeps Esc; it never reaches the program. A held key's
// repeats are consumed and dropped, so one press toggles once. nil, the default, has no such key:
// Ctrl+\ Ctrl+n (and Esc with WithVimKeys) enter normal mode, as before. A host giving Esc to its
// programs (agents interrupt on it) pairs WithVimKeys(false) with a mode key.
func WithModeKey(match func(tui.KeyEvent) bool) TerminalOption {
	return func(c *terminalConfig) { c.modeKey = match }
}

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

// WithMinimumContrast is SetMinimumContrast at construction: a TerminalTabs tab's own look.
func WithMinimumContrast(ratio float64) TerminalOption {
	return func(c *terminalConfig) { c.minContrast = ratio }
}

// WithCursorColor is SetCursorColor at construction: a TerminalTabs tab's own look.
func WithCursorColor(col style.Color) TerminalOption {
	return func(c *terminalConfig) { c.cursorColor, c.cursorColored = col, true }
}

// WithStarter replaces how the program is started: a host can run it
// somewhere else, and tests run a fake. The default is pty.Start.
func WithStarter(fn func(pty.Cmd) (TerminalProcess, error)) TerminalOption {
	return func(c *terminalConfig) { c.start = fn }
}

// Terminal runs one program on a pseudo-terminal and shows its screen. In
// input mode the keys go to the program; in normal mode they move over the
// scrollback, select and yank, and the host's own keys work. Normal mode's
// word motions are the editor's: w, b, e and ge by word, W, B, E and gE by
// WORD, a wide character one cluster over its two cells.
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
//
// The wheel scrolls the scrollback in normal mode. With WithModelessScroll that view is no mode to
// leave: the next key or paste goes back to input and to the program.
type Terminal struct {
	Base
	cfg terminalConfig

	scr        *vt.Screen
	rows, cols int // the size applied to the screen and the program
	w, h       int // the size layout gave

	view TerminalView  // draws the terminal at a cell size of its own; nil: the window's cells
	off  tui.Offscreen // where Render draws for the view

	proc      TerminalProcess
	procMu    sync.Mutex // guards the lifecycle records and shutdown handle
	lifecycle *TerminalStopResult
	lifetimes []*TerminalStopResult // active/closing/error generations; completed successes are pruned at Start
	shutdown  *TerminalShutdownResult
	gen       int // the program's generation: output and exits of older ones are dropped
	wr        *bufWriter
	in        *inputQueue
	pumps     sync.WaitGroup
	running   bool
	exited    bool
	exitCode  int
	pending   bool // Start was asked before the mount
	alive     bool // mounted
	watching  bool // a goroutine ends the program with the App

	mode   TerminalMode
	prefix bool // Ctrl-\ is pending
	mouse  mouseTracker
	title  string

	normal normalState
	look   style.Style // the program's default colours, with theme colours on (SetDefaultLook)
	// cursorColor is the hardware cursor's colour while cursorColored (tui.CursorColorer)
	cursorColor   style.Color
	cursorColored bool
	hostKeys      func(tui.KeyEvent) bool // SetHostKeys: the keys left to the host, never the program's
	cellFace      string                  // SetCellFace: the family its cells are drawn in; "" the window's
	// minContrast is the ratio a foreground is raised to against its background; 0: off
	// (terminal_contrast.go). contrastCache holds the colours it gave.
	minContrast   float64
	contrastCache map[contrastKey]style.Color
}

// NewTerminal returns a Terminal; Start runs its program.
func NewTerminal(opts ...TerminalOption) *Terminal {
	cfg := terminalConfig{scrollback: vt.DefaultScrollback, themeColors: true}
	for _, o := range opts {
		if o != nil { // a nil option is skipped, as every other constructor here does
			o(&cfg)
		}
	}
	cfg.path = defaultShell(cfg.path)
	if cfg.start == nil {
		cfg.start = func(c pty.Cmd) (TerminalProcess, error) { return pty.Start(c) }
	}
	t := &Terminal{cfg: cfg, rows: 24, cols: 80, minContrast: cfg.minContrast,
		cursorColor: cfg.cursorColor, cursorColored: cfg.cursorColored}
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

// SetModelessScroll sets WithModelessScroll while the Terminal runs; a scrollback view already open
// keeps the way it was opened.
func (t *Terminal) SetModelessScroll(on bool) { t.cfg.modeless = on }

// SetModeKey sets WithModeKey's key while the Terminal runs; nil removes it.
func (t *Terminal) SetModeKey(match func(tui.KeyEvent) bool) { t.cfg.modeKey = match }

// isModeKey reports the mode key (WithModeKey), its press or a repeat of it: match judges the press.
func (t *Terminal) isModeKey(e tui.KeyEvent) bool { return pressMatches(t.cfg.modeKey, e) }

// pressMatches reports whether match, if set, accepts e as the press it is, a repeat or a release
// included: a key the host claims is claimed in every phase.
func pressMatches(match func(tui.KeyEvent) bool, e tui.KeyEvent) bool {
	if match == nil {
		return false
	}
	e.Kind = tui.KeyPress
	return match(e)
}

// toggleMode is the mode key: normal mode back to input, input mode into normal mode while a
// program runs (Ctrl+\ Ctrl+n's condition). A Ctrl+\ waiting for its second key is sent as itself.
func (t *Terminal) toggleMode() bool {
	if t.mode == TerminalNormal {
		t.setMode(TerminalInput)
		return true
	}
	if !t.running {
		return false
	}
	t.flushPrefix()
	t.setMode(TerminalNormal)
	return true
}

// SetDefaultLook sets what the program's default foreground and background are painted in, with
// theme colours on: st's foreground and background, each where it sets one, else the theme's.
// A host gives the palette's text on base, so the terminal sits on the page's colours.
func (t *Terminal) SetDefaultLook(st style.Style) {
	t.look = st
	t.contrastCache = nil
	t.MarkDirty()
}

// SetThemeColors turns WithThemeColors on or off while the Terminal runs.
func (t *Terminal) SetThemeColors(on bool) {
	t.cfg.themeColors = on
	t.MarkDirty()
}

// Running reports whether the program is running.
func (t *Terminal) Running() bool {
	t.procMu.Lock()
	shuttingDown := t.shutdown != nil
	t.procMu.Unlock()
	return t.running && !shuttingDown
}

// Title is the title the program last set.
func (t *Terminal) Title() string { return t.title }

// Screen is the emulator, for reading what the program shows. It belongs
// to the UI loop.
func (t *Terminal) Screen() *vt.Screen { return t.scr }

// ErrTerminalNotRunning rejects typed input when there is no live process.
var ErrTerminalNotRunning = errs.Sentinel(errs.ErrPrecondition, "terminal is not running")

// ErrTerminalInputFull means the bounded input queue cannot accept this whole send.
var ErrTerminalInputFull = errs.Sentinel(errs.ErrPrecondition, "terminal input queue is full")

// TerminalInputLimit bounds queued and in-flight input bytes per process.
const TerminalInputLimit = 1 << 20

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
// the program after layout. With a view, that size is the view's cells for
// the area placed (TerminalView.Cells), so the one resize registered is it.
func (t *Terminal) Layout(c tui.Constraints) tui.Size {
	t.w = fillW(c)
	t.h = fillH(c)
	w, h := t.w, t.h
	if t.view != nil {
		w, h = t.view.Cells(t.w, t.h)
		w, h = max(w, 1), max(h, 1)
	}
	if ctx := t.Context(); ctx != nil && (w != t.cols || h != t.rows) {
		ctx.AfterLayout("terminal.resize", func() { t.resize(h, w) })
	}
	return c.Constrain(tui.Size{W: t.w, H: t.h})
}

// shownRows is the rows the terminal shows: the view's, or the area layout gave.
func (t *Terminal) shownRows() int {
	if t.view != nil {
		return t.rows
	}
	return t.h
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
		t.endTransientView()
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
		if t.view != nil {
			e = t.view.Pointer(e) // in the terminal's own cells
		}
		return t.handleMouse(e)
	case tui.KeyEvent:
		if e.Kind == tui.KeyRelease {
			return false
		}
		if t.isModeKey(e) {
			if e.Kind == tui.KeyRepeat {
				return true // held: one press toggles once
			}
			return t.toggleMode()
		}
		t.endTransientView()
		if t.mode == TerminalNormal {
			return t.normal.key(t, e)
		}
		if t.isHostKey(e) {
			// the host's: none of it is sent. A Ctrl+\ waiting for its second key is sent as
			// itself, so it cannot complete with the key after (Ctrl+\ F11 Ctrl+n is no Normal
			// mode). The press goes on to the Window's keys; a held key's repeats are swallowed, so
			// the host acts once per press.
			t.flushPrefix()
			return e.Kind == tui.KeyRepeat
		}
		return t.inputKey(e)
	}
	return false
}

// KeyScope is the scope of the Terminal's mode: its own keys in input mode, normal mode's, or the
// search line's while one is open.
func (t *Terminal) KeyScope() keymap.Scope {
	switch {
	case t.mode != TerminalNormal:
		return TerminalScope
	case t.normal.searching:
		return TerminalSearchScope
	}
	return TerminalNormalScope
}

// KeyBindings is the commands of the Terminal's mode as m binds them (tui.KeyBinder). In input mode
// every other key is the program's; the host's mode key and host keys (WithModeKey, SetHostKeys) are
// its code, not commands.
func (t *Terminal) KeyBindings(m *keymap.Map) []keymap.Binding {
	switch t.KeyScope() {
	case TerminalSearchScope:
		return tui.BindingsOf(m, terminalSearchCommands...)
	case TerminalNormalScope:
		return tui.BindingsOf(m, terminalNormalCommands...)
	}
	return tui.BindingsOf(m, terminalCommands...)
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
		t.normal.transient = t.cfg.modeless
	}
	t.normal.scroll(t, n)
	if t.normal.transient && t.normal.atBottom(t) {
		t.setMode(TerminalInput) // wheeled back to the bottom: the view is closed
	}
	return true
}

// endTransientView closes a scrollback view the wheel opened under WithModelessScroll: the event
// that ends it is the program's, handled in input mode as if no view had opened.
func (t *Terminal) endTransientView() {
	if t.mode == TerminalNormal && t.normal.transient {
		t.setMode(TerminalInput)
	}
}

// isHostKey reports a host key (SetHostKeys), its press or a repeat of it: match judges the press.
func (t *Terminal) isHostKey(e tui.KeyEvent) bool { return pressMatches(t.hostKeys, e) }

// SetHostKeys leaves the keys match accepts to the host: in input mode the Terminal sends none of
// such a key. Its press is not consumed, so it goes on to its ancestors and the Window's Shortcuts
// (a host's F11, say) as an unconsumed key does; a held key's repeats are consumed and dropped, so
// the host acts once per press. match judges the press. Normal mode is unchanged. The program then never sees
// those keys (an F11 that mc or htop would take). nil, the default, gives every key to the program.
func (t *Terminal) SetHostKeys(match func(tui.KeyEvent) bool) { t.hostKeys = match }

// TerminalView is a host that draws a Terminal itself, at a cell size of its own (a GUI's native
// painter). With none, a Terminal is drawn in the window's cells, as before.
type TerminalView interface {
	// Cells is the terminal's size for a placed area of w×h window cells: its screen's and its
	// program's columns and rows (at least 1×1).
	Cells(w, h int) (cols, rows int)
	// Pointer maps a pointer event in the terminal's window cells (with SubX, SubY) to its own.
	Pointer(e tui.MouseEvent) tui.MouseEvent
	// Frame is the terminal's frame, drawn off the window at cols×rows (row-major), and its caret:
	// its cursor in its own cells, its shape, and show, true when the terminal has the focus and its
	// cursor shows. cells are the terminal's own, reused at its next frame: the view copies what it
	// keeps.
	Frame(cells []tui.Cell, cols, rows int, cursor tui.Point, shape tui.CursorShape, show bool)
	// Detached is the view taken off its terminal (SetTerminalView, or its tab closed): it drops
	// its frame and its caret, and posts nothing more.
	Detached()
}

// SetTerminalView gives the terminal its view; nil is the window's cells again. A view replaced is
// told Detached.
func (t *Terminal) SetTerminalView(v TerminalView) {
	if t.view == v {
		return
	}
	if t.view != nil {
		t.view.Detached()
	}
	t.view = v
	t.MarkDirty()
	if ctx := t.Context(); ctx != nil && ctx.Mounted() {
		ctx.RequestLayout()
	}
}

// TerminalView is the view installed, nil for none: what a native style reads back to paint the
// terminal.
func (t *Terminal) TerminalView() TerminalView { return t.view }
