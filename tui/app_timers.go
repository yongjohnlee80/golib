package tui

import (
	"container/heap"
	"time"
)

// Demand-Scheduled Timer Heap Architecture
//
// The TUI runtime manages all timing concerns (component one-shot timers,
// periodic intervals, and frame throttle deadlines) using a single,
// demand-armed min-heap owned exclusively by the main loop goroutine.
//
//               ┌────────────────────────────────────────────────────────┐
//               │ Loop Goroutine Heap: []*timerEntry                     │
//               │   - Component After (one-shot: d > 0, every = 0)       │
//               │   - Component Every (periodic: every > 0, fixed-delay) │
//               │   - Frame Throttle  (frame = true, min-frame-interval) │
//               └──────────────────────────┬─────────────────────────────┘
//                                          │
//                                          ▼
//                                 App.rearmTimer()
//                                          │
//                    ┌─────────────────────┴─────────────────────┐
//                    ▼                                           ▼
//           [Heap has deadlines]                         [Heap is empty]
//                    │                                           │
//     Point single time.Timer at earliest             Disarm time.Timer completely
//     deadline: min(Until(timers[0].at), 0)           Set timerC = nil
//     Set timerC = a.timer.C                          Loop blocks ONLY on input / tasks:
//                    │                                ZERO wakeups, ZERO CPU, ZERO power
//                    ▼                                           │
//        Loop select on timerC                                   ▼
//                    │                                  [True Idle State]
//                    ▼
//           App.fireDueTimers()
//                    │
//        ┌───────────┴───────────┐
//        ▼                       ▼
//   [Frame Deadline]      [Component Deadline]
//        │                       │
//   framePending = false  deliverAddressed(owner, TickEvent)
//   maybeFrame()                 │
//                         Is Every > 0 and owner alive?
//                           ├── YES ──> Re-arm at Now() + every (fixed-delay)
//                           └── NO  ──> Done / cancelled
//
// Key Architectural Invariants:
//  1. Single time.Timer / Zero-Idle-Cost: No scattered time.AfterFunc or
//     per-component goroutines. When no timers or pending frames exist, the
//     underlying time.Timer is disarmed and timerC is nil. The select blocks
//     strictly on Lane A (input) and Lane B (program queue) with 0% CPU.
//  2. Loop-Goroutine-Only Ownership: The heap is mutated strictly on the loop
//     goroutine. Insertion, removal, and tick firing require no mutexes.
//  3. Direct Addressed Delivery: Component ticks (TickEvent) are addressed
//     directly to the owner node ID and do NOT bubble.
//  4. Automatic Cleanup on Unmount: Context.After and Context.Every register
//     their cancel functions via Context.OnUnmount. When a component unmounts,
//     all its pending timers are purged immediately from the heap.
//  5. Fixed-Delay Scheduling: Periodic timers (Every) reschedule at
//     time.Now().Add(every) upon tick delivery. This prevents burst catch-up
//     or queue explosions if a heavy render frame delays the loop.
//
// Usage Examples:
//
// One-shot timer:
//
//	func (c *MyComponent) Init(ctx *tui.Context) {
//	    c.ctx = ctx
//	    // Dismiss a status banner after 3 seconds:
//	    ctx.After(3 * time.Second)
//	}
//
// Periodic ticker:
//
//	func (c *ClockComponent) Init(ctx *tui.Context) {
//	    c.ctx = ctx
//	    // Re-render clock every second (cancelled automatically on unmount):
//	    ctx.Every(1 * time.Second)
//	}
//
// Handling in HandleEvent:
//
//	func (c *ClockComponent) HandleEvent(ev tui.Event) bool {
//	    if _, ok := ev.(tui.TickEvent); ok {
//	        c.now = time.Now()
//	        c.ctx.MarkDirty()
//	        return true
//	    }
//	    return false
//	}

// timerEntry is one pending deadline in the App's demand-scheduled timer
// heap: a component timer (After/Every) or a pending frame deadline (the
// min-frame-interval cap rides the same timer).
type timerEntry struct {
	at    time.Time
	seq   uint64 // allocation order; heap tie-break and the TimerID
	owner NodeID
	every time.Duration // 0 = one-shot
	frame bool          // a pending-frame deadline, not a component timer

	cancelled bool
	index     int // heap index; -1 once popped
}

// timerHeap is a min-heap over deadlines, tie-broken by allocation order
// for determinism. Loop-goroutine-owned, and so torn down for free: it is
// loop-local state, where scattered time.AfterFunc goroutines would each have
// to be found and stopped.
type timerHeap []*timerEntry

