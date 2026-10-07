package tui

import (
	"strings"
	"sync/atomic"
	"testing"
)

func probeEvents(p *probe) []Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Event(nil), p.events...)
}

// keyEvents is the key events p received.
func keyEvents(p *probe) []KeyEvent {
	var out []KeyEvent
	for _, ev := range probeEvents(p) {
		if k, ok := ev.(KeyEvent); ok {
			out = append(out, k)
		}
	}
	return out
}

func screenHas(tb *TestBackend, text string) bool { return strings.Contains(tb.String(), text) }

// quitButtonAt finds "[ Quit ]" on the screen and returns a cell inside it.
func quitButtonAt(t *testing.T, tb *TestBackend) (x, y int) {
	t.Helper()
	for row, line := range strings.Split(tb.String(), "\n") {
		if i := strings.Index(line, "[ Quit ]"); i >= 0 {
			return len([]rune(line[:i])) + 2, row
		}
	}
	t.Fatalf("no Quit button on the screen:\n%s", tb.String())
	return 0, 0
}

// Below the minimum the App lays nothing out: it shows the sizes and the request to enlarge,
// and the application receives no keys, pointer or paste. Enlarged, it is laid out again and
// receives input as before.
func TestTooSmall_ShowsTheNoticeAndHoldsInput(t *testing.T) {
	root := &probe{name: "root", fill: "A"}
	h := startApp(t, root, 30, 8, WithMinimumSize(Size{W: 40, H: 10}))
	waitFor(t, "the too-small screen", func() bool {
		return screenHas(h.tb, "Screen too small") && screenHas(h.tb, "30 × 8 — needs 40 × 10")
	})
	if screenHas(h.tb, "A") && !screenHas(h.tb, "Screen too small") {
		t.Fatal("the application painted under the notice")
	}
	if !screenHas(h.tb, "Enlarge the window to continue") || !screenHas(h.tb, "or press q / Ctrl+C") {
		t.Fatalf("the notice lacks its instructions:\n%s", h.tb.String())
	}
	h.inject(KeyEvent{Code: 'x', Text: "x"}, MouseEvent{Kind: MousePress, Button: MouseLeft, X: 0, Y: 0}, PasteEvent{Text: "p"})
	h.sync()
	for _, ev := range probeEvents(root) {
		switch ev.(type) {
		case KeyEvent, MouseEvent, PasteEvent:
			t.Fatalf("the application received %T under the too-small screen", ev)
		}
	}
	if root.layouts.Load() != 0 {
		t.Fatalf("the application was laid out %d times on a screen under its minimum", root.layouts.Load())
	}

	h.tb.InjectResize(50, 12)
	waitFor(t, "the application again", func() bool { return !screenHas(h.tb, "Screen too small") && screenHas(h.tb, "A") })
	h.inject(KeyEvent{Code: 'x', Text: "x"})
	waitFor(t, "the key delivered", func() bool { return len(keyEvents(root)) == 1 })
}

// q, Ctrl+C and a click on the Quit button call the application's quit; nothing else does.
func TestTooSmall_QuitKeysAndButton(t *testing.T) {
	var quits atomic.Int64
	h := startApp(t, &probe{name: "root"}, 30, 8, WithMinimumSize(Size{W: 40, H: 10}), WithQuit(func() { quits.Add(1) }))
	waitFor(t, "the too-small screen", func() bool { return screenHas(h.tb, "[ Quit ]") })

	h.inject(KeyEvent{Kind: KeyRelease, Code: 'q'}, KeyEvent{Code: 'c', Mods: ModCtrl | ModShift}, KeyEvent{Code: 'x', Text: "x"})
	h.sync()
	if n := quits.Load(); n != 0 {
		t.Fatalf("a release, Ctrl+Shift+C or x quit (%d)", n)
	}
	h.inject(KeyEvent{Code: 'q', Text: "q"})
	waitFor(t, "q quits", func() bool { return quits.Load() == 1 })
	h.inject(KeyEvent{Code: 'c', Mods: ModCtrl | ModNumLock}) // a lock modifier is not a chord
	waitFor(t, "Ctrl+C quits", func() bool { return quits.Load() == 2 })

	x, y := quitButtonAt(t, h.tb)
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 0, Y: 0})
	h.sync()
	if quits.Load() != 2 {
		t.Fatal("a click away from the button quit")
	}
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: x, Y: y})
	waitFor(t, "the Quit button quits", func() bool { return quits.Load() == 3 })
}

// Without WithQuit, Quit ends Run, as cancelling its context does.
func TestTooSmall_QuitEndsRunByDefault(t *testing.T) {
	h := startApp(t, &probe{name: "root"}, 30, 8, WithMinimumSize(Size{W: 40, H: 10}))
	waitFor(t, "the too-small screen", func() bool { return screenHas(h.tb, "[ Quit ]") })
	h.inject(KeyEvent{Code: 'q', Text: "q"})
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

// The minimum changes at run time; a zero axis sets no minimum on it.
func TestTooSmall_SetMinimumSizeAtRunTime(t *testing.T) {
	root := &probe{name: "root", fill: "A"}
	h := startApp(t, root, 30, 8)
	waitFor(t, "the application", func() bool { return screenHas(h.tb, "A") })

	h.onLoop(func() { h.app.SetMinimumSize(Size{W: 40}) }) // width only
	waitFor(t, "the too-small screen", func() bool { return screenHas(h.tb, "30 × 8 — needs 40 × 0") })
	h.onLoop(func() { h.app.SetMinimumSize(Size{H: 8}) }) // height only, and met
	waitFor(t, "the application again", func() bool { return !screenHas(h.tb, "Screen too small") })
	h.onLoop(func() { h.app.SetMinimumSize(Size{}) })
	var got Size
	h.onLoop(func() { got = h.app.MinimumSize() })
	if got != (Size{}) {
		t.Fatalf("MinimumSize() = %v after clearing it", got)
	}
}

// A drag in progress ends when the screen becomes too small: its owner is no longer on screen.
func TestTooSmall_EndsAPointerCapture(t *testing.T) {
	owner := &dragger{size: Size{W: 10, H: 4}}
	h := startApp(t, owner, 20, 6, WithMinimumSize(Size{W: 15, H: 5}))
	startDrag(t, h, owner, 2, 1)
	h.tb.InjectResize(12, 4)
	waitFor(t, "the capture lost", func() bool { return owner.losses.Load() == 1 })
	if r := CaptureLostReason(owner.lastReason.Load()); r != CaptureLostHidden {
		t.Fatalf("capture lost with %v; want CaptureLostHidden", r)
	}
}
