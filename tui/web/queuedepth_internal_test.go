package web

import (
	"testing"

	"github.com/yongjohnlee80/golib/auth"
	"github.com/yongjohnlee80/golib/tui"
)

// queueOf is the event queue a new session of m gets: the Backend register builds for it.
func queueOf(t *testing.T, m *Manager) int {
	t.Helper()
	s, _, err := m.register(&auth.Identity{Subject: "u"}, "")
	if err != nil {
		t.Fatal(err)
	}
	return s.backend.eventCap
}

// A session's event queue is an explicit QueueDepth, else a BackendOptions EventQueue, else
// DefaultQueueDepth when a Handler serves the Manager, else the Backend's own default: what each
// was before QueueDepth became a ManagerOption, which made every Manager force DefaultQueueDepth
// over a consumer's EventQueue.
func TestASessionsQueueIsTheMostSpecificGiven(t *testing.T) {
	factory := func(tui.Backend, *SessionInfo) Runner { return newFakeApp() }
	manager := func(opts ...ManagerOption) *Manager {
		m, err := NewManager(factory, opts...)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	if got := queueOf(t, manager()); got != DefaultEventQueue {
		t.Errorf("a Manager on its own: queue %d, want the Backend's default %d", got, DefaultEventQueue)
	}
	if got := queueOf(t, manager(BackendOptions(EventQueue(10)))); got != 10 {
		t.Errorf("a Manager given BackendOptions(EventQueue(10)): queue %d, want 10", got)
	}
	if got := queueOf(t, manager(BackendOptions(EventQueue(10)), QueueDepth(20))); got != 20 {
		t.Errorf("QueueDepth(20) over EventQueue(10): queue %d, want 20", got)
	}
	h, _ := loginHandler(t, "pw", "u")
	if got := queueOf(t, h.mgr); got != DefaultQueueDepth {
		t.Errorf("a Manager a Handler serves: queue %d, want %d", got, DefaultQueueDepth)
	}
}