func (h timerHeap) Len() int { return len(h) }
func (h timerHeap) Less(i, j int) bool {
	if h[i].at.Equal(h[j].at) {
		return h[i].seq < h[j].seq
	}
	return h[i].at.Before(h[j].at)
}
func (h timerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *timerHeap) Push(x any) {
	e := x.(*timerEntry)
	e.index = len(*h)
	*h = append(*h, e)
}
func (h *timerHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	e.index = -1
	*h = old[:n-1]
	return e
}

// addTimer registers a deadline for owner (loop goroutine only; reached via
// Context.After and Context.Every).
//
// Parameters:
//   - owner: the NodeID to which the resulting TickEvent will be addressed.
//   - d: duration until the first deadline expires.
//   - every: interval for periodic rescheduling (0 = one-shot timer).
//
// The returned cancel closure is idempotent and safe to call multiple times on
// the loop goroutine. It is automatically registered in Context.OnUnmount so
// that unmounting a component cleanly purges all its pending timers.
func (a *App) addTimer(owner NodeID, d, every time.Duration) (cancel func()) {
	a.nextTimerID++
	e := &timerEntry{
		at:    time.Now().Add(d),
		seq:   a.nextTimerID,
		owner: owner,
		every: every,
	}
	heap.Push(&a.timers, e)
	a.rearmTimer()
	return func() {
		if e.cancelled {
			return
		}
		e.cancelled = true
		if e.index >= 0 {
			heap.Remove(&a.timers, e.index)
			a.rearmTimer()
		}
	}
}

// scheduleFrame pushes a pending-frame deadline for the min-frame-interval
// throttle cap.
//
// A pending frame deadline rides directly inside the App's single timer heap
// alongside component timers. This avoids maintaining a separate ticker or frame
// loop: when the UI is clean and idle, the heap is empty, costing zero wakeups,
// zero memory allocations, and zero CPU cycles.
func (a *App) scheduleFrame(at time.Time) {
	a.nextTimerID++
	heap.Push(&a.timers, &timerEntry{at: at, seq: a.nextTimerID, frame: true})
	a.rearmTimer()
}

// rearmTimer points the App's single time.Timer at the earliest pending
// deadline — or fully disarms it when the heap is empty.
//
// Demand-Armed Lifecycle:
//   - Empty Heap: If no component timers or pending frames exist, any existing
//     timer is stopped, drained, and a.timerC is set to nil. The main loop's
//     select statement then blocks purely on Lane A (terminal input) and
//     Lane B (program events).
//   - Non-Empty Heap: Calculates max(Until(timers[0].at), 0), resets or allocates
//     the single timer, and exposes a.timer.C on a.timerC for the main select loop.
func (a *App) rearmTimer() {
	if len(a.timers) == 0 {
		if a.timer != nil {
			if !a.timer.Stop() {
				select {
				case <-a.timer.C:
				default:
				}
			}
		}
		a.timerC = nil
		return
	}
	d := max(time.Until(a.timers[0].at), 0)
	if a.timer == nil {
		a.timer = time.NewTimer(d)
	} else {
		if !a.timer.Stop() {
			select {
			case <-a.timer.C:
			default:
			}
		}
		a.timer.Reset(d)
	}
	a.timerC = a.timer.C
	a.timerArms++ // instrumentation: lets a test assert the idle app arms nothing
}

// fireDueTimers delivers every due deadline on the loop goroutine:
//   - Frame deadlines: Clear framePending and invoke maybeFrame() to flush
//     the throttled render pass.
//   - Component timers: Post an addressed TickEvent{Owner, Timer, At} directly
//     to the owner node via deliverAddressed (no bubbling).
//   - Periodic timers (Every): Rescheduled at time.Now().Add(every). This provides
//     fixed-delay (not fixed-rate) semantics: if an expensive render frame stalls
//     the loop, it will NOT be followed by a burst of queued catch-up ticks.
func (a *App) fireDueTimers() {
	now := time.Now()
	for len(a.timers) > 0 && !a.timers[0].at.After(now) {
		e := heap.Pop(&a.timers).(*timerEntry)
		if e.cancelled {
			continue
		}
		if e.frame {
			a.framePending = false
			a.maybeFrame()
			continue
		}
		a.deliverAddressed(e.owner, TickEvent{Owner: e.owner, Timer: TimerID(e.seq), At: now})
		// The handler may have cancelled its own timer or unmounted the
		// owner — re-arm only a live registration.
		if e.every > 0 && !e.cancelled {
			if _, alive := a.nodes[e.owner]; alive {
				e.at = time.Now().Add(e.every) // fixed-delay: no burst catch-up
				heap.Push(&a.timers, e)
			}
		}
	}
	a.rearmTimer()
}
