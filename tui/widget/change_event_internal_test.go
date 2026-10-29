package widget

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// changeLog collects the ChangeEvent values a subscriber received.
type changeLog struct {
	mu  sync.Mutex
	got []string
}

func (l *changeLog) add(e ChangeEvent) { l.mu.Lock(); l.got = append(l.got, e.Value); l.mu.Unlock() }

func (l *changeLog) values() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.got)
}

// TestAnEditorBuildsItsChangeValueOnlyForASubscriber: with no ChangeEvent subscriber an edit
// builds no value (the editor's whole text) and publishes nothing; a subscriber gets each edit's
// value; and once it cancels, mid-lane, with an edit's event still queued, the edits after it
// build none, and the queued event is not delivered to it.
func TestAnEditorBuildsItsChangeValueOnlyForASubscriber(t *testing.T) {
	ed := NewEditor()
	h := startAppInternal(t, ed, 40, 6)
	defer h.stopInternal()
	c := ed.Core()
	insert := func(s string) {
		if _, err := c.Insert(TextPos{}, s); err != nil {
			t.Fatal(err)
		}
	}

	h.onLoopInternal(func() { insert("a"); insert("b"); insert("c") })
	if c.changeValues != 0 {
		t.Fatalf("with no subscriber, edits built %d change values, want 0", c.changeValues)
	}

	var log changeLog
	cancel := tui.Subscribe(h.app.Bus(), log.add)
	h.onLoopInternal(func() { insert("d") })
	h.syncInternal()
	if c.changeValues != 1 || !slices.Equal(log.values(), []string{"dcba"}) {
		t.Fatalf("with a subscriber: %d values built, received %q; want 1 and [dcba]", c.changeValues, log.values())
	}

	h.onLoopInternal(func() {
		insert("e") // built, its event queued on the lane
		cancel()    // mid-lane: before that event is delivered
		insert("f")
	})
	h.syncInternal()
	if c.changeValues != 2 {
		t.Fatalf("after the cancel: %d values built in all, want 2 (the edit before it only)", c.changeValues)
	}
	if got := log.values(); len(got) != 1 {
		t.Fatalf("the cancelled subscriber received %q after its cancel", got[1:])
	}
}

// TestATextAreaBuildsItsChangeValueOnlyForASubscriber: the same for a TextArea, whose value is
// its whole text too.
func TestATextAreaBuildsItsChangeValueOnlyForASubscriber(t *testing.T) {
	ta := NewTextArea()
	h := startAppInternal(t, ta, 40, 6)
	defer h.stopInternal()
	h.onLoopInternal(func() { ta.insert("one\ntwo") })
	if ta.changeValues != 0 {
		t.Fatalf("with no subscriber, an insert built %d change values, want 0", ta.changeValues)
	}
	var log changeLog
	defer tui.Subscribe(h.app.Bus(), log.add)()
	h.onLoopInternal(func() { ta.insert("!") })
	h.syncInternal()
	if ta.changeValues != 1 || !slices.Equal(log.values(), []string{"one\ntwo!"}) {
		t.Fatalf("with a subscriber: %d values built, received %q; want 1 and [one\\ntwo!]", ta.changeValues, log.values())
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
