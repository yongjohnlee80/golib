package core

import (
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// TestATextAreaBuildsItsChangeValueOnlyForASubscriber: the same for a TextArea, whose value is
// its whole text too.
func TestATextAreaBuildsItsChangeValueOnlyForASubscriber(t *testing.T) {
	ta := NewTextArea()
	h := widgettest.StartApp(t, ta, 40, 6)
	defer h.Stop()
	h.OnLoop(func() { ta.InsertText("one\ntwo") })
	if ta.changeValues != 0 {
		t.Fatalf("with no subscriber, an insert built %d change values, want 0", ta.changeValues)
	}
	var log widgettest.Log[string]
	defer tui.Subscribe(h.App.Bus(), func(e ChangeEvent) { log.Add(e.Value) })()
	h.OnLoop(func() { ta.InsertText("!") })
	h.Sync()
	if ta.changeValues != 1 || !slices.Equal(log.Values(), []string{"one\ntwo!"}) {
		t.Fatalf("with a subscriber: %d values built, received %q; want 1 and [one\\ntwo!]", ta.changeValues, log.Values())
	}
}
