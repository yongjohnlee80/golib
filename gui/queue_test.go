package gui

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

func typed(r rune) tui.Event { return tui.KeyEvent{Code: r, Text: string(r)} }

// Events queued before anything reads them (before Start returns, before the App's intake)
// arrive later, in order, and never block the producer.
func TestQueueBeforeTheReader(t *testing.T) {
	q := newEventQueue()
	pushed := make(chan struct{})
	go func() {
		for r := 'a'; r <= 'z'; r++ {
			q.push(typed(r))
		}
		close(pushed)
	}()
	select {
	case <-pushed:
	case <-time.After(2 * time.Second):
		t.Fatal("push blocked with no reader")
	}
	go q.forward()
	for r := 'a'; r <= 'z'; r++ {
		if got := <-q.events; got != typed(r) {
			t.Fatalf("got %+v; want %+v", got, typed(r))
		}
	}
	q.end(nil)
	<-q.exited
	if _, open := <-q.events; open {
		t.Fatal("events still open after the forwarder exited")
	}
}

// Ending the queue while the forwarder is blocked sending must not panic: only the forwarder
// closes events, after it has stopped sending. Run with -race.
func TestQueueEndDuringBlockedSend(t *testing.T) {
	for range 200 {
		q := newEventQueue()
		go q.forward()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 50 {
				q.push(typed('x'))
			}
		}()
		go func() {
			defer wg.Done()
			q.end(nil)
		}()
		wg.Wait()
		<-q.exited
		for range q.events { // drains whatever was sent; returns because events is closed
		}
	}
}

func TestQueueOverflowEnds(t *testing.T) {
	q := newEventQueue() // no forwarder: nobody reads
	for i := range maxQueued + 1 {
		q.push(typed(rune('a' + i%26)))
	}
	select {
	case <-q.done:
	default:
		t.Fatal("the queue kept growing past maxQueued")
	}
	if !errors.Is(q.reason(), ErrEventOverflow) {
		t.Fatalf("reason = %v; want ErrEventOverflow", q.reason())
	}
}

func TestQueueKeepsTheFirstReason(t *testing.T) {
	q := newEventQueue()
	first := errors.New("window lost")
	q.end(first)
	q.end(nil)
	q.end(ErrEventOverflow)
	if q.reason() != first {
		t.Fatalf("reason = %v; want the first, %v", q.reason(), first)
	}
	q.push(typed('z')) // after the end: dropped, no panic
}
