package terminal

import (
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/errs"
)

// The Terminal's start off the UI loop (StartAsync): the program's spawn, which on Windows creates a
// pseudo-console as well and can take a good part of a second, runs on a goroutine of its own, and
// its outcome lands back on the loop. Until it does, the terminal says it is starting. A start in
// flight is one only: another start joins it. Stop and Shutdown cancel it, and a program spawned for
// a cancelled start is closed, never adopted, whether the loop is there to hear of it or not.

// ErrTerminalStartCancelled is a start that Stop, Shutdown or the tab's removal overtook.
var ErrTerminalStartCancelled = errs.Sentinel(errs.ErrPrecondition, "terminal start cancelled")

// ErrTerminalStartStillOut is a shutdown that gave up waiting for a start in flight
// (five seconds): its spawn had not returned. The program it spawns is still closed when it does.
var ErrTerminalStartStillOut = errs.Sentinel(errs.ErrTimeout, "terminal start still in flight at shutdown")

// startOutGrace bounds how long a shutdown waits for a start in flight: a spawn that never returns
// (a wedged pseudo-console) must not hold a quit for ever. A variable, so a test can shorten it.
var startOutGrace = 5 * time.Second

// defaultStartingText is what the terminal says while a start is in flight (WithStartingText).
const defaultStartingText = "starting…"

// WithStartingText is what the Terminal says on its screen while StartAsync's start is in flight:
// a host shows it in its own language. "" keeps the default, "starting…".
func WithStartingText(text string) TerminalOption {
	return func(c *terminalConfig) { c.startingText = text }
}

// startFlight is one start in flight: its outcome lands on the loop once, or it is cancelled and
// its program, when the spawn returns one, is closed. state is guarded by mu; the goroutine and the
// loop (or a shutdown off it) race to settle it.
type startFlight struct {
	mu      sync.Mutex
	state   flightState
	cancel  chan struct{} // closed on cancellation
	landed  chan struct{} // closed once the loop has settled the outcome
	done    chan struct{} // closed once the flight is over: adopted, failed, or its program closed
	once    sync.Once
	waiters []func(error)
}

type flightState int

const (
	flightOut       flightState = iota // the spawn has not been settled
	flightLanded                       // its outcome landed on the loop
	flightCancelled                    // cancelled: its program, if any, is closed by whoever sees it
)

func newStartFlight() *startFlight {
	return &startFlight{cancel: make(chan struct{}), landed: make(chan struct{}), done: make(chan struct{})}
}

// cancelOut cancels f unless its outcome has landed, answering whether it did.
func (f *startFlight) cancelOut() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.state != flightOut {
		return false
	}
	f.state = flightCancelled
	close(f.cancel)
	return true
}

func (f *startFlight) finish() { f.once.Do(func() { close(f.done) }) }

// closeLate closes a program spawned for a cancelled start, and reaps it: it is never adopted, so
// nothing else would.
func closeLate(proc TerminalProcess) {
	if proc == nil {
		return
	}
	_ = proc.Close()
	_, _ = proc.Wait()
}

// StartAsync runs the program as Start does, but spawns it off the UI loop, which goes on drawing
// and taking keys meanwhile; the screen says it is starting (WithStartingText). done, when not nil,
// runs on the loop with the start's outcome: nil once the program runs (or when it already does),
// the start's error, ErrTerminalShutdown, or ErrTerminalStartCancelled when Stop or Shutdown
// overtook it. A start in flight is joined, never repeated. Before the mount, the start waits for
// it, as Start's does. Enter or i retry a failed start the same way. On the UI loop.
func (t *Terminal) StartAsync(done func(error)) {
	answer := func(err error) {
		if done != nil {
			done(err)
		}
	}
	t.procMu.Lock()
	shuttingDown := t.shutdown != nil
	t.procMu.Unlock()
	if shuttingDown {
		answer(ErrTerminalShutdown)
		return
	}
	t.async = true
	if t.running {
		answer(nil)
		return
	}
	if f := t.starting; f != nil {
		if done != nil {
			f.waiters = append(f.waiters, done)
		}
		return
	}
	ctx := t.Context()
	if ctx == nil || !t.alive {
		t.pending = true
		if done != nil {
			t.pendingDone = append(t.pendingDone, done)
		}
		return
	}
	f := newStartFlight()
	if done != nil {
		f.waiters = append(f.waiters, done)
	}
	t.starting = f
	t.procMu.Lock()
	t.flights = append(t.flights, f)
	t.procMu.Unlock()
	t.sayStarting()
	cmd, start, app := t.startCmd(), t.cfg.start, ctx.App()
	go func() {
		defer f.finish()
		proc, err := start(cmd)
		app.Update(func() { t.landFlight(f, proc, err) })
		// the loop settles it, unless the flight is cancelled first or the loop ends with it
		// unheard: then this goroutine closes the program itself
		select {
		case <-f.landed:
			return
		case <-f.cancel:
		case <-app.Done():
			f.cancelOut()
		}
		f.mu.Lock()
		cancelled := f.state == flightCancelled
		f.mu.Unlock()
		if cancelled && err == nil {
			closeLate(proc)
		}
	}()
}

