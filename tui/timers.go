package tui

import (
	"container/heap"
	"time"
)

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
// Context.After/Every). every > 0 re-arms after delivery. The returned
// cancel is idempotent and loop-goroutine-only (it is what OnUnmount runs).
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
// cap. A pending frame is just one more deadline in the heap, so the idle
// case costs nothing: no dirt, no deadline, no wakeup.
func (a *App) scheduleFrame(at time.Time) {
	a.nextTimerID++
	heap.Push(&a.timers, &timerEntry{at: at, seq: a.nextTimerID, frame: true})
	a.rearmTimer()
}

// rearmTimer points the App's single time.Timer at the earliest pending
// deadline — or fully disarms it when the heap is empty: an idle app has an
// empty heap and a nil timer channel, so the loop's select blocks on input
// alone; zero wakeups, zero bytes.
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

// fireDueTimers delivers every due deadline (loop goroutine): component
// timers post an addressed TickEvent; frame deadlines release the pending
// frame. Every timers re-arm fixed-delay after delivery.
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
