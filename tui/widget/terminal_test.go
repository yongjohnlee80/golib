package widget_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/pty"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// fakeProc is a program the test plays: what it writes to out shows on the
// screen, and what the Terminal sends collects in input.
type fakeProc struct {
	outR *io.PipeReader
	outW *io.PipeWriter

	mu     sync.Mutex
	input  bytes.Buffer
	sizes  [][2]int
	closed bool

	once sync.Once
	done chan struct{}
	code int
}

func newFakeProc() *fakeProc {
	r, w := io.Pipe()
	return &fakeProc{outR: r, outW: w, done: make(chan struct{})}
}

func (p *fakeProc) Read(b []byte) (int, error) { return p.outR.Read(b) }

func (p *fakeProc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.input.Write(b)
}

func (p *fakeProc) Resize(rows, cols int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sizes = append(p.sizes, [2]int{rows, cols})
	return nil
}

func (p *fakeProc) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.exit(129)
	return nil
}

func (p *fakeProc) Wait() (int, error) {
	<-p.done
	return p.code, nil
}

// exit ends the program with code: its output closes.
func (p *fakeProc) exit(code int) {
	p.once.Do(func() {
		p.code = code
		p.outW.Close()
		close(p.done)
	})
}

func (p *fakeProc) sent() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.input.String()
}

func (p *fakeProc) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *fakeProc) lastSize() [2]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sizes) == 0 {
		return [2]int{}
	}
	return p.sizes[len(p.sizes)-1]
}

// termFixture is a running Terminal in a shell, focused, over fake
// programs (each Start takes the next).
type termFixture struct {
	h     *harness
	sh    *shell
	term  *widget.Terminal
	procs []*fakeProc
	cmds  []pty.Cmd
	mu    sync.Mutex
}

func startTerm(t *testing.T, w, ht int, opts ...widget.TerminalOption) *termFixture {
	return startTermApp(t, w, ht, nil, opts...)
}

func startTermApp(t *testing.T, w, ht int, appOpts []tui.AppOption, opts ...widget.TerminalOption) *termFixture {
	t.Helper()
	f := &termFixture{}
	opts = append(opts, widget.WithStarter(func(c pty.Cmd) (widget.TerminalProcess, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		p := newFakeProc()
		f.procs = append(f.procs, p)
		f.cmds = append(f.cmds, c)
		return p, nil
	}))
	f.term = widget.NewTerminal(opts...)
	f.sh = newShell(f.term)
	f.h = startAppOpts(t, f.sh, w, ht, appOpts...)
	t.Cleanup(f.h.stop)
	f.h.onLoop(func() {
		f.term.Context().RequestFocus()
		if err := f.term.Start(); err != nil {
			t.Errorf("Start: %v", err)
		}
	})
	f.h.sync()
	return f
}

func (f *termFixture) proc() *fakeProc {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.procs[len(f.procs)-1]
}

// output plays the program writing s, and waits for the screen to take it.
func (f *termFixture) output(s string) {
	f.h.t.Helper()
	f.proc().outW.Write([]byte(s))
	f.h.settle()
}

func (f *termFixture) waitSent(want string) {
	f.h.t.Helper()
	f.h.waitFor("program received "+strings.ReplaceAll(want, "\x1b", "ESC"), func() bool {
		return strings.Contains(f.proc().sent(), want)
	})
}

func (f *termFixture) mode() widget.TerminalMode {
	var m widget.TerminalMode
	f.h.onLoop(func() { m = f.term.Mode() })
	return m
}

func TestTerminalShowsOutput(t *testing.T) {
	f := startTerm(t, 20, 4)
	f.output("hello\r\n\x1b[31mworld\x1b[0m")
	f.h.waitFor("output painted", func() bool { return strings.HasPrefix(f.h.row(1), "world") })
	if got := f.h.row(0); !strings.HasPrefix(got, "hello") {
		t.Errorf("row 0 = %q", got)
	}
	if a := cellAttrs(f.h, 0, 1); a.FG.Kind != tui.CellColorANSI || a.FG.Index != 1 {
		t.Errorf("red cell = %+v", a.FG)
	}
	env := strings.Join(f.cmds[0].Env, "\n")
	if !strings.Contains(env, "TERM=xterm-256color") || !strings.Contains(env, "COLORTERM=truecolor") {
		t.Errorf("environment lacks TERM/COLORTERM")
	}
}

