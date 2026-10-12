package widget

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/pty"
)

type resultProc struct {
	r                      *io.PipeReader
	w                      *io.PipeWriter
	done, entered, release chan struct{}
	endOnce, enterOnce     sync.Once
	closes                 atomic.Int32
	waits                  atomic.Int32
	closeErr, waitErr      error
	code                   int
}

func newResultProc() *resultProc {
	r, w := io.Pipe()
	return &resultProc{r: r, w: w, done: make(chan struct{}), entered: make(chan struct{}), release: make(chan struct{})}
}
func (p *resultProc) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *resultProc) Write(b []byte) (int, error) { return len(b), nil }
func (*resultProc) Resize(int, int) error         { return nil }
func (p *resultProc) Wait() (int, error) {
	p.waits.Add(1)
	<-p.done
	return p.code, p.waitErr
}
func (p *resultProc) end() { p.endOnce.Do(func() { p.w.Close(); close(p.done) }) }
func (p *resultProc) Close() error {
	p.closes.Add(1)
	p.enterOnce.Do(func() { close(p.entered) })
	<-p.release
	p.end()
	return p.closeErr
}

func awaitResultEvent(t *testing.T, event <-chan struct{}) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(5 * time.Second):
		t.Fatal("terminal lifecycle event did not arrive")
	}
}

func waitTerminalResult(t *testing.T, r *TerminalStopResult) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.Wait(ctx)
}

func TestTerminalStopResultSharesCleanupAndRetainsErrors(t *testing.T) {
	for _, kind := range []string{"success", "close error", "wait error"} {
		t.Run(kind, func(t *testing.T) {
			p := newResultProc()
			p.code = 129 // expected SIGHUP/nonzero status is not a cleanup error
			want := errors.New(kind)
			reapFailure := errors.New("reap failed after Close error")
			if kind == "close error" {
				p.closeErr = want
				p.waitErr = reapFailure
			}
			if kind == "wait error" {
				p.waitErr = want
			}
			term, h, stop := mountTerminal(t, p)
			var r, repeated *TerminalStopResult
			h.onLoopInternal(func() { r = term.StopWithResult(); repeated = term.StopWithResult() })
			if r != repeated || !r.Started() || r.Generation() == 0 {
				t.Fatal("stop lost its immutable generation handle")
			}
			awaitResultEvent(t, p.entered)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if !errors.Is(r.Wait(ctx), context.Canceled) {
				t.Fatal("cancelling a waiter reported successful cleanup")
			}
			select {
			case <-r.Done():
				t.Fatal("waiter cancellation completed cleanup")
			default:
			}
			waiters := make(chan error, 2)
			for range 2 {
				go func() { waiters <- r.Wait(context.Background()) }()
			}
			close(p.release)
			err := waitTerminalResult(t, r)
			if kind == "success" && err != nil {
				t.Fatal(err)
			}
			if kind != "success" && !errors.Is(err, want) {
				t.Fatalf("cleanup error %v, want %v", err, want)
			}
			if kind == "close error" {
				if !errors.Is(err, reapFailure) {
					t.Fatal("Close failure hid the independent Wait failure")
				}
				pumpsGone(t, term)
				if p.waits.Load() != 2 {
					t.Fatalf("output and cleanup called Wait %d times, want both owners to observe reap", p.waits.Load())
				}
			}
			for range 2 {
				select {
				case observed := <-waiters:
					if !errors.Is(observed, err) {
						t.Fatal("concurrent waiters observed different cleanup results")
					}
				case <-time.After(5 * time.Second):
					t.Fatal("a concurrent cleanup waiter was left pending")
				}
			}
			if again := r.Wait(context.Background()); !errors.Is(again, err) {
				t.Fatal("completed handle changed its error")
			}
			stop()
			shutdown := term.ShutdownWithResult()
			ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel2()
			if err2 := shutdown.Wait(ctx2); !errors.Is(err2, err) {
				t.Fatalf("shutdown hid cleanup error: %v", err2)
			}
			if p.closes.Load() != 1 {
				t.Fatalf("cleanup had %d owners", p.closes.Load())
			}
		})
	}
}

func TestTerminalNaturalExitAndAppEndShareTheStartHandle(t *testing.T) {
	p := newResultProc()
	p.code = 7
	term, h, stop := mountTerminal(t, p)
	var r *TerminalStopResult
	h.onLoopInternal(func() { r = term.CleanupResult() })
	p.end() // natural exit requests cleanup before posting the UI notification
	awaitResultEvent(t, p.entered)
	stop() // race shutdown against an already-closing natural exit
	shutdown := term.ShutdownWithResult()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(shutdown.Wait(ctx), context.Canceled) {
		t.Fatal("shutdown timeout/cancellation claimed completion")
	}
	close(p.release)
	if err := waitTerminalResult(t, r); err != nil {
		t.Fatal(err)
	}
	if err := shutdown.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if term.CleanupResult() != r || p.closes.Load() != 1 {
		t.Fatal("exit/shutdown replaced the result or closed twice")
	}
}

