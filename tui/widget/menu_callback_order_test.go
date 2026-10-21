package widget_test

import (
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A Menu reports a selection on the bus before its owner hears of it, so an owner that answers by
// selecting another row is reported after it: the bus sees [two three], in the order they happened.
// (Before the change the owner was told first, and the bus saw [three two].)
func TestAMenuReportsAReentrantSelectionInTheOrderItHappened(t *testing.T) {
	var m *widget.Menu
	m = widget.NewMenu(widget.WithOnSelectionChanged(func(id widget.ItemID) {
		if id == "two" {
			m.Select("three")
		}
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("one", "One", nil),
		widget.NewCommand("two", "Two", nil),
		widget.NewCommand("three", "Three", nil),
	}); err != nil {
		t.Fatal(err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()
	var events []widget.ItemID
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.MenuSelectionChangedEvent) { events = append(events, ev.ItemID) })
	defer unsub()
	h.onLoop(func() { m.Select("two") })
	h.waitFor("two selection events", func() bool {
		var n int
		h.onLoop(func() { n = len(events) })
		return n >= 2
	})
	h.onLoop(func() {
		if !reflect.DeepEqual(events, []widget.ItemID{"two", "three"}) {
			t.Errorf("selection events = %v, want [two three]", events)
		}
	})
}

// A check row whose owner, told of the toggle, disables the row does not then run the row's action:
// the row is found again by its id and must still be choosable. Unchanged, it runs once (control).
func TestAMenuRowItsOwnerDisablesOnTheToggleDoesNotRunItsAction(t *testing.T) {
	for _, disable := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged control", true: "disabled by the owner"}[disable], func(t *testing.T) {
			var m *widget.Menu
			ran, toggled := 0, false
			m = widget.NewMenu(
				widget.WithActionExecutor(func(tui.ActionInvocation) bool { ran++; return true }),
				widget.WithMenuOnToggled(func(id widget.ItemID, _, _ bool) {
					toggled = true
					if disable {
						m.SetEnabled(id, false)
					}
				}),
			)
			if err := m.SetModel([]widget.MenuItemModel{widget.NewCheck("item", "Item", saveAction{})}); err != nil {
				t.Fatal(err)
			}
			m.HandleAction(tui.ActionInvocation{Action: widget.MenuActivateAction{ItemID: "item"}})
			if !toggled {
				t.Fatal("the toggle hook never ran")
			}
			if want := map[bool]int{false: 1, true: 0}[disable]; ran != want {
				t.Errorf("the action ran %d times, want %d", ran, want)
			}
		})
	}
}

// A press arms a row, and an owner told of the selection may replace the model under it: the release
// then activates nothing, as the pressed row is gone. Unchanged, press and release run it once. A
// pin of the outcome: it holds whether arm records the press before or after telling the owner
// (that order is the C1 rule, not something this cell observes).
func TestAMenuPressOnARowItsOwnerReplacesActivatesNothing(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged control", true: "model replaced on select"}[replace], func(t *testing.T) {
			ran := 0
			var m *widget.Menu
			m = widget.NewMenu(
				widget.WithActionExecutor(func(tui.ActionInvocation) bool { ran++; return true }),
				widget.WithOnSelectionChanged(func(id widget.ItemID) {
					if replace && id == "two" {
						_ = m.SetModel([]widget.MenuItemModel{
							widget.NewCommand("four", "Four", saveAction{}),
							widget.NewCommand("five", "Five", saveAction{}),
						})
					}
				}),
			)
			if err := m.SetModel([]widget.MenuItemModel{
				widget.NewCommand("one", "One", saveAction{}),
				widget.NewCommand("two", "Two", saveAction{}),
			}); err != nil {
				t.Fatal(err)
			}
			h, _ := menuFixture(t, m, 30, 10)
			defer h.stop()
			h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 2, Y: 1})
			h.settle()
			h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 2, Y: 1})
			h.settle()
			var n int
			h.onLoop(func() { n = ran })
			if want := map[bool]int{false: 1, true: 0}[replace]; n != want {
				t.Errorf("press and release on Two ran %d actions, want %d", n, want)
			}
		})
	}
}
