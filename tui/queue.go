package tui

import (
	"fmt"
	"sync"

	"github.com/yongjohnlee80/golib/logger"
)

// programQueueHighWaterStart is the first pending-count threshold that emits
// a high-water log entry; subsequent entries fire at each doubling
// (ADR-0005 §2.4 rev 1: high-water-mark logging is mandatory).
const programQueueHighWaterStart = 64

// programItem is one lane-B entry: a posted Event or a queued closure
// (exactly one is set).
type programItem struct {
	ev Event
	fn func()
}

// programQueue is lane B of the two-lane event queue: program events from
// any goroutine — Post, Update closures, Bus.Publish deliveries,
// TaskResult/TaskProgress. Never blocks, never drops: a mutex-guarded
// growable slice (swap-and-drain on the loop side, so the producer lock is
// O(append)), paired with a capacity-1 wake channel — the close-and-replace
// broadcast idiom of server/registry.go:46-49 reduced to its single-waiter
// form: many producers, one wakeup, zero lost wakeups, zero blocking.
type programQueue struct {
	mu    sync.Mutex
	items []programItem
	spare []programItem // last drained batch, recycled as the next append target

	wake chan struct{} // cap 1; non-blocking send on every push

	limit     int // 0 = unlimited; exceeding panics (WithEventQueueLimit)
	nextHW    int // next pending count that logs a high-water entry
	highWater int // maximum pending count observed (diagnostics)

	log logger.Logger
}

// init prepares the queue. Called once from NewApp.
func (q *programQueue) init(limit int, log logger.Logger) {
	q.wake = make(chan struct{}, 1)
	q.limit = limit
	q.nextHW = programQueueHighWaterStart
	q.log = log
}

// push enqueues one item and wakes the loop. Safe from any goroutine,
// including the loop itself; never blocks. Panics only when the app opted
// into WithEventQueueLimit and the ceiling is exceeded.
func (q *programQueue) push(it programItem) {
	q.mu.Lock()
	q.items = append(q.items, it)
	n := len(q.items)
	if n > q.highWater {
		q.highWater = n
	}
	logHW := false
	if n >= q.nextHW {
		logHW = true
		for n >= q.nextHW {
			q.nextHW *= 2
		}
	}
	overLimit := q.limit > 0 && n > q.limit
	q.mu.Unlock()

	if logHW {
		logger.Warning(q.log, nil, map[string]any{
			"tui": "program event queue high water", "pending": n,
		})
	}
	if overLimit {
		panic(fmt.Sprintf("tui: program event queue exceeded %d — runaway producer", q.limit))
	}
	q.wakeUp()
}

// wakeUp performs the non-blocking capacity-1 wake send.
func (q *programQueue) wakeUp() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// drain swaps the pending batch out and returns it (loop goroutine only).
// It drains ONLY the batch snapshot taken at wake — items enqueued while
// the batch is processed land in the next drain, so publish-during-drain
// cannot livelock the frame. The returned slice is recycled on the drain
// after next; the caller must finish with it before calling drain again
// (single consumer — the loop).
func (q *programQueue) drain() []programItem {
	q.mu.Lock()
	batch := q.items
	q.items = q.spare[:0]
	q.spare = batch
	q.mu.Unlock()
	return batch
}

// pending reports the current queued count (diagnostics/tests).
func (q *programQueue) pending() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}
