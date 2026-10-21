package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// shrinkingRows is a source a test changes in place.
type shrinkingRows struct{ rows []string }

func (s *shrinkingRows) Len() int          { return len(s.rows) }
func (s *shrinkingRows) Item(i int) string { return s.rows[i] }

// A paint changes nothing (Render is pure): a list whose source shrank without RefreshSource keeps
// its cursor through a repaint, and paints without reading past the end. Told (RefreshSource), the
// cursor follows the rows, the positive control (tui-widget-data-S5).
func TestAListPaintChangesNothingItWasNotTold(t *testing.T) {
	src := &shrinkingRows{rows: []string{"a", "b", "c", "d", "e"}}
	l := widget.NewList(widget.WithSource[string](src, func(s string) string { return s }))
	sh := newShell(l)
	h := startApp(t, sh, 20, 6)
	h.barrier(sh)
	selected := func() (i int, ok bool) {
		h.onLoop(func() { i, ok = l.Selected() })
		return i, ok
	}
	h.onLoop(func() { l.SetCursor(4) })
	h.settle()
	h.onLoop(func() {
		src.rows = src.rows[:2] // a caller violation: no RefreshSource
		l.MarkDirty()
	})
	h.settle()
	if i, ok := selected(); i != 4 || !ok {
		t.Errorf("after a repaint the cursor is %d (%v): the paint moved it, want 4 until the list is told", i, ok)
	}
	h.onLoop(func() { l.RefreshSource() })
	h.settle()
	if i, ok := selected(); i != 1 || !ok {
		t.Errorf("told, the cursor is %d (%v), want 1, the last row", i, ok)
	}
}
