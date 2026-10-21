package tui

import (
	"sync/atomic"
	"testing"
)

// A window's close request is the program's quit: WithQuit's function, which may keep the program
// running (it asked first), and the keys still reach the application after it.
func TestACloseRequestCallsTheProgramsQuit(t *testing.T) {
	var quits atomic.Int64
	root := &probe{name: "root", fill: "A"}
	h := startApp(t, root, 30, 8, WithQuit(func() { quits.Add(1) }))
	waitFor(t, "the application", func() bool { return screenHas(h.tb, "A") })
	h.inject(CloseRequestEvent{})
	waitFor(t, "the quit called", func() bool { return quits.Load() == 1 })
	select {
	case <-h.app.Done():
		t.Fatal("Run ended: WithQuit's function decides, and it did not end it")
	default:
	}
	h.inject(KeyEvent{Code: 'x', Text: "x"})
	waitFor(t, "the key delivered", func() bool { return len(keyEvents(root)) == 1 })
}

// Without WithQuit, a close request ends Run.
func TestACloseRequestEndsRunByDefault(t *testing.T) {
	h := startApp(t, &probe{name: "root", fill: "A"}, 30, 8)
	waitFor(t, "the application", func() bool { return screenHas(h.tb, "A") })
	h.inject(CloseRequestEvent{})
	waitFor(t, "Run to end", func() bool {
		select {
		case <-h.app.Done():
			return true
		default:
			return false
		}
	})
	if res := h.wait(); res.err != nil || res.rec != nil {
		t.Fatalf("Run ended with %v / %v", res.err, res.rec)
	}
}

// On the too-small screen the request still quits: it is not input the screen swallows.
func TestACloseRequestQuitsFromTheTooSmallScreen(t *testing.T) {
	var quits atomic.Int64
	h := startApp(t, &probe{name: "root"}, 30, 8, WithMinimumSize(Size{W: 40, H: 10}), WithQuit(func() { quits.Add(1) }))
	waitFor(t, "the too-small screen", func() bool { return screenHas(h.tb, "[ Quit ]") })
	h.inject(CloseRequestEvent{})
	waitFor(t, "the quit called", func() bool { return quits.Load() == 1 })
}

// An input-queue overflow names a dropped close request as what it is.
func TestTheIntakeNamesACloseRequest(t *testing.T) {
	if got := eventName(CloseRequestEvent{}); got != "CloseRequestEvent" {
		t.Fatalf("eventName = %q", got)
	}
}

// eventName names every event kind the intake's overflow line and an addressed delivery's dead
// letter report, and any other event as "Event".
func TestEventNameNamesEachKind(t *testing.T) {
	for ev, want := range map[Event]string{
		KeyEvent{}: "KeyEvent", MouseEvent{}: "MouseEvent", PasteEvent{}: "PasteEvent", ResizeEvent{}: "ResizeEvent",
		FocusEvent{}: "FocusEvent", CloseRequestEvent{}: "CloseRequestEvent", TaskResult{}: "TaskResult",
		TaskProgress{}: "TaskProgress", TickEvent{}: "TickEvent", UserEvent{}: "Event",
	} {
		if got := eventName(ev); got != want {
			t.Errorf("eventName(%T) = %q, want %q", ev, got, want)
		}
	}
}
