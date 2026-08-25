package tui

// Double-click as activation (ADR-0010 §2.5, criteria 19-25).
//
// The press ORDINAL is synthesised once in dispatch, from timing and position,
// because a click count is behaviour rather than decode shape. These assert the
// three things that make a run continue — same button, same cell, inside the
// window — and the three that break it.

import (
	"testing"
	"time"
)

// counts extracts the Count of every MousePress the probe received.
func pressCounts(p *probe) []int {
	var out []int
	for _, ev := range p.recorded() {
		if m, ok := ev.(MouseEvent); ok && m.Kind == MousePress {
			out = append(out, m.Count)
		}
	}
	return out
}

// waitPresses polls until n presses have been DELIVERED. h.sync() is not enough:
// it runs an Update callback on the loop, while injected events travel through
// the backend channel and the intake goroutine, so Update can win the race and
// the probe still holds nothing. Every assertion here would then read an empty
// slice — which is how these tests first "failed", and how the non-press one
// first passed vacuously.
func waitPresses(t *testing.T, p *probe, n int) []int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if got := pressCounts(p); len(got) >= n {
			return got
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d presses; got %v", n, pressCounts(p))
	return nil
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Criterion 19 — two presses on the same cell inside the window count 1 then 2.
func TestDoubleClick_SameCellInsideWindowCounts(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	h.inject(pressAt(2, 1), pressAt(2, 1))

	if got := waitPresses(t, &a.probe, 2); !equalInts(got, []int{1, 2}) {
		t.Errorf("press counts = %v, want [1 2]", got)
	}
}

// Criterion 20 — ONE CELL APART is not a double-click. A terminal row is one
// cell tall, so a drift is a different row, and activating the row the user did
// not click is worse than requiring a steady hand.
func TestDoubleClick_DifferentCellRestarts(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 3})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 3, WithDoubleClickWindow(10*time.Second))

	h.inject(pressAt(2, 1), pressAt(2, 2)) // one row apart

	if got := waitPresses(t, &a.probe, 2); !equalInts(got, []int{1, 1}) {
		t.Errorf("press counts = %v, want [1 1] — a different cell must restart the run", got)
	}
}

// Criterion 21 — outside the window is not a double-click. A 1ns window makes
// this deterministic without giving App an injectable clock.
func TestDoubleClick_OutsideWindowRestarts(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(time.Nanosecond))

	h.inject(pressAt(2, 1), pressAt(2, 1))

	if got := waitPresses(t, &a.probe, 2); !equalInts(got, []int{1, 1}) {
		t.Errorf("press counts = %v, want [1 1] — a 1ns window can never continue a run", got)
	}
}

// Criterion 22 — a different button restarts the run.
func TestDoubleClick_DifferentButtonRestarts(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	h.inject(
		MouseEvent{Kind: MousePress, Button: MouseLeft, X: 2, Y: 1},
		MouseEvent{Kind: MousePress, Button: MouseRight, X: 2, Y: 1},
	)

	if got := waitPresses(t, &a.probe, 2); !equalInts(got, []int{1, 1}) {
		t.Errorf("press counts = %v, want [1 1]", got)
	}
}

// Criterion 23 — a release between the presses is NORMAL and must not interrupt
// the run; that is what a real double-click looks like on the wire.
func TestDoubleClick_InterveningReleaseDoesNotReset(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	h.inject(
		pressAt(2, 1),
		MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 2, Y: 1},
		pressAt(2, 1),
	)

	if got := waitPresses(t, &a.probe, 2); !equalInts(got, []int{1, 2}) {
		t.Errorf("press counts = %v, want [1 2] — a release is part of a double-click", got)
	}
}

// Criterion 24 — Count is 0 on every non-press kind, so no consumer can read a
// count that was never computed.
func TestDoubleClick_NonPressKindsCarryNoCount(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	h.inject(
		pressAt(2, 1),
		MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 2, Y: 1},
		MouseEvent{Kind: MouseMotion, Button: MouseNone, X: 2, Y: 1},
		MouseEvent{Kind: MouseWheel, Button: WheelDown, X: 2, Y: 1},
	)
	waitPresses(t, &a.probe, 1)
	waitFor(t, "all four mouse events delivered", func() bool {
		n := 0
		for _, ev := range a.probe.recorded() {
			if _, ok := ev.(MouseEvent); ok {
				n++
			}
		}
		return n >= 4
	})

	seen := 0
	for _, ev := range a.probe.recorded() {
		m, ok := ev.(MouseEvent)
		if !ok || m.Kind == MousePress {
			continue
		}
		seen++
		if m.Count != 0 {
			t.Errorf("%v carries Count %d, want 0", m.Kind, m.Count)
		}
	}
	// Without this the test passes when NO non-press event arrived at all, which
	// is how it first "passed" while the probe was receiving nothing.
	if seen != 3 {
		t.Fatalf("saw %d non-press mouse events, want 3 — the assertion above was vacuous", seen)
	}
}

