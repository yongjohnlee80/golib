package gui

import (
	"errors"
	"sync"

	"github.com/yongjohnlee80/golib/tui"
)

// ErrEventOverflow indicates that input events accumulated faster than the consuming
// application could drain them, exceeding the internal capacity limit ([maxQueued]).
//
// In accordance with the [tui.Backend] contract, input events must be delivered strictly in
// arrival order without dropping or coalescing at the driver boundary. When the queue overflows,
// the backend terminates cleanly with this error rather than silently degrading event integrity.
var ErrEventOverflow = errors.New("gui: event queue overflow")

// maxQueued is the maximum number of pending input events buffered in the [eventQueue] before
// signaling [ErrEventOverflow]. Under ordinary execution, the TUI loop drains events promptly;
// reaching this threshold indicates a blocked or unresponsive application event loop.
const maxQueued = 4096

// eventQueue provides thread-safe, ordered buffering of [tui.Event] instances across the
// Gio event loop and the TUI application loop.
//
// The Gio goroutine appends events via [eventQueue.push] without blocking. A dedicated forwarder
// goroutine ([eventQueue.forward]) owns delivery to the unbuffered [tui.Backend.Events] channel,
// ensuring that closing the channel never panics against active event producers.
type eventQueue struct {
	events chan tui.Event // unbuffered: order and backpressure stay with the forwarder

	mu     sync.Mutex
	queue  []tui.Event
	ready  chan struct{} // cap 1: the queue is not empty
	done   chan struct{} // closed once, by end
	exited chan struct{} // closed by the forwarder after it closes events

	endOnce sync.Once
	err     error // the FIRST reason the queue ended; later ones are dropped
}

func newEventQueue() *eventQueue {
	return &eventQueue{
		events: make(chan tui.Event),
		ready:  make(chan struct{}, 1),
		done:   make(chan struct{}),
		exited: make(chan struct{}),
	}
}

// push queues ev for the App. It never blocks. Past maxQueued it ends the queue with
// ErrEventOverflow; after the queue has ended it drops ev.
func (q *eventQueue) push(ev tui.Event) {
	q.mu.Lock()
	select {
	case <-q.done:
		q.mu.Unlock()
		return
	default:
	}
	if len(q.queue) >= maxQueued {
		q.mu.Unlock()
		q.end(ErrEventOverflow)
		return
	}
	q.queue = append(q.queue, ev)
	q.mu.Unlock()
	select {
	case q.ready <- struct{}{}:
	default: // already signalled
	}
}

// forward delivers queued events in order until the queue ends, then closes events. Run it on
// its own goroutine; it is the only goroutine that sends on or closes events.
func (q *eventQueue) forward() {
	defer close(q.exited)
	defer close(q.events)
	for {
		select {
		case <-q.ready:
		case <-q.done:
			return
		}
		q.mu.Lock()
		batch := q.queue
		q.queue = nil
		q.mu.Unlock()
		for _, ev := range batch {
			select {
			case q.events <- ev:
			case <-q.done:
				return // the App is stopping: what it did not read is dropped
			}
		}
	}
}

// end stops the queue once. err (nil for a clean stop) is kept only on the first call, so Err
// reports what actually ended the backend.
func (q *eventQueue) end(err error) {
	q.endOnce.Do(func() {
		q.mu.Lock()
		q.err = err
		q.mu.Unlock()
		close(q.done)
	})
}

// ended reports whether the queue has ended.
func (q *eventQueue) ended() bool {
	select {
	case <-q.done:
		return true
	default:
		return false
	}
}

// reason is the error the queue ended with.
func (q *eventQueue) reason() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.err
}
