package gui

import (
	"errors"
	"sync"

	"github.com/yongjohnlee80/golib/tui"
)

// ErrEventOverflow means the window produced more events than the App read: maxQueued were
// waiting. The backend ends rather than grow the queue or coalesce it, because an ordered,
// un-coalesced event source is part of the Backend contract (tui/backend.go), as tui/web does.
var ErrEventOverflow = errors.New("gui: event queue overflow")

// maxQueued bounds the events waiting for the App. Input arrives at human rates and tui's intake
// drains its channel promptly, so reaching it means the App stopped reading.
const maxQueued = 4096

// eventQueue gives tui.Backend.Events one owner (ADR §4.2). The Gio goroutine appends to the
// queue and never blocks; the forwarder goroutine is the channel's only sender and only closer.
// A channel closed by anyone else could be closed under a pending send, which panics.
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

// reason is the error the queue ended with.
func (q *eventQueue) reason() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.err
}
