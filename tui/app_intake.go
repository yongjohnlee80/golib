package tui

import "github.com/yongjohnlee80/golib/logger"

// intake is the App-owned input stage, Lane A: it pulls promptly from
// backend.Events() and applies all input policies that the backend deliberately
// does not own — bounded queueing (WithInputQueueSize), smart drop-oldest
// overflow, resize latest-wins slot, and motion coalescing — before handing
// events to the main loop over a.input.
//
// # Architectural Diagram & Intake Stage (Lane A)
//
//	               +───────────────────────────────────+
//	               │    Backend.Events() (Raw Stream)   │
//	               +───────────────────────────────────+
//	                                 │
//	                                 │ (Pulls promptly, non-blocking)
//	                                 ▼
//	+─────────────────────────────────────────────────────────────────+
//	│                 App.intake() Stage (Lane A)                     │
//	│                                                                 │
//	│  [Event Classifier]                                             │
//	│         │                                                       │
//	│         ├─► ResizeEvent? ──► [ Atomic Slot: Latest-Wins ]       │
//	│         │                    (Delivered In Its Place)           │
//	│         │                                                       │
//	│         ├─► MouseMotion? ──► [ Consecutive Motion Coalesce ]    │
//	│         │                    (Collapses consecutive moves)      │
//	│         │                               │                       │
//	│         └─► Other (Key/Click/Paste) ────┴────────────────────┐  │
//	│                                                              │  │
//	│  [Bounded FIFO Queue: limit = cfg.inputQueueSize] ◄──────────┘  │
//	│         │                                                       │
//	│         ▼ (If Capacity Exceeded)                                │
//	│  [Smart Drop-Oldest Overflow Policy]                            │
//	│    1. Scan & drop oldest MouseMotion first                      │
//	│    2. Fallback: drop oldest general event                       │
//	│    3. Log warning & increment inputDrops counter                │
//	+─────────────────────────────────────────────────────────────────+
//	             │ (Order: the queue ahead of the slot, the slot, the rest)
//	             ▼
//	+─────────────────────────────────────────────────────────────────+
//	│                      chan Event (a.input)                       │
//	+─────────────────────────────────────────────────────────────────+
//	                                 │
//	                                 │ Drained by Single Loop Goroutine
//	                                 ▼
//	+─────────────────────────────────────────────────────────────────+
//	│                   App.loop()    (Dispatch)                      │
//	│     - Focused node / Component.HandleEvent()                    │
//	│     - Global keybindings & focus navigation                     │
//	+─────────────────────────────────────────────────────────────────+
//
// # Key Functional Invariants & Policy Pipeline
//
//  1. Promptness Invariant (Normative):
//     intake runs on its own dedicated goroutine and does zero component, layout,
//     or dispatch work. It only classifies, coalesces, and enqueues (O(1) amortized
//     per event). As a result, backend.Events() is drained promptly and the backend's
//     single reader goroutine (e.g. websocket or terminal reader) never blocks on the
//     App, even when the App loop is delayed inside a slow component handler.
//
//  2. Resize Latest-Wins, Delivered In Its Place (Atomic Slot):
//     Incoming resizes are captured in a dedicated single-event slot (haveResize),
//     overwriting a resize still waiting there, so a storm is one delivery of the
//     final size and never a queue of sizes. The slot keeps its place in the
//     input: the events queued before it (resizeAt of them) are delivered first,
//     then the resize, then what came after. A key typed while the screen was too
//     small is held by the too-small screen, and a click is hit-tested against the
//     layout it was aimed at; a resize delivered ahead of them would hand that key
//     to the application and that click to another layout.
//
//  3. Mouse Motion Coalescing:
//     Consecutive MouseMotion events at the tail of the FIFO queue are collapsed
//     in-place into the newest coordinate. This prevents high-frequency pointer
//     sampling from overwhelming the bounded queue or flooding the loop goroutine.
//
//     Consecutive precise scrolls (a MouseWheel with no Button) at the same place sum
//     theirs, so a touchpad's flood costs one event and loses none of the scroll.
//
//  4. Smart Drop-Oldest Overflow (intakeEnqueue):
//     When pending reaches cfg.inputQueueSize, intake sheds load instead of
//     blocking: it prioritizes dropping the oldest MouseMotion before dropping any
//     other event type, protecting discrete semantic actions (keystrokes, clicks,
//     paste chunks). Drops are atomically counted (a.inputDrops) and logged.
//
//  5. Orderly Shutdown:
//     When backend.Events() closes, intake drains any remaining pending events,
//     closes a.input, and exits cleanly. The loop then collects backend.Err().
func (a *App) intake() {
	in := a.backend.Events()
	limit := a.cfg.inputQueueSize

	var pending []Event // bounded FIFO, cap = limit
	var resize ResizeEvent
	haveResize := false
	resizeAt := 0 // the pending events queued before the resize: delivered ahead of it

	for {
		var out chan<- Event
		var next Event
		fromResize := false
		switch {
		case haveResize && resizeAt == 0:
			out, next, fromResize = a.input, resize, true
		case len(pending) > 0:
			out, next = a.input, pending[0]
		}
		if in == nil && out == nil {
			close(a.input)
			return
		}

		select {
		case <-a.quit: // the loop is gone; stop pumping
			return

		case ev, ok := <-in:
			if !ok {
				in = nil // deliver the remainder, then close a.input
				continue
			}
			switch e := ev.(type) {
			case ResizeEvent:
				// Latest-wins atomic slot — never a queue of sizes — after everything queued now.
				resize, haveResize, resizeAt = e, true, len(pending)
			case MouseEvent:
				// an event collapses into the last queued one only if that one came after the resize
				// waiting: one before it was aimed at the old size
				tail := len(pending) > 0 && (!haveResize || resizeAt < len(pending))
				if e.Kind == MouseMotion && tail {
					if last, ok := pending[len(pending)-1].(MouseEvent); ok && last.Kind == MouseMotion {
						pending[len(pending)-1] = e // consecutive motions collapse to the newest
						continue
					}
				}
				if e.precise() && tail {
					// consecutive precise scrolls at the same place sum: none of the scroll is lost
					if last, ok := pending[len(pending)-1].(MouseEvent); ok && last.precise() &&
						last.X == e.X && last.Y == e.Y && last.Mods == e.Mods && last.Source == e.Source {
						e.ScrollX, e.ScrollY = last.ScrollX+e.ScrollX, last.ScrollY+e.ScrollY
						pending[len(pending)-1] = e
						continue
					}
				}
				pending, resizeAt = a.intakeEnqueue(pending, ev, limit, resizeAt)
			default:
				pending, resizeAt = a.intakeEnqueue(pending, ev, limit, resizeAt)
			}

		case out <- next:
			if fromResize {
				haveResize = false
			} else {
				if resizeAt > 0 {
					resizeAt-- // one fewer ahead of the resize waiting
				}
				copy(pending, pending[1:])
				pending[len(pending)-1] = nil
				pending = pending[:len(pending)-1]
			}
		}
	}
}

