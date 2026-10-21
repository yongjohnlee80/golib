package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// pointerList is a list of rows whose owner's cursor callback is onCursor, and the activations and
// menu requests it heard, through the real pointer handler.
func pointerList(rows []string, onCursor func(l *List[string])) (l *List[string], acts *[]int, menus *[]int) {
	acts, menus = new([]int), new([]int)
	l = NewList(WithItems(rows, func(v string) string { return v }),
		WithListOnCursor[string](func(int) {
			if onCursor != nil {
				onCursor(l)
			}
		}),
		WithListOnActivate[string](func(i int) { *acts = append(*acts, i) }),
		WithListOnContextMenu[string](func(i int, _ tui.Point) { *menus = append(*menus, i) }))
	l.SetContextMenu(true)
	return l, acts, menus
}

func pressRow(l *List[string], b tui.MouseButton, y, count int) {
	l.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: b, Y: y, Count: count})
}

// A press whose cursor callback replaces the rows, or changes them in place, has pressed rows that
// are gone: it is not paired with the next press, and asks no menu for them. With the rows
// unchanged, a double press activates the row once and a right press asks the menu for it.
func TestAListPressDoesNotOutliveTheRowsItPressed(t *testing.T) {
	t.Run("unchanged rows, the positive control", func(t *testing.T) {
		l, acts, menus := pointerList([]string{"a", "b"}, nil)
		pressRow(l, tui.MouseLeft, 1, 1)
		pressRow(l, tui.MouseLeft, 1, 2)
		pressRow(l, tui.MouseRight, 0, 1)
		if len(*acts) != 1 || (*acts)[0] != 1 || len(*menus) != 1 || (*menus)[0] != 0 {
			t.Errorf("activated %v, menus %v; want [1] and [0]", *acts, *menus)
		}
	})
	t.Run("a new source", func(t *testing.T) {
		replaced := false
		l, acts, _ := pointerList([]string{"old-a", "old-b"}, func(l *List[string]) {
			if !replaced {
				replaced = true
				l.SetItems([]string{"new-a", "new-b"})
			}
		})
		pressRow(l, tui.MouseLeft, 1, 1)
		pressRow(l, tui.MouseLeft, 1, 2)
		if len(*acts) != 0 {
			t.Errorf("a press on old-b and one on new-b activated %v", *acts)
		}
	})
	t.Run("an in-place refresh", func(t *testing.T) {
		rows := []string{"old-a", "old-b"}
		refreshed := false
		var l *List[string]
		var acts *[]int
		l, acts, _ = pointerList(nil, func(l *List[string]) {
			if !refreshed {
				refreshed = true
				rows[0], rows[1] = "new-a", "new-b"
				l.RefreshSource()
			}
		})
		l.SetSource(SliceSource(rows))
		pressRow(l, tui.MouseLeft, 1, 1)
		pressRow(l, tui.MouseLeft, 1, 2)
		if len(*acts) != 0 {
			t.Errorf("a press on old-b and one on new-b, refreshed in place, activated %v", *acts)
		}
	})
	t.Run("a right press whose rows were emptied", func(t *testing.T) {
		l, _, menus := pointerList([]string{"a", "b"}, func(l *List[string]) { l.SetItems(nil) })
		pressRow(l, tui.MouseRight, 1, 1)
		if len(*menus) != 0 {
			t.Errorf("the menu was asked for %v over a list of %d rows", *menus, l.Len())
		}
	})
}
