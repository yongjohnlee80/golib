// Package decltest tests a QML program in `go test` — so a broken document
// fails the build, not the program in front of its user.
//
//	func TestTheQMLIsSound(t *testing.T) {
//	    decltest.Check(t, programOptions()...)
//	}
//
//	func TestSaveAsksForAName(t *testing.T) {
//	    s := decltest.Run(t, 80, 24, programOptions()...)
//	    s.Keys(t, decltest.Ctrl('s'))
//	    s.WaitForText(t, "Save As")
//	}
//
// [Check] judges every document the program can load, the ones its layout
// never reads included; [Run] runs the program on a test backend and stops it
// when the test ends. Both take the options [tuidecl.NewProgram] takes, so a
// program's own options function is all a test needs.
package decltest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// Check fails t for every problem [tuidecl.Check] finds, one error each — so
// the test output lists every broken file, not the first.
func Check(t testing.TB, opts ...tuidecl.ProgramOption) {
	t.Helper()
	err := tuidecl.Check(opts...)
	if err == nil {
		return
	}
	var joined interface{ Unwrap() []error }
	if errors.As(err, &joined) {
		for _, e := range joined.Unwrap() {
			t.Errorf("%v", e)
		}
		return
	}
	t.Errorf("%v", err)
}

// Screen is a program running on a test backend.
type Screen struct {
	Program *tuidecl.Program
	Backend *tui.TestBackend
	quit    chan struct{}
}

// WaitTimeout bounds every wait in this package: for text to appear, and for
// the program to stop when the test ends.
var WaitTimeout = 3 * time.Second

// Run builds the program on a width × height test backend and runs it until
// the test ends. A handler error fails the test, unless the options give an
// ErrorSink of their own; Run's own error fails it too, unless it is the
// cancellation that ended it.
func Run(t testing.TB, width, height int, opts ...tuidecl.ProgramOption) *Screen {
	t.Helper()
	return RunWith(t, width, height, nil, opts...)
}

// RunWith is Run with a setup step between building the program and running
// it — where a program's own main binds its host to the Program (finds the
// widgets it reaches into, loads a file) before the first frame. Setup runs
// before Run, so it may touch widgets directly; its error fails the test.
func RunWith(t testing.TB, width, height int, setup func(*tuidecl.Program) error, opts ...tuidecl.ProgramOption) *Screen {
	t.Helper()
	s := &Screen{Backend: tui.NewTestBackend(width, height), quit: make(chan struct{})}
	all := append([]tuidecl.ProgramOption{
		tuidecl.ErrorSink(func(err error) { t.Errorf("handler error: %v", err) }),
	}, opts...)
	all = append(all, tuidecl.AppOptions(tui.WithBackend(s.Backend), tui.WithMinFrameInterval(0)))
	p, err := tuidecl.NewProgram(all...)
	if err != nil {
		t.Fatalf("the program did not mount: %v", err)
	}
	s.Program = p
	if setup != nil {
		if err := setup(p); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		err := p.Run(ctx)
		close(s.quit)
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(WaitTimeout):
			t.Error("the program did not stop")
		}
	})
	return s
}

// String is the screen as text, one line per row.
func (s *Screen) String() string { return s.Backend.String() }

// Quit is closed when the program stops — by App.quit, say.
func (s *Screen) Quit() <-chan struct{} { return s.quit }

// WaitFor waits until cond holds for the screen, and fails t — naming what
// and showing the screen — if it does not within [WaitTimeout]. Changes reach
// the screen a frame or more after the event that caused them, so a test
// polls; it never reads once.
func (s *Screen) WaitFor(t testing.TB, what string, cond func(screen string) bool) {
	t.Helper()
	deadline := time.Now().Add(WaitTimeout)
	for {
		screen := s.String()
		if cond(screen) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never appeared:\n%s", what, screen)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// WaitForText waits until text is on the screen.
func (s *Screen) WaitForText(t testing.TB, text string) {
	t.Helper()
	s.WaitFor(t, "text "+`"`+text+`"`, func(screen string) bool { return strings.Contains(screen, text) })
}

// Keys delivers events as the terminal would.
func (s *Screen) Keys(t testing.TB, evs ...tui.Event) {
	t.Helper()
	if err := s.Backend.Inject(evs...); err != nil {
		t.Fatalf("inject: %v", err)
	}
}

// Rune is the key event for typing ch.
func Rune(ch rune) tui.KeyEvent {
	return tui.KeyEvent{Kind: tui.KeyPress, Code: ch, Base: ch, Text: string(ch)}
}

// Type is the key events for typing text.
func Type(text string) []tui.Event {
	out := make([]tui.Event, 0, len(text))
	for _, ch := range text {
		out = append(out, Rune(ch))
	}
	return out
}

// Ctrl is ch with Control held: Ctrl('s').
func Ctrl(ch rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: ch, Mods: tui.ModCtrl} }

// Alt is ch with Alt held: Alt('f').
func Alt(ch rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: ch, Mods: tui.ModAlt} }
