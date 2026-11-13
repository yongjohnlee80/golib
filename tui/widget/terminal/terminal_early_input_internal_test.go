package terminal

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/pty"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// typedStarter is a program's spawn held until released, as heldStarter's is, answering with a
// program that records what is written to it.
type typedStarter struct {
	out     chan struct{}
	release chan struct{}
	err     error
	mu      sync.Mutex
	procs   []*typedProc
}

func newTypedStarter() *typedStarter {
	return &typedStarter{out: make(chan struct{}, 8), release: make(chan struct{}, 8)}
}

func (s *typedStarter) start(pty.Cmd) (TerminalProcess, error) {
	s.out <- struct{}{}
	<-s.release
	if s.err != nil {
		return nil, s.err
	}
	p := &typedProc{slowProc: newSlowProc()}
	close(p.slowProc.release)
	s.mu.Lock()
	s.procs = append(s.procs, p)
	s.mu.Unlock()
	return p, nil
}

// spawnOut waits for a spawn to be out; free lets one return.
func (s *typedStarter) spawnOut(t *testing.T) {
	t.Helper()
	select {
	case <-s.out:
	case <-time.After(5 * time.Second):
		t.Fatal("no spawn began")
	}
}
func (s *typedStarter) free() { s.release <- struct{}{} }

// written is what program i was sent, waited for until it is want.
func (s *typedStarter) written(t *testing.T, i int, want string) {
	t.Helper()
	got := ""
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		s.mu.Lock()
		if i < len(s.procs) {
			got = s.procs[i].text()
		}
		s.mu.Unlock()
		if got == want {
			return
		}
	}
	t.Fatalf("program %d was sent %q, want %q", i, got, want)
}

// typedProc is a slowProc that records what it is written.
type typedProc struct {
	*slowProc
	mu  sync.Mutex
	buf bytes.Buffer
}

func (p *typedProc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.Write(b)
}

func (p *typedProc) text() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.String()
}

// typedTerminal mounts a Terminal whose spawn s holds, its host keys Ctrl+PgDn.
func typedTerminal(t *testing.T, s *typedStarter) (*Terminal, *widgettest.Harness) {
	t.Helper()
	term := NewTerminal(WithStarter(s.start))
	term.SetHostKeys(func(k tui.KeyEvent) bool { return k.Code == tui.KeyPageDown && k.Mods&tui.ModCtrl != 0 })
	h := widgettest.StartApp(t, term, 30, 4)
	stop := sync.OnceFunc(h.Stop)
	t.Cleanup(func() {
		for range 4 { // a spawn the cell left out returns, so the App can end
			select {
			case s.release <- struct{}{}:
			default:
			}
		}
		stop()
	})
	return term, h
}

// key presses r in the terminal, on the loop, and says whether the terminal took it.
func key(h *widgettest.Harness, term *Terminal, k tui.KeyEvent) bool {
	var took bool
	h.OnLoop(func() { took = term.HandleEvent(k) })
	return took
}

func runeKey(r rune) tui.KeyEvent { return tui.KeyEvent{Code: r, Text: string(r), Kind: tui.KeyPress} }

// TestKeysTypedWhileATerminalStartsReachItsProgram: a terminal opened with its spawn out takes the
// keys typed meanwhile as its program's, never the host's, and its program reads them once it runs,
// in order, before what is typed after. A host key stays the host's.
func TestKeysTypedWhileATerminalStartsReachItsProgram(t *testing.T) {
	s := newTypedStarter()
	term, h := typedTerminal(t, s)
	h.OnLoop(func() { term.StartAsync(nil) })
	s.spawnOut(t)
	for _, r := range "ls" {
		if !key(h, term, runeKey(r)) {
			t.Fatalf("%q typed while the terminal starts went to the host", r)
		}
	}
	if key(h, term, tui.KeyEvent{Code: tui.KeyPageDown, Mods: tui.ModCtrl, Kind: tui.KeyPress}) {
		t.Fatal("a host key typed while the terminal starts was the program's")
	}
	if !key(h, term, tui.KeyEvent{Code: tui.KeyEnter, Kind: tui.KeyPress}) {
		t.Fatal("Enter typed while the terminal starts went to the host")
	}
	s.free()
	s.written(t, 0, "ls\r")
	key(h, term, runeKey('x'))
	s.written(t, 0, "ls\rx")
}

// TestKeysTypedWhileAStartIsCancelledReachNoProgram: the keys typed while a start is out go with
// it when it is cancelled: the next start's program reads only what is typed for it.
func TestKeysTypedWhileAStartIsCancelledReachNoProgram(t *testing.T) {
	s := newTypedStarter()
	term, h := typedTerminal(t, s)
	h.OnLoop(func() { term.StartAsync(nil) })
	s.spawnOut(t)
	key(h, term, runeKey('a'))
	h.OnLoop(func() { term.Stop() }) // cancels the start in flight
	s.free()                         // its late program is closed, never run
	h.OnLoop(func() { term.StartAsync(nil) })
	s.spawnOut(t)
	key(h, term, runeKey('b'))
	s.free()
	s.written(t, 1, "b") // the second start's program: the first, cancelled, never ran
	s.written(t, 0, "")
}