// Criterion 25 — the ordinal is stamped BEFORE the §2.1 focus step, so a
// double-click that also moves focus still delivers Count 2.
func TestDoubleClick_SurvivesTheFocusStep(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 1})
	b := newFocusProbe("b", Size{W: 8, H: 1})
	root := NewFlex(Vertical)
	root.Add(a, b)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	h.inject(pressAt(1, 0)) // focus a
	waitFor(t, "a focused", func() bool { return focusedID(h) == a.nodeID() })

	// Both presses land on b: the first moves focus, the second doubles.
	h.inject(pressAt(1, 1), pressAt(1, 1))
	waitFor(t, "b focused", func() bool { return focusedID(h) == b.nodeID() })

	if got := waitPresses(t, &b.probe, 2); !equalInts(got, []int{1, 2}) {
		t.Errorf("press counts on the newly focused widget = %v, want [1 2]", got)
	}
}

// Finding 1 (lector r1) — a press delivered to NOBODY must not advance the run.
//
// The ordinal used to be committed on arrival, before hit-testing and before the
// focus step could decide to skip. So the victim's press counted, and the
// widget that REPLACED it saw Count == 2 as its first ever delivered press.
// Count drives activation, so that is a spurious activation on a first click.
func TestDoubleClick_SkippedPressDoesNotAdvanceTheRun(t *testing.T) {
	t.Parallel()
	victim := newFocusProbe("victim", Size{W: 8, H: 1})
	replacement := newFocusProbe("replacement", Size{W: 8, H: 1})
	filler := &probe{name: "filler", pref: Size{W: 8, H: 1}}
	root := NewFlex(Vertical)
	root.Add(victim, filler)
	h := startApp(t, root, 8, 4, WithDoubleClickWindow(10*time.Second))

	// The replacement must end up in the VICTIM'S CELL, not merely in the tree:
	// at a different cell the run could not continue anyway and the assertion
	// below would pass without testing anything. Flex only appends, so the filler
	// is re-added after the replacement to restore the row order.
	victim.onEvent = func(p *probe, ev Event) bool {
		if fe, ok := ev.(FocusEvent); ok && fe.Gained {
			root.Remove(victim)
			root.Remove(filler)
			root.Add(replacement)
			root.Add(filler)
		}
		return false
	}

	h.inject(pressAt(1, 0)) // skipped: focus handling unmounts the target
	waitFor(t, "focus handler unmounted the pointer target", func() bool {
		return nodeGone(h, victim.nodeID())
	})
	if got := pressCounts(&victim.probe); len(got) != 0 {
		t.Fatalf("precondition: the victim received %v, want nothing — the press must be skipped", got)
	}

	// The replacement is mounted but has measured=false/placed=false until a
	// layout pass, so it is not hit-testable yet. Waiting for its first Layout is
	// what makes the click below actually land — without it the test times out
	// with no presses and says nothing about counting.
	waitFor(t, "replacement laid out and hit-testable", func() bool {
		return replacement.layouts.Load() > 0
	})

	// The replacement now occupies that cell — a DIFFERENT target at the same
	// coordinates. Its FIRST delivered press is a first click and must say so.
	h.inject(pressAt(1, 0))
	got := waitPresses(t, &replacement.probe, 1)
	if got[0] != 1 {
		t.Errorf("the replacement's first delivered press has Count %d, want 1 — "+
			"a press nobody received advanced the run", got[0])
	}
}

// Finding 4 — non-press Count is CANONICALISED, not merely left alone. dispatch
// used to rewrite presses only, so a producer (or a test) could hand a wheel a
// count and it would be delivered verbatim, contradicting the ADR.
func TestDoubleClick_NonPressCountIsCanonicalisedNotIgnored(t *testing.T) {
	t.Parallel()
	a := newFocusProbe("a", Size{W: 8, H: 2})
	root := NewFlex(Vertical)
	root.Add(a)
	h := startApp(t, root, 8, 2, WithDoubleClickWindow(10*time.Second))

	// SEEDED nonzero: the previous version of this assertion passed because
	// nothing ever set a non-press count, not because anything cleared it.
	h.inject(
		MouseEvent{Kind: MouseWheel, Button: WheelDown, X: 2, Y: 1, Count: 99},
		MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 2, Y: 1, Count: 42},
		MouseEvent{Kind: MouseMotion, Button: MouseNone, X: 2, Y: 1, Count: 7},
	)
	waitFor(t, "all three non-press events delivered", func() bool {
		n := 0
		for _, ev := range a.probe.recorded() {
			if m, ok := ev.(MouseEvent); ok && m.Kind != MousePress {
				n++
			}
		}
		return n >= 3
	})
	for _, ev := range a.probe.recorded() {
		m, ok := ev.(MouseEvent)
		if !ok || m.Kind == MousePress {
			continue
		}
		if m.Count != 0 {
			t.Errorf("%v was delivered with Count %d, want 0 — a seeded count survived", m.Kind, m.Count)
		}
	}
}
