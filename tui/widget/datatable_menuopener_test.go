package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// openerProbe is a menu of the host's own kind, not an EditorMenu.
type openerProbe struct {
	on             bool
	opened, closed int
	at             tui.Point
}

func (o *openerProbe) Enabled() bool { return o.on }
func (o *openerProbe) OpenAt(at tui.Point) bool {
	o.opened++
	o.at = at
	return true
}
func (o *openerProbe) Close() { o.closed++ }

// A DataTable opens any MenuOpener on a right press, at the pointer, only while it is enabled; a
// second menu closes the first; and SetMenu(nil) takes it away. Menu reports an EditorMenu only.
func TestADataTableOpensAnyMenuOpener(t *testing.T) {
	h, sh, dt := dataTable(t, numbered(5), 30, 8)
	first := &openerProbe{on: false}
	h.onLoop(func() { dt.SetMenuOpener(first) })
	h.inject(rightPress(5, 3))
	h.barrier(sh)
	var opened int
	h.onLoop(func() { opened = first.opened })
	if opened != 0 {
		t.Fatalf("a disabled opener was opened %d times", opened)
	}
	h.onLoop(func() { first.on = true })
	h.inject(rightPress(5, 3))
	h.barrier(sh)
	var at tui.Point
	var menu *widget.EditorMenu
	h.onLoop(func() { opened, at, menu = first.opened, first.at, dt.Menu() })
	if opened != 1 || at != (tui.Point{X: 5, Y: 3}) || menu != nil {
		t.Fatalf("enabled: opened %d at %v, Menu() %v; want once at {5 3}, Menu() nil for a non-EditorMenu", opened, at, menu)
	}
	second := &openerProbe{on: true}
	var closed int
	var cur widget.MenuOpener
	h.onLoop(func() { dt.SetMenuOpener(second); closed = first.closed })
	if closed != 1 {
		t.Errorf("a second menu: the first closed %d times, want 1", closed)
	}
	h.onLoop(func() { dt.SetMenu(nil); cur = dt.MenuOpener() })
	if cur != nil {
		t.Errorf("SetMenu(nil) left the opener %v; a nil EditorMenu is no menu", cur)
	}
}

// plainOpener is a MenuOpener with no Enabled: a menu that cannot be switched off.
type plainOpener struct{ opened int }

func (o *plainOpener) OpenAt(tui.Point) bool { o.opened++; return true }
func (o *plainOpener) Close()                {}

// A menu without an Enabled of its own is always on: a right press opens it.
func TestADataTableOpensAMenuThatHasNoSwitch(t *testing.T) {
	h, sh, dt := dataTable(t, numbered(5), 30, 8)
	o := &plainOpener{}
	h.onLoop(func() { dt.SetMenuOpener(o) })
	h.inject(rightPress(5, 3))
	h.barrier(sh)
	var n int
	h.onLoop(func() { n = o.opened })
	if n != 1 {
		t.Errorf("a right press opened a switchless menu %d times, want 1", n)
	}
}

// sliceOpener is a valid MenuOpener of a type that cannot be compared: a named slice.
type sliceOpener []int

func (o sliceOpener) OpenAt(tui.Point) bool { return true }
func (o sliceOpener) Close()                { o[0]++ }

// A menu of a type that cannot be compared replaces another of its type without a panic, and the
// one replaced is closed once.
func TestADataTableReplacesAMenuOfANoncomparableType(t *testing.T) {
	table := widget.NewDataTable(widget.NewTableCore(nil))
	first := sliceOpener{0}
	table.SetMenuOpener(first)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("replacing a noncomparable MenuOpener panicked: %v", r)
		}
	}()
	table.SetMenuOpener(sliceOpener{0})
	if first[0] != 1 {
		t.Fatalf("the replaced menu was closed %d times, want once", first[0])
	}
}
