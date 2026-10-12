package widget

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/yongjohnlee80/golib/errs"
)

// ErrTerminalShutdown means this Terminal's owner has begun final destruction.
var ErrTerminalShutdown = errs.Sentinel(errs.ErrPrecondition, "terminal owner is shutting down")

// terminalCompletion publishes an immutable result before closing done. A
// waiter may cancel its own wait without cancelling process cleanup.
type terminalCompletion struct {
	done chan struct{}
	err  error
}

func (r *terminalCompletion) wait(ctx context.Context) error {
	select {
	case <-r.done:
		return r.err
	default:
	}
	select {
	case <-r.done:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TerminalStopResult is one process generation's cleanup result. Stop, natural
// exit and application shutdown share this handle and one cleanup owner.
// Successful cleanup includes Close and Wait (reap); an expected nonzero exit
// status is not a cleanup error. Methods are safe for concurrent waiters.
type TerminalStopResult struct {
	completion terminalCompletion
	generation int
	started    bool
	startErr   error
	proc       TerminalProcess
	input      *inputQueue
	bridge     *bufWriter
	once       sync.Once
}

func newTerminalStopResult(proc TerminalProcess, generation int, input *inputQueue, bridge *bufWriter) *TerminalStopResult {
	return &TerminalStopResult{completion: terminalCompletion{done: make(chan struct{})},
		proc: proc, generation: generation, started: true, input: input, bridge: bridge}
}

func inactiveTerminalStopResult(startErr error) *TerminalStopResult {
	r := &TerminalStopResult{completion: terminalCompletion{done: make(chan struct{})}, startErr: startErr}
	close(r.completion.done) // no process was created, so no cleanup is pending
	return r
}

// Generation is the immutable successful-start generation, or zero if none started.
func (r *TerminalStopResult) Generation() int { return r.generation }

// Started distinguishes a real process from a never-started or failed-start handle.
func (r *TerminalStopResult) Started() bool { return r.started }

// StartError is the failed Start's error; nil for never-started and successful processes.
// It is separate from cleanup errors because a failed Start created no process to reap.
func (r *TerminalStopResult) StartError() error { return r.startErr }

// Done closes after cleanup's result is published, including a cleanup failure.
// A closed Done alone does not establish success: inspect Wait's error.
func (r *TerminalStopResult) Done() <-chan struct{} { return r.completion.done }

// Wait waits for cleanup off the UI loop. Cancellation affects only this waiter;
// the same handle can be awaited again, and cleanup continues independently.
func (r *TerminalStopResult) Wait(ctx context.Context) error { return r.completion.wait(ctx) }

func (r *TerminalStopResult) cleanup(drained <-chan struct{}) {
	r.once.Do(func() {
		go func() {
			if drained != nil {
				// NOTE: queued input may finish before hang-up, but a program that
				// stopped reading cannot delay cleanup beyond the existing one-second grace.
				timer := time.NewTimer(time.Second)
				select {
				case <-drained:
				case <-timer.C:
				}
				timer.Stop()
			}
			err := r.proc.Close()
			if err == nil {
				_, err = r.proc.Wait() // status is expected; only a reap/wait error is failure
			}
			r.completion.err = err
			close(r.completion.done)
		}()
	})
}

// TerminalShutdownResult joins all active, closing and failed-cleanup generations
// owned by a Terminal. It can be awaited after the UI loop has ended; no callback
// is posted to an ended application. Timeout/cancellation never means success.
type TerminalShutdownResult struct{ completion terminalCompletion }

func (r *TerminalShutdownResult) Done() <-chan struct{}          { return r.completion.done }
func (r *TerminalShutdownResult) Wait(ctx context.Context) error { return r.completion.wait(ctx) }

func joinTerminalCleanup(generations []*TerminalStopResult) *TerminalShutdownResult {
	r := &TerminalShutdownResult{completion: terminalCompletion{done: make(chan struct{})}}
	go func() {
		var failures []error
		for _, generation := range generations {
			// These immutable per-generation references are safe after App.Done,
			// including while decl destroys the Terminal on its own goroutine.
			generation.input.close()
			generation.bridge.close()
			generation.cleanup(nil)
			if err := generation.Wait(context.Background()); err != nil {
				failures = append(failures, err)
			}
		}
		r.completion.err = errors.Join(failures...)
		close(r.completion.done)
	}()
	return r
}
