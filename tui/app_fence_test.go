package tui

import (
	"testing"
	"time"
)

// app_fence_test.go: TestBackend.InjectFenced's fence is passed only once every event queued
// before it has been dispatched. Each cell holds the loop while it injects (stallLoop), so the
// whole script waits in the intake together and the order it is handed on is the intake's.

func typed(s string) KeyEvent { return KeyEvent{Kind: KeyPress, Code: rune(s[0]), Text: s} }

// passed reports whether the fence is closed now.
func passed(fence <-chan struct{}) bool {
	select {
	case <-fence:
		return true
	default:
		return false
	}
}

// waitPassed waits for the fence.
func waitPassed(t *testing.T, fence <-chan struct{}) {
	t.Helper()
	select {
	case <-fence:
	case <-time.After(3 * time.Second):
		t.Fatal("the fence was never passed")
	}
}

// The fence is passed after the input before it, and not while the loop is held: when it closes,
// the application has already got every event, in order.
func TestAFenceIsPassedOnceTheInputBeforeItIsDispatched(t *testing.T) {
	h, got := startWidthProbe(t, 40, 10)
	release := stallLoop(t, h)
	fence, err := h.tb.InjectFenced(typed("a"), typed("b"), typed("c"))
	if err != nil {
		t.Fatal(err)
	}
	absorbed(t, h)
	if passed(fence) {
		t.Fatal("the fence was passed while the loop was held")
	}
	close(release)
	waitPassed(t, fence)
	if s := got(); !sameSeen(s, []seen{{"a", 40}, {"b", 40}, {"c", 40}}) {
		t.Fatalf("at the fence the application had got %v, want a, b, c", s)
	}
}

// A fence queued after a resize is passed after it: the intake holds a resize behind what came
// before it, and the fence came after.
func TestAFenceAfterAResizeIsPassedAfterIt(t *testing.T) {
	h, got := startWidthProbe(t, 40, 10)
	release := stallLoop(t, h)
	if err := h.tb.Inject(typed("a")); err != nil {
		t.Fatal(err)
	}
	h.tb.InjectResize(60, 10)
	fence, err := h.tb.InjectFenced(typed("b"))
	if err != nil {
		t.Fatal(err)
	}
	absorbed(t, h)
	close(release)
	waitPassed(t, fence)
	if s := got(); !sameSeen(s, []seen{{"a", 40}, {"b", 60}}) {
		t.Fatalf("at the fence the application had got %v, want a at 40 columns, then b at 60", s)
	}
}

// Motions before a fence collapse among themselves as ever, and the fence is passed after the one
// they collapse to; a motion after the fence doesn't collapse into one before it.
func TestAFenceKeepsMotionsOnItsSide(t *testing.T) {
	h, got := startWidthProbe(t, 40, 10)
	release := stallLoop(t, h)
	motion := func(x int) MouseEvent { return MouseEvent{Kind: MouseMotion, X: x, Y: 1} }
	fence, err := h.tb.InjectFenced(motion(1), motion(2))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.tb.Inject(motion(3)); err != nil {
		t.Fatal(err)
	}
	absorbed(t, h)
	close(release)
	waitPassed(t, fence)
	// the motion after the fence may follow it at once: what the fence vouches for is the first
	if s := got(); len(s) == 0 || s[0] != (seen{"motion2", 40}) {
		t.Fatalf("at the fence the application had got %v, want the motions before it collapsed to the last", s)
	}
	if s := waitSeen(t, got, 2); !sameSeen(s, []seen{{"motion2", 40}, {"motion3", 40}}) {
		t.Fatalf("after the fence the application got %v, want the motion after it on its own", s)
	}
}

// A fence the intake drops in an overflow is passed then: what came before it was dispatched or
// dropped too, so nothing it stands for is still to come.
func TestAFenceDroppedInAnOverflowIsPassed(t *testing.T) {
	h, _ := startWidthProbe(t, 40, 10, WithInputQueueSize(2))
	release := stallLoop(t, h)
	defer close(release)
	fence, err := h.tb.InjectFenced()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.tb.Inject(typed("a"), typed("b")); err != nil { // the queue holds two: the fence goes
		t.Fatal(err)
	}
	waitPassed(t, fence)
}