// TestKeysTypedWhileAStartFailsReachNoProgram: a start that fails drops what was typed while it
// was out; a retry's program reads only what is typed for it.
func TestKeysTypedWhileAStartFailsReachNoProgram(t *testing.T) {
	s := newTypedStarter()
	s.err = errors.New("no such program")
	term, h := typedTerminal(t, s)
	h.OnLoop(func() { term.StartAsync(nil) })
	s.spawnOut(t)
	key(h, term, runeKey('a'))
	s.free()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		var exited bool
		h.OnLoop(func() { exited = term.exited })
		if exited {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the failed start never landed")
		}
	}
	s.err = nil
	h.OnLoop(func() { term.StartAsync(nil) })
	s.spawnOut(t)
	key(h, term, runeKey('b'))
	s.free()
	s.written(t, 0, "b")
}

// TestWhatIsTypedWhileStartingIsBounded: what is typed while a start is out is kept up to
// TerminalInputLimit; the excess is dropped, and the screen says so once.
func TestWhatIsTypedWhileStartingIsBounded(t *testing.T) {
	s := newTypedStarter()
	term, h := typedTerminal(t, s)
	h.OnLoop(func() { term.StartAsync(nil) })
	s.spawnOut(t)
	chunk := bytes.Repeat([]byte("a"), TerminalInputLimit/4)
	h.OnLoop(func() {
		for range 6 {
			term.send(chunk)
		}
	})
	var kept int
	var screen bytes.Buffer // its lines run together: the notice wraps on a narrow screen
	h.OnLoop(func() {
		kept = term.earlyLen
		for i := 0; i < term.lineCount(); i++ {
			for _, c := range term.lineCells(i) {
				screen.WriteString(c.Content)
			}
		}
	})
	said := bytes.Count(bytes.ReplaceAll(screen.Bytes(), []byte(" "), nil),
		bytes.ReplaceAll([]byte(ErrTerminalInputFull.Error()), []byte(" "), nil))
	if kept != TerminalInputLimit || said != 1 {
		t.Fatalf("kept %d bytes, said full %d times; want %d kept and one notice", kept, said, TerminalInputLimit)
	}
}

// modedTerminal is typedTerminal with Ctrl+T its mode key.
func modedTerminal(t *testing.T, s *typedStarter) (*Terminal, *widgettest.Harness) {
	t.Helper()
	term, h := typedTerminal(t, s)
	h.OnLoop(func() { term.SetModeKey(func(k tui.KeyEvent) bool { return k.Code == 't' && k.Mods&tui.ModCtrl != 0 }) })
	return term, h
}

// TestTheModeKeyLeavesAStartingTerminalAsARunningOne: while a start is out, the mode key (Ctrl+T)
// takes the Terminal to Normal mode, as it does a running one, so the Esc after it is the host's and
// the program never reads it; the mode key again returns to input, where keys are the program's.
func TestTheModeKeyLeavesAStartingTerminalAsARunningOne(t *testing.T) {
	s := newTypedStarter()
	term, h := modedTerminal(t, s)
	h.OnLoop(func() { term.StartAsync(nil) })
	s.spawnOut(t)
	ctrlT := tui.KeyEvent{Code: 't', Mods: tui.ModCtrl, Kind: tui.KeyPress}
	if !key(h, term, ctrlT) {
		t.Fatal("the mode key did nothing while the terminal starts")
	}
	var mode TerminalMode
	h.OnLoop(func() { mode = term.Mode() })
	if mode != TerminalNormal {
		t.Fatalf("mode %v after the mode key while starting, want Normal", mode)
	}
	if key(h, term, tui.KeyEvent{Code: tui.KeyEscape, Kind: tui.KeyPress}) {
		t.Fatal("Esc in Normal mode while starting was the terminal's, not the host's")
	}
	key(h, term, ctrlT) // back to input
	key(h, term, runeKey('a'))
	s.free()
	s.written(t, 0, "a")
}

// TestAPasteIntoAStartingTerminalReachesItsProgram: a paste while a start is out is the program's,
// as into a running one: kept, and read once it runs.
func TestAPasteIntoAStartingTerminalReachesItsProgram(t *testing.T) {
	s := newTypedStarter()
	term, h := typedTerminal(t, s)
	h.OnLoop(func() { term.StartAsync(nil) })
	s.spawnOut(t)
	var took bool
	h.OnLoop(func() { took = term.HandleEvent(tui.PasteEvent{Text: "pasted"}) })
	if !took {
		t.Fatal("a paste while the terminal starts went to the host")
	}
	s.free()
	s.written(t, 0, "pasted")
}
