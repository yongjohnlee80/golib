package terminal

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/pty"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// heldStarter is a program's spawn held until released: it says when a spawn is out, counts the
// spawns, and answers each with a program whose Close it counts (or with err).
type heldStarter struct {
	out     chan struct{}
	release chan struct{}
	once    sync.Once
	spawns  atomic.Int32
	err     error
	mu      sync.Mutex
	procs   []*countedProc
}

func newHeldStarter() *heldStarter {
	return &heldStarter{out: make(chan struct{}, 8), release: make(chan struct{})}
}

func (s *heldStarter) free() { s.once.Do(func() { close(s.release) }) }

func (s *heldStarter) start(pty.Cmd) (TerminalProcess, error) {
	s.spawns.Add(1)
	s.out <- struct{}{}
	<-s.release
	if s.err != nil {
		return nil, s.err
	}
	p := &countedProc{slowProc: newSlowProc()}
	close(p.slowProc.release) // its hang-up is at once
	s.mu.Lock()
	s.procs = append(s.procs, p)
	s.mu.Unlock()
	return p, nil
}

// spawnOut waits for a spawn to be out.
func (s *heldStarter) spawnOut(t *testing.T) {
	t.Helper()
	select {
	case <-s.out:
	case <-time.After(5 * time.Second):
		t.Fatal("no spawn began")
	}
}

