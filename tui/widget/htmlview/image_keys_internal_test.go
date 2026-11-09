package htmlview

import (
	"cmp"
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// oldScrollStep is scrollStep's key decision before commands, verbatim but for the wheel, which is
// unchanged: the oracle of the no-map differential.
func oldScrollStep(ev tui.Event, rows, height int) (dx, dy int, ok bool) {
	const col = 4 * CellPixelsW
	e, isKey := ev.(tui.KeyEvent)
	if !isKey || e.Kind == tui.KeyRelease || e.Mods.Chord() != 0 {
		return 0, 0, false
	}
	page := max(rows-1, 1) * CellPixelsH
	switch e.Code {
	case tui.KeyDown, 'j':
		return 0, CellPixelsH, true
	case tui.KeyUp, 'k':
		return 0, -CellPixelsH, true
	case tui.KeyRight, 'l':
		return col, 0, true
	case tui.KeyLeft, 'h':
		return -col, 0, true
	case tui.KeyPageDown, ']', ' ':
		return 0, page, true
	case tui.KeyPageUp, '[':
		return 0, -page, true
	case tui.KeyEnd, 'G':
		return 0, height, true
	case tui.KeyHome, 'g':
		return 0, -height, true
	}
	return 0, 0, false
}

func stepName(dx, dy int, ok bool) string {
	if !ok {
		return ""
	}
	return fmt.Sprint(dx, dy)
}

func TestImageKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	const rows, height = 7, 300
	widgettest.KeyGridDiff(t, "Image",
		func(e tui.KeyEvent) string { return stepName(oldScrollStep(e, rows, height)) },
		func(e tui.KeyEvent) string { return stepName(scrollStep(widgettest.NoKeymap, e, rows, height)) },
		func(e tui.KeyEvent) string {
			return cmp.Or(widgettest.HyperOrMeta(e), widgettest.ShiftSpentOnPrintable(e))
		})
}
