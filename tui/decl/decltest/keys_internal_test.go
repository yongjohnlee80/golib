package decltest

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// keys_internal_test.go: Keys' and KeysAsync's every way out, on a Screen with no program reading
// its backend, so no fence is ever passed unless a cell says so: the program's end, a held loop's
// timeout, and a backend that refuses the input.

// fatal is a testing.TB whose Fatalf records the message and ends the calling goroutine, as
// testing.T's does.
type fatal struct {
	*testing.T
	mu  sync.Mutex
	msg string
}

func (f *fatal) Fatalf(format string, args ...any) {
	f.mu.Lock()
	f.msg = fmt.Sprintf(format, args...)
	f.mu.Unlock()
	runtime.Goexit()
}

// failureOf runs fn in a goroutine of its own with a fatal and returns what it failed with, "" for
// nothing.
func failureOf(t *testing.T, fn func(tb testing.TB)) string {
	t.Helper()
	f := &fatal{T: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(f)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the helper never returned")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.msg
}

// unread is a Screen whose backend no program reads.
func unread() *Screen {
	return &Screen{Backend: tui.NewTestBackend(10, 2), quit: make(chan struct{})}
}

var tab = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}

// The program stopping before it reaches the input ends the wait: Keys returns, and fails nothing.
func TestKeysReturnsWhenTheProgramStopsFirst(t *testing.T) {
	s := unread()
	f := &fatal{T: t}
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		s.Keys(f, tab)
	}()
	select {
	case <-returned:
		t.Fatalf("Keys returned before the program stopped or dispatched the input (%q)", f.msg)
	case <-time.After(50 * time.Millisecond):
	}
	close(s.quit)
	select {
	case <-returned:
	case <-time.After(WaitTimeout):
		t.Fatal("Keys did not return when the program stopped")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.msg != "" {
		t.Fatalf("Keys failed when the program stopped: %s", f.msg)
	}
}

// A loop that never reaches the input fails the cell after WaitTimeout, naming a held loop.
func TestKeysFailsNamingAHeldLoop(t *testing.T) {
	was := WaitTimeout
	WaitTimeout = 50 * time.Millisecond
	defer func() { WaitTimeout = was }()
	s := unread()
	if msg := failureOf(t, func(tb testing.TB) { s.Keys(tb, tab) }); !strings.Contains(msg, "its loop is held") {
		t.Fatalf("Keys on a loop that never dispatched failed with %q, want it to name a held loop", msg)
	}
}

// A backend that refuses the input fails Keys and KeysAsync at once, with the backend's reason.
func TestKeysFailsWhenTheBackendRefusesTheInput(t *testing.T) {
	s := unread()
	if err := s.Backend.Stop(); err != nil {
		t.Fatal(err)
	}
	for name, keys := range map[string]func(testing.TB, ...tui.Event){"Keys": s.Keys, "KeysAsync": s.KeysAsync} {
		if msg := failureOf(t, func(tb testing.TB) { keys(tb, tab) }); !strings.Contains(msg, "inject: ") || !strings.Contains(msg, "after Stop") {
			t.Errorf("%s on a stopped backend failed with %q, want the inject error", name, msg)
		}
	}
}

// KeysAsync queues the input and returns before anything has read it.
func TestKeysAsyncQueuesAndReturns(t *testing.T) {
	s := unread()
	if msg := failureOf(t, func(tb testing.TB) { s.KeysAsync(tb, tab) }); msg != "" {
		t.Fatalf("KeysAsync failed: %s", msg)
	}
	select {
	case ev := <-s.Backend.Events():
		if ev != tui.Event(tab) {
			t.Fatalf("KeysAsync queued %#v, want the Tab", ev)
		}
	default:
		t.Fatal("KeysAsync queued nothing")
	}
}
