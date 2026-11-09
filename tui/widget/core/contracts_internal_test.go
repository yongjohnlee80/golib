package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// Put of a window already collected replaces its entry in place, and tells the observers each time.
func TestMinimizedWindowsPutReplacesAnOwnersEntry(t *testing.T) {
	m := NewMinimizedWindows()
	told := 0
	m.Observe(func() { told++ })
	m.Put(MinimizedWindow{Owner: 1, Title: "a"})
	m.Put(MinimizedWindow{Owner: 2, Title: "b"})
	m.Put(MinimizedWindow{Owner: 1, Title: "a2"})
	got := m.Entries()
	if len(got) != 2 || got[0].Title != "a2" || got[1].Title != "b" || told != 3 {
		t.Errorf("entries %+v, observers told %d times; want a2 then b, 3", got, told)
	}
}

// A Box takes one child (none: a chrome-only pane), at index 0, and its alignments are the three
// named ones.
func TestABoxsContract(t *testing.T) {
	for name, bad := range map[string]func(){
		"a title alignment past AlignRight":  func() { WithTitleAlign(style.AlignRight + 1) },
		"a status alignment past AlignRight": func() { WithStatusAlign(style.AlignRight + 1) },
		"adding a nil child":                 func() { NewBox(nil).Add(nil) },
		"a second child":                     func() { NewBox(NewText("a")).Add(NewText("b")) },
	} {
		if !widgettest.Panics(bad) {
			t.Errorf("%s did not panic", name)
		}
	}
	child := NewText("a")
	b := NewBox(nil, WithTitleAlign(style.AlignCenter), WithStatusAlign(style.AlignLeft)) // chrome only
	b.Add(child)
	b.SetStatus("ok")
	b.Move(NewText("stranger"), 3) // not its child: nothing to move
	b.Move(child, 0)
	if !widgettest.Panics(func() { b.Move(child, 1) }) {
		t.Error("moving the one child past index 0 did not panic")
	}
	if b.status != "ok" {
		t.Errorf("status %q, want ok", b.status)
	}
}