// intakeEnqueue appends ev to the bounded lane-A queue, applying the
// drop-oldest overflow policy: input is refreshable, so a full lane drops
// the OLDEST MOTION first and only then the oldest event of any kind — at
// that point the app is seconds behind and stale keys are the least-bad
// loss. Key presses and paste chunks are never coalesced (each is
// semantically distinct). Drops are counted and logged via WithLogger. resizeAt is how many queued
// events precede the resize waiting in the slot (0 with none); it is returned one less when the
// dropped event was one of them. The slot is not in the queue, so it is never the one dropped.
func (a *App) intakeEnqueue(pending []Event, ev Event, limit, resizeAt int) ([]Event, int) {
	if len(pending) >= limit {
		drop := 0 // default: the oldest event
		for i, p := range pending {
			if m, ok := p.(MouseEvent); ok && m.Kind == MouseMotion {
				drop = i // the oldest motion goes first
				break
			}
		}
		dropped := pending[drop]
		if f, ok := dropped.(inputFence); ok {
			f.pass() // what came before it was dispatched or dropped too
		}
		if drop < resizeAt {
			resizeAt--
		}
		copy(pending[drop:], pending[drop+1:])
		pending[len(pending)-1] = nil
		pending = pending[:len(pending)-1]
		n := a.inputDrops.Add(1)
		logger.Warning(a.cfg.logger, nil, map[string]any{
			"tui": "input queue overflow — dropped oldest", "event": eventName(dropped),
			"dropped_total": n, "cap": limit,
		})
	}
	return append(pending, ev), resizeAt
}

// eventName names an event for diagnostics without fmt in the hot path: the input queue's
// overflow line and the dead letter of an addressed delivery.
func eventName(ev Event) string {
	switch ev.(type) {
	case KeyEvent:
		return "KeyEvent"
	case MouseEvent:
		return "MouseEvent"
	case PasteEvent:
		return "PasteEvent"
	case ResizeEvent:
		return "ResizeEvent"
	case FocusEvent:
		return "FocusEvent"
	case CloseRequestEvent:
		return "CloseRequestEvent"
	case TaskResult:
		return "TaskResult"
	case TaskProgress:
		return "TaskProgress"
	case TickEvent:
		return "TickEvent"
	default:
		return "Event"
	}
}