func TestTerminalResizeReachesTheProgram(t *testing.T) {
	f := startTerm(t, 30, 7)
	// Laid out before Start, the program starts at the laid-out size.
	if c := f.cmds[0]; c.Rows != 7 || c.Cols != 30 {
		t.Errorf("started at %dx%d, want 7x30", c.Rows, c.Cols)
	}
	f.h.tb.InjectResize(40, 9)
	f.h.waitFor("second resize", func() bool { return f.proc().lastSize() == [2]int{9, 40} })
}

func TestTerminalEncodesKeys(t *testing.T) {
	f := startTerm(t, 20, 4)
	f.h.inject(typeString("ls")...)
	f.h.inject(key(tui.KeyEnter), key(tui.KeyUp), ctrl('c'), keyMod('x', tui.ModAlt))
	f.waitSent("ls\r\x1b[A\x03\x1bx")
	f.output("\x1b[?1h") // DECCKM
	f.h.inject(key(tui.KeyUp), keyMod(tui.KeyRight, tui.ModCtrl), key(tui.KeyF5))
	f.waitSent("\x1bOA\x1b[1;5C\x1b[15~")
}

func TestTerminalPastes(t *testing.T) {
	f := startTerm(t, 20, 4)
	f.h.inject(tui.PasteEvent{Text: "a\nb"})
	f.waitSent("a\rb")
	f.output("\x1b[?2004h")
	f.h.inject(tui.PasteEvent{Text: "c\nd"})
	f.waitSent("\x1b[200~c\nd\x1b[201~")
}

func TestTerminalPrefixWaysOut(t *testing.T) {
	t.Run("Ctrl-\\ Ctrl-n reaches normal mode", func(t *testing.T) {
		f := startTerm(t, 20, 4)
		f.h.inject(ctrl('\\'), ctrl('n'))
		f.h.waitFor("normal mode", func() bool { return f.mode() == widget.TerminalNormal })
		f.h.inject(key('x'), key('i'), key('Z'))
		f.waitSent("Z")
		if got := f.proc().sent(); got != "Z" {
			t.Errorf("sent %q; normal mode leaked keys or the prefix sent one", got)
		}
	})
	t.Run("Ctrl-\\ Ctrl-\\ sends one", func(t *testing.T) {
		f := startTerm(t, 20, 4)
		f.h.inject(ctrl('\\'), ctrl('\\'), key('Z'))
		f.waitSent("Z")
		if got := f.proc().sent(); got != "\x1cZ" {
			t.Errorf("sent %q, want one 0x1c", got)
		}
	})
	t.Run("Ctrl-\\ then a key sends both", func(t *testing.T) {
		f := startTerm(t, 20, 4)
		f.h.inject(ctrl('\\'), key('x'))
		f.waitSent("\x1cx")
	})
	t.Run("Ctrl-\\ then a paste sends both", func(t *testing.T) {
		f := startTerm(t, 20, 4)
		f.h.inject(ctrl('\\'), tui.PasteEvent{Text: "p"})
		f.waitSent("\x1cp")
	})
	t.Run("Ctrl-\\ then losing focus sends it", func(t *testing.T) {
		f := startTerm(t, 20, 4)
		f.h.inject(ctrl('\\'))
		f.h.settle()
		f.h.inject(tui.FocusEvent{Gained: false})
		f.waitSent("\x1c")
		f.h.inject(key('Z'))
		f.waitSent("Z")
		if got := f.proc().sent(); got != "\x1cZ" {
			t.Errorf("sent %q; the prefix was left pending", got)
		}
	})
	t.Run("Ctrl-\\ then Stop sends it before the hang-up", func(t *testing.T) {
		f := startTerm(t, 20, 4)
		f.h.inject(ctrl('\\'))
		f.h.settle()
		f.h.onLoop(f.term.Stop)
		f.h.waitFor("hang-up", f.proc().isClosed)
		if got := f.proc().sent(); got != "\x1c" {
			t.Errorf("sent %q before the hang-up, want one 0x1c", got)
		}
	})
	t.Run("Ctrl-\\ then unmount sends it", func(t *testing.T) {
		f := startTerm(t, 20, 4)
		f.h.inject(ctrl('\\'))
		f.h.settle()
		f.h.onLoop(f.sh.unmountChild)
		f.waitSent("\x1c")
		if f.proc().isClosed() {
			t.Error("unmounting hung the program up")
		}
	})
}

