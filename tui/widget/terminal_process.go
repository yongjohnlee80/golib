package widget

import (
	"fmt"
	"os"

	"github.com/yongjohnlee80/golib/tui/pty"
)

// The Terminal's process supervision: starting its program (one generation at a time), stopping
// and shutting down, the exit and the output it ingests.

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