func TestTerminalStopCancelsPremountStartAndDistinguishesFailedStart(t *testing.T) {
	var starts atomic.Int32
	failure := errors.New("start refused")
	term := NewTerminal(WithStarter(func(pty.Cmd) (TerminalProcess, error) { starts.Add(1); return nil, failure }))
	if err := term.Start(); err != nil {
		t.Fatal(err)
	} // pending before mount
	r := term.StopWithResult()
	if r.Started() || r.StartError() != nil || r.Generation() != 0 || waitTerminalResult(t, r) != nil {
		t.Fatal("never-started result is not explicit")
	}
	h := startAppInternal(t, term, 20, 4)
	stop := sync.OnceFunc(h.stopInternal)
	defer stop()
	h.syncInternal()
	if starts.Load() != 0 {
		t.Fatal("mount revived a cancelled pending Start")
	}
	h.onLoopInternal(func() {
		if err := term.Start(); !errors.Is(err, failure) {
			t.Errorf("start failure %v", err)
		}
		r = term.StopWithResult()
	})
	if r.Started() || !errors.Is(r.StartError(), failure) || waitTerminalResult(t, r) != nil {
		t.Fatal("failed Start confused cleanup with creation")
	}
}

func TestTerminalRestartDoesNotReplaceOldCleanupAndShutdownJoinsBoth(t *testing.T) {
	first, second := newResultProc(), newResultProc()
	var starts int
	term := NewTerminal(WithStarter(func(pty.Cmd) (TerminalProcess, error) {
		starts++
		if starts == 1 {
			return first, nil
		}
		return second, nil
	}))
	h := startAppInternal(t, term, 20, 4)
	stop := sync.OnceFunc(h.stopInternal)
	defer stop()
	var old, current *TerminalStopResult
	h.onLoopInternal(func() {
		if err := term.Start(); err != nil {
			t.Error(err)
		}
		old = term.StopWithResult()
	})
	awaitResultEvent(t, first.entered)
	h.onLoopInternal(func() {
		if err := term.Start(); err != nil {
			t.Error(err)
		}
		current = term.CleanupResult()
	})
	if current == old || current.Generation() == old.Generation() {
		t.Fatal("restart reused an old generation")
	}
	close(first.release)
	if err := waitTerminalResult(t, old); err != nil {
		t.Fatal(err)
	}
	h.syncInternal()
	h.onLoopInternal(func() {
		if !term.Running() || term.CleanupResult() != current {
			t.Error("old completion stopped the new generation")
		}
	})
	stop()
	shutdown := term.ShutdownWithResult()
	awaitResultEvent(t, second.entered)
	close(second.release)
	if err := shutdown.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if first.closes.Load() != 1 || second.closes.Load() != 1 {
		t.Fatal("shutdown did not converge on each generation's cleanup owner")
	}
	if err := term.Start(); !errors.Is(err, ErrTerminalShutdown) {
		t.Fatalf("destroyed owner restarted: %v", err)
	}
}

func TestTerminalShutdownRequestsNewCleanupBeforeOldCloseFinishes(t *testing.T) {
	first, second := newResultProc(), newResultProc()
	releaseFirst := sync.OnceFunc(func() { close(first.release) })
	releaseSecond := sync.OnceFunc(func() { close(second.release) })
	defer releaseFirst()
	defer releaseSecond()
	var starts int
	term := NewTerminal(WithStarter(func(pty.Cmd) (TerminalProcess, error) {
		starts++
		if starts == 1 {
			return first, nil
		}
		return second, nil
	}))
	h := startAppInternal(t, term, 20, 4)
	stop := sync.OnceFunc(h.stopInternal)
	defer stop()
	h.onLoopInternal(func() {
		if err := term.Start(); err != nil {
			t.Error(err)
		}
		term.StopWithResult()
	})
	awaitResultEvent(t, first.entered)
	h.onLoopInternal(func() {
		if err := term.Start(); err != nil {
			t.Error(err)
		}
	})
	stop()
	shutdown := term.ShutdownWithResult()
	awaitResultEvent(t, second.entered) // the first Close is deliberately still blocked
	releaseSecond()
	select {
	case <-shutdown.Done():
		t.Fatal("shutdown ignored the old closing generation")
	default:
	}
	releaseFirst()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if first.closes.Load() != 1 || second.closes.Load() != 1 {
		t.Fatal("cleanup requests did not retain one owner per generation")
	}
}
