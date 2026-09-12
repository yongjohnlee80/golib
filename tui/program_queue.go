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
}

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
//	│                       App.runLoop() Single Consumer                         │
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
