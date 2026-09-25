package tui

import (
	"testing"
	"time"
)

// FocusInto a control that is laid out with no cells — a pane clipped to
// nothing — is not deferred: another layout would leave it the same, and
// retrying after each one would run frames for ever in an idle App.
func TestFocusIntoAZeroSizedControlDoesNotSpinLayouts(t *testing.T) {
	zero := newFocusProbe("zero", Size{})
	root := NewFlex(Vertical)
	root.Add(zero)
	h := startApp(t, root, 20, 4)
	h.onLoop(func() { h.app.FocusInto(zero) })
	h.sync()
	time.Sleep(20 * time.Millisecond)
	h.sync()
	settled := zero.layouts.Load()
	time.Sleep(60 * time.Millisecond)
	h.sync()
	if more := zero.layouts.Load() - settled; more != 0 {
		t.Errorf("%d more layouts in an idle App after FocusInto on a zero-sized control", more)
	}
}
