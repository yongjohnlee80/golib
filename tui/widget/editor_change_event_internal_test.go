package widget

import (
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// TestAnEditorBuildsItsChangeValueOnlyForASubscriber: with no ChangeEvent subscriber an edit
// builds no value (the editor's whole text) and publishes nothing; a subscriber gets each edit's
// value; and once it cancels, mid-lane, with an edit's event still queued, the edits after it
// build none, and the queued event is not delivered to it.
func TestAnEditorBuildsItsChangeValueOnlyForASubscriber(t *testing.T) {
	ed := NewEditor()
	h := widgettest.StartApp(t, ed, 40, 6)
	defer h.Stop()
	c := ed.Core()
	insert := func(s string) {
		if _, err := c.Insert(TextPos{}, s); err != nil {
			t.Fatal(err)
		}
	}

	h.OnLoop(func() { insert("a"); insert("b"); insert("c") })
	if c.changeValues != 0 {
		t.Fatalf("with no subscriber, edits built %d change values, want 0", c.changeValues)
	}

	var log widgettest.Log[string]
	cancel := tui.Subscribe(h.App.Bus(), func(e ChangeEvent) { log.Add(e.Value) })
	h.OnLoop(func() { insert("d") })
	h.Sync()
	if c.changeValues != 1 || !slices.Equal(log.Values(), []string{"dcba"}) {
		t.Fatalf("with a subscriber: %d values built, received %q; want 1 and [dcba]", c.changeValues, log.Values())
	}

	h.OnLoop(func() {
		insert("e") // built, its event queued on the lane
		cancel()    // mid-lane: before that event is delivered
		insert("f")
	})
	h.Sync()
	if c.changeValues != 2 {
		t.Fatalf("after the cancel: %d values built in all, want 2 (the edit before it only)", c.changeValues)
	}
	if got := log.Values(); len(got) != 1 {
		t.Fatalf("the cancelled subscriber received %q after its cancel", got[1:])
	}
}

// BenchmarkAnEditOnALargeTextWithNoListener: one-character edits on a 1M-line text with no
// ChangeEvent subscriber. Building the event's value joined the whole text per edit, O(document)
// per keystroke; B/op is the number to watch.
func BenchmarkAnEditOnALargeTextWithNoListener(b *testing.B) {
	lines := make([]string, 1<<20)
	for i := range lines {
		lines[i] = "a line of an ordinary length, about forty bytes"
	}
	c := NewEditorCore(CoreInitialText(strings.Join(lines, "\n")))
	i := 0
	for b.Loop() {
		if _, err := c.Insert(TextPos{Line: i % len(lines)}, "x"); err != nil {
			b.Fatal(err)
		}
		i += 4093
	}
}
