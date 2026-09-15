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

// TestTheWRAPPERDoesNotImplementFocusableEither.
//
// Wrapping arbitrary content must change Tab order in NO way, so neither the
// grips nor the wrapper may be a stop. The resize actions stay reachable from
// the keyboard because resolvers run at every node on the bubble path: an
// unhandled Shift-arrow from a focused DESCENDANT meets this node's resolver on
// its way up.
func TestTheWRAPPERDoesNotImplementFocusableEither(t *testing.T) {
	var c tui.Component = &Resizable{}
	if _, ok := c.(tui.Focusable); ok {
		t.Error("Resizable implements tui.Focusable; wrapping content must not add a " +
			"tab stop, and the keyboard reaches the wrapper by bubbling instead")
	}
}

// TestAControlStillImplementsFocusable is the positive control for both
// assertions above: a type-assertion test that could never fail proves nothing.
func TestAControlStillImplementsFocusable(t *testing.T) {
	var c tui.Component = NewButton("x")
	if _, ok := c.(tui.Focusable); !ok {
		t.Error("Button does not implement tui.Focusable, so the assertions above " +
			"are not measuring anything")
	}
}
