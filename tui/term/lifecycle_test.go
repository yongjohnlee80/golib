package term

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

func TestLifecycleCleanStop(t *testing.T) {
	// Events() is closed after Stop returns and never
	// receives a send afterwards (this test runs under -race); Err()
	// returns nil after a clean Stop. Double-Stop is a no-op.
	s := newScript(t)
	s.respond(fullModernReplies)
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Input decodes and is delivered before Stop.
	s.write("a")
	ev, ok := s.waitEvent(2 * time.Second)
	if !ok {
		t.Fatal("no event for typed input")
	}
	if want := (tui.KeyEvent{Code: 'a', Text: "a"}); !reflect.DeepEqual(ev, want) {
		t.Fatalf("got %#v want %#v", ev, want)
	}

	if err := s.b.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Channel closes (drain anything decoded before teardown).
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-s.b.Events():
			if !open {
				goto closed
			}
		case <-deadline:
			t.Fatal("Events() not closed after Stop")
		}
	}
closed:
	if err := s.b.Err(); err != nil {
		t.Fatalf("Err() after clean Stop = %v, want nil", err)
	}
	if err := s.b.Stop(); err != nil {
		t.Fatalf("double Stop = %v, want nil no-op", err)
	}
}

func TestLifecycleTeardownOrder(t *testing.T) {
	// Restore in REVERSE order of acquisition — kitty pop,
	// mode disables, cursor restore, alt-screen leave — as one final write.
	s := newScript(t)
	s.respond(fullModernReplies)
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.w.Reset()
	if err := s.b.Stop(); err != nil {
		t.Fatal(err)
	}
	out := s.w.String()
	order := []string{
		"\x1b[<u", // 1. kitty pop (§5.4: on every Stop path that pushed)
		"\x1b[?1004l",
		"\x1b[?2048l",
		"\x1b[?1006l",
		"\x1b[?1002l",
		"\x1b[?2004l",
		"\x1b[0 q",    // 3. cursor: default shape...
		"\x1b[?25h",   //    ...shown...
		"\x1b[m",      //    ...SGR reset
		"\x1b[?1049l", // 4. leave alternate screen
	}
	last := -1
	for _, seq := range order {
		i := strings.Index(out, seq)
		if i < 0 {
			t.Fatalf("teardown missing %q in %q", seq, out)
		}
		if i < last {
			t.Fatalf("teardown out of order at %q\nout: %q", seq, out)
		}
		last = i
	}
	if s.w.Writes() != 1 {
		t.Fatalf("teardown took %d writes, want one final write", s.w.Writes())
	}
}

func TestLifecycleRestoreOnPanic(t *testing.T) {
	// A panic inside a render callback (the App.Run defer
	// discipline) leaves the scripted terminal restored — kitty popped,
	// modes disabled, cursor shown/reset, alt screen exited — and the panic
	// still propagates. Double-Stop is a no-op.
	s := newScript(t)
	s.respond(fullModernReplies)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		defer s.b.Stop() // the App.Run discipline
		if err := s.start(t.Context()); err != nil {
			t.Error(err)
			return
		}
		panic("render exploded")
	}()

	if recovered != "render exploded" {
		t.Fatalf("panic did not propagate: %v", recovered)
	}
	out := s.w.String()
	for _, seq := range []string{"\x1b[<u", "\x1b[?2004l", "\x1b[?1002l", "\x1b[?25h", "\x1b[0 q", "\x1b[m", "\x1b[?1049l"} {
		if !strings.Contains(out, seq) {
			t.Errorf("restore missing %q after panic", seq)
		}
	}
	if err := s.b.Stop(); err != nil {
		t.Fatalf("double Stop after panic = %v", err)
	}
	if _, open := <-s.b.Events(); open {
		t.Fatal("Events() still open after Stop")
	}
}

func TestLifecycleReaderFailureSetsErr(t *testing.T) {
	// A term fixture whose input pipe fails mid-read closes
	// Events() and Err() returns the terminal error.
	s := newScript(t)
	s.respond("\x1b[?62c")
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("pty went away")
	_ = s.pw.CloseWithError(boom)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-s.b.Events():
			if !open {
				goto closed
			}
		case <-deadline:
			t.Fatal("Events() not closed after reader failure")
		}
	}
closed:
	if err := s.b.Err(); !errors.Is(err, boom) {
		t.Fatalf("Err() = %v, want %v", err, boom)
	}
}

