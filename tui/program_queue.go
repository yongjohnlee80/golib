package tui

import (
	"fmt"
	"sync"

	"github.com/yongjohnlee80/golib/logger"
)

// programQueueHighWaterStart is the first pending-count threshold that emits
// a high-water log entry; subsequent entries fire at each doubling
// — the lane is unbounded by default, so the log is the only warning that a
// producer is outrunning the loop.
const programQueueHighWaterStart = 64

// programItem is one lane-B entry: a posted Event or a queued closure
// (exactly one is set).
type programItem struct {
	ev Event
	fn func()

	// latest is a coalescing call's slot (App.UpdateLatest, Bus.PublishLatest), set in place of
	// fn: the closure to run is the cell's fn when the batch runs, the last one its key was given.
	latest *latestCell

	// isUpdate marks a closure that came from App.Update, as opposed to one
	// lane B merely carries — a Bus delivery, for instance. The phases in which
	// a component may mutate loop-owned state are named ones, and travelling on
	// the same lane is not the same as being an Update: a Bus subscriber that
	// needs such a mutation has to enqueue an Update of its own.
	isUpdate bool
}

// latestCell is the one queued slot of a pending coalescing key: a later call for the key, while
// the slot waits in the queue, replaces fn and appends nothing.
type latestCell struct{ fn func() }

// programQueue is Lane B of the two-lane event architecture: it carries internal
// program events from any goroutine — App.Post events, App.Update closures,
// Bus.Publish deliveries, and async background worker results (TaskResult,
// TaskProgress).
//
// In contrast to Lane A (intake / backend input), Lane B never drops events by default
// and never blocks the caller.
//
// # Architectural Diagram & Program Queue (Lane B)
//
//	  [ Background Worker ]   [ Bus.Publish ]   [ App.Update ]   [ App.Post / Tasks ]
//	            │                   │                 │                  │
//	            ▼                   ▼                 ▼                  ▼
//	+─────────────────────────────────────────────────────────────────────────────+
//	│                    App.programQueue.push(programItem)                       │
//	│                                                                             │
//	│  [Short Critical Section: q.mu.Lock()]                                      │
//	│    1. Append item to q.items (O(1) amortized, memory-bounded lock)          │
//	│    2. Update high-water watermark (diagnostics)                             │
//	│    3. Exponential threshold log warning (64, 128, 256, 512...)             │
//	│    4. Enforce optional ceiling (WithEventQueueLimit: fail-loud panic)       │
//	│  [Unlock: q.mu.Unlock()]                                                    │
//	│                                                                             │
//	│  [Non-Blocking Wakeup Trigger]                                              │
//	│    select { case q.wake <- struct{}{}: default: }                           │
//	+─────────────────────────────────────────────────────────────────────────────+
//	                    │                                        │
//	    (Wake Token)    │                                        │ (Enqueued Items)
//	                    ▼                                        ▼
//	+──────────────────────────────────────+     +────────────────────────────────+
//	│         q.wake (chan cap 1)          │     │     Double-Buffered Slices     │
//	│  - Capacity-1 coalesce channel       │     │  - q.items: active ingest      │
//	│  - Non-blocking send: never blocks   │     │  - q.spare: recycled buffer    │
//	│  - Zero lost wakeups                 │     │  (Zero-alloc slice swap)       │
//	+──────────────────────────────────────+     +────────────────────────────────+
//	                    │                                        │
//	                    │ Awakens Event Loop                     │ Atomic Swap at Drain
//	                    ▼                                        ▼
//	+─────────────────────────────────────────────────────────────────────────────+
//	│                       App.loop()    Single Consumer                         │
//	│                                                                             │
//	│  1. Receives <-q.wake notification                                          │
//	│  2. batch := q.drain()                                                      │
//	│     - Swaps q.items <-> q.spare[:0] under lock                              │
//	│     - Unlocks immediately; processing occurs without holding q.mu           │
//	│  3. Iterates and executes batch sequentially on loop goroutine:             │
//	│     - item.fn() -> Update closure or Bus typed delivery                     │
//	│     - item.ev   -> Post event or TaskResult delivered to node HandleEvent   │
//	│  4. Livelock Immunity: work enqueued during batch drain lands in the new    │
//	│     q.items and is processed in a subsequent frame.                         │
//	+─────────────────────────────────────────────────────────────────────────────+
//
// # Key Architectural Invariants & Mechanics
//
//  1. Multi-Producer, Single-Consumer (MPSC) Non-Blocking Contract:
//     Producers on any goroutine call push() without blocking. The mutex is held
//     only for the duration of a slice append. It never blocks on channel sends or
//     component execution, eliminating self-deadlocks (such as tview's QueueUpdate).
//
//  2. Zero-Allocation Double-Buffering (Swap-and-Drain):
//     drain() swaps q.items with q.spare (reset via [:0]). The loop processes the
//     swapped batch while incoming pushes populate the new slice. The backing arrays
//     are continuously recycled, yielding zero heap allocations under steady state.
//
//  3. Livelock & Starvation Prevention:
//     drain() captures only the snapshot of items present at the instant of drain.
//     Any item pushed as a side effect of running a handler lands in the next drain
//     cycle. This prevents recursive publishes from starving frame rendering or
//     monopolizing the loop goroutine.
//
//  4. Capacity-1 Coalesced Wakeup:
//     q.wake is a buffered channel of capacity 1. A non-blocking send ensures that
//     n concurrent pushes coalesce into a single wake trigger if the loop has not
//     yet drained, preventing queue wake overflow while guaranteeing that every
//     pending item is drained.
//
//  5. Runaway Producer Detection:
//     Unbounded by default to allow high-throughput telemetry, but logs high-water
//     warnings at exponential doublings starting at 64 items. If configured via
//     WithEventQueueLimit, exceeding the ceiling triggers an immediate fail-loud
//     panic to catch runaway goroutines before system memory is exhausted.
//
//  6. Latest-Wins Keys (App.UpdateLatest, Bus.PublishLatest):
//     A coalescing call for a key that already has a slot waiting in q.items
//     replaces that slot's closure and appends nothing, so a producer of newest
//     values holds one slot per key however fast it calls. The slot keeps the
//     first call's place in the order. drain clears the key map with the swap: a
//     call after it, a handler's in the batch included, takes a slot in the next
//     batch (invariant 3 holds, and a key runs at most once per batch).
type programQueue struct {
	mu    sync.Mutex
	items []programItem
	spare []programItem // last drained batch, recycled as the next append target

	wake chan struct{} // cap 1; non-blocking send on every push

	latest map[any]*latestCell // the coalescing keys with a slot waiting in items

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
	n, logHW, overLimit := q.appendLocked(it)
	q.mu.Unlock()
	q.pushed(n, logHW, overLimit)
}

