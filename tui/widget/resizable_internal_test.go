package widget

// The grip's one structural guarantee, asserted structurally.
//
// "Not a tab stop" is enforced by the ABSENCE of an interface, which no
// behavioural test can pin down: the runtime asks for tui.Focusable, and a type
// that does not have it can never be offered focus. A test that tabs around and
// observes where focus lands measures the whole ring — useful, and it is in
// resizable_test.go — but it passes for reasons that have nothing to do with
// this type, so it cannot tell you the rule still holds.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// TestAGripDoesNotImplementFocusable.
//
// The rule is that the handle component does not implement Focusable at all.
// Written as absence rather than as AcceptsFocus() returning false, because the
// two are not equivalent to the next person editing this file — a method
// returning false invites a condition, and that is how a grip becomes a tab stop
// in one edit.
func TestAGripDoesNotImplementFocusable(t *testing.T) {
	var c tui.Component = &resizeHandle{}
	if _, ok := c.(tui.Focusable); ok {
		t.Error("resizeHandle implements tui.Focusable; adding nodes to the tree must " +
			"not pollute Tab order, and the guarantee is that the interface is absent " +
			"rather than that a method happens to return false")
	}
}

// TestTheWrapperDoesImplementFocusable is the positive control for the above: a
// type-assertion test that could never fail proves nothing, and the wrapper has
// to be focusable for the keyboard resize vocabulary to be reachable at all.
func TestTheWrapperDoesImplementFocusable(t *testing.T) {
	var c tui.Component = &Resizable{}
	if _, ok := c.(tui.Focusable); !ok {
		t.Error("Resizable does not implement tui.Focusable, so its resize actions " +
			"cannot be reached from the keyboard")
	}
}