func TestTerminalEscape(t *testing.T) {
	t.Run("vim keys on the primary screen", func(t *testing.T) {
		f := startTerm(t, 20, 4, widget.WithVimKeys(true))
		f.h.inject(key(tui.KeyEscape))
		f.h.waitFor("normal mode", func() bool { return f.mode() == widget.TerminalNormal })
	})
	t.Run("vim keys on the alternate screen", func(t *testing.T) {
		f := startTerm(t, 20, 4, widget.WithVimKeys(true))
		f.output("\x1b[?1049h")
		f.h.inject(key(tui.KeyEscape))
		f.waitSent("\x1b")
		if f.mode() != widget.TerminalInput {
			t.Error("Esc left input mode on the alternate screen")
		}
	})
	t.Run("without vim keys", func(t *testing.T) {
		f := startTerm(t, 20, 4)
		f.h.inject(key(tui.KeyEscape))
		f.waitSent("\x1b")
		if f.mode() != widget.TerminalInput {
			t.Error("Esc left input mode without vim keys")
		}
	})
}

func TestTerminalNormalModeYanks(t *testing.T) {
	f := startTerm(t, 20, 4, widget.WithVimKeys(true))
	f.output("first line\r\nsecond word here\r\n$ ")
	f.h.inject(key(tui.KeyEscape))
	f.h.waitFor("normal mode", func() bool { return f.mode() == widget.TerminalNormal })
	// Cursor at the prompt (row 2). Up to "second", then select a word.
	f.h.inject(key('k'), key('0'), key('w'), key('v'), key('e'), key('y'))
	f.h.waitFor("yank", func() bool { return string(f.h.tb.Clipboard()) == "word" })
	f.h.inject(key('g'), key('g'), key('V'), key('j'), key('y'))
	f.h.waitFor("line yank", func() bool {
		return string(f.h.tb.Clipboard()) == "first line\nsecond word here"
	})
	// Esc with nothing to cancel reaches the host.
	f.h.inject(key(tui.KeyEscape))
	f.h.barrier(f.sh)
	var esc bool
	for _, k := range f.sh.bubbledKeys() {
		esc = esc || k.Code == tui.KeyEscape
	}
	if !esc {
		t.Error("Esc in normal mode with nothing to cancel did not reach the host")
	}
	f.h.inject(key('i'))
	f.h.waitFor("input mode", func() bool { return f.mode() == widget.TerminalInput })
}

func TestTerminalNormalModeSearches(t *testing.T) {
	f := startTerm(t, 20, 5, widget.WithVimKeys(true))
	f.output("alpha\r\nbeta\r\ngamma beta\r\n")
	f.h.inject(key(tui.KeyEscape))
	f.h.waitFor("normal mode", func() bool { return f.mode() == widget.TerminalNormal })
	f.h.inject(key('?'))
	f.h.inject(typeString("beta")...)
	f.h.inject(key(tui.KeyEnter))
	f.h.waitFor("search", func() bool { x, y, _ := f.h.tb.CursorPos(); return x == 6 && y == 2 })
	f.h.inject(key('n'))
	f.h.waitFor("next", func() bool { x, y, _ := f.h.tb.CursorPos(); return x == 0 && y == 1 })
}

func TestTerminalWheel(t *testing.T) {
	f := startTerm(t, 20, 3)
	for i := 0; i < 10; i++ {
		f.output("line\r\n")
	}
	f.h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelUp})
	f.h.waitFor("wheel enters normal mode", func() bool { return f.mode() == widget.TerminalNormal })
	f.h.inject(key('i'))
	f.output("\x1b[?1049h")
	f.h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown})
	f.waitSent("\x1b[B\x1b[B\x1b[B")
	f.output("\x1b[?1000;1006h")
	f.h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 2, Y: 1})
	f.waitSent("\x1b[<0;3;2M")
}

