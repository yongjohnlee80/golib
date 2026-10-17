package widget

import (
	"fmt"
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
		o(&cfg)
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

// SetModeKey sets WithModeKey's key while the Terminal runs; nil removes it.
func (t *Terminal) SetModeKey(match func(tui.KeyEvent) bool) { t.cfg.modeKey = match }

// isModeKey reports the mode key (WithModeKey), its press or a repeat of it: match judges the press.
func (t *Terminal) isModeKey(e tui.KeyEvent) bool {
	if t.cfg.modeKey == nil {
		return false
	}
	press := e
	press.Kind = tui.KeyPress
	return t.cfg.modeKey(press)
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

// Start runs the program, unless it is running. Before the mount it runs
// once the Terminal is mounted. After an exit it starts a new one, below
// what the last one left on the screen. A start that fails says so on the
// screen and returns the error; Enter or i in the Terminal, or another
// Start, tries again.
func (t *Terminal) Start() error {
	t.procMu.Lock()
	shuttingDown := t.shutdown != nil
	t.procMu.Unlock()
	if shuttingDown {
		return ErrTerminalShutdown
	}
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
		t.procMu.Lock()
		t.lifecycle = inactiveTerminalStopResult(err)
		t.procMu.Unlock()
		t.exited = true
		t.report(err)
		return err
	}
	t.gen++
	gen := t.gen
	t.proc = proc
	t.running, t.exited = true, false
	t.in = newInputQueue(proc)
	t.wr = newBufWriter(func(b []byte) {
		if gen == t.gen { // hidden or not: the screen keeps taking output
			t.ingest(b)
		}
	})
	t.wr.bind(ctx.App())
	lifecycle := newTerminalStopResult(proc, gen, t.in, t.wr)
	t.procMu.Lock()
	kept := t.lifetimes[:0]
	for _, old := range t.lifetimes {
		select {
		case <-old.Done():
			if old.completion.err != nil {
				kept = append(kept, old)
			}
		default:
			kept = append(kept, old)
		}
	}
	t.lifetimes = append(kept, lifecycle)
	t.lifecycle = lifecycle
	t.procMu.Unlock()
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
		lifecycle.cleanup(nil) // cleanup also runs if exit cannot be posted to an ended loop
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
func (t *Terminal) Stop() { t.StopWithResult() }

// StopWithResult requests hang-up on the UI loop and returns immediately. The
// generation handle survives repeated calls, natural exit and restart. Before
// mount, it cancels pending Start. Never-started and failed-start outcomes have
// Started false, with StartError distinguishing them; neither needs cleanup.
func (t *Terminal) StopWithResult() *TerminalStopResult {
	t.pending = false
	t.flushPrefix()
	r := t.CleanupResult()
	if !t.running {
		return r
	}
	t.gen++ // output/exit callbacks are invalidated, not the result's immutable generation
	t.running = false
	t.wr.close()
	r.cleanup(t.in.finish())
	t.MarkDirty()
	return r
}

// CleanupResult observes the current attempt without requesting Stop. Obtain
// it on the UI loop, or after App.Done; its Wait/Done methods are concurrent-safe.
func (t *Terminal) CleanupResult() *TerminalStopResult {
	t.procMu.Lock()
	if t.lifecycle == nil {
		t.lifecycle = inactiveTerminalStopResult(nil)
	}
	r := t.lifecycle
	t.procMu.Unlock()
	return r
}

// ShutdownWithResult is safe on the UI loop or after App.Done. It uses captured
// lifecycle records, never mutating loop-owned fields concurrently with decl
// destruction. It joins active and already-closing generations off-loop. A caller
// can retain the result and observe it after component destruction.
func (t *Terminal) ShutdownWithResult() *TerminalShutdownResult {
	t.procMu.Lock()
	if t.shutdown != nil {
		r := t.shutdown
		t.procMu.Unlock()
		return r
	}
	t.shutdown = joinTerminalCleanup(append([]*TerminalStopResult(nil), t.lifetimes...))
	r := t.shutdown
	t.procMu.Unlock()
	return r
}

// endWithApp hangs the program up once the App has ended (the loop is
// gone, so this runs on the watcher's goroutine).
func (t *Terminal) endWithApp() {
	t.ShutdownWithResult()
}

func (t *Terminal) stop() {
	t.StopWithResult()
}

func (t *Terminal) exitWith(code int) {
	t.running, t.exited, t.exitCode = false, true, code
	t.wr.close()
	t.in.close()
	t.procMu.Lock()
	r := t.lifecycle
	t.procMu.Unlock()
	if r != nil {
		r.cleanup(nil)
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
	if t.Running() && len(b) > 0 {
		if err := t.in.offer(b); err != nil {
			t.report(err)
		}
	}
}

// ErrTerminalNotRunning rejects typed input when there is no live process.
var ErrTerminalNotRunning = errs.Sentinel(errs.ErrPrecondition, "terminal is not running")

// ErrTerminalInputFull means the bounded input queue cannot accept this whole send.
var ErrTerminalInputFull = errs.Sentinel(errs.ErrPrecondition, "terminal input queue is full")

// TerminalInputLimit bounds queued and in-flight input bytes per process.
const TerminalInputLimit = 1 << 20

// SendText accepts literal text into the running generation's bounded input queue.
// On the UI loop. Nil means queued, not written or processed by the child. A
// rejected send queues no bytes; no PTY writer is exposed to the caller.
func (t *Terminal) SendText(text string) error {
	if !t.Running() {
		return ErrTerminalNotRunning
	}
	if len(text) > TerminalInputLimit {
		return ErrTerminalInputFull
	}
	return t.in.offer([]byte(text))
}

// SendPaste sends text as a paste, as the terminal's own paste does: bracketed when the program
// asked for bracketed paste (mode 2004, which agent CLIs and shells with line editing set), so a
// multi-line text arrives whole rather than line by line as Enters; otherwise its newlines go as
// Enter. A text holding the bracket's end has it removed, so it cannot end the paste early. Like
// SendText it is queued, not written, and a rejected send queues nothing. On the UI loop.
func (t *Terminal) SendPaste(text string) error {
	if !t.Running() {
		return ErrTerminalNotRunning
	}
	b := encodePaste(text, t.scr.Modes())
	if len(b) > TerminalInputLimit {
		return ErrTerminalInputFull
	}
	return t.in.offer(b)
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
		if t.isModeKey(e) {
			if e.Kind == tui.KeyRepeat {
				return true // held: one press toggles once
			}
			return t.toggleMode()
		}
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

func isCtrl(k tui.KeyEvent, r rune) bool {
	return k.Mods.Chord()&^tui.ModShift == tui.ModCtrl && (k.Code == r || k.Code == r-'a'+'A')
}

// termInput is what a key does in input mode.
type termInput int

const (
	termNone       termInput = iota // not the Terminal's: it goes on to the host
	termSend                        // the program's: encoded and sent
	termRestart                     // a program started again, after an exit
	termPrefix                      // the escape prefix armed
	termToNormal                    // normal mode
	termSendPrefix                  // the prefix's own byte sent (the prefix twice)
	termSendBoth                    // the prefix's byte, then the key, sent (an unknown key after it)
)

// inputAct is what k does in input mode. Every key that is not one of the Terminal's commands, or is
// one whose condition does not hold, is the program's.
func inputAct(ctx *tui.Context, k tui.KeyEvent, running, exited, prefix, vimKeys, altScreen bool) termInput {
	cmd, isCmd := ctx.KeyCommand(k, terminalCommands...)
	switch {
	case !running && exited && isCmd && cmd.ID == TerminalRestart.ID:
		return termRestart // after an exit or a failed start, Enter or i starts a program
	case !running:
		return termNone
	case prefix:
		// the key after the prefix is its grammar: Ctrl+n for normal mode, the prefix again to send it
		switch {
		case isCtrl(k, 'n'):
			return termToNormal
		case isCmd && cmd.ID == TerminalPrefix.ID:
			return termSendPrefix
		}
		return termSendBoth
	case isCmd && cmd.ID == TerminalPrefix.ID:
		return termPrefix
	case isCmd && cmd.ID == TerminalToNormal.ID && vimKeys && !altScreen:
		return termToNormal
	}
	return termSend
}

// inputKey handles a key in input mode.
func (t *Terminal) inputKey(k tui.KeyEvent) bool {
	act := inputAct(t.Context(), k, t.running, t.exited, t.prefix, t.cfg.vimKeys, t.running && t.scr.Modes().AltScreen)
	if t.prefix && t.running {
		t.prefix = false
	}
	switch act {
	case termNone:
		return false
	case termRestart:
		_ = t.Start() // a failure shows on the screen again
	case termPrefix:
		t.prefix = true
	case termToNormal:
		t.setMode(TerminalNormal)
	case termSendPrefix:
		t.send([]byte{0x1c})
	case termSendBoth:
		t.send([]byte{0x1c})
		t.send(encodeKey(k, t.scr.Modes()))
	case termSend:
		b := encodeKey(k, t.scr.Modes())
		if b == nil {
			return false
		}
		t.send(b)
	}
	return true
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
		if fg, ok := t.look.GetForeground(); ok {
			st = st.Foreground(fg)
		} else {
			st = st.Foreground(style.TokenForeground)
		}
	}
	switch {
	case !c.BG.IsDefault():
		st = st.Background(c.BG)
	case t.cfg.themeColors:
		if bg, ok := t.look.GetBackground(); ok {
			st = st.Background(bg)
		} else {
			st = st.Background(style.TokenBackground)
		}
	}
	// a foreground raised to the minimum contrast against the background it is drawn on
	if t.minContrast > 1 && c.Attr&vt.AttrReverse == 0 {
		fg, fgSet := st.GetForeground()
		bg, bgSet := st.GetBackground()
		if !bgSet {
			bg, bgSet = t.look.GetBackground()
		}
		if fgSet && bgSet {
			st = st.Foreground(t.legible(fg, bg))
		}
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

// CursorShape shows the mode, as the Editor's does: a block in normal mode, and in input mode a
// thin bar, the Editor's while typing, unless the program asked for a shape of its own with
// DECSCUSR (vim's block, an underline). DECSCUSR 0 asks for the host's own, which is the bar.
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
	}
	return tui.CursorShapeBar
}

// SetCellFace draws the Terminal's cells in family, a monospace font of its own, at the window's
// cell size (tui.CellFacer); "" is the window's own. A backend that draws no fonts — a terminal —
// ignores it, its terminal owning the font. A menu or tooltip over it keeps the window's.
func (t *Terminal) SetCellFace(family string) {
	if t.cellFace == family {
		return
	}
	t.cellFace = family
	t.MarkDirty()
}

// CellFace is SetCellFace's family: tui.CellFacer.
func (t *Terminal) CellFace() string { return t.cellFace }

// isHostKey reports a host key (SetHostKeys), its press or a repeat of it: match judges the press.
func (t *Terminal) isHostKey(e tui.KeyEvent) bool {
	if t.hostKeys == nil {
		return false
	}
	press := e
	press.Kind = tui.KeyPress
	return t.hostKeys(press)
}

// SetHostKeys leaves the keys match accepts to the host: in input mode the Terminal sends none of
// such a key. Its press is not consumed, so it goes on to its ancestors and the Window's Shortcuts
// (a host's F11, say) as an unconsumed key does; a held key's repeats are consumed and dropped, so
// the host acts once per press. match judges the press. Normal mode is unchanged. The program then never sees
// those keys (an F11 that mc or htop would take). nil, the default, gives every key to the program.
func (t *Terminal) SetHostKeys(match func(tui.KeyEvent) bool) { t.hostKeys = match }

// SetCursorColor gives the hardware cursor a colour of its own, as the Editor's SetCursorColor
// does, so the two carets look alike; the terminal's own colour otherwise.
func (t *Terminal) SetCursorColor(c style.Color) {
	t.cursorColor, t.cursorColored = c, true
	t.MarkDirty()
}

// CursorColor implements tui.CursorColorer.
func (t *Terminal) CursorColor() (style.Color, bool) { return t.cursorColor, t.cursorColored }

// inputQueue writes keys to the program on its own goroutine, so a program
// that stops reading never blocks the UI loop.
type inputQueue struct {
	mu        sync.Mutex
	cond      *sync.Cond
	q         [][]byte
	pending   int           // queued and currently being written, bounded by TerminalInputLimit
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
			iq.q[0] = nil
			iq.q = iq.q[1:]
			iq.mu.Unlock()
			n, err := w.Write(b)
			iq.mu.Lock()
			iq.pending -= len(b)
			iq.mu.Unlock()
			if err != nil || n != len(b) {
				iq.close()
				return
			}
		}
	}()
	return iq
}

func (iq *inputQueue) offer(b []byte) error {
	iq.mu.Lock()
	defer iq.mu.Unlock()
	if iq.closed || iq.finishing {
		return ErrTerminalNotRunning
	}
	if len(b) > TerminalInputLimit-iq.pending {
		return ErrTerminalInputFull
	}
	if len(b) > 0 {
		iq.q = append(iq.q, append([]byte(nil), b...))
		iq.pending += len(b)
		iq.cond.Signal()
	}
	return nil
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
	for _, b := range iq.q {
		iq.pending -= len(b)
	}
	iq.q = nil
	iq.cond.Broadcast()
	iq.mu.Unlock()
}
