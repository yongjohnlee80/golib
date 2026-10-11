package gui

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// The close button becomes a request while the App reads events; before it does, and once Stop
// ended the queue, the window closes.
func TestTheCloseButtonAsksTheProgramWhileItRuns(t *testing.T) {
	b := &Backend{q: newEventQueue()}
	if b.askToClose() {
		t.Fatal("a close before the App reads events was held open: there is no one to answer it")
	}
	b.forwarding.Store(true)
	if !b.askToClose() {
		t.Fatal("a close while the App runs was not held open")
	}
	b.q.mu.Lock()
	queued := append([]tui.Event(nil), b.q.queue...)
	b.q.mu.Unlock()
	if len(queued) != 1 || queued[0] != (tui.CloseRequestEvent{}) {
		t.Fatalf("queued %v, want one CloseRequestEvent", queued)
	}
	b.q.end(nil) // Stop's first step
	if b.askToClose() {
		t.Fatal("Stop's own close was held open")
	}
}