func TestTerminalExitAndRestart(t *testing.T) {
	var codes []int
	f := startTerm(t, 30, 4, widget.WithOnExit(func(c int) { codes = append(codes, c) }))
	f.proc().exit(3)
	f.h.waitFor("exit shown", func() bool { return strings.Contains(f.h.grid(), "[process exited 3]") })
	f.h.waitFor("the exited program's terminal released", f.proc().isClosed)
	var running bool
	f.h.onLoop(func() { running = f.term.Running() })
	if running || len(codes) != 1 || codes[0] != 3 {
		t.Fatalf("running %v, exits %v", running, codes)
	}
	f.h.inject(key(tui.KeyEnter))
	f.h.waitFor("restart", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.procs) == 2
	})
	f.h.inject(key('Z'))
	f.waitSent("Z")
}

func TestTerminalTitle(t *testing.T) {
	var got []string
	f := startTerm(t, 20, 3, widget.WithOnTitle(func(s string) { got = append(got, s) }))
	f.output("\x1b]2;build\x07")
	f.h.waitFor("title", func() bool {
		var s string
		f.h.onLoop(func() { s = f.term.Title() })
		return s == "build"
	})
	if len(got) != 1 || got[0] != "build" {
		t.Errorf("titles %q", got)
	}
}

func TestTerminalThemeColors(t *testing.T) {
	th := style.NewTheme(style.ANSI(4),
		style.WithToken(style.TokenForeground, style.RGB(1, 2, 3)),
		style.WithToken(style.TokenBackground, style.RGB(4, 5, 6)))
	f := startTermApp(t, 20, 3, []tui.AppOption{tui.WithTheme(&th)})
	f.output("a\x1b[31mb\x1b[38;2;9;9;9mc")
	f.h.waitFor("painted", func() bool { return strings.HasPrefix(f.h.row(0), "abc") })
	if a := cellAttrs(f.h, 0, 0); a.FG != (tui.CellColor{Kind: tui.CellColorRGB, R: 1, G: 2, B: 3}) ||
		a.BG != (tui.CellColor{Kind: tui.CellColorRGB, R: 4, G: 5, B: 6}) {
		t.Errorf("default colours = %+v / %+v, want the theme's", a.FG, a.BG)
	}
	if a := cellAttrs(f.h, 1, 0); a.FG.Kind != tui.CellColorANSI || a.FG.Index != 1 {
		t.Errorf("ANSI red = %+v", a.FG)
	}
	if a := cellAttrs(f.h, 2, 0); a.FG != (tui.CellColor{Kind: tui.CellColorRGB, R: 9, G: 9, B: 9}) {
		t.Errorf("truecolor = %+v, want unchanged", a.FG)
	}

	f = startTermApp(t, 20, 3, []tui.AppOption{tui.WithTheme(&th)}, widget.WithThemeColors(false))
	f.output("a")
	f.h.waitFor("painted", func() bool { return strings.HasPrefix(f.h.row(0), "a") })
	if a := cellAttrs(f.h, 0, 0); a.FG.Kind != tui.CellColorDefault {
		t.Errorf("WithThemeColors(false) default fg = %+v", a.FG)
	}
}

func TestTerminalOptionsReachTheProgram(t *testing.T) {
	var modes []widget.TerminalMode
	f := startTerm(t, 20, 4,
		widget.WithCommand("/bin/prog", "-x", "y"), widget.WithDir("/work"),
		widget.WithEnv("A=1", "B=2"), widget.WithScrollback(2),
		widget.WithOnMode(func(m widget.TerminalMode) { modes = append(modes, m) }))
	c := f.cmds[0]
	if c.Path != "/bin/prog" || strings.Join(c.Args, " ") != "-x y" || c.Dir != "/work" {
		t.Errorf("cmd = %+v", c)
	}
	env := strings.Join(c.Env, "\n")
	if !strings.Contains(env, "\nA=1\n") || !strings.Contains(env, "\nB=2\n") {
		t.Error("WithEnv variables missing")
	}
	for i := 0; i < 10; i++ {
		f.output("x\r\n")
	}
	var sb int
	f.h.onLoop(func() { sb = f.term.Screen().Scrollback() })
	if sb != 2 {
		t.Errorf("scrollback %d, want WithScrollback's 2", sb)
	}
	f.h.inject(ctrl('\\'), ctrl('n'), key('i'))
	f.h.waitFor("modes", func() bool {
		var n int
		f.h.onLoop(func() { n = len(modes) })
		return n == 2
	})
	if modes[0].String() != "NORMAL" || modes[1].String() != "TERMINAL" {
		t.Errorf("modes %v", modes)
	}
}