// closedOnce waits for the one program spawned to be closed, once.
func (s *heldStarter) closedOnce(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		var n int32 = -1
		if len(s.procs) == 1 {
			n = s.procs[0].closes.Load()
		}
		s.mu.Unlock()
		if n == 1 {
			time.Sleep(50 * time.Millisecond) // and not again
			if c := s.procs[0].closes.Load(); c != 1 {
				t.Fatalf("the late program was closed %d times, want once", c)
			}
			return
		}
		if n > 1 {
			t.Fatalf("the late program was closed %d times, want once", n)
		}
		if time.Now().After(deadline) {
			t.Fatalf("the late program was never closed (closes %d)", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// countedProc is a slowProc whose Close is counted.
type countedProc struct {
	*slowProc
	closes atomic.Int32
}

func (p *countedProc) Close() error {
	p.closes.Add(1)
	return p.slowProc.Close()
}

// asyncTerminal mounts a Terminal whose spawn s holds, with opts.
// stop ends the App, once, whether the cell or its cleanup asks.
func asyncTerminal(t *testing.T, s *heldStarter, opts ...TerminalOption) (term *Terminal, h *widgettest.Harness, stop func()) {
	t.Helper()
	term = NewTerminal(append(opts, WithStarter(s.start))...)
	h = widgettest.StartApp(t, term, 30, 4)
	stop = sync.OnceFunc(h.Stop)
	t.Cleanup(func() { s.free(); stop() })
	return term, h, stop
}

// screenHas says whether the terminal's screen shows text, read on the loop.
func screenHas(h *widgettest.Harness, term *Terminal, text string) bool {
	var has bool
	h.OnLoop(func() {
		for i := 0; i < term.lineCount(); i++ {
			var b strings.Builder
			for _, c := range term.lineCells(i) {
				b.WriteString(c.Content)
			}
			if strings.Contains(b.String(), text) {
				has = true
				return
			}
		}
	})
	return has
}

// answers collects StartAsync's outcomes.
type answers struct{ ch chan error }

func newAnswers() *answers        { return &answers{ch: make(chan error, 8)} }
func (a *answers) done(err error) { a.ch <- err }
func (a *answers) next(t *testing.T) error {
	t.Helper()
	select {
	case err := <-a.ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("StartAsync never answered")
		return nil
	}
}

// TestStartAsyncLeavesTheLoopFree: while the spawn is out, the loop goes on (an Update runs, and
// the screen says it is starting); once released, the program runs and done says nil.
func TestStartAsyncLeavesTheLoopFree(t *testing.T) {
	s := newHeldStarter()
	term, h, _ := asyncTerminal(t, s)
	a := newAnswers()
	h.OnLoop(func() { term.StartAsync(a.done) })
	s.spawnOut(t)
	h.Sync() // the loop runs while the spawn is out: OnLoop would fail after 3 s otherwise
	if !screenHas(h, term, "[starting…]") {
		t.Error("the screen does not say it is starting while the spawn is out")
	}
	var running bool
	h.OnLoop(func() { running = term.Running() })
	if running {
		t.Fatal("running before the spawn returned")
	}
	s.free()
	if err := a.next(t); err != nil {
		t.Fatalf("done: %v, want nil", err)
	}
	h.OnLoop(func() { running = term.Running() })
	if !running {
		t.Fatal("not running once the start landed")
	}
}

// TestStartAsyncSaysTheHostsStartingText: WithStartingText is what a start in flight says.
func TestStartAsyncSaysTheHostsStartingText(t *testing.T) {
	s := newHeldStarter()
	term, h, _ := asyncTerminal(t, s, WithStartingText("démarrage…"))
	h.OnLoop(func() { term.StartAsync(nil) })
	s.spawnOut(t)
	if !screenHas(h, term, "[démarrage…]") {
		t.Error("the host's starting text is not shown")
	}
}

// TestAStartStoppedInFlightClosesItsLateProgram: Stop while the spawn is out answers the start
// cancelled at once; the program the spawn then returns is closed, once, and never runs.
func TestAStartStoppedInFlightClosesItsLateProgram(t *testing.T) {
	s := newHeldStarter()
	term, h, _ := asyncTerminal(t, s)
	a := newAnswers()
	h.OnLoop(func() { term.StartAsync(a.done) })
	s.spawnOut(t)
	h.OnLoop(term.Stop)
	if err := a.next(t); !errors.Is(err, ErrTerminalStartCancelled) {
		t.Fatalf("done after Stop: %v, want ErrTerminalStartCancelled", err)
	}
	s.free()
	s.closedOnce(t)
	h.Sync()
	h.Sync() // the landing, posted after the spawn, has run
	var running bool
	h.OnLoop(func() { running = term.Running() })
	if running {
		t.Fatal("the cancelled start's program was adopted")
	}
}

// TestAStartOvertakenByShutdownClosesItsLateProgram: a shutdown while the spawn is out waits for it
// and closes its program, which is never adopted.
func TestAStartOvertakenByShutdownClosesItsLateProgram(t *testing.T) {
	s := newHeldStarter()
	term, h, _ := asyncTerminal(t, s)
	h.OnLoop(func() { term.StartAsync(nil) })
	s.spawnOut(t)
	r := term.ShutdownWithResult()
	s.free()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Wait(ctx); err != nil {
		t.Fatalf("the shutdown: %v", err)
	}
	s.closedOnce(t)
	h.Sync()
	var running bool
	h.OnLoop(func() { running = term.Running() })
	if running {
		t.Fatal("a start overtaken by the shutdown was adopted")
	}
}

// TestAShutdownGivesUpOnASpawnThatNeverReturns: a shutdown waits for a start in flight only
// its terminal's startGrace, then says a start was still out; the program, when the spawn returns at last, is
// closed all the same.
func TestAShutdownGivesUpOnASpawnThatNeverReturns(t *testing.T) {
	s := newHeldStarter()
	term, h, _ := asyncTerminal(t, s)
	h.OnLoop(func() {
		term.startGrace = 100 * time.Millisecond // this terminal's alone: another test's ends read their own
		term.StartAsync(nil)
	})
	s.spawnOut(t)
	r := term.ShutdownWithResult()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Wait(ctx); !errors.Is(err, ErrTerminalStartStillOut) {
		t.Fatalf("the shutdown over a wedged spawn: %v, want ErrTerminalStartStillOut", err)
	}
	s.free()
	s.closedOnce(t)
}

// TestTheAppEndingClosesALateProgram: the App ending while the spawn is out (its posted landing
// never to run) closes the program the spawn returns.
func TestTheAppEndingClosesALateProgram(t *testing.T) {
	s := newHeldStarter()
	term, h, stop := asyncTerminal(t, s)
	h.OnLoop(func() { term.StartAsync(nil) })
	s.spawnOut(t)
	stop()
	s.free()
	s.closedOnce(t)
}

// TestASecondStartJoinsTheStartInFlight: StartAsync again, or Start, while one is out spawns
// nothing more; every StartAsync is answered when the one start lands.
func TestASecondStartJoinsTheStartInFlight(t *testing.T) {
	s := newHeldStarter()
	term, h, _ := asyncTerminal(t, s)
	a := newAnswers()
	h.OnLoop(func() { term.StartAsync(a.done) })
	s.spawnOut(t)
	h.OnLoop(func() {
		term.StartAsync(a.done)
		if err := term.Start(); err != nil {
			t.Errorf("Start during a start in flight: %v", err)
		}
	})
	s.free()
	for range 2 {
		if err := a.next(t); err != nil {
			t.Fatalf("done: %v", err)
		}
	}
	if n := s.spawns.Load(); n != 1 {
		t.Fatalf("%d spawns, want one", n)
	}
}

// TestAFailedAsyncStartSaysSo: a spawn that fails is said on the screen, as Start's is, and done
// has its error.
func TestAFailedAsyncStartSaysSo(t *testing.T) {
	s := newHeldStarter()
	s.err = errors.New("no such shell")
	term, h, _ := asyncTerminal(t, s)
	a := newAnswers()
	h.OnLoop(func() { term.StartAsync(a.done) })
	s.spawnOut(t)
	s.free()
	if err := a.next(t); err == nil || !strings.Contains(err.Error(), "no such shell") {
		t.Fatalf("done: %v, want the spawn's error", err)
	}
	if !screenHas(h, term, "no such shell") {
		t.Error("the failure is not said on the screen")
	}
}

// TestATabRemovedWhileStartingClosesItsLateProgram: TerminalTabs.StartAsync, the tab removed while
// its spawn is out: done says cancelled, and the late program is closed.
func TestATabRemovedWhileStartingClosesItsLateProgram(t *testing.T) {
	s := newHeldStarter()
	tabs := NewTerminalTabs()
	h := widgettest.StartApp(t, tabs, 40, 6)
	stop := sync.OnceFunc(h.Stop)
	t.Cleanup(func() { s.free(); stop() })
	var id TerminalTabID
	h.OnLoop(func() {
		var err error
		if id, err = tabs.Add("slow", WithStarter(s.start)); err != nil {
			t.Fatal(err)
		}
	})
	a := newAnswers()
	h.OnLoop(func() { tabs.StartAsync(id, a.done) })
	s.spawnOut(t)
	h.OnLoop(func() {
		if _, err := tabs.Remove(id); err != nil {
			t.Fatal(err)
		}
	})
	if err := a.next(t); !errors.Is(err, ErrTerminalStartCancelled) {
		t.Fatalf("done after Remove: %v, want ErrTerminalStartCancelled", err)
	}
	s.free()
	s.closedOnce(t)
}

// waitShutdown waits for r within 5 s, answering its error.
func waitShutdown(t *testing.T, r interface{ Wait(context.Context) error }) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.Wait(ctx)
}

// notDoneYet fails when done closes within 100 ms.
func notDoneYet(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
		t.Fatal(what)
	case <-time.After(100 * time.Millisecond):
	}
}

// slowTab is a TerminalTabs with one tab whose spawn s holds, mounted.
func slowTab(t *testing.T, s *heldStarter) (*TerminalTabs, TerminalTabID, *widgettest.Harness) {
	t.Helper()
	tabs := NewTerminalTabs()
	h := widgettest.StartApp(t, tabs, 40, 6)
	t.Cleanup(func() { s.free(); h.Stop() })
	var id TerminalTabID
	h.OnLoop(func() {
		var err error
		if id, err = tabs.Add("slow", WithStarter(s.start)); err != nil {
			t.Error(err)
		}
	})
	return tabs, id, h
}

// TestARemovalWaitsForItsStartsLateProgram: a tab removed while its start is out is not done until
// the program its spawn returns is closed; then it is, with no error.
func TestARemovalWaitsForItsStartsLateProgram(t *testing.T) {
	s := newHeldStarter()
	tabs, id, h := slowTab(t, s)
	h.OnLoop(func() { tabs.StartAsync(id, nil) })
	s.spawnOut(t)
	var removal *TerminalTabRemovalResult
	h.OnLoop(func() {
		var err error
		if removal, err = tabs.Remove(id); err != nil {
			t.Error(err)
		}
	})
	notDoneYet(t, removal.Done(), "the removal was done while its start's spawn was still out")
	s.free()
	s.closedOnce(t)
	if err := waitShutdown(t, removal); err != nil {
		t.Fatalf("the removal: %v, want done once the late program is closed", err)
	}
}

// TestAShutdownAfterStopJoinsTheCancelledStart: Stop cancels a start in flight; a shutdown after
// it still joins that start, until its program is closed, or for the terminal's grace when the
// spawn never returns.
func TestAShutdownAfterStopJoinsTheCancelledStart(t *testing.T) {
	t.Run("the spawn returns", func(t *testing.T) {
		s := newHeldStarter()
		term, h, _ := asyncTerminal(t, s)
		h.OnLoop(func() { term.StartAsync(nil) })
		s.spawnOut(t)
		var r *TerminalShutdownResult
		h.OnLoop(func() { term.Stop(); r = term.ShutdownWithResult() })
		notDoneYet(t, r.Done(), "the shutdown was done while the stopped start's spawn was still out")
		s.free()
		s.closedOnce(t)
		if err := waitShutdown(t, r); err != nil {
			t.Fatalf("the shutdown: %v, want done once the late program is closed", err)
		}
	})
	t.Run("the spawn never returns", func(t *testing.T) {
		s := newHeldStarter()
		term, h, _ := asyncTerminal(t, s)
		h.OnLoop(func() {
			term.startGrace = 100 * time.Millisecond
			term.StartAsync(nil)
		})
		s.spawnOut(t)
		var r *TerminalShutdownResult
		h.OnLoop(func() { term.Stop(); r = term.ShutdownWithResult() })
		if err := waitShutdown(t, r); !errors.Is(err, ErrTerminalStartStillOut) {
			t.Fatalf("the shutdown over a stopped, wedged start: %v, want ErrTerminalStartStillOut", err)
		}
	})
}

// TestAShutdownOnTheLoopAnswersItsStart: a shutdown on the loop while a start is out answers the
// start's waiter, once, with ErrTerminalStartCancelled, without waiting for the spawn.
func TestAShutdownOnTheLoopAnswersItsStart(t *testing.T) {
	s := newHeldStarter()
	term, h, _ := asyncTerminal(t, s)
	a := newAnswers()
	h.OnLoop(func() { term.StartAsync(a.done) })
	s.spawnOut(t)
	h.OnLoop(func() { term.ShutdownWithResult() })
	if err := a.next(t); !errors.Is(err, ErrTerminalStartCancelled) {
		t.Fatalf("the start overtaken by a shutdown on the loop answered %v, want ErrTerminalStartCancelled", err)
	}
	s.free()
	s.closedOnce(t)
	h.Sync()
	select {
	case err := <-a.ch:
		t.Fatalf("the start answered again: %v", err)
	default:
	}
}

// lateFailingStarter answers its spawn, once released, with p, whose Close fails with err.
func lateFailingStarter(err error) (start func(pty.Cmd) (TerminalProcess, error), entered <-chan struct{}, release func()) {
	p := newResultProc()
	p.closeErr = err
	close(p.release) // Close answers at once
	in, out := make(chan struct{}), make(chan struct{})
	return func(pty.Cmd) (TerminalProcess, error) {
		close(in)
		<-out
		return p, nil
	}, in, sync.OnceFunc(func() { close(out) })
}

// TestALateProgramsCleanupErrorReachesTheShutdown: the program a cancelled start's spawn returns
// is closed, and when its Close fails, the shutdown (or the tab's removal) that joined the start
// says so, rather than done.
func TestALateProgramsCleanupErrorReachesTheShutdown(t *testing.T) {
	want := errors.New("the late program's close failed")
	t.Run("a shutdown", func(t *testing.T) {
		start, entered, release := lateFailingStarter(want)
		term := NewTerminal(WithStarter(start))
		h := widgettest.StartApp(t, term, 40, 6)
		t.Cleanup(func() { release(); h.Stop() })
		h.OnLoop(func() { term.StartAsync(nil) })
		awaitResultEvent(t, entered)
		var r *TerminalShutdownResult
		h.OnLoop(func() { r = term.ShutdownWithResult() })
		release()
		if err := waitShutdown(t, r); !errors.Is(err, want) {
			t.Fatalf("the shutdown: %v, want the late program's cleanup error", err)
		}
	})
	t.Run("a tab's removal", func(t *testing.T) {
		start, entered, release := lateFailingStarter(want)
		tabs := NewTerminalTabs()
		h := widgettest.StartApp(t, tabs, 40, 6)
		t.Cleanup(func() { release(); h.Stop() })
		var id TerminalTabID
		h.OnLoop(func() {
			var err error
			if id, err = tabs.Add("slow", WithStarter(start)); err != nil {
				t.Error(err)
			}
			tabs.StartAsync(id, nil)
		})
		awaitResultEvent(t, entered)
		var removal *TerminalTabRemovalResult
		h.OnLoop(func() {
			var err error
			if removal, err = tabs.Remove(id); err != nil {
				t.Error(err)
			}
		})
		release()
		if err := waitShutdown(t, removal); !errors.Is(err, want) {
			t.Fatalf("the removal: %v, want the late program's cleanup error", err)
		}
	})
}
