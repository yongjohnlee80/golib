package widget

import (
	"cmp"
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
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
	keyGridDiff(t, "Image",
		func(e tui.KeyEvent) string { return stepName(oldScrollStep(e, rows, height)) },
		func(e tui.KeyEvent) string { return stepName(scrollStep(noKeymap, e, rows, height)) },
		func(e tui.KeyEvent) string { return cmp.Or(hyperOrMeta(e), shiftSpentOnPrintable(e)) })
}

// oldEscCloses is the Float's and the popup's Esc test before commands, verbatim.
func oldEscCloses(e tui.KeyEvent) string {
	if e.Kind != tui.KeyRelease && e.Code == tui.KeyEscape && e.Mods.Chord() == 0 {
		return "close"
	}
	return ""
}

func TestFloatAndPopupEscMatchTheOldTestWithNoMap(t *testing.T) {
	l, p := &floatLayer{}, &popupLayer{}
	keyGridDiff(t, "Float", oldEscCloses, func(e tui.KeyEvent) string {
		_, ok := l.Context().KeyCommand(e, FloatClose)
		return map[bool]string{true: "close"}[ok]
	}, hyperOrMeta)
	keyGridDiff(t, "popup", oldEscCloses, func(e tui.KeyEvent) string {
		_, ok := p.Context().KeyCommand(e, PopupClose)
		return map[bool]string{true: "close"}[ok]
	}, hyperOrMeta)
}