func TestTerminalStartBeforeMountAndFailure(t *testing.T) {
	term := widget.NewTerminal(widget.WithStarter(func(pty.Cmd) (widget.TerminalProcess, error) {
		return nil, io.ErrClosedPipe
	}))
	if err := term.Start(); err != nil {
		t.Fatalf("Start before mount = %v, want it deferred", err)
	}
	h := startApp(t, term, 40, 3)
	defer h.stop()
	h.waitFor("failure shown", func() bool { return strings.Contains(h.grid(), "io: read/write on closed pipe") })
	var err error
	h.onLoop(func() { err = term.Start() })
	if err == nil {
		t.Error("a failing start returned nil")
	}
}

func TestTerminalAttributesAndCursor(t *testing.T) {
	f := startTerm(t, 20, 3)
	f.output("\x1b[1;2;3;4;5;7;9mA\x1b[0m\x1b[8mB\x1b[0m\x1b[44mC")
	f.h.waitFor("painted", func() bool { return strings.HasPrefix(f.h.row(0), "A C") })
	a := cellAttrs(f.h, 0, 0)
	for _, want := range []tui.AttrMask{tui.AttrBold, tui.AttrFaint, tui.AttrItalic, tui.AttrUnderline, tui.AttrBlink, tui.AttrReverse, tui.AttrStrikethrough} {
		if a.Mask&want == 0 {
			t.Errorf("attribute %b missing from %b", want, a.Mask)
		}
	}
	if bg := cellAttrs(f.h, 2, 0).BG; bg.Kind != tui.CellColorANSI || bg.Index != 4 {
		t.Errorf("blue background = %+v", bg)
	}
	for seq, want := range map[string]tui.CursorShape{
		"\x1b[2 q": tui.CursorShapeBlock, "\x1b[4 q": tui.CursorShapeUnderline,
		"\x1b[6 q": tui.CursorShapeBar, "\x1b[0 q": tui.CursorShapeDefault,
	} {
		f.output(seq)
		var got tui.CursorShape
		f.h.onLoop(func() { got = f.term.CursorShape() })
		if got != want {
			t.Errorf("%q: shape %v, want %v", seq, got, want)
		}
	}
	f.output("\x1b[?25l")
	var ok bool
	f.h.onLoop(func() { _, _, ok = f.term.Cursor() })
	if ok {
		t.Error("cursor shown after DECTCEM off")
	}
	f.h.inject(ctrl('\\'), ctrl('n'))
	f.h.waitFor("normal", func() bool { return f.mode() == widget.TerminalNormal })
	var shape tui.CursorShape
	f.h.onLoop(func() { shape = f.term.CursorShape() })
	if shape != tui.CursorShapeBlock {
		t.Errorf("normal-mode shape %v", shape)
	}
}

func TestTerminalNormalModeMotions(t *testing.T) {
	f := startTerm(t, 20, 4, widget.WithVimKeys(true), widget.WithScrollback(50))
	for i := 0; i < 20; i++ {
		f.output("l" + strings.Repeat("x", i%5) + " word\r\n")
	}
	f.output("$ ")
	f.h.inject(key(tui.KeyEscape))
	f.h.waitFor("normal", func() bool { return f.mode() == widget.TerminalNormal })
	cur := func() (int, int) {
		x, y, _ := f.h.tb.CursorPos()
		return x, y
	}
	f.h.inject(key('k'), key('$'))
	f.h.waitFor("$", func() bool { x, y := cur(); return y == 2 && x == 9 })
	f.h.inject(key('b'))
	f.h.waitFor("b", func() bool { x, _ := cur(); return x == 6 })
	f.h.inject(key('b'), key('h'), key('l'), key('l'))
	f.h.waitFor("b h l l", func() bool { x, _ := cur(); return x == 2 })
	f.h.inject(key('0'), key(tui.KeyEnd), key(tui.KeyHome), key(tui.KeyRight), key(tui.KeyLeft), key(tui.KeyDown), key(tui.KeyUp))
	f.h.waitFor("arrows", func() bool { x, y := cur(); return x == 0 && y == 2 })
	f.h.inject(key('g'), key('g'))
	f.h.waitFor("gg", func() bool { _, y := cur(); return y == 0 && strings.HasPrefix(f.h.row(0), "l word") })
	f.h.inject(ctrl('f'), ctrl('d'), ctrl('b'), ctrl('u'), key(tui.KeyPageDown), key(tui.KeyPageUp))
	f.h.waitFor("pages", func() bool { return strings.HasPrefix(f.h.row(0), "l word") })
	f.h.inject(key('G'))
	f.h.waitFor("G", func() bool { _, y := cur(); return y == 3 })
	f.h.inject(key('/'), key('w'), key('x'), key(tui.KeyBackspace), key('o'), key(tui.KeyEnter), key('N'))
	f.h.inject(key('/'), key(tui.KeyBackspace), key('?'), key('q'), key(tui.KeyEscape), key('g'), key(tui.KeyEscape))
	f.h.inject(key('v'), key('V'), key('V'), key(tui.KeyEscape), key('y'), ctrl('z'))
	f.h.barrier(f.sh)
	if f.mode() != widget.TerminalNormal {
		t.Error("left normal mode")
	}
}

