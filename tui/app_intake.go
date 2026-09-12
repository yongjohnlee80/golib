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
//	│         │                    (Priority 1: Delivered First)      │
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
//	             │ (Priority: Atomic Slot > FIFO Queue)
//	             ▼
//	+─────────────────────────────────────────────────────────────────+
//	│                      chan Event (a.input)                       │
//	+─────────────────────────────────────────────────────────────────+
//	                                 │
//	                                 │ Drained by Single Loop Goroutine
//	                                 ▼
//	+─────────────────────────────────────────────────────────────────+
//	│                   App.runLoop() (Dispatch)                      │
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
//  2. Resize Priority (Latest-Wins Atomic Slot):
//     ResizeEvent supersedes all queued user input. Geometry changes invalidate
//     everything behind them, so resizes must not queue behind stale keystrokes.
//     Incoming resizes are captured in a dedicated single-event slot (haveResize),
//     overwriting previous pending resizes, and are delivered before any FIFO event.
//
//  3. Mouse Motion Coalescing:
//     Consecutive MouseMotion events at the tail of the FIFO queue are collapsed
//     in-place into the newest coordinate. This prevents high-frequency pointer
//     sampling from overwhelming the bounded queue or flooding the loop goroutine.
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

	for {
		var out chan<- Event
		var next Event
		fromResize := false
		switch {
		case haveResize:
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
				// Latest-wins atomic slot — never a queue of sizes.
				resize, haveResize = e, true
			case MouseEvent:
				if e.Kind == MouseMotion && len(pending) > 0 {
					if last, ok := pending[len(pending)-1].(MouseEvent); ok && last.Kind == MouseMotion {
						pending[len(pending)-1] = e // consecutive motions collapse to the newest
						continue
					}
				}
				pending = a.intakeEnqueue(pending, ev, limit)
			default:
				pending = a.intakeEnqueue(pending, ev, limit)
			}

		case out <- next:
			if fromResize {
				haveResize = false
			} else {
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
// semantically distinct). Drops are counted and logged via WithLogger.
func (a *App) intakeEnqueue(pending []Event, ev Event, limit int) []Event {
	if len(pending) >= limit {
		drop := 0 // default: the oldest event
		for i, p := range pending {
			if m, ok := p.(MouseEvent); ok && m.Kind == MouseMotion {
				drop = i // the oldest motion goes first
				break
			}
		}
		dropped := pending[drop]
		copy(pending[drop:], pending[drop+1:])
		pending[len(pending)-1] = nil
		pending = pending[:len(pending)-1]
		n := a.inputDrops.Add(1)
		logger.Warning(a.cfg.logger, nil, map[string]any{
			"tui": "input queue overflow — dropped oldest", "event": typeName(dropped),
			"dropped_total": n, "cap": limit,
		})
	}
	return append(pending, ev)
}

// typeName names an event for diagnostics without fmt in the hot path.
func typeName(ev Event) string {
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
	default:
		return "Event"
	}
}