// landFlight settles f's outcome on the loop: adopted, or said as a failure (land, which also
// closes a program a shutdown overtook). A flight cancelled meanwhile lands nothing: its goroutine
// closes its program, and its waiters were answered. f.mu is never held with t.procMu, which a
// shutdown takes first.
func (t *Terminal) landFlight(f *startFlight, proc TerminalProcess, err error) {
	f.mu.Lock()
	if f.state != flightOut {
		f.mu.Unlock()
		return
	}
	f.state = flightLanded
	f.mu.Unlock()
	close(f.landed)
	waiters := f.waiters
	f.waiters = nil
	if t.starting == f {
		t.starting = nil
	}
	t.forgetFlight(f)
	if t.Context() == nil { // destroyed meanwhile: nothing to run the program in
		if err == nil {
			closeLate(proc)
		}
		err = ErrTerminalStartCancelled
	} else {
		err = t.land(proc, err)
	}
	for _, w := range waiters {
		w(err)
	}
}

// endFlight forgets f and answers its waiters with err. On the UI loop.
func (t *Terminal) endFlight(f *startFlight, err error) {
	if t.starting == f {
		t.starting = nil
	}
	t.forgetFlight(f)
	for _, w := range f.waiters {
		w(err)
	}
	f.waiters = nil
	t.MarkDirty()
}

// forgetFlight drops f from the flights a shutdown would join.
func (t *Terminal) forgetFlight(f *startFlight) {
	t.procMu.Lock()
	defer t.procMu.Unlock()
	for i, g := range t.flights {
		if g == f {
			t.flights = append(t.flights[:i], t.flights[i+1:]...)
			return
		}
	}
}

// cancelStart cancels the start in flight, if any, answering its waiters ErrTerminalStartCancelled;
// a pending start's waiters are answered too. On the UI loop (Stop).
func (t *Terminal) cancelStart() {
	for _, w := range t.pendingDone {
		w(ErrTerminalStartCancelled)
	}
	t.pendingDone = nil
	f := t.starting
	if f == nil {
		return
	}
	if f.cancelOut() {
		t.endFlight(f, ErrTerminalStartCancelled)
	}
}

// cancelFlightsLocked cancels every start in flight and answers one shutdown result for each: done
// once its spawn has returned and its program is closed, or ErrTerminalStartStillOut after
// startOutGrace. With t.procMu held (ShutdownWithResult, on the loop or off it): waiters are not
// answered, the loop perhaps gone.
func (t *Terminal) cancelFlightsLocked() []*TerminalShutdownResult {
	var out []*TerminalShutdownResult
	for _, f := range t.flights {
		f.cancelOut()
		r := &TerminalShutdownResult{completion: terminalCompletion{done: make(chan struct{})}}
		go func(f *startFlight) {
			timer := time.NewTimer(startOutGrace)
			defer timer.Stop()
			select {
			case <-f.done:
			case <-timer.C:
				r.completion.err = ErrTerminalStartStillOut
			}
			close(r.completion.done)
		}(f)
		out = append(out, r)
	}
	return out
}

// sayStarting writes what a start in flight says on the screen, on a line of its own.
func (t *Terminal) sayStarting() {
	text := t.cfg.startingText
	if text == "" {
		text = defaultStartingText
	}
	if row, col, _, _ := t.scr.Cursor(); row != 0 || col != 0 || t.scr.Scrollback() > 0 {
		t.scr.Write([]byte("\r\n"))
	}
	t.scr.Write([]byte("[" + text + "]\r\n"))
	t.MarkDirty()
}

// retryStart starts the program the way it was last asked: off the loop after a StartAsync, with
// the waiters of a start pending the mount answered once it lands; else Start's way.
func (t *Terminal) retryStart() {
	if !t.async {
		_ = t.Start()
		return
	}
	waiters := t.pendingDone
	t.pendingDone = nil
	t.StartAsync(func(err error) {
		for _, w := range waiters {
			w(err)
		}
	})
}