// Output while scrolled back keeps the view on the same text, even as the
// oldest lines leave a full scrollback.
func TestTerminalNormalModeFollowsDroppedLines(t *testing.T) {
	f := startTerm(t, 20, 3, widget.WithVimKeys(true), widget.WithScrollback(6))
	for i := 0; i < 9; i++ {
		f.output(fmt.Sprintf("line %d\r\n", i))
	}
	// The scrollback is full: the next lines drop the oldest.
	f.h.inject(key(tui.KeyEscape))
	f.h.waitFor("normal", func() bool { return f.mode() == widget.TerminalNormal })
	f.h.inject(key('?'))
	f.h.inject(typeString("line 4")...)
	f.h.inject(key(tui.KeyEnter))
	f.h.waitFor("cursor on line 4", func() bool {
		_, y, _ := f.h.tb.CursorPos()
		return y >= 0 && strings.HasPrefix(f.h.row(y), "line 4")
	})
	_, y, _ := f.h.tb.CursorPos()
	f.output("more a\r\nmore b\r\n") // two lines leave the scrollback
	f.h.waitFor("view kept", func() bool {
		_, y2, _ := f.h.tb.CursorPos()
		return y2 == y && strings.HasPrefix(f.h.row(y2), "line 4")
	})
}

func TestTerminalCommandAndDirApplyAtTheNextStart(t *testing.T) {
	f := startTerm(t, 20, 3, widget.WithCommand("/bin/first"), widget.WithDir("/one"))
	f.h.onLoop(func() {
		f.term.SetCommand("/bin/second", "-l")
		f.term.SetDir("/two")
	})
	if c := f.cmds[0]; c.Path != "/bin/first" || c.Dir != "/one" {
		t.Errorf("first start: %+v", c)
	}
	f.proc().exit(0)
	f.h.waitFor("exit", func() bool { return strings.Contains(f.h.grid(), "[process exited 0]") })
	f.h.inject(key(tui.KeyEnter))
	f.h.waitFor("restart", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.cmds) == 2
	})
	if c := f.cmds[1]; c.Path != "/bin/second" || strings.Join(c.Args, " ") != "-l" || c.Dir != "/two" {
		t.Errorf("second start: %+v", c)
	}
}

// A drawer that hides the Terminal unmounts it: the program keeps running,
// its output keeps reaching the screen, a remount shows it, and the App's
// end hangs it up.
func TestTerminalOutlivesItsMount(t *testing.T) {
	f := startTerm(t, 20, 4)
	f.output("before\r\n")
	f.h.onLoop(f.sh.unmountChild)
	f.h.settle()
	f.proc().outW.Write([]byte("while hidden\r\n"))
	f.h.waitFor("hidden output taken", func() bool {
		var s string
		f.h.onLoop(func() {
			scr := f.term.Screen()
			for r := 0; r < 4; r++ {
				for c := 0; c < 20; c++ {
					s += scr.Cell(r, c).Content
				}
			}
		})
		return strings.Contains(s, "while hidden")
	})
	if f.proc().isClosed() {
		t.Fatal("hiding hung the program up")
	}
	var running bool
	f.h.onLoop(func() { running = f.term.Running() })
	if !running {
		t.Fatal("not running while hidden")
	}
	f.h.stop()
	f.h.waitFor("hang-up at the App's end", f.proc().isClosed)
}
