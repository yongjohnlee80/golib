package widget

import (
	"errors"
	"sync"

	"github.com/yongjohnlee80/golib/tui"
)

// ErrClosed is returned by writes to a Writer whose BufferView has been
// unmounted (or never mounted).
var ErrClosed = errors.New("widget: buffer view closed")

// Streaming data ingestion pipeline and concurrent writer handle for [BufferView].
//
// # Architectural Model: Thread-Safe Ingestion Handle
//
// [BufferView] itself is strictly loop-owned and deliberately does NOT implement [io.Writer],
// which would falsely imply that the widget struct can be mutated concurrently from background
// goroutines. Instead, [BufferView.Writer] returns [bufWriter], a separate, thread-safe handle
// designed for cross-goroutine streaming (e.g. standard output/error from exec.Cmd):
//
//	Background Goroutine (exec.Cmd)           Application Event Loop Goroutine
//	             │                                           │
//	             ▼                                           │
//	     bufWriter.Write(chunk)                              │
//	             │                                           │
//	             ├── 1. Acquire writeMu (order serialization)│
//	             │                                           │
//	             ├── 2. Acquire byte budget (backpressure)   │
//	             │      (blocks if pending > budget)         │
//	             │                                           │
//	             ├── 3. Dispatch app.Update ─────────────────►
//	             │                                           │
//	             │                                     view.ingest(chunk)
//	             │                                           │
//	             │                                     bufWriter.release(n)
//	             ▼                                           │
//	        return (n, nil)                            MarkDirty()
//
// # Architectural Invariants
//
//  1. The Concurrent Handle Seam: The [bufWriter] handle is the sole concurrent entry point into
//     [BufferView]. It serializes writes via writeMu to preserve exact byte sequence order across
//     competing goroutines.
//  2. Bounded Backpressure Semaphore: Pending bytes queued via [tui.App.Update] are bounded by
//     [writerBudget] (default 256 KiB). When the UI event loop lags, [bufWriter.Write] blocks,
//     applying natural backpressure to producing processes without unbounded heap growth.
//  3. Clean Lifecycle Termination: When the owning [BufferView] unmounts, [bufWriter.close] wakes
//     all blocked writer goroutines and immediately returns [ErrClosed] on current and subsequent writes.
//  4. Loop Self-Deadlock Prevention: Loop goroutine code must never write payloads exceeding
//     the pending budget, as a blocked loop cannot execute the app.Update callback needed to drain quota.
//
// # Concurrency Model
//
//   - Handle Safety: thread-safe. All exported methods on [bufWriter] ([io.Writer]) can be called
//     concurrently from any number of background goroutines.
//   - Widget Isolation: The referenced [BufferView] is mutated strictly inside app.Update closures
//     on the application loop goroutine.

// writerBudget bounds pending (loop-unprocessed) bytes per view;
// writerChunk is the enqueue granularity.
const (
	writerBudget = 256 << 10
	writerChunk  = 32 << 10
)

// bufWriter is the separate any-goroutine handle behind BufferView.Writer.
// writeMu serializes whole Write calls (order = acquisition order); mu guards
// the byte budget and closed flag.
type bufWriter struct {
	view *BufferView // touched only inside app.Update closures (loop)

	writeMu sync.Mutex

	mu      sync.Mutex
	cond    *sync.Cond
	app     *tui.App
	pending int
	closed  bool

	// budget is writerBudget for every production writer. It is per-INSTANCE
	// rather than the constant read directly so a test can prove the blocking
	// contract at a small size: the contract is scale-free, but the drain cost
	// is not, and a test forced to fill 256 KiB queues nine-plus 32 KiB chunks
	// that the loop must still render at shutdown. Measured on one CPU under
	// -race: 512 KiB of backlog costs ~731ms to drain and 2 MiB costs ~5.0s,
	// which is what made `TestBufferViewBoundedPending` fail on a loaded CI
	// runner against the harness's fixed 5s shutdown budget while passing
	// everywhere else. A per-instance field keeps that out of package state,
	// so nothing is shared between concurrent apps.
	budget int
}

func newBufWriter(v *BufferView) *bufWriter {
	w := &bufWriter{view: v, closed: true, budget: writerBudget}
	w.cond = sync.NewCond(&w.mu)
	return w
}

// bind opens the handle against the owning App (called from Init, loop
// goroutine).
func (w *bufWriter) bind(app *tui.App) {
	w.mu.Lock()
	w.app = app
	w.closed = false
	w.mu.Unlock()
}

// close marks the handle closed and wakes blocked writers (unmount hook).
func (w *bufWriter) close() {
	w.mu.Lock()
	w.closed = true
	w.cond.Broadcast()
	w.mu.Unlock()
}

// release returns quota after the loop ingests a chunk.
func (w *bufWriter) release(n int) {
	w.mu.Lock()
	w.pending -= n
	w.cond.Broadcast()
	w.mu.Unlock()
}

// Write implements io.Writer per the handle contract: any-goroutine,
// bounded pending bytes (blocks when the loop lags), ordered, ErrClosed
// after unmount.
func (w *bufWriter) Write(p []byte) (int, error) {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	total := 0
	for len(p) > 0 {
		n := min(len(p), writerChunk)
		chunk := append([]byte(nil), p[:n]...)

		w.mu.Lock()
		for !w.closed && w.pending > 0 && w.pending+n > w.budget {
			w.cond.Wait()
		}
		if w.closed {
			w.mu.Unlock()
			return total, ErrClosed
		}
		w.pending += n
		app := w.app
		w.mu.Unlock()

		view := w.view
		app.Update(func() {
			w.release(len(chunk))
			if view.alive {
				view.ingest(chunk)
			}
		})
		total += n
		p = p[n:]
	}
	return total, nil
}

// SetWriterBudgetForTest shrinks the pending-byte budget of v's writer handle.
//
// Call before mounting: the handle reads its budget on the writing goroutine,
// so changing it under a live writer would be a race rather than a fixture.
func SetWriterBudgetForTest(v *BufferView, n int) {
	v.wr.mu.Lock()
	defer v.wr.mu.Unlock()
	v.wr.budget = n
}

// WriterBudgetDefaultForTest is the production pending-byte budget, exposed so
// a test can assert the shipped value rather than restate the number in a
// comment.
const WriterBudgetDefaultForTest = writerBudget

// WriterChunkForTest is the enqueue granularity, exposed so a test can compute
// how many chunks a given write produces instead of hard-coding a count that
// silently stops matching if the granularity changes.
const WriterChunkForTest = writerChunk

// WriterBudgetOfForTest reports the budget v's handle is actually using.
func WriterBudgetOfForTest(v *BufferView) int {
	v.wr.mu.Lock()
	defer v.wr.mu.Unlock()
	return v.wr.budget
}