func TestLifecycleStopBeforeStart(t *testing.T) {
	s := newScript(t)
	if err := s.b.Stop(); err != nil {
		t.Fatalf("Stop before Start = %v", err)
	}
	if _, open := <-s.b.Events(); open {
		t.Fatal("Events() open after Stop-before-Start")
	}
	if err := s.b.Start(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Start after Stop = %v, want ErrClosed", err)
	}
}

func TestEscTimeoutDeliversLoneEsc(t *testing.T) {
	// On a non-kitty terminal a lone ESC is held for
	// WithEscTimeout and then delivered as the Escape key.
	s := newScript(t, WithEscTimeout(20*time.Millisecond))
	s.respond("\x1b[?62c") // no kitty support: the timeout path is active
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.write("\x1b")
	ev, ok := s.waitEvent(2 * time.Second)
	if !ok {
		t.Fatal("lone ESC never delivered")
	}
	if want := (tui.KeyEvent{Code: tui.KeyEscape}); !reflect.DeepEqual(ev, want) {
		t.Fatalf("got %#v want %#v", ev, want)
	}
}

func TestEscTimeoutContinuationWins(t *testing.T) {
	// A continuation chunk arriving before the (deliberately huge) timeout
	// resolves the ESC as a sequence — no spurious Escape key.
	s := newScript(t, WithEscTimeout(10*time.Second))
	s.respond("\x1b[?62c")
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.write("\x1b")
	time.Sleep(10 * time.Millisecond)
	s.write("[A")
	ev, ok := s.waitEvent(2 * time.Second)
	if !ok {
		t.Fatal("no event decoded")
	}
	if want := (tui.KeyEvent{Code: tui.KeyUp}); !reflect.DeepEqual(ev, want) {
		t.Fatalf("got %#v want %#v", ev, want)
	}
	// And nothing else pending (no ESC misfire).
	if ev, ok := s.waitEvent(50 * time.Millisecond); ok {
		t.Fatalf("spurious extra event: %#v", ev)
	}
}

func TestEscTimeoutDisabledUnderKitty(t *testing.T) {
	// On kitty terminals the timeout path is disabled
	// entirely — a raw ESC is genuinely a sequence prefix and is held.
	s := newScript(t, WithEscTimeout(20*time.Millisecond))
	s.respond(fullModernReplies) // kitty negotiated + pushed
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !s.b.Capabilities().KittyKeyboard {
		t.Fatal("script should negotiate kitty")
	}
	s.write("\x1b")
	if ev, ok := s.waitEvent(150 * time.Millisecond); ok {
		t.Fatalf("ESC timeout fired under kitty: %#v", ev)
	}
	// The held ESC then completes as a kitty Escape-key sequence.
	s.write("[27u")
	ev, ok := s.waitEvent(2 * time.Second)
	if !ok {
		t.Fatal("completed kitty sequence not decoded")
	}
	if want := (tui.KeyEvent{Code: tui.KeyEscape}); !reflect.DeepEqual(ev, want) {
		t.Fatalf("got %#v want %#v", ev, want)
	}
}

func TestInBandResizeEmitsOrderedEvents(t *testing.T) {
	// Mode-2048 reports become ResizeEvents through the same
	// producer path, ordered and un-coalesced relative to keys.
	s := newScript(t)
	s.respond(fullModernReplies)
	if err := s.start(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.write("a\x1b[48;40;120;800;1920tb")
	want := []tui.Event{
		tui.KeyEvent{Code: 'a', Text: "a"},
		tui.ResizeEvent{W: 120, H: 40},
		tui.KeyEvent{Code: 'b', Text: "b"},
	}
	for i, w := range want {
		ev, ok := s.waitEvent(2 * time.Second)
		if !ok {
			t.Fatalf("event %d never arrived", i)
		}
		if !reflect.DeepEqual(ev, w) {
			t.Fatalf("event %d: got %#v want %#v", i, ev, w)
		}
	}
}

func TestOpenRejectsNonTerminal(t *testing.T) {
	// Open errors with ErrNotTerminal when IsTerminal fails
	// on either fd. Pipes are not terminals, so this is hermetic.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	if _, err := Open(WithTTY(r, w)); !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("Open on pipe = %v, want ErrNotTerminal", err)
	}
	if _, err := Open(WithTTY(nil, nil)); !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("Open on nil ttys = %v, want ErrNotTerminal", err)
	}
}
