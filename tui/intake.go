package tui

import "github.com/yongjohnlee80/golib/logger"

// intake is the App-owned input stage (lane A — ADR-0005 §2.4, rev 1): it
// pulls promptly from backend.Events() and applies ALL input policy the
// backend deliberately does not own — bounded capacity (WithInputQueueSize),
// drop-oldest overflow, resize latest-wins, motion coalescing — before
// handing events to the loop over a.input.
//
// PROMPTNESS INVARIANT (normative): intake does no component work — it only
// classifies, coalesces, and enqueues (O(1) amortized per event, no
// dispatch). backend.Events() is therefore always drained promptly and the
// backend's single reader goroutine (one-reader contract per
// server/ws/ws.go:36-48) effectively never blocks on the App — even while
// the loop goroutine is stuck inside a slow handler. When Events() closes,
// intake drains its remainder and closes a.input; the loop then collects
// backend.Err() (ADR-0005 §2.2).
//
// Ordering note: the resize slot has delivery priority — a pending resize
// supersedes queued input (geometry invalidates everything behind it; the
// dossier §8 resize-storm rule). Everything else delivers in arrival order.
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