// pushLatest enqueues fn under key, or, while key has a slot waiting, makes fn that slot's
// closure and appends nothing: a replacing call adds no item, so it counts toward neither the
// high-water log nor WithEventQueueLimit. key is the caller's, already checked comparable and
// namespaced. Safe from any goroutine; never blocks.
func (q *programQueue) pushLatest(key any, fn func(), isUpdate bool) {
	q.mu.Lock()
	if c, ok := q.latest[key]; ok {
		c.fn = fn // the slot is not drained yet: the loop reads fn only after the drain that takes it
		q.mu.Unlock()
		return // its push already woke the loop
	}
	if q.latest == nil {
		q.latest = make(map[any]*latestCell)
	}
	c := &latestCell{fn: fn}
	q.latest[key] = c
	n, logHW, overLimit := q.appendLocked(programItem{latest: c, isUpdate: isUpdate})
	q.mu.Unlock()
	q.pushed(n, logHW, overLimit)
}

// appendLocked appends it and keeps the watermarks (q.mu held). It reports the pending count, and
// whether it crossed a high-water mark or the ceiling, for pushed to act on outside the lock.
func (q *programQueue) appendLocked(it programItem) (n int, logHW, overLimit bool) {
	q.items = append(q.items, it)
	n = len(q.items)
	if n > q.highWater {
		q.highWater = n
	}
	if n >= q.nextHW {
		logHW = true
		for n >= q.nextHW {
			q.nextHW *= 2
		}
	}
	return n, logHW, q.limit > 0 && n > q.limit
}

// pushed logs, enforces the ceiling and wakes the loop after an append, outside the lock.
func (q *programQueue) pushed(n int, logHW, overLimit bool) {
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
	clear(q.latest) // the batch's slots go with it: the next call for a key takes a new one
	q.mu.Unlock()
	return batch
}

// pending reports the current queued count (diagnostics/tests).
func (q *programQueue) pending() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// enqueue posts fn to the program lane as a plain lane-B closure, not an Update: the Bus's
// delivery (busLane).
func (a *App) enqueue(fn func()) { a.queue.push(programItem{fn: fn}) }

// enqueueLatest posts fn under key as a plain lane-B closure, latest wins: Bus.PublishLatest's.
func (a *App) enqueueLatest(key any, fn func()) { a.queue.pushLatest(key, fn, false) }
